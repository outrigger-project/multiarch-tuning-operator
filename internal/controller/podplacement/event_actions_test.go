package podplacement

import (
	"context"
	"errors"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/openshift/multiarch-tuning-operator/api/common/plugins"
	"github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/internal/controller/podplacement/metrics"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/framework"
	imagefake "github.com/openshift/multiarch-tuning-operator/pkg/testing/image/fake"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

type failingEventImageCache struct{}

func (failingEventImageCache) GetCompatibleArchitecturesSet(context.Context, string, bool, [][]byte) (sets.Set[string], error) {
	return nil, errors.New("image failed at 100% %s")
}

type eventUpdateFailure struct{ client.Client }

func (c eventUpdateFailure) Update(context.Context, client.Object, ...client.UpdateOption) error {
	return errors.New("update failed")
}

func TestPod_EventActions(t *testing.T) {
	metrics.InitPodPlacementControllerMetrics()
	s := runtime.NewScheme()
	g := NewWithT(t)
	g.Expect(scheme.AddToScheme(s)).To(Succeed())
	g.Expect(v1beta1.AddToScheme(s)).To(Succeed())
	oldCache := imageInspectionCache
	imageInspectionCache = imagefake.FacadeSingleton()
	t.Cleanup(func() { imageInspectionCache = oldCache })
	preference := &plugins.NodeAffinityScoring{Platforms: []plugins.NodeAffinityScoringPlatformTerm{{Architecture: "amd64", Weight: 10}}}
	cases := []struct {
		name, reason, action, eventType, note string
		trigger                               func(*Pod)
	}{
		{"required", "ArchAwarePredicateSet", "NodeAffinityUpdate", "Normal", ArchitecturePredicateSetupMsg + "{amd64}", func(p *Pod) {
			_, err := p.SetNodeAffinityArchRequirement(nil)
			g.Expect(err).NotTo(HaveOccurred())
		}},
		{"no architectures", "NoSupportedArchitecturesFound", "ImageArchitectureCheck", "Normal", NoSupportedArchitecturesFoundMsg, func(p *Pod) {
			p.Spec.Containers = append(p.Spec.Containers, corev1.Container{Name: "arm", Image: imagefake.SingleArchArm64Image})
			_, err := p.SetNodeAffinityArchRequirement(nil)
			g.Expect(err).NotTo(HaveOccurred())
		}},
		{"preferred", "ArchAwarePredicateSet", "NodeAffinityUpdate", "Normal", ArchitecturePreferredPredicateSetupMsg + " source: test", func(p *Pod) { p.SetPreferredArchNodeAffinity(preference, "test") }},
		{"partial preferred", "ArchAwarePredicateSet", "NodeAffinityUpdate", "Normal", ArchitecturePreferredAffinityWithDuplicatesMsg + " source: test, skipped: amd64", func(p *Pod) {
			p.SetPreferredArchNodeAffinity(preference, "initial")
			p.SetPreferredArchNodeAffinity(&plugins.NodeAffinityScoring{Platforms: []plugins.NodeAffinityScoringPlatformTerm{{Architecture: "amd64", Weight: 10}, {Architecture: "arm64", Weight: 10}}}, "test")
		}},
		{"duplicates", "ArchAwarePreferredAffinityDuplicates", "NodeAffinitySkip", "Normal", ArchitecturePreferredAffinityAllDuplicatesMsg + " source: test, architectures: amd64", func(p *Pod) {
			p.SetPreferredArchNodeAffinity(preference, "initial")
			p.SetPreferredArchNodeAffinity(preference, "test")
		}},
		{"fallback", "ArchAwareFallbackPredicateSet", "NodeAffinityUpdate", "Warning", ArchitectureFallbackSetupMsg + "{arm64}", func(p *Pod) { p.setRequiredNodeAffinityToFallbackArchitecture("arm64") }},
		{"conflict", "ArchAwarePredicatesConflict", "NodeAffinitySkip", "Normal", ArchitecturePredicatesConflictMsg, func(p *Pod) {
			p.Spec.NodeSelector = map[string]string{utils.ArchLabel: "amd64"}
			_, err := p.SetNodeAffinityArchRequirement(nil)
			g.Expect(err).NotTo(HaveOccurred())
		}},
		{"inspection error", "ArchAwareInspectionError", "ImageArchitectureCheck", "Warning", ImageArchitectureInspectionErrorMsg + "image failed at 100% %s", func(p *Pod) { p.handleError(errors.New("image failed at 100% %s"), "inspection") }},
		{"max retries", "ArchAwareInspectionError", "ImageArchitectureCheck", "Warning", ImageInspectionErrorMaxRetriesMsg + ": image failed at 100% %s", func(p *Pod) {
			imageInspectionCache = failingEventImageCache{}
			defer func() { imageInspectionCache = imagefake.FacadeSingleton() }()
			p.Labels = map[string]string{utils.ImageInspectionErrorCountLabel: "4"}
			c := fake.NewClientBuilder().WithScheme(s).Build()
			(&PodReconciler{Client: c, APIReader: c}).processPod(p.Ctx(), p)
		}},
		{"preferred skipped", "ArchAwarePredicateSet", "NodeAffinitySkip", "Normal", ArchitecturePreferredPredicateSkippedMsg, func(p *Pod) {
			p.Labels = map[string]string{utils.PreferredNodeAffinityLabel: utils.LabelValueNotSet}
			c := fake.NewClientBuilder().WithScheme(s).Build()
			(&PodReconciler{Client: c, APIReader: c}).processPod(p.Ctx(), p)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			recorder, events, cleanup, err := framework.NewTestEventRecorder(t.Context(), s, utils.OperatorName)
			g.Expect(err).NotTo(HaveOccurred())
			t.Cleanup(cleanup)
			p := newPod(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "events-test", UID: "uid"}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: imagefake.SingleArchAmd64Image}}}}, t.Context(), recorder)
			tc.trigger(p)
			var event *eventsv1.Event
			g.Eventually(func() bool {
				select {
				case event = <-events:
					return event.Reason == tc.reason && event.Note == tc.note
				default:
					return false
				}
			}, 2*time.Second).Should(BeTrue())
			g.Expect(event.Action).To(Equal(tc.action))
			g.Expect(event.Type).To(Equal(tc.eventType))
			g.Expect(event.Related).To(BeNil())
			g.Expect(event.Regarding).To(Equal(corev1.ObjectReference{APIVersion: "v1", Kind: "Pod", Name: "pod", Namespace: "events-test", UID: "uid"}))
			g.Expect(event.ReportingController).To(Equal(utils.OperatorName))
		})
	}
}

