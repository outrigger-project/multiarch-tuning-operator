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

// TestApplyArchitectureNodeAffinityInPlaceUpdate verifies that architecture constraints
// are updated in-place without removing and re-adding NodeSelectorTerms, which would
// cause Kubernetes to reject the update with:
// "no additions/deletions to non-empty NodeSelectorTerms list are allowed"
func TestApplyArchitectureNodeAffinityInPlaceUpdate(t *testing.T) {
	tests := []struct {
		name                   string
		pod                    *corev1.Pod
		architectures          []string
		expectedTermCount      int
		expectedArchInEachTerm []string
		verifyOtherConstraints bool
	}{
		{
			name: "update existing arch constraint in-place",
			pod: &corev1.Pod{
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
												Key:      "kubernetes.io/os",
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{"linux"},
											},
											{
												Key:      utils.ArchLabel,
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{"amd64", "ppc64le", "s390x"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			architectures:          []string{"ppc64le"},
			expectedTermCount:      1,
			expectedArchInEachTerm: []string{"ppc64le"},
			verifyOtherConstraints: true,
		},
		{
			name: "update multiple terms with arch constraints",
			pod: &corev1.Pod{
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
												Key:      "kubernetes.io/os",
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{"linux"},
											},
											{
												Key:      utils.ArchLabel,
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{"amd64"},
											},
										},
									},
									{
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
								},
							},
						},
					},
				},
			},
			architectures:          []string{"ppc64le"},
			expectedTermCount:      2,
			expectedArchInEachTerm: []string{"ppc64le"},
			verifyOtherConstraints: true,
		},
		{
			name: "add arch to term without arch constraint",
			pod: &corev1.Pod{
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
			},
			architectures:          []string{"ppc64le"},
			expectedTermCount:      1,
			expectedArchInEachTerm: []string{"ppc64le"},
			verifyOtherConstraints: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Store original term count to verify in-place update
			originalTermCount := 0
			if tt.pod.Spec.Affinity != nil &&
				tt.pod.Spec.Affinity.NodeAffinity != nil &&
				tt.pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
				originalTermCount = len(tt.pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms)
			}

			applyArchitectureNodeAffinity(tt.pod, tt.architectures)

			// Verify the term count matches expected (should be preserved for in-place update)
			if tt.pod.Spec.Affinity == nil ||
				tt.pod.Spec.Affinity.NodeAffinity == nil ||
				tt.pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
				t.Fatal("Expected affinity structure to be created")
			}

			terms := tt.pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
			if len(terms) != tt.expectedTermCount {
				t.Errorf("Expected %d terms, got %d", tt.expectedTermCount, len(terms))
			}

			// For in-place updates, term count should be preserved
			if originalTermCount > 0 && len(terms) != originalTermCount {
				t.Errorf("Term count changed from %d to %d - this would cause Kubernetes API rejection", originalTermCount, len(terms))
			}

			// Verify each term has the correct architecture constraint
			for i, term := range terms {
				foundArch := false
				for _, expr := range term.MatchExpressions {
					if expr.Key == utils.ArchLabel {
						foundArch = true
						if expr.Operator != corev1.NodeSelectorOpIn {
							t.Errorf("Term %d: Expected operator In, got %s", i, expr.Operator)
						}
						if len(expr.Values) != len(tt.expectedArchInEachTerm) {
							t.Errorf("Term %d: Expected %d architectures, got %d", i, len(tt.expectedArchInEachTerm), len(expr.Values))
						}
						for j, expectedArch := range tt.expectedArchInEachTerm {
							if j >= len(expr.Values) || expr.Values[j] != expectedArch {
								t.Errorf("Term %d: Expected architecture %s at index %d, got %v", i, expectedArch, j, expr.Values)
							}
						}
					}
				}
				if !foundArch {
					t.Errorf("Term %d: Architecture constraint not found", i)
				}

				// Verify other constraints are preserved
				if tt.verifyOtherConstraints {
					nonArchCount := 0
					for _, expr := range term.MatchExpressions {
						if expr.Key != utils.ArchLabel {
							nonArchCount++
						}
					}
					if i == 0 && nonArchCount == 0 && originalTermCount > 0 {
						t.Errorf("Term %d: Other constraints were removed but should be preserved", i)
					}
				}
			}
		})
	}
}

// TestApplyArchitectureConstraintsPreservesTermStructure verifies that the complete
// applyArchitectureConstraints function preserves the NodeSelectorTerms structure
func TestApplyArchitectureConstraintsPreservesTermStructure(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				utils.ArchLabel: "amd64",
			},
			Affinity: &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "kubernetes.io/os",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"linux"},
									},
									{
										Key:      utils.ArchLabel,
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"amd64", "ppc64le", "s390x"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	originalTermCount := len(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms)

	modified := applyArchitectureConstraints(pod, []string{"ppc64le"})

	if !modified {
		t.Error("Expected modified to be true")
	}

	// Verify nodeSelector arch was removed
	if _, exists := pod.Spec.NodeSelector[utils.ArchLabel]; exists {
		t.Error("Architecture constraint should be removed from nodeSelector")
	}

	// Verify term count is preserved (in-place update)
	terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != originalTermCount {
		t.Errorf("Term count changed from %d to %d - this would cause Kubernetes API rejection", originalTermCount, len(terms))
	}

	// Verify architecture was updated to ppc64le
	foundPpc64le := false
	for _, term := range terms {
		for _, expr := range term.MatchExpressions {
			if expr.Key == utils.ArchLabel {
				if len(expr.Values) == 1 && expr.Values[0] == "ppc64le" {
					foundPpc64le = true
				}
			}
		}
	}
	if !foundPpc64le {
		t.Error("Expected architecture to be updated to ppc64le")
	}

	// Verify os constraint is preserved
	foundOS := false
	for _, term := range terms {
		for _, expr := range term.MatchExpressions {
			if expr.Key == "kubernetes.io/os" {
				foundOS = true
			}
		}
	}
	if !foundOS {
		t.Error("OS constraint should be preserved")
	}
}
