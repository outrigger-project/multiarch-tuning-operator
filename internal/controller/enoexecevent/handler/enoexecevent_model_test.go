package handler

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/framework"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

func TestENoExecEventPublish(t *testing.T) {
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := v1beta1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	for _, onPod := range []bool{false, true} {
		t.Run(map[bool]string{false: "CR forwarding", true: "pod forwarding"}[onPod], func(t *testing.T) {
			g := NewWithT(t)
			recorder, events, cleanup, err := framework.NewTestEventRecorder(t.Context(), s, utils.EnoexecControllerName)
			g.Expect(err).NotTo(HaveOccurred())
			t.Cleanup(cleanup)
			metadata := metav1.ObjectMeta{Name: "source", Namespace: "ns", UID: "uid"}
			e := NewENoExecEvent(&v1beta1.ENoExecEvent{ObjectMeta: metadata}, t.Context(), recorder)
			want := corev1.ObjectReference{APIVersion: v1beta1.GroupVersion.String(), Kind: "ENoExecEvent", Name: "source", Namespace: "ns", UID: "uid"}
			if onPod {
				e.PublishEventOnPod(&corev1.Pod{ObjectMeta: metadata}, corev1.EventTypeWarning, "ExecFormatError", "ExecFormatErrorReport", "literal 100% %s")
				want.APIVersion, want.Kind = "v1", "Pod"
			} else {
				e.PublishEvent(corev1.EventTypeWarning, "ExecFormatError", "ExecFormatErrorReport", "literal 100% %s")
			}
			var event *eventsv1.Event
			g.Eventually(events, 2*time.Second).Should(Receive(&event))
			g.Expect(event.Regarding).To(Equal(want))
			g.Expect(event.Related).To(BeNil())
			g.Expect(event.Type).To(Equal(corev1.EventTypeWarning))
			g.Expect(event.Reason).To(Equal("ExecFormatError"))
			g.Expect(event.Action).To(Equal("ExecFormatErrorReport"))
			g.Expect(event.Note).To(Equal("literal 100% %s"))
			g.Expect(event.ReportingController).To(Equal(utils.EnoexecControllerName))
			e.PublishEventOnPod(nil, corev1.EventTypeWarning, "Reason", "Action", "message")
			g.Consistently(events, 50*time.Millisecond).ShouldNot(Receive())
		})
	}
	e := NewENoExecEvent(&v1beta1.ENoExecEvent{}, t.Context(), nil)
	e.PublishEvent(corev1.EventTypeNormal, "Reason", "Action", "message")
	e.PublishEventOnPod(&corev1.Pod{}, corev1.EventTypeNormal, "Reason", "Action", "message")
}