func TestPod_GateRemovalEventActions(t *testing.T) {
	for _, updateFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[updateFails], func(t *testing.T) {
			g := NewWithT(t)
			s := runtime.NewScheme()
			g.Expect(scheme.AddToScheme(s)).To(Succeed())
			g.Expect(v1beta1.AddToScheme(s)).To(Succeed())
			recorder, events, cleanup, err := framework.NewTestEventRecorder(t.Context(), s, utils.OperatorName)
			g.Expect(err).NotTo(HaveOccurred())
			t.Cleanup(cleanup)
			p := eventJobPod()
			p.UID = "uid"
			p.Spec.NodeName = "already-scheduled"
			p.Spec.SchedulingGates = []corev1.PodSchedulingGate{{Name: utils.SchedulingGateName}}
			c := fake.NewClientBuilder().WithScheme(s).WithObjects(p).Build()
			var writer client.Client = c
			if updateFails {
				writer = eventUpdateFailure{c}
			}
			r := &PodReconciler{Client: writer, APIReader: c, Recorder: recorder}
			_, err = r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(p)})
			if updateFails {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
			observed := make(map[string]*eventsv1.Event)
			g.Eventually(func() int {
				select {
				case event := <-events:
					observed[event.Reason] = event
				default:
				}
				return len(observed)
			}, 2*time.Second).Should(Equal(2))
			ignored := observed["ArchAwareGatedPodIgnored"]
			removedReason := "ArchAwareSchedGateRemovalSuccess"
			if updateFails {
				removedReason = "ArchAwareSchedGateRemovalFailed"
			}
			removed := observed[removedReason]
			g.Expect(ignored).NotTo(BeNil())
			g.Expect(removed).NotTo(BeNil())
			g.Expect(ignored.Reason).To(Equal("ArchAwareGatedPodIgnored"))
			g.Expect(ignored.Action).To(Equal("PodIgnore"))
			g.Expect(ignored.Note).To(Equal(ArchitectureAwareGatedPodIgnoredMsg))
			g.Expect(removed.Action).To(Equal("SchedulingGateRemove"))
			if updateFails {
				g.Expect(removed.Reason).To(Equal("ArchAwareSchedGateRemovalFailed"))
				g.Expect(removed.Type).To(Equal("Warning"))
				g.Expect(removed.Note).To(Equal(SchedulingGateRemovalFailureMsg))
			} else {
				g.Expect(removed.Reason).To(Equal("ArchAwareSchedGateRemovalSuccess"))
				g.Expect(removed.Type).To(Equal("Normal"))
				g.Expect(removed.Note).To(Equal(SchedulingGateRemovalSuccessMsg))
			}
		})
	}
}
