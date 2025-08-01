package podplacementconfig_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/framework"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

var _ = Describe("The Multiarch Tuning Operator", func() {
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
				builder.NewPodPlacementConfig().
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
				builder.NewPodPlacementConfig().
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
				builder.NewPodPlacementConfig().
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
		It("The webhook should deny creation when update a local ppc priority to an exsiting one", func() {
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err := client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Creating a local PodPlacementConfig with priority 30")
			err = client.Create(ctx,
				builder.NewPodPlacementConfig().
					WithName("test-ppc").
					WithNamespace(ns.Name).
					WithPriority(30).
					WithPlugins().
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 50).
					Build(),
			)
			Expect(err).NotTo(HaveOccurred(), "the PodPlacementConfig should be accepted", err)
			By("Creating another local PodPlacementConfig with priority 50")
			err = client.Create(ctx,
				builder.NewPodPlacementConfig().
					WithName("test-ppc-2").
					WithNamespace(ns.Name).
					WithPriority(50).
					WithPlugins().
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 50).
					Build(),
			)
			Expect(err).NotTo(HaveOccurred(), "the PodPlacementConfig should be accepted", err)
			By("Update the first local PodPlacementConfig priority to 50")
			ppc1 := &v1beta1.PodPlacementConfig{}
			err = client.Get(ctx, crclient.ObjectKey{
				Name:      "test-ppc",
				Namespace: ns.Name,
			}, ppc1)
			Expect(err).NotTo(HaveOccurred())
			ppc1.Spec.Priority = 50
			err = client.Update(ctx, ppc1)
			Expect(err).To(HaveOccurred(), "the PodPlacementConfig update should not be accepted", err)
		})
		It("The webhook should allow creation when the ppc is recreated with the same priority after delation", func() {
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err := client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Creating a local PodPlacementConfig with priority 50")
			err = client.Create(ctx,
				builder.NewPodPlacementConfig().
					WithName("test-ppc").
					WithNamespace(ns.Name).
					WithPriority(50).
					WithPlugins().
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 50).
					Build(),
			)
			By("Check it can be created")
			Expect(err).NotTo(HaveOccurred(), "the PodPlacementConfig should be accepted", err)
			By("Deleting above created PodPlacementConfig")
			err = client.Delete(ctx, builder.NewPodPlacementConfig().
				WithName("test-ppc").
				WithNamespace(ns.Name).Build())
			Expect(err).NotTo(HaveOccurred())
			By("Creating the PodPlacementConfig with the same priority 50 again")
			err = client.Create(ctx,
				builder.NewPodPlacementConfig().
					WithName("test-ppc").
					WithNamespace(ns.Name).
					WithPriority(50).
					WithPlugins().
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 50).
					Build(),
			)
			By("Check it can be created again")
			Expect(err).NotTo(HaveOccurred(), "the PodPlacementConfig should be accepted", err)
		})
	})
})
