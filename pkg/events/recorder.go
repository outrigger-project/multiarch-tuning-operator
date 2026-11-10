// Package events owns native Kubernetes event recorder lifetimes.
package events

import (
	"context"
	"sync"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoevents "k8s.io/client-go/tools/events"
)

// NewRecorder starts an events/v1 broadcaster. Stop producers before cleanup.
func NewRecorder(ctx context.Context, client kubernetes.Interface, scheme *runtime.Scheme, reportingController string) (clientgoevents.EventRecorderLogger, func(), error) {
	return newRecorder(ctx, clientgoevents.NewBroadcaster(&clientgoevents.EventSinkImpl{Interface: client.EventsV1()}), scheme, reportingController)
}

func newRecorder(ctx context.Context, broadcaster clientgoevents.EventBroadcaster, scheme *runtime.Scheme, reportingController string) (clientgoevents.EventRecorderLogger, func(), error) {
	ctx, cancel := context.WithCancel(ctx)
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			cancel()
			broadcaster.Shutdown()
		})
	}
	if err := broadcaster.StartRecordingToSinkWithContext(ctx); err != nil {
		cleanup()
		return nil, cleanup, err
	}
	return broadcaster.NewRecorder(scheme, reportingController), cleanup, nil
}
