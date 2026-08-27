/*
Copyright 2026 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package podplacement_test

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/multiarch-tuning-operator/api/common/plugins"
	"github.com/openshift/multiarch-tuning-operator/pkg/e2e"
	. "github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/framework"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

var _ = Describe("CEL Architecture Placement E2E", func() {
	var (
		podLabel            = map[string]string{"app": "cel-test"}
		schedulingGateLabel = map[string]string{utils.SchedulingGateLabel: utils.SchedulingGateLabelValueRemoved}
	)

	BeforeEach(func() {
		By("Verifying the operand is ready")
		Eventually(framework.ValidateCreation(client, ctx)).Should(Succeed())
	})
	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			_ = framework.StorePodsLog(ctx, clientset, client, utils.Namespace(), "control-plane", "controller-manager", "manager", os.Getenv("ARTIFACT_DIR"))
			_ = framework.StorePodsLog(ctx, clientset, client, utils.Namespace(), "controller", utils.PodPlacementControllerName, utils.PodPlacementControllerName, os.Getenv("ARTIFACT_DIR"))
			_ = framework.StorePodsLog(ctx, clientset, client, utils.Namespace(), "controller", utils.PodPlacementWebhookName, utils.PodPlacementWebhookName, os.Getenv("ARTIFACT_DIR"))
		}
		Eventually(framework.ValidateCreation(client, ctx)).Should(Succeed())
	})

	Context("When a PodPlacementConfig with a matching CEL rule exists", func() {
		It("should apply the CEL rule architecture instead of image-detected architectures", func() {
			By("Creating an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err := client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			defer client.Delete(ctx, ns) //nolint:errcheck

			By("Creating a PodPlacementConfig with a CEL rule matching all pods")
			ppc := NewPodPlacementConfig().
				WithGenerateName("cel-e2e-match-").
				WithNamespace(ns.Name).
				WithPriority(100).
				WithCelArchitecturePlacement(true,
					[]string{utils.ArchitectureAmd64},
					[]plugins.ArchitectureRule{
						NewRule("match-all", "true", utils.ArchitecturePpc64le),
					}).
				Build()
			err = client.Create(ctx, ppc)
			Expect(err).NotTo(HaveOccurred())
			defer client.Delete(ctx, ppc) //nolint:errcheck

			By("Creating a deployment with a multiarch image")
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				Build()
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("cel-match-test").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the scheduling gate was applied and removed")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "cel-test",
				e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())

			By("Verifying the CEL rule architecture (ppc64le) was applied")
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitecturePpc64le).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "cel-test",
				*expectedNSTs), e2e.WaitShort).Should(Succeed())
		})
	})

	Context("When a PodPlacementConfig with a non-matching CEL rule exists", func() {
		It("should apply the fallback architecture", func() {
			By("Creating an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err := client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			defer client.Delete(ctx, ns) //nolint:errcheck

			By("Creating a PodPlacementConfig with a CEL rule that does not match")
			ppc := NewPodPlacementConfig().
				WithGenerateName("cel-e2e-fallback-").
				WithNamespace(ns.Name).
				WithPriority(100).
				WithCelArchitecturePlacement(true,
					[]string{utils.ArchitectureArm64},
					[]plugins.ArchitectureRule{
						NewRule("no-match", "self.metadata.name == 'this-will-never-match'", utils.ArchitecturePpc64le),
					}).
				Build()
			err = client.Create(ctx, ppc)
			Expect(err).NotTo(HaveOccurred())
			defer client.Delete(ctx, ppc) //nolint:errcheck

			By("Creating a deployment")
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				Build()
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("cel-fallback-test").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the scheduling gate was applied and removed")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "cel-test",
				e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())

			By("Verifying the fallback architecture (arm64) was applied")
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureArm64).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "cel-test",
				*expectedNSTs), e2e.WaitShort).Should(Succeed())
		})
	})

	Context("When multiple PodPlacementConfigs exist with different priorities", func() {
		It("should apply the highest-priority matching PPC", func() {
			By("Creating an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err := client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			defer client.Delete(ctx, ns) //nolint:errcheck

			By("Creating a low-priority PPC with a matching CEL rule")
			ppcLow := NewPodPlacementConfig().
				WithGenerateName("cel-e2e-low-").
				WithNamespace(ns.Name).
				WithPriority(50).
				WithCelArchitecturePlacement(true,
					[]string{utils.ArchitectureAmd64},
					[]plugins.ArchitectureRule{
						NewRule("match-all-low", "true", utils.ArchitectureArm64),
					}).
				Build()
			err = client.Create(ctx, ppcLow)
			Expect(err).NotTo(HaveOccurred())
			defer client.Delete(ctx, ppcLow) //nolint:errcheck

			By("Creating a high-priority PPC with a matching CEL rule")
			ppcHigh := NewPodPlacementConfig().
				WithGenerateName("cel-e2e-high-").
				WithNamespace(ns.Name).
				WithPriority(200).
				WithCelArchitecturePlacement(true,
					[]string{utils.ArchitectureAmd64},
					[]plugins.ArchitectureRule{
						NewRule("match-all-high", "true", utils.ArchitectureS390x),
					}).
				Build()
			err = client.Create(ctx, ppcHigh)
			Expect(err).NotTo(HaveOccurred())
			defer client.Delete(ctx, ppcHigh) //nolint:errcheck

			By("Creating a deployment")
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				Build()
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("cel-priority-test").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the scheduling gate was applied and removed")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "cel-test",
				e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())

			By("Verifying the high-priority PPC architecture (s390x) was applied")
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureS390x).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "cel-test",
				*expectedNSTs), e2e.WaitShort).Should(Succeed())
		})
	})

	Context("When the CEL plugin is disabled", func() {
		It("should fall through to image-based architecture detection", func() {
			By("Creating an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err := client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			defer client.Delete(ctx, ns) //nolint:errcheck

			By("Creating a PPC with CEL disabled")
			ppc := NewPodPlacementConfig().
				WithGenerateName("cel-e2e-disabled-").
				WithNamespace(ns.Name).
				WithPriority(100).
				WithCelArchitecturePlacement(false,
					[]string{utils.ArchitecturePpc64le},
					[]plugins.ArchitectureRule{
						NewRule("should-not-fire", "true", utils.ArchitecturePpc64le),
					}).
				Build()
			err = client.Create(ctx, ppc)
			Expect(err).NotTo(HaveOccurred())
			defer client.Delete(ctx, ppc) //nolint:errcheck

			By("Creating a deployment with a multiarch image")
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				Build()
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("cel-disabled-test").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the scheduling gate was applied and removed")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "cel-test",
				e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())

			By("Verifying image-based detection was used (all architectures from the multiarch image)")
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn,
					utils.ArchitectureAmd64, utils.ArchitectureArm64,
					utils.ArchitectureS390x, utils.ArchitecturePpc64le).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "cel-test",
				*expectedNSTs), e2e.WaitShort).Should(Succeed())
		})
	})

	Context("When a CEL rule uses metadata matching", func() {
		It("should match pods by label selectors", func() {
			By("Creating an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err := client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			defer client.Delete(ctx, ns) //nolint:errcheck

			By("Creating a PPC with a CEL rule matching a specific label")
			ppc := NewPodPlacementConfig().
				WithGenerateName("cel-e2e-label-").
				WithNamespace(ns.Name).
				WithPriority(100).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: podLabel,
				}).
				WithCelArchitecturePlacement(true,
					[]string{utils.ArchitectureAmd64},
					[]plugins.ArchitectureRule{
						NewRule("match-by-annotation",
							"self.metadata.annotations.exists(k, k == 'cel-test/arch') && self.metadata.annotations['cel-test/arch'] == 'ppc64le'",
							utils.ArchitecturePpc64le),
					}).
				Build()
			err = client.Create(ctx, ppc)
			Expect(err).NotTo(HaveOccurred())
			defer client.Delete(ctx, ppc) //nolint:errcheck

			By("Creating a deployment with the matching annotation")
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				Build()
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("cel-label-test").
				WithNamespace(ns.Name).
				WithPodAnnotations(map[string]string{"cel-test/arch": "ppc64le"}).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the scheduling gate was applied and removed")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "cel-test",
				e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())

			By("Verifying the CEL-matched architecture (ppc64le) was applied")
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitecturePpc64le).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "cel-test",
				*expectedNSTs), e2e.WaitShort).Should(Succeed())
		})
	})

	Context("When a PodPlacementConfig has both CEL and NodeAffinityScoring enabled", func() {
		It("should apply both required (CEL) and preferred (scoring) node affinity", func() {
			By("Creating an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err := client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)

			By("Creating a PPC with both CEL and NodeAffinityScoring enabled")
			ppc := NewPodPlacementConfig().
				WithGenerateName("cel-e2e-coexist-").
				WithNamespace(ns.Name).
				WithPriority(100).
				WithCelArchitecturePlacement(true,
					[]string{utils.ArchitecturePpc64le},
					[]plugins.ArchitectureRule{
						NewRule("match-all", "true", utils.ArchitecturePpc64le),
					}).
				WithNodeAffinityScoring(true).
				WithNodeAffinityScoringTerm(utils.ArchitecturePpc64le, 50).
				Build()
			err = client.Create(ctx, ppc)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ppc)

			By("Creating a deployment with a multiarch image")
			podLabel := map[string]string{"app": "cel-coexist-test"}
			ps := NewPodSpec().WithContainersImages(helloOpenshiftPublicMultiarchImage).Build()
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("cel-coexist-test").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the scheduling gate was applied and removed")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "cel-coexist-test",
				e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())

			By("Verifying the required affinity (from CEL) was applied")
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitecturePpc64le).Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "cel-coexist-test",
				*expectedNSTs), e2e.WaitShort).Should(Succeed())

			By("Verifying the preferred affinity (from PPC + CPPC NodeAffinityScoring) was applied")
			expectedPreferred := NewPreferredSchedulingTerms().
				WithArchitectureWeight(utils.ArchitecturePpc64le, 50).
				WithArchitectureWeight(utils.ArchitectureAmd64, 50).Build()
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "cel-coexist-test",
				expectedPreferred), e2e.WaitShort).Should(Succeed())
		})
	})

	Context("When a PodPlacementConfig is deleted while pods are pending", func() {
		It("should still ungate the pod using image-based detection", func() {
			By("Creating an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err := client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)

			By("Creating a PPC with CEL")
			ppc := NewPodPlacementConfig().
				WithGenerateName("cel-e2e-delete-").
				WithNamespace(ns.Name).
				WithPriority(100).
				WithCelArchitecturePlacement(true,
					[]string{utils.ArchitecturePpc64le},
					[]plugins.ArchitectureRule{
						NewRule("match-all", "true", utils.ArchitecturePpc64le),
					}).
				Build()
			err = client.Create(ctx, ppc)
			Expect(err).NotTo(HaveOccurred())

			By("Creating a deployment")
			podLabel := map[string]string{"app": "cel-delete-test"}
			ps := NewPodSpec().WithContainersImages(helloOpenshiftPublicMultiarchImage).Build()
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("cel-delete-test").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())

			By("Deleting the PPC immediately")
			err = client.Delete(ctx, ppc)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the pod still gets ungated via image-based detection")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "cel-delete-test",
				e2e.Present, schedulingGateLabel), e2e.WaitMedium).Should(Succeed())
		})
	})

	Context("When a pod has an existing nodeSelector for kubernetes.io/arch", func() {
		It("should override the nodeSelector with the CEL rule architecture", func() {
			By("Creating an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err := client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)

			By("Creating a PPC with a CEL rule")
			ppc := NewPodPlacementConfig().
				WithGenerateName("cel-e2e-nodeselector-").
				WithNamespace(ns.Name).
				WithPriority(100).
				WithCelArchitecturePlacement(true,
					[]string{utils.ArchitectureAmd64},
					[]plugins.ArchitectureRule{
						NewRule("match-all", "true", utils.ArchitecturePpc64le),
					}).
				Build()
			err = client.Create(ctx, ppc)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ppc)

			By("Creating a deployment with a nodeSelector for amd64")
			podLabel := map[string]string{"app": "cel-nodeselector-test"}
			ps := NewPodSpec().WithContainersImages(helloOpenshiftPublicMultiarchImage).Build()
			// Add nodeSelector for arch - CEL should override this
			ps.NodeSelector = map[string]string{utils.ArchLabel: utils.ArchitectureAmd64}
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("cel-nodeselector-test").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the scheduling gate was applied and removed")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "cel-nodeselector-test",
				e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())

			By("Verifying the CEL architecture (ppc64le) was applied, not the original nodeSelector (amd64)")
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitecturePpc64le).Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "cel-nodeselector-test",
				*expectedNSTs), e2e.WaitShort).Should(Succeed())
		})
	})

	Context("When PPC LabelSelector does not match the pod", func() {
		It("should fall through to image-based detection instead of CEL", func() {
			By("Creating an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err := client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			defer client.Delete(ctx, ns) //nolint:errcheck

			By("Creating a PPC targeting pods with label team=backend")
			ppc := NewPodPlacementConfig().
				WithGenerateName("cel-e2e-labelmiss-").
				WithNamespace(ns.Name).
				WithPriority(100).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{"team": "backend"},
				}).
				WithCelArchitecturePlacement(true,
					[]string{utils.ArchitecturePpc64le},
					[]plugins.ArchitectureRule{
						NewRule("match-all", "true", utils.ArchitecturePpc64le),
					}).
				Build()
			err = client.Create(ctx, ppc)
			Expect(err).NotTo(HaveOccurred())
			defer client.Delete(ctx, ppc) //nolint:errcheck

			By("Creating a deployment with a non-matching label app=cel-label-test")
			podLabel := map[string]string{"app": "cel-label-test"}
			ps := NewPodSpec().WithContainersImages(helloOpenshiftPublicMultiarchImage).Build()
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("cel-labelmiss-test").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the scheduling gate was applied and removed")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "cel-label-test",
				e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())

			By("Verifying image-based detection was used (all architectures from multiarch image), NOT ppc64le from CEL")
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn,
					utils.ArchitectureAmd64, utils.ArchitectureArm64,
					utils.ArchitectureS390x, utils.ArchitecturePpc64le).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "cel-label-test",
				*expectedNSTs), e2e.WaitShort).Should(Succeed())
		})
	})

	Context("When PPCs exist in different namespaces", func() {
		It("should apply each namespace's PPC independently", func() {
			By("Creating the first ephemeral namespace")
			ns1 := framework.NewEphemeralNamespace()
			err := client.Create(ctx, ns1)
			Expect(err).NotTo(HaveOccurred())
			defer client.Delete(ctx, ns1) //nolint:errcheck

			By("Creating the second ephemeral namespace")
			ns2 := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns2)
			Expect(err).NotTo(HaveOccurred())
			defer client.Delete(ctx, ns2) //nolint:errcheck

			By("Creating a PPC in ns1 forcing ppc64le")
			ppc1 := NewPodPlacementConfig().
				WithGenerateName("cel-e2e-ns1-").
				WithNamespace(ns1.Name).
				WithPriority(100).
				WithCelArchitecturePlacement(true,
					[]string{utils.ArchitecturePpc64le},
					[]plugins.ArchitectureRule{
						NewRule("match-all", "true", utils.ArchitecturePpc64le),
					}).
				Build()
			err = client.Create(ctx, ppc1)
			Expect(err).NotTo(HaveOccurred())
			defer client.Delete(ctx, ppc1) //nolint:errcheck

			By("Creating a PPC in ns2 forcing arm64")
			ppc2 := NewPodPlacementConfig().
				WithGenerateName("cel-e2e-ns2-").
				WithNamespace(ns2.Name).
				WithPriority(100).
				WithCelArchitecturePlacement(true,
					[]string{utils.ArchitectureArm64},
					[]plugins.ArchitectureRule{
						NewRule("match-all", "true", utils.ArchitectureArm64),
					}).
				Build()
			err = client.Create(ctx, ppc2)
			Expect(err).NotTo(HaveOccurred())
			defer client.Delete(ctx, ppc2) //nolint:errcheck

			By("Creating deployments in both namespaces")
			podLabel := map[string]string{"app": "cel-ns-test"}
			ps := NewPodSpec().WithContainersImages(helloOpenshiftPublicMultiarchImage).Build()

			d1 := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("cel-ns1-test").
				WithNamespace(ns1.Name).
				Build()
			err = client.Create(ctx, d1)
			Expect(err).NotTo(HaveOccurred())

			d2 := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("cel-ns2-test").
				WithNamespace(ns2.Name).
				Build()
			err = client.Create(ctx, d2)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying ns1 pod gets ppc64le from its PPC")
			Eventually(framework.VerifyPodLabels(ctx, client, ns1, "app", "cel-ns-test",
				e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			archNSR1 := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitecturePpc64le).Build()
			expectedNSTs1 := NewNodeSelectorTerm().WithMatchExpressions(archNSR1).Build()
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns1, "app", "cel-ns-test",
				*expectedNSTs1), e2e.WaitShort).Should(Succeed())

			By("Verifying ns2 pod gets arm64 from its PPC")
			Eventually(framework.VerifyPodLabels(ctx, client, ns2, "app", "cel-ns-test",
				e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			archNSR2 := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureArm64).Build()
			expectedNSTs2 := NewNodeSelectorTerm().WithMatchExpressions(archNSR2).Build()
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns2, "app", "cel-ns-test",
				*expectedNSTs2), e2e.WaitShort).Should(Succeed())
		})
	})
})
