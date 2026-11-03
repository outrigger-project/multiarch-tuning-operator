package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
)

type operandManager struct {
	ctrl.Manager
	start func(context.Context) error
}

func (m operandManager) GetLogger() logr.Logger          { return logr.Discard() }
func (m operandManager) Start(ctx context.Context) error { return m.start(ctx) }

func TestRunOperandManagerStartFailureCleansUp(t *testing.T) {
	testRunOperand(t, false, errors.New("manager failed"))
}
func TestRunOperandSetupFailureCleansUp(t *testing.T) {
	testRunOperand(t, true, errors.New("setup failed"))
}
func TestRunOperandProducerBeforeRecorderCleanup(t *testing.T) { testRunOperand(t, false, nil) }

func testRunOperand(t *testing.T, failSetup bool, wantErr error) {
	t.Helper()
	var order []string
	var recording context.Context
	manager := operandManager{start: func(context.Context) error {
		order = append(order, "manager stopped")
		return wantErr
	}}
	err := runOperand(t.Context(), manager, func(ctx context.Context, _ ctrl.Manager) ([]func(), error) {
		recording = ctx
		callbacks := []func(){
			func() {
				if ctx.Err() != nil {
					t.Error("recording canceled before producers joined")
				}
				order = append(order, "jobs joined")
			},
			func() { order = append(order, "recorder stopped") },
		}
		if failSetup {
			return callbacks, wantErr
		}
		return callbacks, nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err=%v want=%v", err, wantErr)
	}
	wantOrder := []string{"manager stopped", "jobs joined", "recorder stopped"}
	if failSetup {
		wantOrder = wantOrder[1:]
	}
	if !reflect.DeepEqual(order, wantOrder) || recording.Err() == nil {
		t.Fatalf("order=%v recording error=%v", order, recording.Err())
	}
}
