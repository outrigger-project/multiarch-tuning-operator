package tracepoint

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"

	"github.com/go-logr/logr"

	"github.com/openshift/multiarch-tuning-operator/enoexec-daemon/internal/types"
	"github.com/openshift/multiarch-tuning-operator/enoexec-daemon/internal/utils"
)

// Tracepoint represents an eBPF tracepoint that monitors the `execve` syscall
// to detect ENOEXEC events. It captures the real parent and current task TGIDs
// and retrieves the corresponding pod and container UUIDs from the CRI-O runtime.
type Tracepoint struct {
	ctx              context.Context
	progSpec         *ebpf.ProgramSpec
	events           *ebpf.Map
	tgidOffset       *int32
	realParentOffset *int32
	prog             *ebpf.Program
	link             link.Link
	outputChannel    chan *types.ENOEXECInternalEvent
	eventsMaxEntries uint32
}

func NewTracepoint(ctx context.Context, eventsMaxEntries uint32) (*Tracepoint, chan *types.ENOEXECInternalEvent, error) {
	tp := &Tracepoint{
		eventsMaxEntries: eventsMaxEntries,
		ctx:              ctx,
		progSpec: &ebpf.ProgramSpec{
			Name:     "multiarch_tuning_enoexec_tracepoint",
			Type:     ebpf.TracePoint,
			AttachTo: "syscalls:sys_enter_execve",
			License:  "GPL",
		},
	}
	if err := tp.initializeOffsets(); err != nil {
		return nil, nil, fmt.Errorf("failed to initialize offsets: %w", err)
	}

	// Create the output channel for ENOEXEC events
	// Keeping the channel size equal to the events map size. TODO: validate Queue parameters
	tp.outputChannel = make(chan *types.ENOEXECInternalEvent, eventsMaxEntries)
	return tp, tp.outputChannel, nil
}

