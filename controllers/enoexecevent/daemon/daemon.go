package daemon

import (
	"context"
	"fmt"
	"math"
	"os"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/time/rate"

	"github.com/openshift/multiarch-tuning-operator/controllers/enoexecevent/daemon/internal/storage"
	"github.com/openshift/multiarch-tuning-operator/controllers/enoexecevent/daemon/internal/tracepoint"
	"github.com/openshift/multiarch-tuning-operator/controllers/enoexecevent/daemon/internal/types"
)

func RunDaemon(ctx context.Context, cancel context.CancelFunc) error {
	log, err := logr.FromContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to get logger from context: %w", err)
	}

	// Buffer Size must be a multiple of page size
	if os.Getpagesize() <= 0 {
		return fmt.Errorf("invalid page size")
	}
	pageSize := uint32(os.Getpagesize()) // [bytes]
	// The payload is 24 bytes. Other 8 bytes are used for the header.
	// 32 [bytes/event].
	// See https://github.com/outrigger-project/multiarch-tuning-operator/blob/eabed5c4e54/enhancements/MTO-0004-enoexec-monitoring.md
	maxEvents := 256
	// The buffer size has to be a multiple of the page size.
	// We calculate the required buffer size based on the maximum number of events as
	// size_max = maxEvents * 24 [bytes].
	// We obtain the number of pages required to store the events rounding up the number of pages required
	// to store size_max bytes: required_pages = Ceil(size_max [bytes] / pageSize [bytes]).
	// Finally, we multiply required_pages by the page size to get the buffer size.
	bufferSize := pageSize * uint32(math.Ceil(float64(maxEvents*24)/float64(pageSize)))

	log.Info("Buffer size calculated", "buffer_size", bufferSize, "page_size", pageSize, "max_events", maxEvents)

	log.Info("Initializing channel")
	ch := make(chan *types.ENOEXECInternalEvent, maxEvents)

	log.Info("Initializing storage")
	storageImpl, err := storage.NewK8sENOExecEventStorage(ctx,
		rate.NewLimiter(5, 10), ch, os.Getenv("NODE_NAME"), os.Getenv("NAMESPACE"), time.Minute,
	)
	if err != nil {
		return fmt.Errorf("failed to create storage: %w", err)
	}

	log.Info("Initializing tracepoint", "buffer_size", bufferSize)
	tp, err := tracepoint.NewTracepoint(ctx, ch, bufferSize)
	if err != nil {
		return fmt.Errorf("failed to create tracepoint: %w", err)
	}

	log.Info("Starting workers")
	wg := sync.WaitGroup{}
	wg.Add(1)
	go runWorker("Kubernetes storage writer", &wg, ctx, cancel, storageImpl.Run)
	wg.Add(1)
	go runWorker("ENOEXEC eBPF tracepoint", &wg, ctx, cancel, tp.Run)
	log.Info("Controller started, waiting for events")

	<-ctx.Done()
	log.Info("Context channel closed. Waiting for the workers to terminate")
	wg.Wait()
	log.Info("Workers terminated, closing channel")
	close(ch)
	log.Info("Controller stopped")

	return nil
}

func runWorker(name string, wg *sync.WaitGroup,
	ctx context.Context, cancelFn func(), runFn func() error) {

	log := logr.FromContextOrDiscard(ctx)
	log.Info("Starting " + name)

	defer wg.Done()
	defer cancelFn()

	if err := runFn(); err != nil {
		log.Error(err, "Failed to run "+name)
	}
}
