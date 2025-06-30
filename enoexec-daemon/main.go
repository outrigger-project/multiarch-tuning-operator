package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	initialLogLevel int
	logDevMode      bool
)

func main() {
	bindFlags()
	ctx, cancel := initContext()
	log, err := logr.FromContext(ctx)
	must(err, "failed to get logger from context")

	log.Info("Starting workers")
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		log.Info("Starting storage writer")
		defer wg.Done()
		defer cancel()
		// TODO: Replace with actual storage implementation
		<-ctx.Done()
	}()
	wg.Add(1)
	go func() {
		log.Info("Starting ENOEXEC eBPF tracepoint")
		defer wg.Done()
		defer cancel()
		// TODO: Replace with actual tracepoint implementation
		<-ctx.Done()
	}()
	wg.Add(1)
	go func() {
		log.Info("Starting main loop")
		defer wg.Done()
		defer cancel()
		// TODO: Replace with actual event processing implementation
		<-ctx.Done()
	}()

	log.Info("Controller started, waiting for events")
	<-ctx.Done()
	log.Info("Context channel closed. Waiting for the workers to terminate")
	wg.Wait()
	log.Info("All workers terminated, exiting")
}

func bindFlags() {
	flag.IntVar(&initialLogLevel, "initial-log-level", 0, "Initial log level. From 0 (Normal) to 5 (TraceAll)")
	flag.BoolVar(&logDevMode, "log-dev-mode", false, "Enable development mode for zap logger")
	flag.Parse()
}

func initContext() (context.Context, context.CancelFunc) {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	var logImpl *zap.Logger
	var err error
	if logDevMode {
		logImpl, err = zap.NewDevelopment()
	} else {
		level := zap.NewAtomicLevelAt(zapcore.Level(-initialLogLevel))
		logImpl, err = zap.NewProduction(zap.IncreaseLevel(level))
	}
	must(err, "failed to create logger")
	ctx = logr.NewContext(ctx, zapr.NewLogger(logImpl))
	return ctx, cancel
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
