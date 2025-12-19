package podplacement

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/image/fake/registry"
)

var _ = Describe("Internal/Controller/PodPlacement/scheduling_gate_mutating_webhook", func() {
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
