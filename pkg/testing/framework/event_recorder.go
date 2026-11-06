package framework

import (
	"context"
	"sync"

	eventsv1 "k8s.io/api/events/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoevents "k8s.io/client-go/tools/events"
)

// NewTestEventRecorder captures native events without starting an API sink.
// Cleanup cancels blocked capture callbacks; the event channel is never closed.
func NewTestEventRecorder(ctx context.Context, scheme *runtime.Scheme, reportingController string) (clientgoevents.EventRecorderLogger, <-chan *eventsv1.Event, func(), error) {
	ctx, cancel := context.WithCancel(ctx)
	broadcaster := clientgoevents.NewBroadcaster(nil)
	captured := make(chan *eventsv1.Event, 64)
	stopWatcher, err := broadcaster.StartEventWatcher(func(object runtime.Object) {
		event := object.(*eventsv1.Event).DeepCopy()
		select {
		case captured <- event:
		case <-ctx.Done():
		}
	})
	if err != nil {
		cancel()
		broadcaster.Shutdown()
		return nil, nil, func() {}, err
	}
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			cancel()
			stopWatcher()
			broadcaster.Shutdown()
		})
	}
	go func() {
		<-ctx.Done()
		cleanup()
	}()
	return broadcaster.NewRecorder(scheme, reportingController), captured, cleanup, nil
}
