package podplacement

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	"github.com/openshift/multiarch-tuning-operator/pkg/testing/framework"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
	"github.com/panjf2000/ants/v2"
)

func eventJobWebhook(t *testing.T, handler http.HandlerFunc) *PodSchedulingGateMutatingWebHook {
	t.Helper()
	return eventJobWebhookWithContext(t, t.Context(), handler)
}

func eventJobWebhookWithContext(t *testing.T, ctx context.Context, handler http.HandlerFunc) *PodSchedulingGateMutatingWebHook {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	clientset, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := ants.NewMultiPool(1, 8, ants.LeastTasks, ants.WithNonblocking(true))
	if err != nil {
		t.Fatal(err)
	}
	wh := NewPodSchedulingGateMutatingWebHook(ctx, nil, nil, nil, clientset, scheme.Scheme, nil, pool)
	t.Cleanup(func() { _ = wh.ShutdownEventJobs(time.Second) })
	return wh
}

func eventJobPod() *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "test-ns"}}
}

func awaitJobSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("job did not finish before deadline")
	}
}

func TestWebhookEventJobsDrain(t *testing.T) {
	g := NewWithT(t)
	recorder, events, cleanup, err := framework.NewTestEventRecorder(t.Context(), scheme.Scheme, utils.OperatorName)
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(cleanup)
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	wh := eventJobWebhook(t, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"apiVersion":"v1","kind":"Pod","metadata":{"name":"pod","namespace":"test-ns","uid":"persisted-uid"}}`)
		close(finished)
	})
	wh.recorder = recorder
	wh.delayedSchedulingGatedEvent(t.Context(), eventJobPod())
	awaitJobSignal(t, started)
	done := make(chan error, 1)
	go func() { done <- wh.ShutdownEventJobs(time.Second) }()
	select {
	case err := <-done:
		t.Fatalf("shutdown finished before producer: %v", err)
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	awaitJobSignal(t, finished)
	var event *eventsv1.Event
	g.Eventually(events, 2*time.Second).Should(Receive(&event))
	g.Expect(event.Regarding).To(Equal(corev1.ObjectReference{APIVersion: "v1", Kind: "Pod", Name: "pod", Namespace: "test-ns", UID: "persisted-uid"}))
	g.Expect(event.Related).To(BeNil())
	g.Expect(event.Type).To(Equal(corev1.EventTypeNormal))
	g.Expect(event.Reason).To(Equal("ArchAwareSchedGateAdded"))
	g.Expect(event.Action).To(Equal("SchedulingGateAdd"))
	g.Expect(event.Note).To(Equal(SchedulingGateAddedMsg))
	g.Expect(event.ReportingController).To(Equal(utils.OperatorName))
}

func TestWebhookEventJobsIgnoreAdmissionCancellation(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	wh := eventJobWebhook(t, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
			t.Error("admission canceled event job")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"apiVersion":"v1","kind":"Pod","metadata":{"name":"pod","namespace":"test-ns","uid":"persisted-uid"}}`)
	})
	ctx, cancel := context.WithCancel(t.Context())
	wh.delayedSchedulingGatedEvent(ctx, eventJobPod())
	awaitJobSignal(t, started)
	cancel()
	close(release)
	if err := wh.ShutdownEventJobs(time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestWebhookEventJobsTimeoutCancelsAPIRead(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	wh := eventJobWebhook(t, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(canceled)
	})
	wh.delayedSchedulingGatedEvent(t.Context(), eventJobPod())
	awaitJobSignal(t, started)
	if err := wh.ShutdownEventJobs(10 * time.Millisecond); err == nil {
		t.Fatal("expected drain timeout")
	}
	awaitJobSignal(t, canceled)
}

func TestWebhookEventJobsTimeoutCancelsBackoff(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	var calls atomic.Int32
	wh := eventJobWebhook(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		once.Do(func() { close(started) })
		http.Error(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404}`, http.StatusNotFound)
	})
	wh.delayedSchedulingGatedEvent(t.Context(), eventJobPod())
	awaitJobSignal(t, started)
	if err := wh.ShutdownEventJobs(10 * time.Millisecond); err == nil {
		t.Fatal("expected drain timeout")
	}
	before := calls.Load()
	wh.delayedSchedulingGatedEvent(t.Context(), eventJobPod())
	if calls.Load() != before {
		t.Fatal("new job admitted after shutdown")
	}
}

func TestWebhookEventJobsRejectedSubmission(t *testing.T) {
	wh := eventJobWebhook(t, func(http.ResponseWriter, *http.Request) { t.Error("rejected job made an API request") })
	if err := wh.workerPool.ReleaseTimeout(time.Second); err != nil {
		t.Fatal(err)
	}
	wh.delayedSchedulingGatedEvent(t.Context(), eventJobPod())
	done := make(chan struct{})
	go func() { _ = wh.ShutdownEventJobs(time.Second); close(done) }()
	awaitJobSignal(t, done)
}

func TestWebhookEventJobsAdmissionShutdownRace(t *testing.T) {
	var calls atomic.Int32
	wh := eventJobWebhook(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"apiVersion":"v1","kind":"Pod","metadata":{"name":"pod","namespace":"test-ns","uid":"persisted-uid"}}`)
	})
	var submitters sync.WaitGroup
	for i := 0; i < 32; i++ {
		submitters.Go(func() { wh.delayedSchedulingGatedEvent(t.Context(), eventJobPod()) })
	}
	if err := wh.ShutdownEventJobs(time.Second); err != nil {
		t.Fatal(err)
	}
	submitters.Wait()
	before := calls.Load()
	wh.delayedSchedulingGatedEvent(t.Context(), eventJobPod())
	if calls.Load() != before {
		t.Fatal("late submission reached API")
	}
}

func TestWebhookEventJobsParentCancellationCancelsAPIRead(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started, canceled := make(chan struct{}), make(chan struct{})
	wh := eventJobWebhookWithContext(t, ctx, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(canceled)
	})
	wh.delayedSchedulingGatedEvent(t.Context(), eventJobPod())
	awaitJobSignal(t, started)
	cancel()
	awaitJobSignal(t, canceled)
}
