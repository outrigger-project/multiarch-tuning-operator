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

var _ = Describe("CEL Architecture Placement Controller Integration", func() {
	const (
		testNamespace = "cel-test-namespace"
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

	Context("Full Reconciliation Flow", func() {
		It("should remove existing architecture constraints and apply new ones based on CEL rule", func() {
			By("Creating a PodPlacementConfig with CEL rule")
			ppc := NewPodPlacementConfig().
				WithName("cel-test-config").
				WithNamespace(testNamespace).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "test",
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
						Name:          "match-database",
						Expression:    `has(self.metadata.labels.component) && self.metadata.labels.component == "database"`,
						Architectures: []string{utils.ArchitecturePpc64le},
					},
				},
			}

			err := k8sClient.Create(ctx, ppc)
			Expect(err).NotTo(HaveOccurred())

			By("Creating a pod with existing architecture constraint that matches the CEL rule")
			pod := NewPod().
				WithName("test-pod-database").
				WithNamespace(testNamespace).
				WithLabels("app", "test", "component", "database").
				WithNodeSelectors(utils.ArchLabel, utils.ArchitectureAmd64). // Existing constraint
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

				// Verify old nodeSelector constraint removed
				g.Expect(pod.Spec.NodeSelector).NotTo(HaveKey(utils.ArchLabel))

				// Verify new architecture affinity applied
				g.Expect(pod.Spec.Affinity).NotTo(BeNil())
				g.Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil())
				g.Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())

				terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
				g.Expect(terms).To(HaveLen(1))
				g.Expect(terms[0].MatchExpressions).To(ContainElement(corev1.NodeSelectorRequirement{
					Key:      utils.ArchLabel,
					Operator: corev1.NodeSelectorOpIn,
					Values:   []string{utils.ArchitecturePpc64le},
				}))
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())
		})

		It("should preserve non-architecture affinity constraints", func() {
			By("Creating a PodPlacementConfig with CEL rule")
			ppc := NewPodPlacementConfig().
				WithName("cel-preserve-config").
				WithNamespace(testNamespace).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "preserve-test",
					},
				}).
				WithPlugins().
				Build()

			ppc.Spec.Plugins.CelArchitecturePlacement = &plugins.CelArchitecturePlacement{
				BasePlugin: plugins.BasePlugin{
					Enabled: true,
				},
				FallbackArchitectures: []string{utils.ArchitectureArm64},
				Rules: []plugins.ArchitectureRule{
					{
						Name:          "match-all",
						Expression:    `true`,
						Architectures: []string{utils.ArchitectureAmd64},
					},
				},
			}

			err := k8sClient.Create(ctx, ppc)
			Expect(err).NotTo(HaveOccurred())

			By("Creating a pod with zone affinity and architecture constraint")
			pod := NewPod().
				WithName("test-pod-preserve").
				WithNamespace(testNamespace).
				WithLabels("app", "preserve-test").
				WithNodeSelectorTermsMatchExpressions(
					[]corev1.NodeSelectorRequirement{
						{
							Key:      utils.ArchLabel,
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{utils.ArchitecturePpc64le},
						},
						{
							Key:      "topology.kubernetes.io/zone",
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{"us-east-1a"},
						},
					},
				).
				WithContainersImages("quay.io/test/image:latest").
				Build()

			err = k8sClient.Create(ctx, pod)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for reconciliation and verifying zone constraint preserved")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
				g.Expect(err).NotTo(HaveOccurred())

				g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
					Name: utils.SchedulingGateName,
				}))

				// applyArchitectureConstraints updates in-place: arch is replaced within the
				// original single term, so zone and the new arch coexist in the same term.
				terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
				g.Expect(terms).To(HaveLen(1))

				// Verify zone constraint is preserved in the (only) term
				var foundZone bool
				var foundArch bool
				for _, expr := range terms[0].MatchExpressions {
					switch expr.Key {
					case "topology.kubernetes.io/zone":
						foundZone = true
						g.Expect(expr.Values).To(ContainElement("us-east-1a"))
					case utils.ArchLabel:
						foundArch = true
						g.Expect(expr.Values).To(Equal([]string{utils.ArchitectureAmd64}))
					}
				}
				g.Expect(foundZone).To(BeTrue(), "zone constraint should be preserved in the merged term")
				g.Expect(foundArch).To(BeTrue(), "new architecture constraint should be applied in the merged term")
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())
		})
	})

	Context("Fallback Architecture Flow", func() {
		It("should apply fallback architectures when no CEL rules match", func() {
			By("Creating a PodPlacementConfig with fallback architectures")
			ppc := NewPodPlacementConfig().
				WithName("cel-fallback-config").
				WithNamespace(testNamespace).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "fallback-test",
					},
				}).
				WithPlugins().
				Build()

			ppc.Spec.Plugins.CelArchitecturePlacement = &plugins.CelArchitecturePlacement{
				BasePlugin: plugins.BasePlugin{
					Enabled: true,
				},
				FallbackArchitectures: []string{utils.ArchitecturePpc64le, utils.ArchitectureAmd64},
				Rules: []plugins.ArchitectureRule{
					{
						Name:          "match-nothing",
						Expression:    `has(self.metadata.labels["never-matches"]) && self.metadata.labels["never-matches"] == "true"`,
						Architectures: []string{utils.ArchitectureArm64},
					},
				},
			}

			err := k8sClient.Create(ctx, ppc)
			Expect(err).NotTo(HaveOccurred())

			By("Creating a pod that doesn't match any rules")
			pod := NewPod().
				WithName("test-pod-fallback").
				WithNamespace(testNamespace).
				WithLabels("app", "fallback-test").
				WithNodeSelectors(utils.ArchLabel, utils.ArchitectureS390x). // Existing constraint
				WithContainersImages("quay.io/test/image:latest").
				Build()

			err = k8sClient.Create(ctx, pod)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for reconciliation and verifying fallback architectures applied")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
				g.Expect(err).NotTo(HaveOccurred())

				g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
					Name: utils.SchedulingGateName,
				}))

				// Verify old nodeSelector constraint removed
				g.Expect(pod.Spec.NodeSelector).NotTo(HaveKey(utils.ArchLabel))

				// Verify fallback architectures applied
				terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
				g.Expect(terms).To(HaveLen(1))
				g.Expect(terms[0].MatchExpressions).To(ContainElement(corev1.NodeSelectorRequirement{
					Key:      utils.ArchLabel,
					Operator: corev1.NodeSelectorOpIn,
					Values:   []string{utils.ArchitecturePpc64le, utils.ArchitectureAmd64},
				}))
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())
		})
	})

	Context("Precedence Behavior", func() {
		It("should apply CEL plugin before image inspection", func() {
			By("Creating a PodPlacementConfig with CEL rule")
			ppc := NewPodPlacementConfig().
				WithName("cel-precedence-config").
				WithNamespace(testNamespace).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "precedence-test",
					},
				}).
				WithPlugins().
				Build()

			ppc.Spec.Plugins.CelArchitecturePlacement = &plugins.CelArchitecturePlacement{
				BasePlugin: plugins.BasePlugin{
					Enabled: true,
				},
				FallbackArchitectures: []string{utils.ArchitectureS390x},
				Rules: []plugins.ArchitectureRule{
					{
						Name:          "force-s390x",
						Expression:    `true`, // Always matches
						Architectures: []string{utils.ArchitectureS390x},
					},
				},
			}

			err := k8sClient.Create(ctx, ppc)
			Expect(err).NotTo(HaveOccurred())

			By("Creating a pod with a multi-arch image")
			// Image inspection would normally detect multiple architectures,
			// but CEL plugin should take precedence
			pod := NewPod().
				WithName("test-pod-precedence").
				WithNamespace(testNamespace).
				WithLabels("app", "precedence-test").
				WithContainersImages("quay.io/test/multiarch:latest").
				Build()

			err = k8sClient.Create(ctx, pod)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for reconciliation and verifying CEL plugin took precedence")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
				g.Expect(err).NotTo(HaveOccurred())

				g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
					Name: utils.SchedulingGateName,
				}))

				// Verify s390x architecture applied (from CEL, not image inspection)
				terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
				g.Expect(terms).To(HaveLen(1))
				g.Expect(terms[0].MatchExpressions).To(ContainElement(corev1.NodeSelectorRequirement{
					Key:      utils.ArchLabel,
					Operator: corev1.NodeSelectorOpIn,
					Values:   []string{utils.ArchitectureS390x},
				}))
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())
		})
	})

	Context("NodeAffinityScoring Coexistence", func() {
		It("should preserve preferred affinity from NodeAffinityScoring plugin", func() {
			By("Creating a PodPlacementConfig with both plugins")
			ppc := NewPodPlacementConfig().
				WithName("cel-coexist-config").
				WithNamespace(testNamespace).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "coexist-test",
					},
				}).
				WithPlugins().
				WithNodeAffinityScoring(true).
				WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 100).
				WithNodeAffinityScoringTerm(utils.ArchitectureArm64, 50).
				Build()

			ppc.Spec.Plugins.CelArchitecturePlacement = &plugins.CelArchitecturePlacement{
				BasePlugin: plugins.BasePlugin{
					Enabled: true,
				},
				FallbackArchitectures: []string{utils.ArchitectureAmd64, utils.ArchitectureArm64},
				Rules:                 []plugins.ArchitectureRule{},
			}

			err := k8sClient.Create(ctx, ppc)
			Expect(err).NotTo(HaveOccurred())

			By("Creating a pod")
			pod := NewPod().
				WithName("test-pod-coexist").
				WithNamespace(testNamespace).
				WithLabels("app", "coexist-test").
				WithContainersImages("quay.io/test/image:latest").
				Build()

			err = k8sClient.Create(ctx, pod)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for reconciliation and verifying both plugins applied")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
				g.Expect(err).NotTo(HaveOccurred())

				g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
					Name: utils.SchedulingGateName,
				}))

				// Verify required affinity from CEL
				g.Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())
				terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
				g.Expect(terms).NotTo(BeEmpty())

				// Verify preferred affinity from NodeAffinityScoring
				g.Expect(pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution).NotTo(BeEmpty())
				preferred := pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution
				g.Expect(preferred).To(HaveLen(2))
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())
		})
	})

	Context("Priority Ordering", func() {
		It("should evaluate highest priority PodPlacementConfig first", func() {
			By("Creating a low priority PodPlacementConfig")
			ppcLow := NewPodPlacementConfig().
				WithName("cel-low-priority").
				WithNamespace(testNamespace).
				WithPriority(10).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "priority-test",
					},
				}).
				WithPlugins().
				Build()

			ppcLow.Spec.Plugins.CelArchitecturePlacement = &plugins.CelArchitecturePlacement{
				BasePlugin: plugins.BasePlugin{
					Enabled: true,
				},
				FallbackArchitectures: []string{utils.ArchitectureArm64},
				Rules: []plugins.ArchitectureRule{
					{
						Name:          "low-priority-rule",
						Expression:    `true`,
						Architectures: []string{utils.ArchitectureArm64},
					},
				},
			}

			err := k8sClient.Create(ctx, ppcLow)
			Expect(err).NotTo(HaveOccurred())

			By("Creating a high priority PodPlacementConfig")
			ppcHigh := NewPodPlacementConfig().
				WithName("cel-high-priority").
				WithNamespace(testNamespace).
				WithPriority(100).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "priority-test",
					},
				}).
				WithPlugins().
				Build()

			ppcHigh.Spec.Plugins.CelArchitecturePlacement = &plugins.CelArchitecturePlacement{
				BasePlugin: plugins.BasePlugin{
					Enabled: true,
				},
				FallbackArchitectures: []string{utils.ArchitecturePpc64le},
				Rules: []plugins.ArchitectureRule{
					{
						Name:          "high-priority-rule",
						Expression:    `true`,
						Architectures: []string{utils.ArchitecturePpc64le},
					},
				},
			}

			err = k8sClient.Create(ctx, ppcHigh)
			Expect(err).NotTo(HaveOccurred())

			By("Creating a pod that matches both configs")
			pod := NewPod().
				WithName("test-pod-priority").
				WithNamespace(testNamespace).
				WithLabels("app", "priority-test").
				WithContainersImages("quay.io/test/image:latest").
				Build()

			err = k8sClient.Create(ctx, pod)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for reconciliation and verifying high priority config applied")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
				g.Expect(err).NotTo(HaveOccurred())

				g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
					Name: utils.SchedulingGateName,
				}))

				// Verify ppc64le architecture applied (from high priority config)
				terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
				g.Expect(terms).To(HaveLen(1))
				g.Expect(terms[0].MatchExpressions).To(ContainElement(corev1.NodeSelectorRequirement{
					Key:      utils.ArchLabel,
					Operator: corev1.NodeSelectorOpIn,
					Values:   []string{utils.ArchitecturePpc64le},
				}))
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())
		})
	})

	Context("Repeated Reconciliation Stability", func() {
		It("should remain stable across multiple reconciliations", func() {
			By("Creating a PodPlacementConfig")
			ppc := NewPodPlacementConfig().
				WithName("cel-stability-config").
				WithNamespace(testNamespace).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "stability-test",
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
						Name:          "stable-rule",
						Expression:    `true`,
						Architectures: []string{utils.ArchitectureAmd64},
					},
				},
			}

			err := k8sClient.Create(ctx, ppc)
			Expect(err).NotTo(HaveOccurred())

			By("Creating a pod")
			pod := NewPod().
				WithName("test-pod-stability").
				WithNamespace(testNamespace).
				WithLabels("app", "stability-test").
				WithContainersImages("quay.io/test/image:latest").
				Build()

			err = k8sClient.Create(ctx, pod)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for initial reconciliation")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
					Name: utils.SchedulingGateName,
				}))
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())

			By("Capturing initial state")
			err = k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
			Expect(err).NotTo(HaveOccurred())

			initialTermCount := len(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms)
			initialResourceVersion := pod.ResourceVersion

			By("Triggering additional reconciliations by updating pod labels")
			pod.Labels["trigger-reconcile"] = "1"
			err = k8sClient.Update(ctx, pod)
			Expect(err).NotTo(HaveOccurred())

			time.Sleep(time.Second) // Allow reconciliation

			pod.Labels["trigger-reconcile"] = "2"
			err = k8sClient.Update(ctx, pod)
			Expect(err).NotTo(HaveOccurred())

			time.Sleep(time.Second) // Allow reconciliation

			By("Verifying pod state remains stable")
			err = k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
			Expect(err).NotTo(HaveOccurred())

			finalTermCount := len(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms)
			Expect(finalTermCount).To(Equal(initialTermCount), "term count should not change")

			// Verify no architecture term accumulation
			archTermCount := 0
			for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
				for _, expr := range term.MatchExpressions {
					if expr.Key == utils.ArchLabel {
						archTermCount++
					}
				}
			}
			Expect(archTermCount).To(Equal(1), "should have exactly one architecture term")

			// ResourceVersion should have changed (pod was updated), but affinity should be stable
			Expect(pod.ResourceVersion).NotTo(Equal(initialResourceVersion), "resource version should change on updates")
		})
	})
})
