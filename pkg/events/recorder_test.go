package events

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	clientgotesting "k8s.io/client-go/testing"
	clientgoevents "k8s.io/client-go/tools/events"
)

func TestNewRecorderPublishesEventsV1(t *testing.T) {
	client := fake.NewClientset()
	published := make(chan *eventsv1.Event, 1)
	client.PrependReactor("create", "events", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		if action.GetResource().Group != "events.k8s.io" {
			t.Errorf("unexpected event API: %v", action.GetResource())
		}
		event := action.(clientgotesting.CreateAction).GetObject().(*eventsv1.Event)
		published <- event.DeepCopy()
		return true, event, nil
	})
	recorder, cleanup, err := NewRecorder(t.Context(), client, scheme.Scheme, "test-controller")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	recorder.Eventf(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "test-ns", UID: "pod-uid"}}, nil,
		corev1.EventTypeNormal, "GateAdded", "SchedulingGateAdd", "%s", "literal 100% %s")
	select {
	case event := <-published:
		if event.Namespace != "test-ns" || event.Regarding.UID != "pod-uid" || event.ReportingController != "test-controller" || event.Action != "SchedulingGateAdd" || event.Note != "literal 100% %s" {
			t.Fatalf("unexpected event: %#v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("event not published")
	}
}

type lifecycleBroadcaster struct {
	clientgoevents.EventBroadcaster
	ctx       context.Context
	err       error
	shutdowns int
}

func (b *lifecycleBroadcaster) StartRecordingToSinkWithContext(ctx context.Context) error {
	b.ctx = ctx
	return b.err
}

func (b *lifecycleBroadcaster) Shutdown() { b.shutdowns++ }

func TestNewRecorderStartupFailureCleansUp(t *testing.T) {
	want := errors.New("start failed")
	b := &lifecycleBroadcaster{err: want}
	recorder, _, err := newRecorder(t.Context(), b, scheme.Scheme, "test")
	if !errors.Is(err, want) || recorder != nil || b.ctx.Err() == nil || b.shutdowns != 1 {
		t.Fatalf("startup cleanup: recorder=%v err=%v canceled=%v shutdowns=%d", recorder, err, b.ctx.Err(), b.shutdowns)
	}
}

func TestNewRecorderCleanupIsIdempotent(t *testing.T) {
	real := clientgoevents.NewBroadcaster(nil)
	t.Cleanup(real.Shutdown)
	b := &lifecycleBroadcaster{EventBroadcaster: real}
	_, cleanup, err := newRecorder(t.Context(), b, scheme.Scheme, "test")
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	cleanup()
	if b.shutdowns != 1 || b.ctx.Err() == nil {
		t.Fatalf("shutdowns=%d canceled=%v", b.shutdowns, b.ctx.Err())
	}
}
