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

	. "github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

func TestRemoveArchitectureFromNodeSelector(t *testing.T) {
	tests := []struct {
		name           string
		pod            *corev1.Pod
		expectedRemove bool
	}{
		{
			name:           "remove arch from nodeSelector",
			pod:            NewPod().WithNodeSelectors(utils.ArchLabel, "amd64", "other-label", "value").Build(),
			expectedRemove: true,
		},
		{
			name:           "no arch in nodeSelector",
			pod:            NewPod().WithNodeSelectors("other-label", "value").Build(),
			expectedRemove: false,
		},
		{
			name:           "nil nodeSelector",
			pod:            NewPod().Build(),
			expectedRemove: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			removed := removeArchitectureFromNodeSelector(tt.pod)
			if removed != tt.expectedRemove {
				t.Errorf("Expected removed=%v, got %v", tt.expectedRemove, removed)
			}
			// Verify arch label was actually removed
			if tt.pod.Spec.NodeSelector != nil {
				if _, exists := tt.pod.Spec.NodeSelector[utils.ArchLabel]; exists {
					t.Error("Architecture label still exists in nodeSelector")
				}
			}
		})
	}
}

func TestRemoveArchitectureFromNodeAffinity(t *testing.T) {
	tests := []struct {
		name           string
		pod            *corev1.Pod
		expectedRemove bool
		checkNil       bool
	}{
		{
			name: "remove arch from nodeAffinity",
			pod: NewPod().WithNodeSelectorTermsMatchExpressions(
				[]corev1.NodeSelectorRequirement{
					{
						Key:      utils.ArchLabel,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"amd64"},
					},
				},
			).Build(),
			expectedRemove: true,
			checkNil:       true,
		},
		{
			name: "remove arch but keep other expressions",
			pod: NewPod().WithNodeSelectorTermsMatchExpressions(
				[]corev1.NodeSelectorRequirement{
					{
						Key:      utils.ArchLabel,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"amd64"},
					},
					{
						Key:      "other-label",
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"value"},
					},
				},
			).Build(),
			expectedRemove: true,
			checkNil:       false,
		},
		{
			name: "preserve preferred affinity",
			pod: NewPod().WithNodeSelectorTermsMatchExpressions(
				[]corev1.NodeSelectorRequirement{
					{
						Key:      utils.ArchLabel,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"amd64"},
					},
				},
			).WithPreferredDuringSchedulingIgnoredDuringExecution(
				&corev1.PreferredSchedulingTerm{
					Weight: 50,
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
			).Build(),
			expectedRemove: true,
			checkNil:       false,
		},
		{
			name:           "nil affinity",
			pod:            NewPod().Build(),
			expectedRemove: false,
			checkNil:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			removed := removeArchitectureFromNodeAffinity(tt.pod)
			if removed != tt.expectedRemove {
				t.Errorf("Expected removed=%v, got %v", tt.expectedRemove, removed)
			}

			// Verify arch expressions were removed from required affinity
			if tt.pod.Spec.Affinity != nil && tt.pod.Spec.Affinity.NodeAffinity != nil {
				if tt.pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
					for _, term := range tt.pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
						for _, expr := range term.MatchExpressions {
							if expr.Key == utils.ArchLabel {
								t.Error("Architecture expression still exists in required affinity")
							}
						}
					}
				}

				// Verify preferred affinity was preserved
				if tt.name == "preserve preferred affinity" {
					if tt.pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution == nil {
						t.Error("Preferred affinity was removed but should be preserved")
					}
				}
			}

			// Check if structures were properly nil'd out
			if tt.checkNil {
				if tt.pod.Spec.Affinity != nil {
					if tt.pod.Spec.Affinity.NodeAffinity != nil {
						if tt.pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
							if len(tt.pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms) > 0 {
								t.Error("Expected empty node selector terms after cleanup")
							}
						}
					}
				}
			}
		})
	}
}

func TestRemoveAllArchitectureConstraints(t *testing.T) {
	tests := []struct {
		name           string
		pod            *corev1.Pod
		expectedRemove bool
	}{
		{
			name: "remove from both nodeSelector and nodeAffinity",
			pod: NewPod().WithNodeSelectors(utils.ArchLabel, "amd64").WithNodeSelectorTermsMatchExpressions(
				[]corev1.NodeSelectorRequirement{
					{
						Key:      utils.ArchLabel,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"amd64"},
					},
				},
			).Build(),
			expectedRemove: true,
		},
		{
			name:           "remove from nodeSelector only",
			pod:            NewPod().WithNodeSelectors(utils.ArchLabel, "amd64").Build(),
			expectedRemove: true,
		},
		{
			name: "remove from nodeAffinity only",
			pod: NewPod().WithNodeSelectorTermsMatchExpressions(
				[]corev1.NodeSelectorRequirement{
					{
						Key:      utils.ArchLabel,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"amd64"},
					},
				},
			).Build(),
			expectedRemove: true,
		},
		{
			name:           "no architecture constraints",
			pod:            NewPod().WithNodeSelectors("other-label", "value").Build(),
			expectedRemove: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			removed := removeAllArchitectureConstraints(tt.pod)
			if removed != tt.expectedRemove {
				t.Errorf("Expected removed=%v, got %v", tt.expectedRemove, removed)
			}

			// Verify all architecture constraints were removed
			if tt.pod.Spec.NodeSelector != nil {
				if _, exists := tt.pod.Spec.NodeSelector[utils.ArchLabel]; exists {
					t.Error("Architecture label still exists in nodeSelector")
				}
			}

			if tt.pod.Spec.Affinity != nil && tt.pod.Spec.Affinity.NodeAffinity != nil {
				if tt.pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
					for _, term := range tt.pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
						for _, expr := range term.MatchExpressions {
							if expr.Key == utils.ArchLabel {
								t.Error("Architecture expression still exists in required affinity")
							}
						}
					}
				}
			}
		})
	}
}

func TestRemoveArchitectureFromNodeAffinityEmptyTermCleanup(t *testing.T) {
	pod := NewPod().WithName("test-pod").WithNodeSelectorTermsMatchExpressions(
		[]corev1.NodeSelectorRequirement{
			{
				Key:      utils.ArchLabel,
				Operator: corev1.NodeSelectorOpIn,
				Values:   []string{"amd64"},
			},
		},
	).Build()

	removed := removeArchitectureFromNodeAffinity(pod)
	if !removed {
		t.Error("Expected architecture to be removed")
	}

	// Verify empty term was cleaned up - the entire RequiredDuringSchedulingIgnoredDuringExecution should be nil
	if pod.Spec.Affinity != nil && pod.Spec.Affinity.NodeAffinity != nil &&
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
		t.Error("Expected RequiredDuringSchedulingIgnoredDuringExecution to be nil after removing all terms")
	}
}
