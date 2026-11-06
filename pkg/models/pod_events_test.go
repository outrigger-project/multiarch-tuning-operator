package models

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/openshift/multiarch-tuning-operator/pkg/testing/framework"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

func TestPod_PublishEvent(t *testing.T) {
	for _, eventType := range []string{corev1.EventTypeNormal, corev1.EventTypeWarning} {
		t.Run(eventType, func(t *testing.T) {
			g := NewWithT(t)
			recorder, events, cleanup, err := framework.NewTestEventRecorder(t.Context(), scheme.Scheme, utils.OperatorName)
			g.Expect(err).NotTo(HaveOccurred())
			t.Cleanup(cleanup)
			pod := NewPod(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "ns", UID: "uid"}}, t.Context(), recorder)
			pod.PublishEvent(eventType, "Reason", "NodeAffinityUpdate", "literal 100% %s")
			var event *eventsv1.Event
			g.Eventually(events, 2*time.Second).Should(Receive(&event))
			g.Expect(event.Regarding).To(Equal(corev1.ObjectReference{APIVersion: "v1", Kind: "Pod", Name: "pod", Namespace: "ns", UID: "uid"}))
			g.Expect(event.Related).To(BeNil())
			g.Expect(event.Type).To(Equal(eventType))
			g.Expect(event.Reason).To(Equal("Reason"))
			g.Expect(event.Action).To(Equal("NodeAffinityUpdate"))
			g.Expect(event.Note).To(Equal("literal 100% %s"))
			g.Expect(event.ReportingController).To(Equal(utils.OperatorName))
		})
	}
	NewPod(&corev1.Pod{}, t.Context(), nil).PublishEvent(corev1.EventTypeNormal, "Reason", "Action", "message")
}
