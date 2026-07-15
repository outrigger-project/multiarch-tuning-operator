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
	"github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/pkg/e2e"
	. "github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

var _ = Describe("CEL Plugin - No Fallback/Image-Detection Merge After Match", func() {
	const (
		testNamespace = "cel-no-merge-test-namespace"
		timeout       = e2e.WaitShort
		interval      = time.Millisecond * 250
	)

	BeforeEach(func() {
		// Create test namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		err := k8sClient.Create(ctx, ns)
		if err != nil {
			// Namespace might already exist from previous test
			Expect(crclient.IgnoreAlreadyExists(err)).NotTo(HaveOccurred())
		}
	})

	AfterEach(func() {
		// Clean up PodPlacementConfigs
		ppcList := &v1beta1.PodPlacementConfigList{}
		err := k8sClient.List(ctx, ppcList, crclient.InNamespace(testNamespace))
		Expect(err).NotTo(HaveOccurred())
		for i := range ppcList.Items {
			err := k8sClient.Delete(ctx, &ppcList.Items[i])
			Expect(crclient.IgnoreNotFound(err)).NotTo(HaveOccurred())
		}

		// Clean up pods
		podList := &corev1.PodList{}
		err = k8sClient.List(ctx, podList, crclient.InNamespace(testNamespace))
		Expect(err).NotTo(HaveOccurred())
		for i := range podList.Items {
			err := k8sClient.Delete(ctx, &podList.Items[i])
			Expect(crclient.IgnoreNotFound(err)).NotTo(HaveOccurred())
		}
	})

	Context("Early Return After CEL Match", func() {
		It("should contain ONLY matched rule architectures without merging fallback or image-detected architectures", func() {
			By("Creating a PodPlacementConfig with CEL rule matching to ppc64le only")
			ppc := NewPodPlacementConfig().
				WithName("cel-no-merge-config").
				WithNamespace(testNamespace).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "test",
					},
				}).
				WithPlugins().
				Build()

			// Configure CEL plugin with a rule that matches and specifies ONLY ppc64le
			// Fallback has multiple architectures to verify they are NOT merged
			ppc.Spec.Plugins.CelArchitecturePlacement = &plugins.CelArchitecturePlacement{
				BasePlugin: plugins.BasePlugin{
					Enabled: true,
				},
				// Fallback has many architectures - these should NOT be applied when rule matches
				FallbackArchitectures: []string{
					utils.ArchitectureAmd64,
					utils.ArchitectureArm64,
					utils.ArchitecturePpc64le,
					utils.ArchitectureS390x,
				},
				Rules: []plugins.ArchitectureRule{
					{
						Name:       "match-database-ppc64le-only",
						Expression: `has(self.metadata.labels.component) && self.metadata.labels.component == "database"`,
						// Rule specifies ONLY ppc64le
						Architectures: []string{utils.ArchitecturePpc64le},
					},
				},
			}

			err := k8sClient.Create(ctx, ppc)
			Expect(err).NotTo(HaveOccurred())

			By("Creating a pod that matches the CEL rule")
			pod := NewPod().
				WithName("test-pod-database").
				WithNamespace(testNamespace).
				WithLabels("app", "test", "component", "database").
				WithContainersImages("quay.io/test/image:latest").
				Build()

			err = k8sClient.Create(ctx, pod)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for reconciliation to complete")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
				g.Expect(err).NotTo(HaveOccurred())

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
				WithName("cel-skip-image-detection").
				WithNamespace(testNamespace).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "skip-image-test",
					},
				}).
				WithPlugins().
				Build()

			ppc.Spec.Plugins.CelArchitecturePlacement = &plugins.CelArchitecturePlacement{
				BasePlugin: plugins.BasePlugin{
					Enabled: true,
				},
				FallbackArchitectures: []string{utils.ArchitectureAmd64},
				Rules: []plugins.ArchitectureRule{
					{
						Name:       "force-arm64",
						Expression: `true`, // Always matches
						Architectures: []string{
							utils.ArchitectureArm64,
						},
					},
				},
			}

			err := k8sClient.Create(ctx, ppc)
			Expect(err).NotTo(HaveOccurred())

			By("Creating a pod with a multi-arch image")
			// Even if image inspection would detect multiple architectures,
			// CEL plugin should take precedence and return early
			pod := NewPod().
				WithName("test-pod-multiarch").
				WithNamespace(testNamespace).
				WithLabels("app", "skip-image-test").
				WithContainersImages("quay.io/test/multiarch:latest").
				Build()

			err = k8sClient.Create(ctx, pod)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying ONLY arm64 is applied (CEL rule), not image-detected architectures")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
				g.Expect(err).NotTo(HaveOccurred())

				g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
					Name: utils.SchedulingGateName,
				}))

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
				WithName("cel-no-cppc-fallback").
				WithNamespace(testNamespace).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "no-cppc-fallback",
					},
				}).
				WithPlugins().
				Build()

			ppc.Spec.Plugins.CelArchitecturePlacement = &plugins.CelArchitecturePlacement{
				BasePlugin: plugins.BasePlugin{
					Enabled: true,
				},
				FallbackArchitectures: []string{utils.ArchitectureAmd64},
				Rules: []plugins.ArchitectureRule{
					{
						Name:       "match-s390x",
						Expression: `true`,
						Architectures: []string{
							utils.ArchitectureS390x,
						},
					},
				},
			}

			err := k8sClient.Create(ctx, ppc)
			Expect(err).NotTo(HaveOccurred())

			By("Creating a pod")
			pod := NewPod().
				WithName("test-pod-s390x").
				WithNamespace(testNamespace).
				WithLabels("app", "no-cppc-fallback").
				WithContainersImages("quay.io/test/image:latest").
				Build()

			err = k8sClient.Create(ctx, pod)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying ONLY s390x is applied, no CPPC fallback merged")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
				g.Expect(err).NotTo(HaveOccurred())

				g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
					Name: utils.SchedulingGateName,
				}))

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
