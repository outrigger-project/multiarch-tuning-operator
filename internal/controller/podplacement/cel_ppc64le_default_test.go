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

var _ = Describe("CEL Plugin - PPC64LE Default and WKC Prefix Tests", func() {
	const (
		testNamespace = "cel-ppc64le-test-namespace"
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

	Context("Default PPC64LE Behavior", func() {
		It("should default all pods to ppc64le without any special CEL rules", func() {
			By("Creating a PodPlacementConfig with ppc64le as fallback architecture and no rules")
			ppc := NewPodPlacementConfig().
				WithName("default-ppc64le-config").
				WithNamespace(testNamespace).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"managed": "true",
					},
				}).
				WithPlugins().
				Build()

			// Configure CEL plugin with ppc64le as fallback and NO rules
			// This means ALL pods matching the label selector will default to ppc64le
			ppc.Spec.Plugins.CelArchitecturePlacement = &plugins.CelArchitecturePlacement{
				BasePlugin: plugins.BasePlugin{
					Enabled: true,
				},
				FallbackArchitectures: []string{utils.ArchitecturePpc64le},
				Rules:                 []plugins.ArchitectureRule{}, // No rules - all pods use fallback
			}

			err := k8sClient.Create(ctx, ppc)
			Expect(err).NotTo(HaveOccurred())

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
					WithNamespace(testNamespace).
					WithLabels("managed", "true").
					WithContainersImages("quay.io/test/image:latest").
					Build()

				// Add extra labels
				for k, v := range testPod.extraLabels {
					pod.Labels[k] = v
				}

				err = k8sClient.Create(ctx, pod)
				Expect(err).NotTo(HaveOccurred())

				By("Verifying pod " + testPod.name + " defaults to ppc64le")
				Eventually(func(g Gomega) {
					err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
					g.Expect(err).NotTo(HaveOccurred())

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
				WithName("override-ppc64le-config").
				WithNamespace(testNamespace).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"managed": "true",
					},
				}).
				WithPlugins().
				Build()

			ppc.Spec.Plugins.CelArchitecturePlacement = &plugins.CelArchitecturePlacement{
				BasePlugin: plugins.BasePlugin{
					Enabled: true,
				},
				FallbackArchitectures: []string{utils.ArchitecturePpc64le},
				Rules:                 []plugins.ArchitectureRule{},
			}

			err := k8sClient.Create(ctx, ppc)
			Expect(err).NotTo(HaveOccurred())

			By("Creating a pod with existing amd64 constraint")
			pod := NewPod().
				WithName("pod-with-amd64").
				WithNamespace(testNamespace).
				WithLabels("managed", "true").
				WithNodeSelectors(utils.ArchLabel, utils.ArchitectureAmd64). // Existing amd64 constraint
				WithContainersImages("quay.io/test/image:latest").
				Build()

			err = k8sClient.Create(ctx, pod)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying existing amd64 constraint is replaced with ppc64le")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
				g.Expect(err).NotTo(HaveOccurred())

				// Verify old nodeSelector constraint removed
				g.Expect(pod.Spec.NodeSelector).NotTo(HaveKey(utils.ArchLabel))

				// Verify ppc64le architecture constraint applied
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
			ppc := NewPodPlacementConfig().
				WithName("wkc-prefix-config").
				WithNamespace(testNamespace).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"managed": "true",
					},
				}).
				WithPlugins().
				Build()

			// Configure CEL plugin with rule to match wkc- prefix in pod name
			ppc.Spec.Plugins.CelArchitecturePlacement = &plugins.CelArchitecturePlacement{
				BasePlugin: plugins.BasePlugin{
					Enabled: true,
				},
				FallbackArchitectures: []string{utils.ArchitecturePpc64le}, // Default to ppc64le
				Rules: []plugins.ArchitectureRule{
					{
						Name: "wkc-prefix-rule",
						// CEL expression to check if pod name starts with "wkc-"
						Expression:    `self.metadata.name.startsWith("wkc-")`,
						Architectures: []string{utils.ArchitectureAmd64}, // Pin wkc- pods to amd64
					},
				},
			}

			err := k8sClient.Create(ctx, ppc)
			Expect(err).NotTo(HaveOccurred())

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
					WithNamespace(testNamespace).
					WithLabels("managed", "true").
					WithContainersImages("quay.io/test/image:latest").
					Build()

				err = k8sClient.Create(ctx, pod)
				Expect(err).NotTo(HaveOccurred())

				By("Verifying " + podName + " is pinned to amd64")
				Eventually(func(g Gomega) {
					err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
					g.Expect(err).NotTo(HaveOccurred())

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
					WithNamespace(testNamespace).
					WithLabels("managed", "true").
					WithContainersImages("quay.io/test/image:latest").
					Build()

				err = k8sClient.Create(ctx, pod)
				Expect(err).NotTo(HaveOccurred())

				By("Verifying " + podName + " defaults to ppc64le")
				Eventually(func(g Gomega) {
					err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
					g.Expect(err).NotTo(HaveOccurred())

					// Verify scheduling gate removed
					g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
						Name: utils.SchedulingGateName,
					}))

					// Verify ppc64le architecture constraint applied (from fallback)
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
			ppc := NewPodPlacementConfig().
				WithName("multi-prefix-config").
				WithNamespace(testNamespace).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"managed": "true",
					},
				}).
				WithPlugins().
				Build()

			// Configure CEL plugin with multiple rules - first match wins
			ppc.Spec.Plugins.CelArchitecturePlacement = &plugins.CelArchitecturePlacement{
				BasePlugin: plugins.BasePlugin{
					Enabled: true,
				},
				FallbackArchitectures: []string{utils.ArchitecturePpc64le},
				Rules: []plugins.ArchitectureRule{
					{
						Name:          "wkc-prefix-rule",
						Expression:    `self.metadata.name.startsWith("wkc-")`,
						Architectures: []string{utils.ArchitectureAmd64},
					},
					{
						Name:          "db-prefix-rule",
						Expression:    `self.metadata.name.startsWith("db-")`,
						Architectures: []string{utils.ArchitectureArm64},
					},
					{
						Name:          "cache-prefix-rule",
						Expression:    `self.metadata.name.startsWith("cache-")`,
						Architectures: []string{utils.ArchitectureS390x},
					},
				},
			}

			err := k8sClient.Create(ctx, ppc)
			Expect(err).NotTo(HaveOccurred())

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
					WithNamespace(testNamespace).
					WithLabels("managed", "true").
					WithContainersImages("quay.io/test/image:latest").
					Build()

				err = k8sClient.Create(ctx, pod)
				Expect(err).NotTo(HaveOccurred())

				By("Verifying " + tc.podName + " is pinned to " + tc.expectedArchitecture)
				Eventually(func(g Gomega) {
					err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
					g.Expect(err).NotTo(HaveOccurred())

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
			ppc := NewPodPlacementConfig().
				WithName("wkc-label-config").
				WithNamespace(testNamespace).
				WithLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"managed": "true",
					},
				}).
				WithPlugins().
				Build()

			// Configure CEL plugin to match based on metadata labels
			ppc.Spec.Plugins.CelArchitecturePlacement = &plugins.CelArchitecturePlacement{
				BasePlugin: plugins.BasePlugin{
					Enabled: true,
				},
				FallbackArchitectures: []string{utils.ArchitecturePpc64le},
				Rules: []plugins.ArchitectureRule{
					{
						Name: "wkc-component-label-rule",
						// Match pods with label "wkc-component=true"
						Expression:    `self.metadata.labels.exists(l, l.key == "wkc-component" && l.value == "true")`,
						Architectures: []string{utils.ArchitectureAmd64},
					},
				},
			}

			err := k8sClient.Create(ctx, ppc)
			Expect(err).NotTo(HaveOccurred())

			By("Creating pod with wkc-component label")
			podWithLabel := NewPod().
				WithName("service-with-wkc-label").
				WithNamespace(testNamespace).
				WithLabels("managed", "true", "wkc-component", "true").
				WithContainersImages("quay.io/test/image:latest").
				Build()

			err = k8sClient.Create(ctx, podWithLabel)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying pod with wkc-component label is pinned to amd64")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(podWithLabel), podWithLabel)
				g.Expect(err).NotTo(HaveOccurred())

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
				WithName("service-without-wkc-label").
				WithNamespace(testNamespace).
				WithLabels("managed", "true").
				WithContainersImages("quay.io/test/image:latest").
				Build()

			err = k8sClient.Create(ctx, podWithoutLabel)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying pod without wkc-component label defaults to ppc64le")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(podWithoutLabel), podWithoutLabel)
				g.Expect(err).NotTo(HaveOccurred())

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
