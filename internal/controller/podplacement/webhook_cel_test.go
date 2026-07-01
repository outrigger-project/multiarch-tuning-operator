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
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	"github.com/openshift/multiarch-tuning-operator/api/common/plugins"
	"github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

// TestApplyCELInWebhook_AppliesArchitecturesBeforePersistence verifies that
// applyCELInWebhook applies architecture constraints to the pod object before
// it is persisted to the API server.
func TestApplyCELInWebhook_AppliesArchitecturesBeforePersistence(t *testing.T) {
	ctx := context.Background()
	recorder := record.NewFakeRecorder(10)

	tests := []struct {
		name                  string
		pod                   *corev1.Pod
		matchingPPCs          []v1beta1.PodPlacementConfig
		expectedArchitectures []string
		expectModified        bool
	}{
		{
			name: "applies CEL architecture from highest priority PPC",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
			},
			matchingPPCs: []v1beta1.PodPlacementConfig{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ppc-high-priority",
						Namespace: "default",
					},
					Spec: v1beta1.PodPlacementConfigSpec{
						Priority: 100,
						Plugins: &plugins.LocalPlugins{
							CelArchitecturePlacement: &plugins.CelArchitecturePlacement{
								BasePlugin: plugins.BasePlugin{
									Enabled: true,
								},
								FallbackArchitectures: []string{"amd64"},
								Rules: []plugins.ArchitectureRule{
									{
										Name:          "test-rule",
										Expression:    "self.metadata.name == 'test-pod'",
										Architectures: []string{"ppc64le"},
									},
								},
							},
						},
					},
				},
			},
			expectedArchitectures: []string{"ppc64le"},
			expectModified:        true,
		},
		{
			name: "no modification when no CEL plugin enabled",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
			},
			matchingPPCs: []v1beta1.PodPlacementConfig{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ppc-no-cel",
						Namespace: "default",
					},
					Spec: v1beta1.PodPlacementConfigSpec{
						Priority: 100,
						Plugins: &plugins.LocalPlugins{
							NodeAffinityScoring: &plugins.NodeAffinityScoring{
								BasePlugin: plugins.BasePlugin{
									Enabled: true,
								},
							},
						},
					},
				},
			},
			expectedArchitectures: nil,
			expectModified:        false,
		},
		{
			name: "applies fallback when no rules match",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-pod",
					Namespace: "default",
				},
			},
			matchingPPCs: []v1beta1.PodPlacementConfig{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ppc-with-fallback",
						Namespace: "default",
					},
					Spec: v1beta1.PodPlacementConfigSpec{
						Priority: 100,
						Plugins: &plugins.LocalPlugins{
							CelArchitecturePlacement: &plugins.CelArchitecturePlacement{
								BasePlugin: plugins.BasePlugin{
									Enabled: true,
								},
								FallbackArchitectures: []string{"amd64", "arm64"},
								Rules: []plugins.ArchitectureRule{
									{
										Name:          "test-rule",
										Expression:    "self.metadata.name == 'test-pod'",
										Architectures: []string{"ppc64le"},
									},
								},
							},
						},
					},
				},
			},
			expectedArchitectures: []string{"amd64", "arm64"},
			expectModified:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			webhook := &PodSchedulingGateMutatingWebHook{}
			pod := newPod(tt.pod, ctx, recorder)

			// Apply CEL in webhook
			webhook.applyCELInWebhook(ctx, pod, tt.matchingPPCs)

			if tt.expectModified {
				// Verify architecture constraints were applied
				if pod.Spec.Affinity == nil ||
					pod.Spec.Affinity.NodeAffinity == nil ||
					pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
					t.Fatal("Expected node affinity to be set")
				}

				found := false
				for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
					for _, expr := range term.MatchExpressions {
						if expr.Key == utils.ArchLabel && expr.Operator == corev1.NodeSelectorOpIn {
							found = true
							if len(expr.Values) != len(tt.expectedArchitectures) {
								t.Errorf("Expected %d architectures, got %d", len(tt.expectedArchitectures), len(expr.Values))
							}
							for i, arch := range tt.expectedArchitectures {
								if expr.Values[i] != arch {
									t.Errorf("Expected architecture %s at index %d, got %s", arch, i, expr.Values[i])
								}
							}
						}
					}
				}
				if !found {
					t.Error("Architecture requirement not found in node affinity")
				}
			} else {
				// Verify no modification occurred
				if pod.Spec.Affinity != nil &&
					pod.Spec.Affinity.NodeAffinity != nil &&
					pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
					for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
						for _, expr := range term.MatchExpressions {
							if expr.Key == utils.ArchLabel {
								t.Error("Unexpected architecture requirement found")
							}
						}
					}
				}
			}
		})
	}
}

