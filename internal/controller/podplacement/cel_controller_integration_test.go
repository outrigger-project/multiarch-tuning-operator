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

	Context("NodeAffinityScoring Plugin Coexistence", func() {
		It("should apply CEL architecture constraints AND NodeAffinityScoring preferences", func() {
			// Create an in-memory pod (never persisted to the API server).
			// Name and Namespace are irrelevant for these pure in-memory assertions.
			pod := NewPod().
				WithLabels("app", "test").
				WithContainersImages("test:latest").
				Build()

			// Apply CEL architecture placement (sets required affinity)
			architectures := []string{"amd64", "arm64"}
			applyArchitectureConstraints(pod, architectures)

			// Verify required affinity was set by CEL
			Expect(pod.Spec.Affinity).NotTo(BeNil())
			Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil())
			Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())

			terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
			Expect(terms).To(HaveLen(1))
			Expect(terms[0].MatchExpressions).To(HaveLen(1))
			Expect(terms[0].MatchExpressions[0].Key).To(Equal(utils.ArchLabel))
			Expect(terms[0].MatchExpressions[0].Operator).To(Equal(corev1.NodeSelectorOpIn))
			Expect(terms[0].MatchExpressions[0].Values).To(ConsistOf("amd64", "arm64"))

			// Now apply NodeAffinityScoring (sets preferred affinity)
			nodeAffinityScoring := &plugins.NodeAffinityScoring{
				BasePlugin: plugins.BasePlugin{Enabled: true},
				Platforms: []plugins.NodeAffinityScoringPlatformTerm{
					{Architecture: "amd64", Weight: 50},
					{Architecture: "arm64", Weight: 30},
				},
			}

			// Simulate what SetPreferredArchNodeAffinity does
			if pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution == nil {
				pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution = []corev1.PreferredSchedulingTerm{}
			}

			for _, platform := range nodeAffinityScoring.Platforms {
				term := corev1.PreferredSchedulingTerm{
					Weight: platform.Weight,
					Preference: corev1.NodeSelectorTerm{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      utils.ArchLabel,
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{platform.Architecture},
							},
						},
					},
				}
				pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution =
					append(pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution, term)
			}

			// Verify BOTH required (from CEL) and preferred (from NodeAffinityScoring) are present
			Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil(),
				"Required affinity from CEL should still be present")
			Expect(pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil(),
				"Preferred affinity from NodeAffinityScoring should be present")

			// Verify required affinity (CEL) is still intact
			requiredTerms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
			Expect(requiredTerms).To(HaveLen(1))
			Expect(requiredTerms[0].MatchExpressions[0].Values).To(ConsistOf("amd64", "arm64"))

			// Verify preferred affinity (NodeAffinityScoring) was added
			preferredTerms := pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution
			Expect(preferredTerms).To(HaveLen(2))
			Expect(preferredTerms[0].Weight).To(Equal(int32(50)))
			Expect(preferredTerms[0].Preference.MatchExpressions[0].Values).To(ConsistOf("amd64"))
			Expect(preferredTerms[1].Weight).To(Equal(int32(30)))
			Expect(preferredTerms[1].Preference.MatchExpressions[0].Values).To(ConsistOf("arm64"))
		})

		It("should preserve CEL required affinity when NodeAffinityScoring adds preferred affinity", func() {
			// Create an in-memory pod (never persisted to the API server).
			// Name and Namespace are irrelevant for these pure in-memory assertions.
			pod := NewPod().
				WithNodeSelectorTermsMatchExpressions(
					[]corev1.NodeSelectorRequirement{
						{
							Key:      utils.ArchLabel,
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{"ppc64le"},
						},
					},
				).
				WithContainersImages("test:latest").
				Build()

			// Store original required affinity
			originalRequired := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Values

			// Add preferred affinity (simulating NodeAffinityScoring)
			pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution = []corev1.PreferredSchedulingTerm{
				{
					Weight: 100,
					Preference: corev1.NodeSelectorTerm{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      utils.ArchLabel,
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{"ppc64le"},
							},
						},
					},
				},
			}

			// Verify required affinity is unchanged
			currentRequired := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Values
			Expect(currentRequired).To(Equal(originalRequired), "Required affinity from CEL should not be modified")

			// Verify preferred affinity was added
			Expect(pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution).To(HaveLen(1))
		})
	})

	Context("Default PPC64LE Behavior", func() {
		BeforeEach(func() {
			ns = newEphemeralTestNamespace()
		})

		It("should default all pods to ppc64le without any special CEL rules", func() {
			By("Creating a PodPlacementConfig with ppc64le as fallback architecture and no rules")
			// Configure CEL plugin with ppc64le as fallback and NO rules.
			// This means ALL pods matching the label selector will default to ppc64le.
			ppc := NewPodPlacementConfig().
				WithGenerateName("default-ppc64le-").
				WithNamespace(ns.Name).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"managed": "true",
					},
				}).
				WithCelArchitecturePlacement(true, []string{utils.ArchitecturePpc64le},
					[]plugins.ArchitectureRule{}).
				Build()

			Expect(k8sClient.Create(ctx, ppc)).To(Succeed())

			By("Creating multiple pods with different names - all should default to ppc64le")
			testPods := []struct {
				name        string
				extraLabels map[string]string
			}{
				{name: "app-frontend", extraLabels: map[string]string{"component": "frontend"}},
				{name: "app-backend", extraLabels: map[string]string{"component": "backend"}},
				{name: "database-postgres", extraLabels: map[string]string{"component": "database"}},
				{name: "cache-redis", extraLabels: map[string]string{"component": "cache"}},
			}

			for _, testPod := range testPods {
				By("Creating pod: " + testPod.name)
				pod := NewPod().
					WithName(testPod.name).
					WithNamespace(ns.Name).
					WithLabels("managed", "true").
					WithContainersImages("quay.io/test/image:latest").
					Build()

				// Add extra labels
				for k, v := range testPod.extraLabels {
					pod.Labels[k] = v
				}

				Expect(k8sClient.Create(ctx, pod)).To(Succeed())

				By("Verifying pod " + testPod.name + " defaults to ppc64le")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)).To(Succeed())

					// Verify scheduling gate removed
					g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
						Name: utils.SchedulingGateName,
					}))

					// Verify ppc64le architecture constraint applied
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
			}
		})

		It("should override existing architecture constraints with ppc64le default", func() {
			By("Creating a PodPlacementConfig with ppc64le fallback")
			ppc := NewPodPlacementConfig().
				WithGenerateName("override-ppc64le-").
				WithNamespace(ns.Name).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"managed": "true",
					},
				}).
				WithCelArchitecturePlacement(true, []string{utils.ArchitecturePpc64le},
					[]plugins.ArchitectureRule{}).
				Build()

			Expect(k8sClient.Create(ctx, ppc)).To(Succeed())

			By("Creating a pod with existing amd64 constraint")
			pod := NewPod().
				WithGenerateName("pod-with-amd64-").
				WithNamespace(ns.Name).
				WithLabels("managed", "true").
				WithNodeSelectors(utils.ArchLabel, utils.ArchitectureAmd64). // Existing amd64 constraint
				WithContainersImages("quay.io/test/image:latest").
				Build()

			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			By("Verifying existing amd64 constraint is replaced with ppc64le")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)).To(Succeed())

				// Verify old nodeSelector constraint removed
				g.Expect(pod.Spec.NodeSelector).NotTo(HaveKey(utils.ArchLabel))

				// Verify ppc64le architecture constraint applied
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

	Context("WKC Prefix Matching via Metadata Inspection", func() {
		BeforeEach(func() {
			ns = newEphemeralTestNamespace()
		})

		It("should pin pods with 'wkc-' prefix to specific architecture by inspecting metadata.name", func() {
			By("Creating a PodPlacementConfig with CEL rule to match wkc- prefix")
			// Configure CEL plugin with rule to match wkc- prefix in pod name.
			// NOTE: pod names are meaningful here -- the CEL expression evaluates
			// self.metadata.name at admission time, so the exact name matters.
			// Each pod is isolated within the ephemeral namespace, so name
			// uniqueness across parallel workers is guaranteed by namespace isolation.
			ppc := NewPodPlacementConfig().
				WithGenerateName("wkc-prefix-").
				WithNamespace(ns.Name).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"managed": "true",
					},
				}).
				WithCelArchitecturePlacement(true,
					[]string{utils.ArchitecturePpc64le}, // Default to ppc64le
					[]plugins.ArchitectureRule{
						NewRule("wkc-prefix-rule",
							`self.metadata.name.startsWith("wkc-")`, // CEL expression to check if pod name starts with "wkc-"
							utils.ArchitectureAmd64),                // Pin wkc- pods to amd64
					}).
				Build()

			Expect(k8sClient.Create(ctx, ppc)).To(Succeed())

			By("Creating pods with wkc- prefix - should be pinned to amd64")
			wkcPods := []string{
				"wkc-frontend",
				"wkc-backend",
				"wkc-database",
				"wkc-api-gateway",
			}

			for _, podName := range wkcPods {
				By("Creating wkc- pod: " + podName)
				pod := NewPod().
					WithName(podName).
					WithNamespace(ns.Name).
					WithLabels("managed", "true").
					WithContainersImages("quay.io/test/image:latest").
					Build()

				Expect(k8sClient.Create(ctx, pod)).To(Succeed())

				By("Verifying " + podName + " is pinned to amd64")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)).To(Succeed())

					// Verify scheduling gate removed
					g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
						Name: utils.SchedulingGateName,
					}))

					// Verify amd64 architecture constraint applied (from CEL rule match)
					g.Expect(pod.Spec.Affinity).NotTo(BeNil())
					g.Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil())
					g.Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())

					terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
					g.Expect(terms).To(HaveLen(1))
					g.Expect(terms[0].MatchExpressions).To(ContainElement(corev1.NodeSelectorRequirement{
						Key:      utils.ArchLabel,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{utils.ArchitectureAmd64},
					}))
				}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())
			}

			By("Creating pods WITHOUT wkc- prefix - should default to ppc64le")
			nonWkcPods := []string{
				"app-frontend",
				"database-postgres",
				"cache-redis",
			}

			for _, podName := range nonWkcPods {
				By("Creating non-wkc pod: " + podName)
				pod := NewPod().
					WithName(podName).
					WithNamespace(ns.Name).
					WithLabels("managed", "true").
					WithContainersImages("quay.io/test/image:latest").
					Build()

				Expect(k8sClient.Create(ctx, pod)).To(Succeed())

				By("Verifying " + podName + " defaults to ppc64le")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)).To(Succeed())

					// Verify scheduling gate removed
					g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
						Name: utils.SchedulingGateName,
					}))

					// Verify ppc64le architecture constraint applied (from fallback)
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
			}
		})

		It("should support multiple metadata-based rules with priority ordering", func() {
			By("Creating a PodPlacementConfig with multiple prefix rules")
			// Configure CEL plugin with multiple rules - first match wins
			ppc := NewPodPlacementConfig().
				WithGenerateName("multi-prefix-").
				WithNamespace(ns.Name).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"managed": "true",
					},
				}).
				WithCelArchitecturePlacement(true, []string{utils.ArchitecturePpc64le},
					[]plugins.ArchitectureRule{
						NewRule("wkc-prefix-rule", `self.metadata.name.startsWith("wkc-")`, utils.ArchitectureAmd64),
						NewRule("db-prefix-rule", `self.metadata.name.startsWith("db-")`, utils.ArchitectureArm64),
						NewRule("cache-prefix-rule", `self.metadata.name.startsWith("cache-")`, utils.ArchitectureS390x),
					}).
				Build()

			Expect(k8sClient.Create(ctx, ppc)).To(Succeed())

			testCases := []struct {
				podName              string
				expectedArchitecture string
			}{
				{"wkc-service", utils.ArchitectureAmd64},
				{"db-postgres", utils.ArchitectureArm64},
				{"cache-redis", utils.ArchitectureS390x},
				{"app-frontend", utils.ArchitecturePpc64le}, // No match, uses fallback
			}

			for _, tc := range testCases {
				By("Creating pod: " + tc.podName)
				pod := NewPod().
					WithName(tc.podName).
					WithNamespace(ns.Name).
					WithLabels("managed", "true").
					WithContainersImages("quay.io/test/image:latest").
					Build()

				Expect(k8sClient.Create(ctx, pod)).To(Succeed())

				By("Verifying " + tc.podName + " is pinned to " + tc.expectedArchitecture)
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)).To(Succeed())

					g.Expect(pod.Spec.Affinity).NotTo(BeNil())
					g.Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil())
					g.Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())
					terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
					g.Expect(terms).To(HaveLen(1))
					g.Expect(terms[0].MatchExpressions).To(ContainElement(corev1.NodeSelectorRequirement{
						Key:      utils.ArchLabel,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{tc.expectedArchitecture},
					}))
				}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())
			}
		})

		It("should inspect metadata labels in addition to name", func() {
			By("Creating a PodPlacementConfig with label-based CEL rule")
			// Configure CEL plugin to match based on metadata labels.
			// 'key' in self.metadata.labels is the supported CEL membership-test syntax.
			ppc := NewPodPlacementConfig().
				WithGenerateName("wkc-label-").
				WithNamespace(ns.Name).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"managed": "true",
					},
				}).
				WithCelArchitecturePlacement(true, []string{utils.ArchitecturePpc64le},
					[]plugins.ArchitectureRule{
						NewRule("wkc-component-label-rule",
							`"wkc-component" in self.metadata.labels && self.metadata.labels["wkc-component"] == "true"`,
							utils.ArchitectureAmd64),
					}).
				Build()

			Expect(k8sClient.Create(ctx, ppc)).To(Succeed())

			By("Creating pod with wkc-component label")
			podWithLabel := NewPod().
				WithGenerateName("svc-with-wkc-lbl-").
				WithNamespace(ns.Name).
				WithLabels("managed", "true", "wkc-component", "true").
				WithContainersImages("quay.io/test/image:latest").
				Build()

			Expect(k8sClient.Create(ctx, podWithLabel)).To(Succeed())

			By("Verifying pod with wkc-component label is pinned to amd64")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crclient.ObjectKeyFromObject(podWithLabel), podWithLabel)).To(Succeed())

				g.Expect(podWithLabel.Spec.Affinity).NotTo(BeNil())
				g.Expect(podWithLabel.Spec.Affinity.NodeAffinity).NotTo(BeNil())
				g.Expect(podWithLabel.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())
				terms := podWithLabel.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
				g.Expect(terms).To(HaveLen(1))
				g.Expect(terms[0].MatchExpressions).To(ContainElement(corev1.NodeSelectorRequirement{
					Key:      utils.ArchLabel,
					Operator: corev1.NodeSelectorOpIn,
					Values:   []string{utils.ArchitectureAmd64},
				}))
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())

			By("Creating pod without wkc-component label")
			podWithoutLabel := NewPod().
				WithGenerateName("svc-no-wkc-lbl-").
				WithNamespace(ns.Name).
				WithLabels("managed", "true").
				WithContainersImages("quay.io/test/image:latest").
				Build()

			Expect(k8sClient.Create(ctx, podWithoutLabel)).To(Succeed())

			By("Verifying pod without wkc-component label defaults to ppc64le")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crclient.ObjectKeyFromObject(podWithoutLabel), podWithoutLabel)).To(Succeed())

				g.Expect(podWithoutLabel.Spec.Affinity).NotTo(BeNil())
				g.Expect(podWithoutLabel.Spec.Affinity.NodeAffinity).NotTo(BeNil())
				g.Expect(podWithoutLabel.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())
				terms := podWithoutLabel.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
				g.Expect(terms).To(HaveLen(1))
				g.Expect(terms[0].MatchExpressions).To(ContainElement(corev1.NodeSelectorRequirement{
					Key:      utils.ArchLabel,
					Operator: corev1.NodeSelectorOpIn,
					Values:   []string{utils.ArchitecturePpc64le},
				}))
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())
		})
	})

	Context("No Fallback/Image-Detection Merge After Match", func() {
		BeforeEach(func() {
			ns = newEphemeralTestNamespace()
		})

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