func (tp *Tracepoint) close() error {
	errs := make([]error, 0)
	if tp.events != nil {
		if err := tp.events.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close events map: %w", err))
		}
		tp.events = nil
	}
	if tp.link != nil {
		if err := tp.link.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close tracepoint link: %w", err))
		}
		tp.link = nil
	}
	if tp.prog != nil {
		if err := tp.prog.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close eBPF program: %w", err))
		}
		tp.prog = nil
	}
	close(tp.outputChannel)
	if tp.ctx != nil {
		if cancelFunc, ok := tp.ctx.Value("cancelFunc").(context.CancelFunc); ok {
			cancelFunc()
		}
		tp.ctx = nil
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (tp *Tracepoint) attach() error {
	var err error
	tp.events, err = ebpf.NewMap(&ebpf.MapSpec{
		Name:       "multiarch_tuning_enoexec_events",
		Type:       ebpf.RingBuf,
		MaxEntries: tp.eventsMaxEntries,
	})
	if err != nil {
		return errors.Join(fmt.Errorf("error creating the ring buffer"), err, tp.close())
	}

	if err = tp.initializeProgSpec(); err != nil {
		return errors.Join(fmt.Errorf("error initializing the eBPF program"), err, tp.close())
	}

	tp.prog, err = ebpf.NewProgram(tp.progSpec)
	if err != nil {
		return errors.Join(fmt.Errorf("failed to create eBPF program"), err, tp.close())
	}

	tp.link, err = link.Tracepoint("syscalls", "sys_exit_execve", tp.prog, nil)
	if err != nil {
		return errors.Join(fmt.Errorf("failed to attach tracepoint"), err, tp.close())
	}
	return nil
}

func (tp *Tracepoint) Run() error {
	log, err := logr.FromContext(tp.ctx)
	if err != nil {
		return errors.Join(fmt.Errorf("failed to get logger from context: %w", err),
			tp.close())
	}
	log.Info("Attaching tracepoint", "name", tp.progSpec.Name,
		"tgid_offset", tp.tgidOffset, "real_parent_offset", tp.realParentOffset,
		"events_max_entries", tp.eventsMaxEntries)
	if err := tp.attach(); err != nil {
		return errors.Join(fmt.Errorf("failed to attach tracepoint: %w", err),
			tp.close())
	}
	defer utils.Should(tp.close())
	log.Info("Tracepoint attached to kernel-land")
	log.Info("Creating ring buffer reader")
	rd, err := ringbuf.NewReader(tp.events)
	if err != nil {
		return errors.Join(fmt.Errorf("failed to create ring buffer reader: %w", err), tp.close())
	}
	defer utils.Should(rd.Close())
	log.Info("Ring buffer reader created in user-land")
	log.Info("Context monitoring")
	// Start a goroutine to monitor the context and close the tracepoint when the context is done
	// This is useful for graceful shutdowns and to unblock the main loop from rd.Read() on context cancellation
	go func() {
		<-tp.ctx.Done()
		log.Info("Context done, shutting down tracepoint")
		if err := tp.close(); err != nil {
			log.Error(err, "failed to close tracepoint resources")
		}
		log.Info("Tracepoint resources closed successfully")
		if err := rd.Close(); err != nil {
			log.Error(err, "failed to close ring buffer reader")
		}
		log.Info("Ring buffer reader closed successfully")
	}()
	log.Info("Starting main loop to read events from ring buffer in user-land")
	for {
		record, err := rd.Read()
		if errors.Is(err, ringbuf.ErrClosed) {
			log.Info("Ring buffer closed, stopping tracepoint processing")
			return nil
		}
		if err != nil {
			log.Error(err, "failed to read from ring buffer")
			return fmt.Errorf("failed to read from ring buffer: %w", err)
		}
		evt, err := tp.processRecord(&record)
		if err != nil {
			// Log the error and continue processing other records
			log.Info("Failed to process record", "error", err, "record_length", len(record.RawSample))
			continue
		}
		log.Info("ENOEXEC event detected", "event", evt)
		tp.outputChannel <- evt
	}
}

func (tp *Tracepoint) processRecord(record *ringbuf.Record) (*types.ENOEXECInternalEvent, error) {
	log := logr.FromContextOrDiscard(tp.ctx)
	if len(record.RawSample) < 8 {
		return nil, fmt.Errorf("record too short: %d bytes, expected at least 8 bytes", len(record.RawSample))
	}
	realParentTGID := int32(binary.LittleEndian.Uint32(record.RawSample[:4]))
	currentTaskTGID := int32(binary.LittleEndian.Uint32(record.RawSample[4:]))
	log.V(4).Info("Processing record",
		"real_parent_tgid", realParentTGID, "current_task_tgid", currentTaskTGID)
	for _, pid := range []int32{currentTaskTGID, realParentTGID} {
		podUUID, containerUUID, err := getPodContainerUUIDFor(pid)
		if err != nil {
			// Log the error and continue processing other pids as this is not a critical error
			// (e.g., the pid might not exist anymore due to a delay in processing this record)
			log.V(5).Info("Failed to get pod and container UUIDs for pid", "pid", pid, "error", err)
			continue
		}
		podName, podNamespace, err := getPodNameFromUUID(tp.ctx, podUUID)
		if err != nil {
			// Errors from getPodNameFromUUID are critical if err is not nil, as it indicates a failure to connect to the CRI-O runtime
			log.V(5).Info("Failed to get pod name from UUID", "pod_uuid", podUUID, "error", err)
			return nil, fmt.Errorf("failed to get pod name from UUID %s: %w", podUUID, err)
		}
		if podName == "" {
			// If podName is empty, it means the pod was not found in the CRI-O runtime, that is not a critical error
			log.V(5).Info("Failed to get pod name from UUID", "pod_uuid", podUUID)
			continue
		}
		log.Info("Found pod/container UUIDs in record", "pod_name", podName,
			"pod_namespace", podNamespace, "container_id", containerUUID)
		return &types.ENOEXECInternalEvent{
			PodName:      podName,
			PodNamespace: podNamespace,
			ContainerID:  containerUUID,
			// Binary:      "", // TODO: get binary name from record
		}, nil
	}
	return nil, fmt.Errorf("failed to find pod/container UUIDs in record: %v", record) // No pod/container found
}