// TestApplyCELInWebhook_MalformedPPCDoesNotBlockLowerPriority verifies that
// a malformed CEL PPC (e.g., with invalid CEL expression) does not prevent
// lower-priority CEL PPCs from being evaluated and applied.
func TestApplyCELInWebhook_MalformedPPCDoesNotBlockLowerPriority(t *testing.T) {
	ctx := context.Background()
	recorder := record.NewFakeRecorder(10)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	matchingPPCs := []v1beta1.PodPlacementConfig{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ppc-malformed-high-priority",
				Namespace: "default",
			},
			Spec: v1beta1.PodPlacementConfigSpec{
				Priority: 200,
				Plugins: &plugins.LocalPlugins{
					CelArchitecturePlacement: &plugins.CelArchitecturePlacement{
						BasePlugin: plugins.BasePlugin{
							Enabled: true,
						},
						FallbackArchitectures: []string{"amd64"},
						Rules: []plugins.ArchitectureRule{
							{
								Name:          "malformed-rule",
								Expression:    "self.metadata.name ==", // Invalid CEL expression
								Architectures: []string{"s390x"},
							},
						},
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ppc-valid-low-priority",
				Namespace: "default",
			},
			Spec: v1beta1.PodPlacementConfigSpec{
				Priority: 100,
				Plugins: &plugins.LocalPlugins{
					CelArchitecturePlacement: &plugins.CelArchitecturePlacement{
						BasePlugin: plugins.BasePlugin{
							Enabled: true,
						},
						FallbackArchitectures: []string{"amd64"},
						Rules: []plugins.ArchitectureRule{
							{
								Name:          "valid-rule",
								Expression:    "self.metadata.name == 'test-pod'",
								Architectures: []string{"ppc64le"},
							},
						},
					},
				},
			},
		},
	}

	webhook := &PodSchedulingGateMutatingWebHook{}
	podWrapper := newPod(pod, ctx, recorder)

	// Apply CEL in webhook
	webhook.applyCELInWebhook(ctx, podWrapper, matchingPPCs)

	// Verify that the lower-priority valid PPC was applied
	if podWrapper.Spec.Affinity == nil ||
		podWrapper.Spec.Affinity.NodeAffinity == nil ||
		podWrapper.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		t.Fatal("Expected node affinity to be set from lower-priority PPC")
	}

	found := false
	for _, term := range podWrapper.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
		for _, expr := range term.MatchExpressions {
			if expr.Key == utils.ArchLabel && expr.Operator == corev1.NodeSelectorOpIn {
				found = true
				if len(expr.Values) != 1 || expr.Values[0] != "ppc64le" {
					t.Errorf("Expected architecture [ppc64le], got %v", expr.Values)
				}
			}
		}
	}
	if !found {
		t.Error("Architecture requirement from lower-priority PPC not found")
	}
}

// TestApplyCELInWebhook_RespectsP PCPriority verifies that applyCELInWebhook
// correctly sorts PPCs by priority and applies only the highest priority PPC
// with CEL plugin enabled.
func TestApplyCELInWebhook_RespectsPPCPriority(t *testing.T) {
	ctx := context.Background()
	recorder := record.NewFakeRecorder(10)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	matchingPPCs := []v1beta1.PodPlacementConfig{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ppc-low-priority",
				Namespace: "default",
			},
			Spec: v1beta1.PodPlacementConfigSpec{
				Priority: 50,
				Plugins: &plugins.LocalPlugins{
					CelArchitecturePlacement: &plugins.CelArchitecturePlacement{
						BasePlugin: plugins.BasePlugin{
							Enabled: true,
						},
						FallbackArchitectures: []string{"arm64"},
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ppc-high-priority",
				Namespace: "default",
			},
			Spec: v1beta1.PodPlacementConfigSpec{
				Priority: 150,
				Plugins: &plugins.LocalPlugins{
					CelArchitecturePlacement: &plugins.CelArchitecturePlacement{
						BasePlugin: plugins.BasePlugin{
							Enabled: true,
						},
						FallbackArchitectures: []string{"ppc64le"},
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ppc-medium-priority",
				Namespace: "default",
			},
			Spec: v1beta1.PodPlacementConfigSpec{
				Priority: 100,
				Plugins: &plugins.LocalPlugins{
					CelArchitecturePlacement: &plugins.CelArchitecturePlacement{
						BasePlugin: plugins.BasePlugin{
							Enabled: true,
						},
						FallbackArchitectures: []string{"s390x"},
					},
				},
			},
		},
	}

	webhook := &PodSchedulingGateMutatingWebHook{}
	podWrapper := newPod(pod, ctx, recorder)

	// Apply CEL in webhook
	webhook.applyCELInWebhook(ctx, podWrapper, matchingPPCs)

	// Verify that only the highest priority PPC (priority 150) was applied
	if podWrapper.Spec.Affinity == nil ||
		podWrapper.Spec.Affinity.NodeAffinity == nil ||
		podWrapper.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		t.Fatal("Expected node affinity to be set")
	}

	found := false
	for _, term := range podWrapper.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
		for _, expr := range term.MatchExpressions {
			if expr.Key == utils.ArchLabel && expr.Operator == corev1.NodeSelectorOpIn {
				found = true
				if len(expr.Values) != 1 || expr.Values[0] != "ppc64le" {
					t.Errorf("Expected architecture [ppc64le] from highest priority PPC, got %v", expr.Values)
				}
			}
		}
	}
	if !found {
		t.Error("Architecture requirement from highest priority PPC not found")
	}
}

// Made with Bob
