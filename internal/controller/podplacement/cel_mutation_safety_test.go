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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

var _ = Describe("CEL Mutation Safety", func() {

	Context("NodeSelector and NodeAffinity Cleanup", func() {

		// TestOnlyArchitectureRemovedFromNodeSelector
		It("should remove ONLY kubernetes.io/arch from nodeSelector", func() {
			pod := NewPod().WithName("test-pod").
				WithNodeSelectors(
					utils.ArchLabel, "amd64",
					"kubernetes.io/os", "linux",
					"node.kubernetes.io/instance-type", "m5.large",
					"topology.kubernetes.io/zone", "us-east-1a",
					"custom-label", "custom-value",
				).Build()

			removeArchitectureFromNodeSelector(pod)

			Expect(pod.Spec.NodeSelector).NotTo(HaveKey(utils.ArchLabel),
				"Architecture label should be removed")

			expectedLabels := map[string]string{
				"kubernetes.io/os":                 "linux",
				"node.kubernetes.io/instance-type": "m5.large",
				"topology.kubernetes.io/zone":      "us-east-1a",
				"custom-label":                     "custom-value",
			}
			for key, expectedValue := range expectedLabels {
				Expect(pod.Spec.NodeSelector).To(HaveKeyWithValue(key, expectedValue),
					"Label %s was removed or changed", key)
			}
		})

		// TestUnrelatedAffinityPreserved
		It("should preserve unrelated pod and pod-anti affinity when removing architecture from node affinity", func() {
			pod := NewPod().WithName("test-pod").
				WithAffinity(&corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      utils.ArchLabel,
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{"amd64"},
										},
										{
											Key:      "kubernetes.io/os",
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{"linux"},
										},
									},
								},
							},
						},
					},
					PodAffinity: &corev1.PodAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
							{
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{"app": "database"},
								},
								TopologyKey: "kubernetes.io/hostname",
							},
						},
					},
					PodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
							{
								Weight: 100,
								PodAffinityTerm: corev1.PodAffinityTerm{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{"app": "cache"},
									},
									TopologyKey: "kubernetes.io/hostname",
								},
							},
						},
					},
				}).Build()

			removeArchitectureFromNodeAffinity(pod)

			// Verify architecture was removed
			if pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
				for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
					for _, expr := range term.MatchExpressions {
						Expect(expr.Key).NotTo(Equal(utils.ArchLabel), "Architecture expression should be removed")
					}
				}
			}

			// Verify OS expression preserved
			osFound := false
			if pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
				for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
					for _, expr := range term.MatchExpressions {
						if expr.Key == "kubernetes.io/os" {
							osFound = true
							Expect(expr.Operator).To(Equal(corev1.NodeSelectorOpIn))
							Expect(expr.Values).To(ConsistOf("linux"))
						}
					}
				}
			}
			Expect(osFound).To(BeTrue(), "OS expression was removed but should be preserved")

			Expect(pod.Spec.Affinity.PodAffinity).NotTo(BeNil(), "Pod affinity was removed")
			Expect(pod.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution).To(HaveLen(1))
			Expect(pod.Spec.Affinity.PodAntiAffinity).NotTo(BeNil(), "Pod anti-affinity was removed")
			Expect(pod.Spec.Affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution).To(HaveLen(1))
		})

		// TestPreferredAffinityPreserved
		It("should preserve preferredDuringSchedulingIgnoredDuringExecution (including arch entries) when removing required arch", func() {
			pod := NewPod().WithName("test-pod").
				WithAffinity(&corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"amd64"}},
									},
								},
							},
						},
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
							{
								Weight: 50,
								Preference: corev1.NodeSelectorTerm{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{Key: "node-type", Operator: corev1.NodeSelectorOpIn, Values: []string{"compute"}},
									},
								},
							},
							{
								Weight: 30,
								Preference: corev1.NodeSelectorTerm{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"arm64"}},
									},
								},
							},
						},
					},
				}).Build()

			removeArchitectureFromNodeAffinity(pod)

			Expect(pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil(),
				"Preferred affinity was removed")
			preferred := pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution
			Expect(preferred).To(HaveLen(2))
			Expect(preferred[0].Weight).To(Equal(int32(50)), "First preferred term weight was modified")
			Expect(preferred[1].Weight).To(Equal(int32(30)), "Second preferred term weight was modified")

			archInPreferred := false
			for _, term := range preferred {
				for _, expr := range term.Preference.MatchExpressions {
					if expr.Key == utils.ArchLabel {
						archInPreferred = true
					}
				}
			}
			Expect(archInPreferred).To(BeTrue(), "Architecture in preferred affinity should be preserved")
		})

		// TestMatchFieldsPreserved
		It("should preserve MatchFields during architecture cleanup", func() {
			pod := NewPod().WithName("test-pod").
				WithAffinity(&corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"amd64"}},
									},
									MatchFields: []corev1.NodeSelectorRequirement{
										{Key: "metadata.name", Operator: corev1.NodeSelectorOpIn, Values: []string{"node-1", "node-2"}},
									},
								},
							},
						},
					},
				}).Build()

			removeArchitectureFromNodeAffinity(pod)

			Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil(),
				"Required affinity was removed")
			terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
			Expect(terms).To(HaveLen(1), "Expected 1 term (with MatchFields)")
			Expect(terms[0].MatchFields).To(HaveLen(1), "Expected 1 MatchField")
			Expect(terms[0].MatchFields[0].Key).To(Equal("metadata.name"), "MatchFields was modified")
		})

		// TestEmptySelectorTermsRemoved
		It("should remove empty selector terms after architecture cleanup", func() {
			pod := NewPod().WithName("test-pod").
				WithAffinity(&corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									// This term will become empty after arch removal
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"amd64"}},
									},
								},
								{
									// This term has other expressions
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"amd64"}},
										{Key: "kubernetes.io/os", Operator: corev1.NodeSelectorOpIn, Values: []string{"linux"}},
									},
								},
							},
						},
					},
				}).Build()

			removeArchitectureFromNodeAffinity(pod)

			Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil(),
				"Required affinity should not be nil (second term has OS expression)")
			terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
			Expect(terms).To(HaveLen(1), "Expected 1 term (empty term removed)")
			Expect(terms[0].MatchExpressions).To(HaveLen(1), "Expected 1 expression in remaining term")
			Expect(terms[0].MatchExpressions[0].Key).To(Equal("kubernetes.io/os"),
				"Remaining expression should be OS, not architecture")
		})

		// TestNonEmptyUnrelatedSelectorTermsPreserved
		It("should preserve non-empty unrelated selector terms", func() {
			pod := NewPod().WithName("test-pod").
				WithAffinity(&corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{Key: "zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"us-east-1a", "us-east-1b"}},
									},
								},
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"amd64"}},
										{Key: "instance-type", Operator: corev1.NodeSelectorOpIn, Values: []string{"m5.large"}},
									},
								},
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{Key: "node-role", Operator: corev1.NodeSelectorOpIn, Values: []string{"worker"}},
									},
								},
							},
						},
					},
				}).Build()

			removeArchitectureFromNodeAffinity(pod)

			terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
			Expect(terms).To(HaveLen(3), "Expected 3 terms preserved")
			Expect(terms[0].MatchExpressions).To(HaveLen(1))
			Expect(terms[0].MatchExpressions[0].Key).To(Equal("zone"), "First term was modified")
			Expect(terms[1].MatchExpressions).To(HaveLen(1))
			Expect(terms[1].MatchExpressions[0].Key).To(Equal("instance-type"),
				"Second term should have instance-type only")
			Expect(terms[2].MatchExpressions).To(HaveLen(1))
			Expect(terms[2].MatchExpressions[0].Key).To(Equal("node-role"), "Third term was modified")
		})

		// TestComplexAffinityStructure
		It("should handle complex affinity structure correctly", func() {
			pod := NewPod().WithName("test-pod").
				WithAffinity(&corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"amd64", "arm64"}},
										{Key: "kubernetes.io/os", Operator: corev1.NodeSelectorOpIn, Values: []string{"linux"}},
										{Key: "node.kubernetes.io/instance-type", Operator: corev1.NodeSelectorOpNotIn, Values: []string{"t2.micro"}},
									},
									MatchFields: []corev1.NodeSelectorRequirement{
										{Key: "metadata.name", Operator: corev1.NodeSelectorOpIn, Values: []string{"node-1"}},
									},
								},
							},
						},
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
							{
								Weight: 100,
								Preference: corev1.NodeSelectorTerm{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{Key: "ssd", Operator: corev1.NodeSelectorOpExists},
									},
								},
							},
						},
					},
					PodAffinity: &corev1.PodAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
							{
								LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "cache"}},
								TopologyKey:   "kubernetes.io/hostname",
							},
						},
					},
					PodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
							{
								Weight: 50,
								PodAffinityTerm: corev1.PodAffinityTerm{
									LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "competitor"}},
									TopologyKey:   "topology.kubernetes.io/zone",
								},
							},
						},
					},
				}).Build()

			removeArchitectureFromNodeAffinity(pod)

			for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
				for _, expr := range term.MatchExpressions {
					Expect(expr.Key).NotTo(Equal(utils.ArchLabel), "Architecture should be removed")
				}
			}

			osFound := false
			for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
				for _, expr := range term.MatchExpressions {
					if expr.Key == "kubernetes.io/os" {
						osFound = true
					}
				}
			}
			Expect(osFound).To(BeTrue(), "OS expression should be preserved")

			instanceTypeFound := false
			for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
				for _, expr := range term.MatchExpressions {
					if expr.Key == "node.kubernetes.io/instance-type" {
						instanceTypeFound = true
						Expect(expr.Operator).To(Equal(corev1.NodeSelectorOpNotIn),
							"Instance-type operator was modified")
					}
				}
			}
			Expect(instanceTypeFound).To(BeTrue(), "Instance-type expression should be preserved")

			matchFieldsFound := false
			for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
				if len(term.MatchFields) > 0 {
					matchFieldsFound = true
				}
			}
			Expect(matchFieldsFound).To(BeTrue(), "MatchFields should be preserved")
			Expect(pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution).To(HaveLen(1),
				"Preferred affinity was modified")
			Expect(pod.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution).To(HaveLen(1),
				"Pod affinity was modified")
			Expect(pod.Spec.Affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution).To(HaveLen(1),
				"Pod anti-affinity was modified")
		})

		// TestMixedArchitectureAndNonArchitectureExpressions
		It("should remove all architecture expressions and preserve non-architecture expressions", func() {
			pod := NewPod().WithName("test-pod").
				WithAffinity(&corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{Key: "zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"us-east-1a"}},
										{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"amd64"}},
										{Key: "instance-type", Operator: corev1.NodeSelectorOpIn, Values: []string{"m5.large"}},
										{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpNotIn, Values: []string{"arm64"}},
										{Key: "ssd", Operator: corev1.NodeSelectorOpExists},
									},
								},
							},
						},
					},
				}).Build()

			removeArchitectureFromNodeAffinity(pod)

			archCount := 0
			nonArchCount := 0
			for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
				for _, expr := range term.MatchExpressions {
					if expr.Key == utils.ArchLabel {
						archCount++
					} else {
						nonArchCount++
					}
				}
			}
			Expect(archCount).To(Equal(0), "Expected 0 architecture expressions")
			Expect(nonArchCount).To(Equal(3), "Expected 3 non-architecture expressions")
		})
	})

	Context("Architecture Constraints Preservation", func() {

		It("should preserve non-architecture required scheduling constraints (zone) when applying new arch", func() {
			pod := NewPod().WithName("test-pod").
				WithAffinity(&corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      "topology.kubernetes.io/zone",
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{"us-east-1a"},
										},
										{
											Key:      utils.ArchLabel,
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{"amd64"},
										},
									},
								},
							},
						},
					},
				}).Build()

			applyArchitectureConstraints(pod, []string{"ppc64le"})

			terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
			Expect(len(terms)).To(BeNumerically(">=", 1), "Expected at least 1 term after applying constraints")

			zoneFound := false
			for _, term := range terms {
				for _, expr := range term.MatchExpressions {
					if expr.Key == "topology.kubernetes.io/zone" {
						zoneFound = true
						Expect(expr.Operator).To(Equal(corev1.NodeSelectorOpIn), "Zone operator was modified")
						Expect(expr.Values).To(ConsistOf("us-east-1a"), "Zone values were modified")
					}
				}
			}
			Expect(zoneFound).To(BeTrue(), "CRITICAL: Zone constraint was removed! Non-architecture required scheduling was destroyed!")

			archFound := false
			for _, term := range terms {
				for _, expr := range term.MatchExpressions {
					if expr.Key == utils.ArchLabel {
						archFound = true
						Expect(expr.Values).To(ConsistOf("ppc64le"), "Architecture constraint not applied correctly")
					}
				}
			}
			Expect(archFound).To(BeTrue(), "Architecture constraint was not applied")
		})

		It("should preserve multiple non-architecture required constraints when applying new arch", func() {
			pod := NewPod().WithName("test-pod").
				WithAffinity(&corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{Key: "topology.kubernetes.io/zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"us-east-1a", "us-east-1b"}},
										{Key: "node.kubernetes.io/instance-type", Operator: corev1.NodeSelectorOpIn, Values: []string{"m5.large", "m5.xlarge"}},
										{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"amd64"}},
										{Key: "kubernetes.io/os", Operator: corev1.NodeSelectorOpIn, Values: []string{"linux"}},
									},
									MatchFields: []corev1.NodeSelectorRequirement{
										{Key: "metadata.name", Operator: corev1.NodeSelectorOpIn, Values: []string{"node-1", "node-2"}},
									},
								},
							},
						},
					},
				}).Build()

			applyArchitectureConstraints(pod, []string{"ppc64le", "arm64"})

			terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
			requiredConstraints := map[string]bool{
				"topology.kubernetes.io/zone":      false,
				"node.kubernetes.io/instance-type": false,
				"kubernetes.io/os":                 false,
			}
			for _, term := range terms {
				for _, expr := range term.MatchExpressions {
					if _, exists := requiredConstraints[expr.Key]; exists {
						requiredConstraints[expr.Key] = true
					}
				}
			}
			for key, found := range requiredConstraints {
				Expect(found).To(BeTrue(), "CRITICAL: Required constraint %s was removed!", key)
			}

			matchFieldsFound := false
			for _, term := range terms {
				if len(term.MatchFields) > 0 {
					matchFieldsFound = true
					Expect(term.MatchFields[0].Key).To(Equal("metadata.name"), "MatchFields was modified")
				}
			}
			Expect(matchFieldsFound).To(BeTrue(), "CRITICAL: MatchFields was removed!")
		})

		It("should handle multiple terms with mixed architecture and non-architecture constraints", func() {
			pod := NewPod().WithName("test-pod").
				WithAffinity(&corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{Key: "topology.kubernetes.io/zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"us-east-1a"}},
										{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"amd64"}},
									},
								},
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{Key: "node.kubernetes.io/instance-type", Operator: corev1.NodeSelectorOpIn, Values: []string{"m5.large"}},
										{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"amd64"}},
									},
								},
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"arm64"}},
									},
								},
							},
						},
					},
				}).Build()

			applyArchitectureConstraints(pod, []string{"ppc64le"})

			terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms

			zoneFound := false
			for _, term := range terms {
				for _, expr := range term.MatchExpressions {
					if expr.Key == "topology.kubernetes.io/zone" {
						zoneFound = true
					}
				}
			}
			Expect(zoneFound).To(BeTrue(), "CRITICAL: Zone constraint from term 1 was removed!")

			instanceTypeFound := false
			for _, term := range terms {
				for _, expr := range term.MatchExpressions {
					if expr.Key == "node.kubernetes.io/instance-type" {
						instanceTypeFound = true
					}
				}
			}
			Expect(instanceTypeFound).To(BeTrue(), "CRITICAL: Instance-type constraint from term 2 was removed!")

			newArchFound := false
			for _, term := range terms {
				for _, expr := range term.MatchExpressions {
					if expr.Key == utils.ArchLabel && len(expr.Values) == 1 && expr.Values[0] == "ppc64le" {
						newArchFound = true
					}
				}
			}
			Expect(newArchFound).To(BeTrue(), "New architecture constraint was not applied")
		})
	})

	Context("Stale Architecture Regression", func() {

		It("should remove stale architecture values and apply only the new CEL-selected architecture (CPD field symptom regression)", func() {
			pod := NewPod().WithName("ibm-lh-lakehouse-ces-0").WithNamespace("cpd-instance").
				WithAffinity(&corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      utils.ArchLabel,
											Operator: corev1.NodeSelectorOpIn,
											Values: []string{
												utils.ArchitectureAmd64,
												utils.ArchitecturePpc64le,
												utils.ArchitectureS390x,
											},
										},
									},
								},
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      "topology.kubernetes.io/zone",
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{"us-east-1a"},
										},
									},
								},
							},
						},
					},
				}).Build()

			changed := applyArchitectureConstraints(pod, []string{utils.ArchitecturePpc64le})
			Expect(changed).To(BeTrue(), "expected architecture constraints application to report a change")

			Expect(pod.Spec.Affinity).NotTo(BeNil())
			Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil())
			Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())

			terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
			Expect(terms).To(HaveLen(1), "expected exactly 1 merged required node selector term")

			var (
				foundZoneConstraint bool
				archExpressions     []corev1.NodeSelectorRequirement
			)

			for _, expr := range terms[0].MatchExpressions {
				switch expr.Key {
				case "topology.kubernetes.io/zone":
					foundZoneConstraint = true
					Expect(expr.Values).To(ConsistOf("us-east-1a"), "zone constraint was modified unexpectedly")
				case utils.ArchLabel:
					archExpressions = append(archExpressions, expr)
				}
			}

			Expect(foundZoneConstraint).To(BeTrue(), "expected non-architecture zone constraint to be preserved")
			Expect(archExpressions).To(HaveLen(1),
				"expected exactly 1 architecture expression after cleanup and reapply")

			archExpr := archExpressions[0]
			Expect(archExpr.Operator).To(Equal(corev1.NodeSelectorOpIn))
			Expect(archExpr.Values).To(ConsistOf(utils.ArchitecturePpc64le),
				"expected stale architectures to be removed and replaced with only ppc64le")

			for _, staleArch := range []string{utils.ArchitectureAmd64, utils.ArchitectureS390x} {
				Expect(archExpr.Values).NotTo(ContainElement(staleArch),
					"stale architecture %q was preserved unexpectedly in final required affinity", staleArch)
			}
		})

		It("should replace broad fallback architecture values with CEL-matched rule architecture", func() {
			pod := NewPod().WithName("lhconsole-api-v3-76575f8566-bbnvb").WithNamespace("cpd-instance").
				WithNodeSelectors(utils.ArchLabel, utils.ArchitectureAmd64).
				WithAffinity(&corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      utils.ArchLabel,
											Operator: corev1.NodeSelectorOpIn,
											Values: []string{
												utils.ArchitectureAmd64,
												utils.ArchitecturePpc64le,
												utils.ArchitectureS390x,
											},
										},
										{
											Key:      "kubernetes.io/os",
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{"linux"},
										},
									},
								},
							},
						},
					},
				}).Build()

			removed := removeAllArchitectureConstraints(pod)
			Expect(removed).To(BeTrue(), "expected stale architecture constraints to be removed")
			Expect(pod.Spec.NodeSelector).NotTo(HaveKey(utils.ArchLabel),
				"expected nodeSelector architecture key to be removed")

			terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
			Expect(terms).To(HaveLen(1),
				"expected one preserved non-architecture term after cleanup")

			for _, expr := range terms[0].MatchExpressions {
				Expect(expr.Key).NotTo(Equal(utils.ArchLabel),
					"expected no architecture expressions after cleanup")
			}

			applyArchitectureNodeAffinity(pod, []string{utils.ArchitecturePpc64le})

			terms = pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
			Expect(terms).To(HaveLen(1), "expected one merged required term after reapply")

			var foundExclusivePPC64LE bool
			var foundOSConstraint bool
			for _, expr := range terms[0].MatchExpressions {
				switch expr.Key {
				case utils.ArchLabel:
					Expect(expr.Values).To(ConsistOf(utils.ArchitecturePpc64le),
						"expected final architecture expression to be exclusive to ppc64le")
					foundExclusivePPC64LE = true
				case "kubernetes.io/os":
					if len(expr.Values) == 1 && expr.Values[0] == "linux" {
						foundOSConstraint = true
					}
				}
			}
			Expect(foundExclusivePPC64LE).To(BeTrue(),
				"expected to find an exclusive ppc64le architecture expression after reapply")
			Expect(foundOSConstraint).To(BeTrue(),
				"expected existing os constraint to be preserved in merged required term")
		})
	})

	Context("Affinity Ordering", func() {

		It("should preserve the relative order of NodeSelectorTerms after applying architecture constraints", func() {
			pod := NewPod().WithName("ordered-pod").
				WithAffinity(&corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{MatchExpressions: []corev1.NodeSelectorRequirement{
									{Key: "topology.kubernetes.io/zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"us-east-1a"}},
									{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"amd64"}},
								}},
								{MatchExpressions: []corev1.NodeSelectorRequirement{
									{Key: "node.kubernetes.io/instance-type", Operator: corev1.NodeSelectorOpIn, Values: []string{"m5.large"}},
									{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"amd64"}},
								}},
								{MatchExpressions: []corev1.NodeSelectorRequirement{
									{Key: "kubernetes.io/os", Operator: corev1.NodeSelectorOpIn, Values: []string{"linux"}},
									{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"amd64"}},
								}},
							},
						},
					},
				}).Build()

			applyArchitectureConstraints(pod, []string{utils.ArchitecturePpc64le})

			terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
			Expect(terms).To(HaveLen(3), "expected 3 terms after in-place update")

			expectedNonArchKeys := []string{
				"topology.kubernetes.io/zone",
				"node.kubernetes.io/instance-type",
				"kubernetes.io/os",
			}
			for i, key := range expectedNonArchKeys {
				found := false
				for _, expr := range terms[i].MatchExpressions {
					if expr.Key == key {
						found = true
					}
				}
				Expect(found).To(BeTrue(), "term[%d] lost its non-arch key %q after applyArchitectureConstraints", i, key)
			}

			for i, term := range terms {
				archFound := false
				for _, expr := range term.MatchExpressions {
					if expr.Key == utils.ArchLabel {
						archFound = true
						Expect(expr.Values).To(ConsistOf(utils.ArchitecturePpc64le),
							"term[%d] arch value = %v, want [ppc64le]", i, expr.Values)
					}
				}
				Expect(archFound).To(BeTrue(), "term[%d] is missing arch expression after applyArchitectureConstraints", i)
			}
		})

		It("should preserve the relative order of non-arch MatchExpressions after removal", func() {
			pod := NewPod().WithName("order-test").
				WithAffinity(&corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{MatchExpressions: []corev1.NodeSelectorRequirement{
									{Key: "alpha", Operator: corev1.NodeSelectorOpIn, Values: []string{"1"}},
									{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"amd64"}},
									{Key: "beta", Operator: corev1.NodeSelectorOpIn, Values: []string{"2"}},
									{Key: "gamma", Operator: corev1.NodeSelectorOpIn, Values: []string{"3"}},
								}},
							},
						},
					},
				}).Build()

			removeArchitectureFromNodeAffinity(pod)

			Expect(pod.Spec.Affinity).NotTo(BeNil())
			Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil())
			Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil(),
				"required affinity should not be nil after removing arch from a term with other keys")

			terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
			Expect(terms).To(HaveLen(1))

			exprs := terms[0].MatchExpressions
			Expect(exprs).To(HaveLen(3), "expected 3 MatchExpressions after arch removal")
			expectedOrder := []string{"alpha", "beta", "gamma"}
			for i, want := range expectedOrder {
				Expect(exprs[i].Key).To(Equal(want),
					"MatchExpressions[%d].Key = %q, want %q (order changed)", i, exprs[i].Key, want)
			}
		})

		It("should not reorder or remove MatchFields entries during in-place update", func() {
			pod := NewPod().WithName("matchfields-pod").
				WithAffinity(&corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"amd64"}},
									},
									MatchFields: []corev1.NodeSelectorRequirement{
										{Key: "metadata.name", Operator: corev1.NodeSelectorOpIn, Values: []string{"node-a", "node-b"}},
									},
								},
							},
						},
					},
				}).Build()

			applyArchitectureConstraints(pod, []string{utils.ArchitecturePpc64le})

			terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
			Expect(terms).To(HaveLen(1))
			Expect(terms[0].MatchFields).To(HaveLen(1), "expected 1 MatchFields entry")
			Expect(terms[0].MatchFields[0].Key).To(Equal("metadata.name"), "MatchFields[0].Key changed")
			Expect(terms[0].MatchFields[0].Values).To(HaveLen(2), "MatchFields[0].Values changed")
		})
	})

	Context("Metadata Preservation", func() {

		It("should leave pod labels and annotations completely unchanged after applyArchitectureConstraints", func() {
			originalLabels := map[string]string{
				"app": "database", "tier": "backend", "managed-by": "helm", "version": "1.2.3",
			}
			originalAnnotations := map[string]string{
				"kubectl.kubernetes.io/last-applied-configuration": `{"some":"json"}`,
				"custom-annotation": "custom-value",
			}

			pod := NewPod().
				WithName("metadata-pod").
				WithNamespace("prod").
				WithLabels("app", "database", "tier", "backend", "managed-by", "helm", "version", "1.2.3").
				WithAnnotations(copyStringMap(originalAnnotations)).
				WithNodeSelectors(utils.ArchLabel, "amd64", "zone", "us-east-1").
				Build()

			applyArchitectureConstraints(pod, []string{utils.ArchitecturePpc64le})

			Expect(pod.Labels).To(HaveLen(len(originalLabels)),
				"label count changed — labels=%v", pod.Labels)
			for k, wantV := range originalLabels {
				Expect(pod.Labels[k]).To(Equal(wantV), "label %q changed", k)
			}

			Expect(pod.Annotations).To(HaveLen(len(originalAnnotations)),
				"annotation count changed")
			for k, wantV := range originalAnnotations {
				Expect(pod.Annotations[k]).To(Equal(wantV), "annotation %q changed", k)
			}

			Expect(pod.Spec.NodeSelector["zone"]).To(Equal("us-east-1"),
				"non-arch nodeSelector key 'zone' was modified or removed")
		})

		It("should leave OwnerReferences untouched after applyArchitectureConstraints", func() {
			truePtr := true
			ownerRef := metav1.OwnerReference{
				APIVersion: "apps/v1", Kind: "Deployment", Name: "my-deploy",
				UID: "uid-12345", Controller: &truePtr, BlockOwnerDeletion: &truePtr,
			}
			pod := NewPod().
				WithName("owned-pod").
				WithNamespace("default").
				WithOwnerReference(ownerRef).
				Build()

			applyArchitectureConstraints(pod, []string{utils.ArchitecturePpc64le})

			Expect(pod.OwnerReferences).To(HaveLen(1), "OwnerReferences count changed")
			got := pod.OwnerReferences[0]
			Expect(got.Name).To(Equal(ownerRef.Name))
			Expect(got.UID).To(Equal(ownerRef.UID))
			Expect(got.Kind).To(Equal(ownerRef.Kind))
		})

		It("should leave finalizers untouched after applyArchitectureConstraints", func() {
			pod := NewPod().WithName("finalized-pod").WithNamespace("default").
				WithFinalizers("example.com/my-finalizer", "storage.kubernetes.io/finalizer").Build()

			applyArchitectureConstraints(pod, []string{utils.ArchitecturePpc64le})

			Expect(pod.Finalizers).To(HaveLen(2),
				"Finalizers count changed — finalizers=%v", pod.Finalizers)
			for i, f := range []string{"example.com/my-finalizer", "storage.kubernetes.io/finalizer"} {
				Expect(pod.Finalizers[i]).To(Equal(f), "Finalizers[%d] changed", i)
			}
		})
	})
})

func copyStringMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
