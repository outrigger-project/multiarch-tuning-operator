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

var _ = Describe("CEL Plugin - PPC64LE Default and WKC Prefix Tests", func() {
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

	Context("Default PPC64LE Behavior", func() {
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
})
