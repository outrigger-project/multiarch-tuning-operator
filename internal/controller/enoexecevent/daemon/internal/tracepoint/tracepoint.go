package tracepoint

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"

	"github.com/go-logr/logr"

	"github.com/openshift/multiarch-tuning-operator/internal/controller/enoexecevent/daemon/internal/types"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

// Tracepoint represents an eBPF tracepoint that monitors the `execve` syscall
// to detect ENOEXEC events. It captures the real parent and current task TGIDs
// and retrieves the corresponding pod and container UUIDs from the CRI-O runtime.
type Tracepoint struct {
	ctx context.Context

	events   *ebpf.Map
	prog     *ebpf.Program
	progSpec *ebpf.ProgramSpec
	link     link.Link

	tgidOffset       *int32
	realParentOffset *int32
	bufferSize       uint32 // Size of the ring buffer in bytes

	ch chan *types.ENOEXECInternalEvent

	order binary.ByteOrder
}

func NewTracepoint(ctx context.Context, ch chan *types.ENOEXECInternalEvent, maxEvents uint32) (*Tracepoint, error) {
	// Buffer Size must be a multiple of page size
	if os.Getpagesize() <= 0 {
		return nil, fmt.Errorf("invalid page size")
	}
	pageSize := uint32(os.Getpagesize()) // [bytes]
	// The payload is 8 bytes. Other 8 bytes are used for the header.
	// 16 [bytes/event].
	payloadSize := PayloadSize + 8

	// The buffer size has to be a multiple of the page size.
	// We calculate the required buffer size based on the maximum number of events as
	// size_max = maxEvents * 8 [bytes].
	// We obtain the number of pages required to store the events rounding up the number of pages required
	// to store size_max bytes: required_pages = Ceil(size_max [bytes] / pageSize [bytes]).
	// Finally, we multiply required_pages by the page size to get the buffer size.
	bufferSize := pageSize * uint32(math.Ceil(float64(maxEvents*payloadSize)/float64(pageSize)))

	var i uint16 = 0x0001
	b := (*[2]byte)(unsafe.Pointer(&i))
	// Little endian: [0x01, 0x00]
	// Big endian: [0x00, 0x01]
	var order binary.ByteOrder
	if b[0] == 0x00 {
		order = binary.BigEndian
		fmt.Println("Detected big endian architecture")
	} else {
		order = binary.LittleEndian
		fmt.Println("Detected little endian architecture")
	}

	tp := &Tracepoint{
		bufferSize: bufferSize,
		ch:         ch,
		ctx:        ctx,
		order:      order,
		progSpec: &ebpf.ProgramSpec{
			Name:     "multiarch_tuning_enoexec_tracepoint",
			Type:     ebpf.TracePoint,
			AttachTo: "syscalls:sys_enter_execve",
			License:  "GPL",
		},
	}
	if err := tp.initializeOffsets(); err != nil {
		return nil, fmt.Errorf("failed to initialize offsets: %w", err)
	}
	return tp, nil
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
		Name: "multiarch_tuning_enoexec_events",
		Type: ebpf.RingBuf,
		// For RingBuf, the MaxEntries field defines the size of the ring buffer in bytes, not the number of entries.
		MaxEntries: tp.bufferSize,
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
		"bufferSize", tp.bufferSize)
	if err := tp.attach(); err != nil {
		return errors.Join(fmt.Errorf("failed to attach tracepoint: %w", err),
			tp.close())
	}
	defer utils.ShouldStdErr(tp.close)

	log.Info("Allocate Ring buffer reader")
	rd, err := ringbuf.NewReader(tp.events)
	if err != nil {
		return errors.Join(fmt.Errorf("failed to create ring buffer reader: %w", err), tp.close())
	}
	defer utils.ShouldStdErr(rd.Close)

	// Start a goroutine to monitor the context and close the tracepoint when the context is done
	// This is useful for graceful shutdowns and to unblock the main loop from the blocking rd.Read()
	// on context cancellation. rd.Read() will return an error when rd.Close() is called.
	log.Info("Starting context monitoring goroutine")
	go tp.monitor(rd)

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
		tp.ch <- evt
	}
}

func (tp *Tracepoint) processRecord(record *ringbuf.Record) (*types.ENOEXECInternalEvent, error) {
	log := logr.FromContextOrDiscard(tp.ctx)
	if len(record.RawSample) < 8 {
		return nil, fmt.Errorf("record too short: %d bytes, expected at least 8 bytes", len(record.RawSample))
	}
	realParentTGID := tp.order.Uint32(record.RawSample[:4])
	currentTaskTGID := tp.order.Uint32(record.RawSample[4:8])
	log.V(4).Info("Processing record",
		"real_parent_tgid", realParentTGID, "current_task_tgid", currentTaskTGID)
	for _, pid := range []uint32{currentTaskTGID, realParentTGID} {
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
		}, nil
	}
	return nil, fmt.Errorf("failed to find pod/container UUIDs in record: hex:[% X] = (%d, %d)", record.RawSample, realParentTGID, currentTaskTGID) // No pod/container found
}

func (tp *Tracepoint) monitor(rd *ringbuf.Reader) {
	log := logr.FromContextOrDiscard(tp.ctx)
	log.Info("Starting context monitoring goroutine for the tracepoint worker")
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
}
