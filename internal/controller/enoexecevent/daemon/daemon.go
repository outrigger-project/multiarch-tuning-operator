package daemon

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/time/rate"

	"github.com/openshift/multiarch-tuning-operator/internal/controller/enoexecevent/daemon/internal/storage"
	"github.com/openshift/multiarch-tuning-operator/internal/controller/enoexecevent/daemon/internal/tracepoint"
	"github.com/openshift/multiarch-tuning-operator/internal/controller/enoexecevent/daemon/internal/types"
)

func RunDaemon(ctx context.Context, cancel context.CancelFunc) error {
	log, err := logr.FromContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to get logger from context: %w", err)
	}
	var (
		maxEvents uint32     = 256
		rateLimit rate.Limit = 5
		burst                = 10
		timeout              = time.Minute
	)

	log.Info("Initializing channel")
	ch := make(chan *types.ENOEXECInternalEvent, maxEvents)

	nodeName := os.Getenv("NODE_NAME")
	namespace := os.Getenv("NAMESPACE")

	log.Info("Initializing storage", "node_name", nodeName, "namespace", namespace,
		"rate limit", rateLimit, "burst", burst, "timeout", timeout)
	storageImpl, err := storage.NewK8sENOExecEventStorage(
		ctx, rate.NewLimiter(rateLimit, burst), ch, nodeName, namespace, timeout)
	if err != nil {
		return fmt.Errorf("failed to create storage: %w", err)
	}

	log.Info("Initializing tracepoint")
	tp, err := tracepoint.NewTracepoint(ctx, ch, maxEvents)
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
