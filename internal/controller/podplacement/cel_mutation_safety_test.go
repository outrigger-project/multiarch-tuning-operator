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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

// TestOnlyArchitectureRemovedFromNodeSelector verifies that ONLY kubernetes.io/arch is removed
func TestOnlyArchitectureRemovedFromNodeSelector(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				utils.ArchLabel:                    "amd64",
				"kubernetes.io/os":                 "linux",
				"node.kubernetes.io/instance-type": "m5.large",
				"topology.kubernetes.io/zone":      "us-east-1a",
				"custom-label":                     "custom-value",
			},
		},
	}

	removeArchitectureFromNodeSelector(pod)

	// Verify only arch was removed
	if _, exists := pod.Spec.NodeSelector[utils.ArchLabel]; exists {
		t.Error("Architecture label should be removed")
	}

	// Verify all other labels preserved
	expectedLabels := map[string]string{
		"kubernetes.io/os":                 "linux",
		"node.kubernetes.io/instance-type": "m5.large",
		"topology.kubernetes.io/zone":      "us-east-1a",
		"custom-label":                     "custom-value",
	}

	for key, expectedValue := range expectedLabels {
		if actualValue, exists := pod.Spec.NodeSelector[key]; !exists {
			t.Errorf("Label %s was removed but should be preserved", key)
		} else if actualValue != expectedValue {
			t.Errorf("Label %s value changed from %s to %s", key, expectedValue, actualValue)
		}
	}
}

// TestUnrelatedAffinityPreserved verifies that unrelated affinity is preserved
func TestUnrelatedAffinityPreserved(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
		Spec: corev1.PodSpec{
			Affinity: &corev1.Affinity{
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
								MatchLabels: map[string]string{
									"app": "database",
								},
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
									MatchLabels: map[string]string{
										"app": "cache",
									},
								},
								TopologyKey: "kubernetes.io/hostname",
							},
						},
					},
				},
			},
		},
	}

	removeArchitectureFromNodeAffinity(pod)

	// Verify architecture was removed
	if pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
		for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
			for _, expr := range term.MatchExpressions {
				if expr.Key == utils.ArchLabel {
					t.Error("Architecture expression should be removed")
				}
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
					if expr.Operator != corev1.NodeSelectorOpIn || len(expr.Values) != 1 || expr.Values[0] != "linux" {
						t.Error("OS expression was modified")
					}
				}
			}
		}
	}
	if !osFound {
		t.Error("OS expression was removed but should be preserved")
	}

	// Verify pod affinity preserved
	if pod.Spec.Affinity.PodAffinity == nil {
		t.Error("Pod affinity was removed")
	} else {
		if len(pod.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution) != 1 {
			t.Error("Pod affinity terms were modified")
		}
	}

	// Verify pod anti-affinity preserved
	if pod.Spec.Affinity.PodAntiAffinity == nil {
		t.Error("Pod anti-affinity was removed")
	} else {
		if len(pod.Spec.Affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution) != 1 {
			t.Error("Pod anti-affinity terms were modified")
		}
	}
}

