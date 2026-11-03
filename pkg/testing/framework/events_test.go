package framework

import (
	"errors"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	clientgotesting "k8s.io/client-go/testing"
	crfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetEventsForObject(t *testing.T) {
	wanted := &eventsv1.Event{ObjectMeta: metav1.ObjectMeta{Name: "wanted", Namespace: "ns"}, Regarding: corev1.ObjectReference{Name: "pod", Namespace: "ns", UID: "uid"}, Note: "message"}
	wrongName := wanted.DeepCopy()
	wrongName.Name = "wrong-name"
	wrongName.Regarding.Name = "other"
	wrongNamespace := wanted.DeepCopy()
	wrongNamespace.Name = "wrong-namespace"
	wrongNamespace.Namespace = "other"
	wrongRegardingNamespace := wanted.DeepCopy()
	wrongRegardingNamespace.Name = "wrong-regarding-namespace"
	wrongRegardingNamespace.Regarding.Namespace = "other"
	client := fake.NewClientset(wanted, wrongName, wrongNamespace, wrongRegardingNamespace)
	events, err := GetEventsForObject(t.Context(), client, "pod", "ns")
	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(events).To(HaveLen(1))
	g.Expect(events[0].Name).To(Equal("wanted"))
	g.Expect(events[0].Note).To(Equal("message"))
	wantErr := errors.New("list failed")
	client.PrependReactor("list", "events", func(clientgotesting.Action) (bool, runtime.Object, error) { return true, nil, wantErr })
	_, err = GetEventsForObject(t.Context(), client, "pod", "ns")
	g.Expect(err).To(MatchError(wantErr))
}

func TestVerifyPodsEventsUsesRegardingUID(t *testing.T) {
	for _, tc := range []struct {
		name, uid, kind, reason string
		succeeds                bool
	}{
		{"current pod series", "new-uid", "Pod", "ExecFormatError", true},
		{"recreated pod", "old-uid", "Pod", "ExecFormatError", false},
		{"unrelated kind", "new-uid", "Node", "ExecFormatError", false},
		{"unrelated reason", "new-uid", "Pod", "Other", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "ns", UID: "new-uid", Labels: map[string]string{"app": "test"}}}
			event := &eventsv1.Event{ObjectMeta: metav1.ObjectMeta{Name: "event", Namespace: "ns"}, Regarding: corev1.ObjectReference{Name: "pod", Namespace: "ns", UID: types.UID(tc.uid), Kind: tc.kind}, Reason: tc.reason, Series: &eventsv1.EventSeries{Count: 3}}
			client := crfake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(pod, event).Build()
			var failures []string
			g := NewGomega(func(message string, _ ...int) { failures = append(failures, message) })
			VerifyPodsEvents(t.Context(), client, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns"}}, "app", "test", "ExecFormatError")(g)
			if (len(failures) == 0) != tc.succeeds {
				t.Fatalf("success=%v failures=%v", tc.succeeds, failures)
			}
		})
	}
}
