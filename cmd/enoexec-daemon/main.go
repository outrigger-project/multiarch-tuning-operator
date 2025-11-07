package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	enoexeceventdaemon "github.com/openshift/multiarch-tuning-operator/controllers/enoexecevent/daemon"
)

var (
	initialLogLevel int
	logDevMode      bool
)

func main() {
	bindFlags()
	ctx, cancel := initContext()
	err := enoexeceventdaemon.RunDaemon(ctx, cancel)
	must(err, "failed to run enoexec daemon")
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
		cfg := zap.NewDevelopmentConfig()
		cfg.Level = zap.NewAtomicLevelAt(zapcore.Level(-initialLogLevel))
		logImpl, err = cfg.Build()
	} else {
		cfg := zap.NewProductionConfig()
		cfg.Level = zap.NewAtomicLevelAt(zapcore.Level(-initialLogLevel))
		logImpl, err = cfg.Build()
	}
	must(err, "failed to create logger")
	ctx = logr.NewContext(ctx, zapr.NewLogger(logImpl))
	return ctx, cancel
}

func must(err error, msg string) {
	if err == nil {
		return
	}
	_, _ = fmt.Fprintf(os.Stderr, "%s: %v\n", msg, err)
	os.Exit(1)
}
