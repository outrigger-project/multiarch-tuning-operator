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

// TestNonArchitectureRequiredSchedulingPreserved verifies that non-architecture
// required scheduling constraints are preserved when applying architecture constraints.
// This is CRITICAL for production safety.
func TestNonArchitectureRequiredSchedulingPreserved(t *testing.T) {
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
			},
		},
	}

	// Apply new architecture constraints
	applyArchitectureConstraints(pod, []string{"ppc64le"})

	// Verify zone constraint is preserved
	terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) < 1 {
		t.Fatal("Expected at least 1 term after applying constraints")
	}

	// Find the zone constraint
	zoneFound := false
	for _, term := range terms {
		for _, expr := range term.MatchExpressions {
			if expr.Key == "topology.kubernetes.io/zone" {
				zoneFound = true
				if expr.Operator != corev1.NodeSelectorOpIn {
					t.Error("Zone operator was modified")
				}
				if len(expr.Values) != 1 || expr.Values[0] != "us-east-1a" {
					t.Error("Zone values were modified")
				}
			}
		}
	}

	if !zoneFound {
		t.Fatal("CRITICAL: Zone constraint was removed! Non-architecture required scheduling was destroyed!")
	}

	// Verify architecture constraint was applied
	archFound := false
	for _, term := range terms {
		for _, expr := range term.MatchExpressions {
			if expr.Key == utils.ArchLabel {
				archFound = true
				if len(expr.Values) != 1 || expr.Values[0] != "ppc64le" {
					t.Error("Architecture constraint not applied correctly")
				}
			}
		}
	}

	if !archFound {
		t.Error("Architecture constraint was not applied")
	}
}

// TestMultipleNonArchRequiredConstraintsPreserved verifies that multiple
// non-architecture required constraints are all preserved
func TestMultipleNonArchRequiredConstraintsPreserved(t *testing.T) {
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
										Key:      "topology.kubernetes.io/zone",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"us-east-1a", "us-east-1b"},
									},
									{
										Key:      "node.kubernetes.io/instance-type",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"m5.large", "m5.xlarge"},
									},
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

	// Apply new architecture constraints
	applyArchitectureConstraints(pod, []string{"ppc64le", "arm64"})

	terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms

	// Verify all non-arch constraints are preserved
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
		if !found {
			t.Errorf("CRITICAL: Required constraint %s was removed!", key)
		}
	}

	// Verify MatchFields preserved
	matchFieldsFound := false
	for _, term := range terms {
		if len(term.MatchFields) > 0 {
			matchFieldsFound = true
			if term.MatchFields[0].Key != "metadata.name" {
				t.Error("MatchFields was modified")
			}
		}
	}
	if !matchFieldsFound {
		t.Error("CRITICAL: MatchFields was removed!")
	}
}

// TestMultipleTermsWithMixedConstraintsPreserved verifies that multiple terms
// with mixed architecture and non-architecture constraints are handled correctly
func TestMultipleTermsWithMixedConstraintsPreserved(t *testing.T) {
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
								// Term 1: Zone + Arch
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
							{
								// Term 2: Instance type + Arch
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "node.kubernetes.io/instance-type",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"m5.large"},
									},
									{
										Key:      utils.ArchLabel,
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"amd64"},
									},
								},
							},
							{
								// Term 3: Only arch (should be removed after cleanup)
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

	// Apply new architecture constraints
	applyArchitectureConstraints(pod, []string{"ppc64le"})

	terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms

	// Verify zone constraint preserved (from term 1)
	zoneFound := false
	for _, term := range terms {
		for _, expr := range term.MatchExpressions {
			if expr.Key == "topology.kubernetes.io/zone" {
				zoneFound = true
			}
		}
	}
	if !zoneFound {
		t.Error("CRITICAL: Zone constraint from term 1 was removed!")
	}

	// Verify instance-type constraint preserved (from term 2)
	instanceTypeFound := false
	for _, term := range terms {
		for _, expr := range term.MatchExpressions {
			if expr.Key == "node.kubernetes.io/instance-type" {
				instanceTypeFound = true
			}
		}
	}
	if !instanceTypeFound {
		t.Error("CRITICAL: Instance-type constraint from term 2 was removed!")
	}

	// Verify new architecture constraint applied
	newArchFound := false
	for _, term := range terms {
		for _, expr := range term.MatchExpressions {
			if expr.Key == utils.ArchLabel && len(expr.Values) == 1 && expr.Values[0] == "ppc64le" {
				newArchFound = true
			}
		}
	}
	if !newArchFound {
		t.Error("New architecture constraint was not applied")
	}
}