// TestPreferredAffinityPreserved verifies that preferredDuringSchedulingIgnoredDuringExecution is preserved
func TestPreferredAffinityPreserved(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
		Spec: corev1.PodSpec{
			Affinity: &corev1.Affinity{
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
								},
							},
						},
					},
					PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
						{
							Weight: 50,
							Preference: corev1.NodeSelectorTerm{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "node-type",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"compute"},
									},
								},
							},
						},
						{
							Weight: 30,
							Preference: corev1.NodeSelectorTerm{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      utils.ArchLabel,
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"arm64"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	removeArchitectureFromNodeAffinity(pod)

	// Verify preferred affinity is completely untouched
	if pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution == nil {
		t.Fatal("Preferred affinity was removed")
	}

	preferred := pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution
	if len(preferred) != 2 {
		t.Errorf("Expected 2 preferred terms, got %d", len(preferred))
	}

	// Verify both preferred terms are intact (including the one with arch)
	if preferred[0].Weight != 50 {
		t.Error("First preferred term weight was modified")
	}
	if preferred[1].Weight != 30 {
		t.Error("Second preferred term weight was modified")
	}

	// Verify architecture in preferred term is preserved
	archInPreferred := false
	for _, term := range preferred {
		for _, expr := range term.Preference.MatchExpressions {
			if expr.Key == utils.ArchLabel {
				archInPreferred = true
			}
		}
	}
	if !archInPreferred {
		t.Error("Architecture in preferred affinity should be preserved")
	}
}

// TestMatchFieldsPreserved verifies that MatchFields are preserved during cleanup
func TestMatchFieldsPreserved(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
		Spec: corev1.PodSpec{
			Affinity: &corev1.Affinity{
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
								},
								MatchFields: []corev1.NodeSelectorRequirement{
									{
										Key:      "metadata.name",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"node-1", "node-2"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	removeArchitectureFromNodeAffinity(pod)

	// Verify MatchFields preserved
	if pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		t.Fatal("Required affinity was removed")
	}

	terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 1 {
		t.Errorf("Expected 1 term (with MatchFields), got %d", len(terms))
	}

	if len(terms[0].MatchFields) != 1 {
		t.Errorf("Expected 1 MatchField, got %d", len(terms[0].MatchFields))
	}

	if terms[0].MatchFields[0].Key != "metadata.name" {
		t.Error("MatchFields was modified")
	}
}

// TestEmptySelectorTermsRemoved verifies that empty selector terms are removed after cleanup
func TestEmptySelectorTermsRemoved(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
		Spec: corev1.PodSpec{
			Affinity: &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								// This term will become empty after arch removal
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      utils.ArchLabel,
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"amd64"},
									},
								},
							},
							{
								// This term has other expressions
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
			},
		},
	}

	removeArchitectureFromNodeAffinity(pod)

	// Verify empty term was removed
	if pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		t.Fatal("Required affinity should not be nil (second term has OS expression)")
	}

	terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 1 {
		t.Errorf("Expected 1 term (empty term removed), got %d", len(terms))
	}

	// Verify remaining term has OS expression
	if len(terms[0].MatchExpressions) != 1 {
		t.Errorf("Expected 1 expression in remaining term, got %d", len(terms[0].MatchExpressions))
	}
	if terms[0].MatchExpressions[0].Key != "kubernetes.io/os" {
		t.Error("Remaining expression should be OS, not architecture")
	}
}

// TestNonEmptyUnrelatedSelectorTermsPreserved verifies that non-empty unrelated selector terms are preserved
func TestNonEmptyUnrelatedSelectorTermsPreserved(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
		Spec: corev1.PodSpec{
			Affinity: &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "zone",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"us-east-1a", "us-east-1b"},
									},
								},
							},
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      utils.ArchLabel,
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"amd64"},
									},
									{
										Key:      "instance-type",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"m5.large"},
									},
								},
							},
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "node-role",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"worker"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	removeArchitectureFromNodeAffinity(pod)

	// Should have 3 terms (all preserved, arch removed from second)
	terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 3 {
		t.Errorf("Expected 3 terms, got %d", len(terms))
	}

	// Verify first term unchanged
	if len(terms[0].MatchExpressions) != 1 || terms[0].MatchExpressions[0].Key != "zone" {
		t.Error("First term was modified")
	}

	// Verify second term has arch removed but instance-type preserved
	if len(terms[1].MatchExpressions) != 1 || terms[1].MatchExpressions[0].Key != "instance-type" {
		t.Error("Second term should have instance-type only")
	}

	// Verify third term unchanged
	if len(terms[2].MatchExpressions) != 1 || terms[2].MatchExpressions[0].Key != "node-role" {
		t.Error("Third term was modified")
	}
}

