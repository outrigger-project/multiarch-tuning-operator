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
