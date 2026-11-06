package framework

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

func TestNewTestEventRecorderCapturesStructuredEvent(t *testing.T) {
	g := NewWithT(t)
	recorder, events, cleanup, err := NewTestEventRecorder(t.Context(), scheme.Scheme, utils.OperatorName)
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(cleanup)
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "test-ns", UID: "pod-uid"}}
	recorder.Eventf(pod, nil, corev1.EventTypeNormal, "ArchAwareSchedGateAdded", "SchedulingGateAdd", "%s", "literal 100% %s")
	var event *eventsv1.Event
	g.Eventually(events, 2*time.Second).Should(Receive(&event))
	g.Expect(event.Regarding).To(Equal(corev1.ObjectReference{APIVersion: "v1", Kind: "Pod", Name: "test-pod", Namespace: "test-ns", UID: "pod-uid"}))
	g.Expect(event.Related).To(BeNil())
	g.Expect(event.Type).To(Equal(corev1.EventTypeNormal))
	g.Expect(event.Reason).To(Equal("ArchAwareSchedGateAdded"))
	g.Expect(event.Action).To(Equal("SchedulingGateAdd"))
	g.Expect(event.Note).To(Equal("literal 100% %s"))
	g.Expect(event.ReportingController).To(Equal(utils.OperatorName))
}

func TestNewTestEventRecorderCleanupUnblocksCapture(t *testing.T) {
	for _, cancelParent := range []bool{false, true} {
		t.Run(map[bool]string{false: "cleanup", true: "parent cancellation"}[cancelParent], func(t *testing.T) {
			g := NewWithT(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			recorder, events, cleanup, err := NewTestEventRecorder(ctx, scheme.Scheme, "test")
			g.Expect(err).NotTo(HaveOccurred())
			t.Cleanup(cleanup)
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "ns", UID: "uid"}}
			for i := 0; i < 128; i++ {
				recorder.Eventf(pod, nil, corev1.EventTypeNormal, "Reason", "Action", "event %d", i)
			}
			g.Eventually(func() int { return len(events) }, 2*time.Second).Should(Equal(cap(events)))
			if cancelParent {
				cancel()
			}
			done := make(chan struct{})
			go func() { cleanup(); cleanup(); close(done) }()
			g.Eventually(done, 2*time.Second).Should(BeClosed())
		})
	}
}
