package podplacementconfig_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/framework"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

var _ = Describe("The Multiarch Tuning Operator", Serial, func() {
	Context("When a pod placement config is created", func() {
		It("should fail creating the PPC with multiple items for the same architecture in the plugins.nodeAffinityScoring.Platforms list", func() {
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err := client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Creating a local PodPlacementConfig with the same architecture")
			err = client.Create(ctx,
				NewPodPlacementConfig().
					WithName("test-ppc").
					WithNamespace(ns.Name).
					WithPlugins().
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 50).
					WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 100).
					Build(),
			)
			Expect(err).To(HaveOccurred(), "the PodPlacementConfig should not be accepted", err)
		})
		It("The webhook should deny creation when a PodPlacementConfig with the same priority already exists in the same namespace", func() {
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err := client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Creating a local PodPlacementConfig with a Prioriry setting")
			err = client.Create(ctx,
				NewPodPlacementConfig().
					WithName("test-ppc").
					WithNamespace(ns.Name).
					WithPriority(50).
					WithPlugins().
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 50).
					Build(),
			)
			Expect(err).NotTo(HaveOccurred(), "the PodPlacementConfig should be accepted", err)
			By("Creating another local PodPlacementConfig with the same Prioriry")
			err = client.Create(ctx,
				NewPodPlacementConfig().
					WithName("test-ppc-2").
					WithNamespace(ns.Name).
					WithPriority(50).
					WithPlugins().
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm(utils.ArchitectureArm64, 50).
					Build(),
			)
			Expect(err).To(HaveOccurred(), "the PodPlacementConfig should not be accepted", err)
		})
	})
})
