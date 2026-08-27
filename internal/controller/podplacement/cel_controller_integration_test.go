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

var _ = Describe("CEL Architecture Placement Controller Integration", func() {
	// timeout / interval are shared constants; they carry no mutable state.
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

	Context("Full Reconciliation Flow", func() {
		It("should remove existing architecture constraints and apply new ones based on CEL rule", func() {
			By("Creating a PodPlacementConfig with CEL rule")
			ppc := NewPodPlacementConfig().
				WithGenerateName("cel-config-").
				WithNamespace(ns.Name).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "test",
					},
				}).
				WithCelArchitecturePlacement(true, []string{utils.ArchitectureAmd64},
					[]plugins.ArchitectureRule{
						NewRule("match-database",
							`has(self.metadata.labels.component) && self.metadata.labels.component == "database"`,
							utils.ArchitecturePpc64le),
					}).
				Build()

			Expect(k8sClient.Create(ctx, ppc)).To(Succeed())

			By("Creating a pod with existing architecture constraint that matches the CEL rule")
			pod := NewPod().
				WithGenerateName("test-pod-").
				WithNamespace(ns.Name).
				WithLabels("app", "test", "component", "database").
				WithNodeSelectors(utils.ArchLabel, utils.ArchitectureAmd64). // Existing constraint
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
				WithGenerateName("cel-preserve-").
				WithNamespace(ns.Name).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "preserve-test",
					},
				}).
				WithCelArchitecturePlacement(true, []string{utils.ArchitectureArm64},
					[]plugins.ArchitectureRule{
						NewRule("match-all", `true`, utils.ArchitectureAmd64),
					}).
				Build()

			Expect(k8sClient.Create(ctx, ppc)).To(Succeed())

			By("Creating a pod with zone affinity and architecture constraint")
			pod := NewPod().
				WithGenerateName("test-pod-preserve-").
				WithNamespace(ns.Name).
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

			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			By("Waiting for reconciliation and verifying zone constraint preserved")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)).To(Succeed())

				g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
					Name: utils.SchedulingGateName,
				}))

				// applyArchitectureConstraints updates in-place: arch is replaced within the
				// original single term, so zone and the new arch coexist in the same term.
				// Guard nil affinity explicitly so gomega records an assertion failure
				// (not a panic) when arch was not yet applied.
				g.Expect(pod.Spec.Affinity).NotTo(BeNil())
				g.Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil())
				g.Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())
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
				WithGenerateName("cel-fallback-").
				WithNamespace(ns.Name).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "fallback-test",
					},
				}).
				WithCelArchitecturePlacement(true, []string{utils.ArchitecturePpc64le, utils.ArchitectureAmd64},
					[]plugins.ArchitectureRule{
						NewRule("match-nothing",
							`"never-matches" in self.metadata.labels && self.metadata.labels["never-matches"] == "true"`,
							utils.ArchitectureArm64),
					}).
				Build()

			Expect(k8sClient.Create(ctx, ppc)).To(Succeed())

			By("Creating a pod that doesn't match any rules")
			pod := NewPod().
				WithGenerateName("test-pod-fallback-").
				WithNamespace(ns.Name).
				WithLabels("app", "fallback-test").
				WithNodeSelectors(utils.ArchLabel, utils.ArchitectureS390x). // Existing constraint
				WithContainersImages("quay.io/test/image:latest").
				Build()

			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			By("Waiting for reconciliation and verifying fallback architectures applied")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)).To(Succeed())

				g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
					Name: utils.SchedulingGateName,
				}))

				// Verify old nodeSelector constraint removed
				g.Expect(pod.Spec.NodeSelector).NotTo(HaveKey(utils.ArchLabel))

				// Verify fallback architectures applied
				g.Expect(pod.Spec.Affinity).NotTo(BeNil())
				g.Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil())
				g.Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())
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
				WithGenerateName("cel-precedence-").
				WithNamespace(ns.Name).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "precedence-test",
					},
				}).
				WithCelArchitecturePlacement(true, []string{utils.ArchitectureS390x},
					[]plugins.ArchitectureRule{
						NewRule("force-s390x", `true`, utils.ArchitectureS390x), // Always matches
					}).
				Build()

			Expect(k8sClient.Create(ctx, ppc)).To(Succeed())

			By("Creating a pod with a multi-arch image")
			// Image inspection would normally detect multiple architectures,
			// but CEL plugin should take precedence
			pod := NewPod().
				WithGenerateName("test-pod-precedence-").
				WithNamespace(ns.Name).
				WithLabels("app", "precedence-test").
				WithContainersImages("quay.io/test/multiarch:latest").
				Build()

			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			By("Waiting for reconciliation and verifying CEL plugin took precedence")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)).To(Succeed())

				g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
					Name: utils.SchedulingGateName,
				}))

				// Verify s390x architecture applied (from CEL, not image inspection)
				g.Expect(pod.Spec.Affinity).NotTo(BeNil())
				g.Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil())
				g.Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())
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
				WithGenerateName("cel-coexist-").
				WithNamespace(ns.Name).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "coexist-test",
					},
				}).
				WithNodeAffinityScoring(true).
				WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 100).
				WithNodeAffinityScoringTerm(utils.ArchitectureArm64, 50).
				WithCelArchitecturePlacement(true, []string{utils.ArchitectureAmd64, utils.ArchitectureArm64},
					[]plugins.ArchitectureRule{}).
				Build()

			Expect(k8sClient.Create(ctx, ppc)).To(Succeed())

			By("Creating a pod")
			pod := NewPod().
				WithGenerateName("test-pod-coexist-").
				WithNamespace(ns.Name).
				WithLabels("app", "coexist-test").
				WithContainersImages("quay.io/test/image:latest").
				Build()

			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			By("Waiting for reconciliation and verifying both plugins applied")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)).To(Succeed())

				g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
					Name: utils.SchedulingGateName,
				}))

				// Verify required affinity from CEL
				g.Expect(pod.Spec.Affinity).NotTo(BeNil())
				g.Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil())
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
				WithGenerateName("cel-low-priority-").
				WithNamespace(ns.Name).
				WithPriority(10).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "priority-test",
					},
				}).
				WithCelArchitecturePlacement(true, []string{utils.ArchitectureArm64},
					[]plugins.ArchitectureRule{
						NewRule("low-priority-rule", `true`, utils.ArchitectureArm64),
					}).
				Build()

			Expect(k8sClient.Create(ctx, ppcLow)).To(Succeed())

			By("Creating a high priority PodPlacementConfig")
			ppcHigh := NewPodPlacementConfig().
				WithGenerateName("cel-high-priority-").
				WithNamespace(ns.Name).
				WithPriority(100).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "priority-test",
					},
				}).
				WithCelArchitecturePlacement(true, []string{utils.ArchitecturePpc64le},
					[]plugins.ArchitectureRule{
						NewRule("high-priority-rule", `true`, utils.ArchitecturePpc64le),
					}).
				Build()

			Expect(k8sClient.Create(ctx, ppcHigh)).To(Succeed())

			// Wait until the high-priority PPC is visible to the API server before
			// creating the Pod. The webhook uses mgr.GetClient() (an informer-backed
			// cache). If the cache hasn't yet observed ppcHigh when the Pod is
			// admitted, it will see only ppcLow and apply arm64 instead of ppc64le,
			// permanently removing the scheduling gate with the wrong architecture.
			// Polling the direct k8sClient (non-cached) is sufficient: once the
			// object is stable in etcd the manager informer converges within one
			// resync cycle, well before the webhook fires for the pod below.
			By("Waiting until the high-priority PodPlacementConfig is observable")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crclient.ObjectKeyFromObject(ppcHigh), ppcHigh)).To(Succeed())
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())

			By("Creating a pod that matches both configs")
			pod := NewPod().
				WithGenerateName("test-pod-priority-").
				WithNamespace(ns.Name).
				WithLabels("app", "priority-test").
				WithContainersImages("quay.io/test/image:latest").
				Build()

			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			By("Waiting for reconciliation and verifying high priority config applied")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)).To(Succeed())

				g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
					Name: utils.SchedulingGateName,
				}))

				// Verify ppc64le architecture applied (from high priority config)
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
	})

	Context("Repeated Reconciliation Stability", func() {
		It("should remain stable across multiple reconciliations", func() {
			By("Creating a PodPlacementConfig")
			ppc := NewPodPlacementConfig().
				WithGenerateName("cel-stability-").
				WithNamespace(ns.Name).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "stability-test",
					},
				}).
				WithCelArchitecturePlacement(true, []string{utils.ArchitectureAmd64},
					[]plugins.ArchitectureRule{
						NewRule("stable-rule", `true`, utils.ArchitectureAmd64),
					}).
				Build()

			Expect(k8sClient.Create(ctx, ppc)).To(Succeed())

			By("Creating a pod")
			pod := NewPod().
				WithGenerateName("test-pod-stability-").
				WithNamespace(ns.Name).
				WithLabels("app", "stability-test").
				WithContainersImages("quay.io/test/image:latest").
				Build()

			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			By("Waiting for initial reconciliation - gate removed AND architecture affinity set")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)).To(Succeed())
				g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
					Name: utils.SchedulingGateName,
				}))
				// Also verify the CEL plugin set the architecture affinity before
				// we capture the initial state for the stability comparison below.
				g.Expect(pod.Spec.Affinity).NotTo(BeNil())
				g.Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil())
				g.Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())

			By("Capturing initial state")
			Expect(k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)).To(Succeed())

			initialTermCount := len(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms)
			initialResourceVersion := pod.ResourceVersion

			By("Triggering first reconciliation by updating pod labels")
			Expect(k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)).To(Succeed())
			pod.Labels["trigger-reconcile"] = "1"
			Expect(k8sClient.Update(ctx, pod)).To(Succeed())

			// Wait for the controller to observe and process the first label update
			// before issuing the second one, using Eventually instead of time.Sleep.
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)).To(Succeed())
				g.Expect(pod.Labels["trigger-reconcile"]).To(Equal("1"))
				// Affinity must still be intact after the first re-reconciliation.
				g.Expect(pod.Spec.Affinity).NotTo(BeNil())
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())

			By("Triggering second reconciliation by updating pod labels")
			Expect(k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)).To(Succeed())
			pod.Labels["trigger-reconcile"] = "2"
			Expect(k8sClient.Update(ctx, pod)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)).To(Succeed())
				g.Expect(pod.Labels["trigger-reconcile"]).To(Equal("2"))
				g.Expect(pod.Spec.Affinity).NotTo(BeNil())
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())

			By("Verifying pod state remains stable")
			Expect(k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)).To(Succeed())

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
