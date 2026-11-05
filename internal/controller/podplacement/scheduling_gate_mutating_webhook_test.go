package podplacement

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/framework"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/image/fake/registry"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

var _ = Describe("Internal/Controller/PodPlacement/scheduling_gate_mutating_webhook", func() {
	It("publishes a gate event through events/v1 and preserves core/v1 conversion", func() {
		pod := builder.NewPod().WithName(fmt.Sprintf("native-event-%d", time.Now().UnixNano())).WithNamespace("test-namespace").
			WithContainersImages(fmt.Sprintf("%s/%s/%s:latest", registryAddress, registry.PublicRepo, registry.ComputeNameByMediaType(imgspecv1.MediaTypeImageIndex))).Build()
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, pod) })
		clientset, err := kubernetes.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())
		Eventually(func(g Gomega) {
			events, err := framework.GetEventsForObject(ctx, clientset, pod.Name, pod.Namespace)
			g.Expect(err).NotTo(HaveOccurred())
			var native *eventsv1.Event
			for i := range events {
				if events[i].Regarding.UID == pod.UID && events[i].Reason == ArchitectureAwareSchedulingGateAdded {
					native = &events[i]
					break
				}
			}
			g.Expect(native).NotTo(BeNil())
			g.Expect(native.Regarding.APIVersion).To(Equal("v1"))
			g.Expect(native.Regarding.Kind).To(Equal("Pod"))
			g.Expect(native.Regarding.Name).To(Equal(pod.Name))
			g.Expect(native.Regarding.Namespace).To(Equal(pod.Namespace))
			g.Expect(native.Regarding.UID).To(Equal(pod.UID))
			g.Expect(native.Related).To(BeNil())
			g.Expect(native.Type).To(Equal(corev1.EventTypeNormal))
			g.Expect(native.Reason).To(Equal("ArchAwareSchedGateAdded"))
			g.Expect(native.Action).To(Equal("SchedulingGateAdd"))
			g.Expect(native.Note).To(Equal(SchedulingGateAddedMsg))
			g.Expect(native.ReportingController).To(Equal(utils.OperatorName))
			legacy, err := clientset.CoreV1().Events(pod.Namespace).Get(ctx, native.Name, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(legacy.InvolvedObject).To(Equal(native.Regarding))
			g.Expect(legacy.Message).To(Equal(native.Note))
			g.Expect(legacy.Reason).To(Equal(native.Reason))
			g.Expect(legacy.Type).To(Equal(native.Type))
			g.Expect(legacy.Action).To(Equal(native.Action))
			g.Expect(legacy.ReportingController).To(Equal(native.ReportingController))
		}, 10*time.Second).Should(Succeed())
	})
	When("The scheduling gat mutating webhook", func() {
		Context("is handling the mutation of pods", func() {
			It("should ignore pods with a non-empty nodeName", func() {
				pod := builder.NewPod().
					WithContainersImages(fmt.Sprintf("%s/%s/%s:latest", registryAddress,
						registry.PublicRepo, registry.ComputeNameByMediaType(imgspecv1.MediaTypeImageIndex))).
					WithGenerateName("test-pod-").
					WithNamespace("test-namespace").
					WithNodeName("test-node-name").
					Build()
				err := k8sClient.Create(ctx, pod)
				Expect(err).NotTo(HaveOccurred(), "failed to create the pod", err)
			})
		})
	})
})
