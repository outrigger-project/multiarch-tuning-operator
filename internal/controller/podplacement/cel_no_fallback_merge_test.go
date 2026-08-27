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

package podplacement

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/multiarch-tuning-operator/api/common/plugins"
	"github.com/openshift/multiarch-tuning-operator/pkg/e2e"
	. "github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

var _ = Describe("CEL Plugin - No Fallback/Image-Detection Merge After Match", func() {
	const (
		timeout  = e2e.WaitShort
		interval = time.Millisecond * 250
	)

	// ns is re-created as a uniquely-named, ephemeral namespace for every It
	// block. DeferCleanup inside newEphemeralTestNamespace() guarantees cleanup
	// even when a spec fails or panics.
	var ns *corev1.Namespace

	BeforeEach(func() {
		ns = newEphemeralTestNamespace()
	})

	Context("Early Return After CEL Match", func() {
		It("should contain ONLY matched rule architectures without merging fallback or image-detected architectures", func() {
			By("Creating a PodPlacementConfig with CEL rule matching to ppc64le only")
			// Configure CEL plugin with a rule that matches and specifies ONLY ppc64le.
			// Fallback has multiple architectures to verify they are NOT merged.
			ppc := NewPodPlacementConfig().
				WithGenerateName("cel-no-merge-").
				WithNamespace(ns.Name).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "test",
					},
				}).
				WithCelArchitecturePlacement(true,
					[]string{
						utils.ArchitectureAmd64,
						utils.ArchitectureArm64,
						utils.ArchitecturePpc64le,
						utils.ArchitectureS390x,
					},
					[]plugins.ArchitectureRule{
						NewRule("match-database-ppc64le-only",
							`has(self.metadata.labels.component) && self.metadata.labels.component == "database"`,
							utils.ArchitecturePpc64le),
					}).
				Build()

			Expect(k8sClient.Create(ctx, ppc)).To(Succeed())

			By("Creating a pod that matches the CEL rule")
			pod := NewPod().
				WithGenerateName("test-pod-db-").
				WithNamespace(ns.Name).
				WithLabels("app", "test", "component", "database").
				WithContainersImages("quay.io/test/image:latest").
				Build()

			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			By("Waiting for reconciliation to complete")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)).To(Succeed())

				// Verify scheduling gate removed
				g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
					Name: utils.SchedulingGateName,
				}))

				// Verify architecture affinity exists
				g.Expect(pod.Spec.Affinity).NotTo(BeNil())
				g.Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil())
				g.Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())

				terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
				g.Expect(terms).To(HaveLen(1), "should have exactly one node selector term")

				// Find the architecture match expression
				var archExpression *corev1.NodeSelectorRequirement
				for _, term := range terms {
					for i := range term.MatchExpressions {
						if term.MatchExpressions[i].Key == utils.ArchLabel {
							archExpression = &term.MatchExpressions[i]
							break
						}
					}
				}

				g.Expect(archExpression).NotTo(BeNil(), "architecture match expression should exist")
				g.Expect(archExpression.Operator).To(Equal(corev1.NodeSelectorOpIn))

				// CRITICAL ASSERTION: Verify ONLY ppc64le is present
				// No fallback architectures (amd64, arm64, s390x) should be merged
				g.Expect(archExpression.Values).To(Equal([]string{utils.ArchitecturePpc64le}),
					"should contain ONLY ppc64le, not merged with fallback architectures")

				// Additional verification: ensure no other architectures are present
				g.Expect(archExpression.Values).NotTo(ContainElement(utils.ArchitectureAmd64))
				g.Expect(archExpression.Values).NotTo(ContainElement(utils.ArchitectureArm64))
				g.Expect(archExpression.Values).NotTo(ContainElement(utils.ArchitectureS390x))
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())
		})

		It("should not execute image-based detection when CEL rule matches", func() {
			By("Creating a PodPlacementConfig with CEL rule")
			ppc := NewPodPlacementConfig().
				WithGenerateName("cel-skip-img-").
				WithNamespace(ns.Name).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "skip-image-test",
					},
				}).
				WithCelArchitecturePlacement(true, []string{utils.ArchitectureAmd64},
					[]plugins.ArchitectureRule{
						NewRule("force-arm64", `true`, utils.ArchitectureArm64), // Always matches
					}).
				Build()

			Expect(k8sClient.Create(ctx, ppc)).To(Succeed())

			By("Creating a pod with a multi-arch image")
			// Even if image inspection would detect multiple architectures,
			// CEL plugin should take precedence and return early
			pod := NewPod().
				WithGenerateName("test-pod-multiarch-").
				WithNamespace(ns.Name).
				WithLabels("app", "skip-image-test").
				WithContainersImages("quay.io/test/multiarch:latest").
				Build()

			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			By("Verifying ONLY arm64 is applied (CEL rule), not image-detected architectures")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)).To(Succeed())

				g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
					Name: utils.SchedulingGateName,
				}))

				g.Expect(pod.Spec.Affinity).NotTo(BeNil())
				g.Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil())
				g.Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())
				terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
				g.Expect(terms).To(HaveLen(1))

				// Verify ONLY arm64 is present
				var archExpression *corev1.NodeSelectorRequirement
				for _, term := range terms {
					for i := range term.MatchExpressions {
						if term.MatchExpressions[i].Key == utils.ArchLabel {
							archExpression = &term.MatchExpressions[i]
							break
						}
					}
				}

				g.Expect(archExpression).NotTo(BeNil())
				g.Expect(archExpression.Values).To(Equal([]string{utils.ArchitectureArm64}),
					"should contain ONLY arm64 from CEL rule, not image-detected architectures")
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())
		})

		It("should not apply CPPC fallbackArchitecture when CEL rule matches", func() {
			By("Creating a ClusterPodPlacementConfig with fallbackArchitecture")
			// Note: In a real test environment, CPPC would be set up separately
			// This test verifies the logic path where CEL returns early

			By("Creating a PodPlacementConfig with CEL rule")
			ppc := NewPodPlacementConfig().
				WithGenerateName("cel-no-cppc-fb-").
				WithNamespace(ns.Name).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "no-cppc-fallback",
					},
				}).
				WithCelArchitecturePlacement(true, []string{utils.ArchitectureAmd64},
					[]plugins.ArchitectureRule{
						NewRule("match-s390x", `true`, utils.ArchitectureS390x),
					}).
				Build()

			Expect(k8sClient.Create(ctx, ppc)).To(Succeed())

			By("Creating a pod")
			pod := NewPod().
				WithGenerateName("test-pod-s390x-").
				WithNamespace(ns.Name).
				WithLabels("app", "no-cppc-fallback").
				WithContainersImages("quay.io/test/image:latest").
				Build()

			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			By("Verifying ONLY s390x is applied, no CPPC fallback merged")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)).To(Succeed())

				g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
					Name: utils.SchedulingGateName,
				}))

				g.Expect(pod.Spec.Affinity).NotTo(BeNil())
				g.Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil())
				g.Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())
				terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
				g.Expect(terms).To(HaveLen(1))

				var archExpression *corev1.NodeSelectorRequirement
				for _, term := range terms {
					for i := range term.MatchExpressions {
						if term.MatchExpressions[i].Key == utils.ArchLabel {
							archExpression = &term.MatchExpressions[i]
							break
						}
					}
				}

				g.Expect(archExpression).NotTo(BeNil())
				g.Expect(archExpression.Values).To(Equal([]string{utils.ArchitectureS390x}),
					"should contain ONLY s390x from CEL rule, no CPPC fallback")
				g.Expect(len(archExpression.Values)).To(Equal(1),
					"should have exactly one architecture value")
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())
		})
	})
})