// TestComplexAffinityStructure verifies handling of complex affinity structures
func TestComplexAffinityStructure(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
		Spec: corev1.PodSpec{
			Affinity: &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      utils.ArchLabel,
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"amd64", "arm64"},
									},
									{
										Key:      "kubernetes.io/os",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"linux"},
									},
									{
										Key:      "node.kubernetes.io/instance-type",
										Operator: corev1.NodeSelectorOpNotIn,
										Values:   []string{"t2.micro"},
									},
								},
								MatchFields: []corev1.NodeSelectorRequirement{
									{
										Key:      "metadata.name",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"node-1"},
									},
								},
							},
						},
					},
					PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
						{
							Weight: 100,
							Preference: corev1.NodeSelectorTerm{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "ssd",
										Operator: corev1.NodeSelectorOpExists,
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
								MatchLabels: map[string]string{"app": "cache"},
							},
							TopologyKey: "kubernetes.io/hostname",
						},
					},
				},
				PodAntiAffinity: &corev1.PodAntiAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
						{
							Weight: 50,
							PodAffinityTerm: corev1.PodAffinityTerm{
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{"app": "competitor"},
								},
								TopologyKey: "topology.kubernetes.io/zone",
							},
						},
					},
				},
			},
		},
	}

	// Apply cleanup
	removeArchitectureFromNodeAffinity(pod)

	// Verify architecture removed
	for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
		for _, expr := range term.MatchExpressions {
			if expr.Key == utils.ArchLabel {
				t.Error("Architecture should be removed")
			}
		}
	}

	// Verify OS expression preserved
	osFound := false
	for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
		for _, expr := range term.MatchExpressions {
			if expr.Key == "kubernetes.io/os" {
				osFound = true
			}
		}
	}
	if !osFound {
		t.Error("OS expression should be preserved")
	}

	// Verify instance-type expression preserved
	instanceTypeFound := false
	for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
		for _, expr := range term.MatchExpressions {
			if expr.Key == "node.kubernetes.io/instance-type" {
				instanceTypeFound = true
				if expr.Operator != corev1.NodeSelectorOpNotIn {
					t.Error("Instance-type operator was modified")
				}
			}
		}
	}
	if !instanceTypeFound {
		t.Error("Instance-type expression should be preserved")
	}

	// Verify MatchFields preserved
	matchFieldsFound := false
	for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
		if len(term.MatchFields) > 0 {
			matchFieldsFound = true
		}
	}
	if !matchFieldsFound {
		t.Error("MatchFields should be preserved")
	}

	// Verify preferred affinity untouched
	if len(pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution) != 1 {
		t.Error("Preferred affinity was modified")
	}

	// Verify pod affinity untouched
	if len(pod.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution) != 1 {
		t.Error("Pod affinity was modified")
	}

	// Verify pod anti-affinity untouched
	if len(pod.Spec.Affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution) != 1 {
		t.Error("Pod anti-affinity was modified")
	}
}

// TestMixedArchitectureAndNonArchitectureExpressions verifies correct handling of mixed expressions
func TestMixedArchitectureAndNonArchitectureExpressions(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
		Spec: corev1.PodSpec{
			Affinity: &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "zone",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"us-east-1a"},
									},
									{
										Key:      utils.ArchLabel,
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"amd64"},
									},
									{
										Key:      "instance-type",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"m5.large"},
									},
									{
										Key:      utils.ArchLabel, // Duplicate arch expression
										Operator: corev1.NodeSelectorOpNotIn,
										Values:   []string{"arm64"},
									},
									{
										Key:      "ssd",
										Operator: corev1.NodeSelectorOpExists,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	removeArchitectureFromNodeAffinity(pod)

	// Verify all architecture expressions removed
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

	if archCount != 0 {
		t.Errorf("Expected 0 architecture expressions, found %d", archCount)
	}
	if nonArchCount != 3 {
		t.Errorf("Expected 3 non-architecture expressions, found %d", nonArchCount)
	}
}
