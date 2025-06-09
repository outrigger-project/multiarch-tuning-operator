package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/signal"
	"sync"
	"syscall"

	storage2 "github.com/openshift/multiarch-tuning-operator/enoexec-daemon/internal/storage"
	tracepoint2 "github.com/openshift/multiarch-tuning-operator/enoexec-daemon/internal/tracepoint"
	"golang.org/x/time/rate"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
)

var nodeName string
var namespace string

func getNodeName() string {
	if nodeName == "" {
		nodeName = os.Getenv("NODE_NAME")
	}
	// NODE_NAME must be set in the environment variables, using the downward API in Kubernetes.
	return nodeName
}

func getNamespace() string {
	if namespace == "" {
		namespace = os.Getenv("POD_NAMESPACE")
	}
	return namespace
}

func must(err error, msg string, fns ...func()) {
	if err == nil {
		return
	}
	_, _ = fmt.Fprintf(os.Stderr, "%s: %v\n", msg, err)
	for _, fn := range fns {
		fn()
	}
	os.Exit(1)
}

func initContext() (context.Context, context.CancelFunc) {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	logImpl, err := zap.NewDevelopment()
	must(err, "failed to create logger")
	ctx = logr.NewContext(ctx, zapr.NewLogger(logImpl))
	return ctx, cancel
}

func main() {
	// TODO: Set up args
	ctx, cancel := initContext()
	log, err := logr.FromContext(ctx)
	must(err, "failed to get logger from context")

	log.Info("My Pid", "pid", os.Getpid())

	log.Info("Creating storage")
	storage, err := storage2.NewK8sENOExecEventStorage(ctx, rate.NewLimiter(5, 10), 256, getNodeName(), getNamespace())
	must(err, "failed to create storage")

	// Buffer Size must be a multiple of page size
	if os.Getpagesize() <= 0 {
		must(fmt.Errorf("invalid page size"), "failed to get page size")
	}
	pageSize := uint32(os.Getpagesize()) // [bytes]
	// The payload is 24 bytes. Other 8 bytes are used for the header.
	// 32 [bytes/event].
	// See https://github.com/outrigger-project/multiarch-tuning-operator/blob/eabed5c4e54/enhancements/MTO-0004-enoexec-monitoring.md
	maxEvents := 256
	bufferSize := pageSize * uint32(math.Ceil(float64(maxEvents*32)/float64(pageSize)))
	log.Info("Creating tracepoint", "buffer_size", bufferSize)
	tracepoint, tracepointOutChan, err := tracepoint2.NewTracepoint(ctx, bufferSize)
	must(err, "failed to create tracepoint")

	log.Info("Starting workers")
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		log.Info("Starting storage worker")
		defer wg.Done()
		defer cancel()
		err := storage.Run()
		must(err, "the storage worker crashed")
	}()
	wg.Add(1)
	go func() {
		log.Info("Starting tracepoint worker")
		defer wg.Done()
		defer cancel()
		err := tracepoint.Run()
		must(err, "the tracepoint worker crashed")
	}()
	wg.Add(1)
	go func() {
		log.Info("Starting main loop for processing tracepoint events (processor)")
		defer wg.Done()
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				log.Info("Context done, stopping main loop")
				return
			case event := <-tracepointOutChan:
				if event == nil {
					// Create error
					err := fmt.Errorf("received nil event from tracepoint, exiting")
					log.Error(err, "nil event received")
					return
				}
				log.Info("Received event")
				err = storage.Store(event)
				if err != nil {
					log.Error(err, "failed to store event, exiting")
					return
				}
				log.Info("Stored event", "pod_name", event.PodName, "pod_namespace", event.PodNamespace, "container_id", event.ContainerID)
			}
		}
	}()

	log.Info("Controller started, waiting for events")
	<-ctx.Done()
	log.Info("Context done, stopping main process")
	log.Info("Stopping tracepoint and storage workers")
	wg.Wait()
}
