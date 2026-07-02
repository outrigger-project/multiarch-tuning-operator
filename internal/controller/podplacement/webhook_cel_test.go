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

// TestApplyCELInWebhook_RemovesNodeSelectorArchBeforeAdmission verifies that
// applyCELInWebhook removes an existing kubernetes.io/arch nodeSelector entry
// and replaces it with a NodeAffinity requirement, before the pod is persisted.
// This is the exact mutation that prevents the KEP-3838 immutability rejection
// for the OPENSHIFTP-636 fix: when the webhook applies constraints to an
// already-gated pod, the nodeSelector arch key must be removed and the
// NodeAffinity must be set — all in a single admission patch.
func TestApplyCELInWebhook_RemovesNodeSelectorArchBeforeAdmission(t *testing.T) {
	ctx := context.Background()
	recorder := record.NewFakeRecorder(10)

	// Pod arrives with an existing kubernetes.io/arch nodeSelector.
	// This simulates a re-created StatefulSet pod or a pod submitted with a
	// pre-set arch nodeSelector.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-with-nodeselector",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				utils.ArchLabel: "amd64",
				"other-key":     "other-value",
			},
		},
	}

	matchingPPCs := []v1beta1.PodPlacementConfig{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ppc-cel",
				Namespace: "default",
			},
			Spec: v1beta1.PodPlacementConfigSpec{
				Priority: 100,
				Plugins: &plugins.LocalPlugins{
					CelArchitecturePlacement: &plugins.CelArchitecturePlacement{
						BasePlugin:            plugins.BasePlugin{Enabled: true},
						FallbackArchitectures: []string{"ppc64le"},
					},
				},
			},
		},
	}

	webhook := &PodSchedulingGateMutatingWebHook{}
	podWrapper := newPod(pod, ctx, recorder)

	webhook.applyCELInWebhook(ctx, podWrapper, matchingPPCs)

	// The kubernetes.io/arch nodeSelector key must have been removed.
	if _, exists := podWrapper.Spec.NodeSelector[utils.ArchLabel]; exists {
		t.Error("kubernetes.io/arch should have been removed from nodeSelector by the webhook")
	}

	// Non-arch nodeSelector keys must be preserved.
	if podWrapper.Spec.NodeSelector["other-key"] != "other-value" {
		t.Error("non-arch nodeSelector key must be preserved")
	}

	// A NodeAffinity requirement for the selected architecture must now exist.
	if podWrapper.Spec.Affinity == nil ||
		podWrapper.Spec.Affinity.NodeAffinity == nil ||
		podWrapper.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		t.Fatal("Expected NodeAffinity to be set by applyCELInWebhook")
	}
	terms := podWrapper.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) == 0 {
		t.Fatal("Expected at least one NodeSelectorTerm after applyCELInWebhook")
	}
	found := false
	for _, term := range terms {
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
		t.Error("Architecture requirement not found in NodeAffinity after applyCELInWebhook")
	}
}

// TestApplyCELInWebhook_ControllerIdempotencyAfterWebhookMutation verifies that
// re-applying the same CEL constraints via the controller path (cel_integration.go)
// on a pod that was already mutated by the webhook does not change the
// NodeSelectorTerms array length.
//
// This is the key idempotency guarantee required by OPENSHIFTP-636: the controller
// must not attempt to add or remove NodeSelectorTerms from a pod that was already
// correctly mutated by the webhook, because Kubernetes would reject such updates
// with "no additions/deletions to non-empty NodeSelectorTerms list are allowed".
func TestApplyCELInWebhook_ControllerIdempotencyAfterWebhookMutation(t *testing.T) {
	ctx := context.Background()
	recorder := record.NewFakeRecorder(10)

	// Construct a pod that represents the state after the webhook has run:
	// - no kubernetes.io/arch nodeSelector (removed by webhook)
	// - NodeAffinity with arch constraint and a non-arch constraint already set
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "post-webhook-pod",
			Namespace: "default",
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
										Values:   []string{"ppc64le"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	matchingPPCs := []v1beta1.PodPlacementConfig{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ppc-cel",
				Namespace: "default",
			},
			Spec: v1beta1.PodPlacementConfigSpec{
				Priority: 100,
				Plugins: &plugins.LocalPlugins{
					CelArchitecturePlacement: &plugins.CelArchitecturePlacement{
						BasePlugin:            plugins.BasePlugin{Enabled: true},
						FallbackArchitectures: []string{"ppc64le"},
					},
				},
			},
		},
	}

	// Capture the term count before the controller re-applies CEL.
	originalTermCount := len(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms)

	// Simulate the controller path: re-apply the same CEL constraints.
	// This calls applyArchitectureConstraints via the same code path used by
	// cel_integration.go's applyCELArchitecturePlacement.
	webhook := &PodSchedulingGateMutatingWebHook{}
	podWrapper := newPod(pod, ctx, recorder)
	webhook.applyCELInWebhook(ctx, podWrapper, matchingPPCs)

	// The NodeSelectorTerms length must not change.
	// A length change on a gated pod would cause Kubernetes to reject the update:
	// "no additions/deletions to non-empty NodeSelectorTerms list are allowed"
	finalTermCount := len(podWrapper.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms)
	if finalTermCount != originalTermCount {
		t.Errorf("NodeSelectorTerms count changed from %d to %d — "+
			"this would cause Kubernetes to reject the controller update with HTTP 422 "+
			"(KEP-3838 immutability constraint)", originalTermCount, finalTermCount)
	}

	// The non-arch constraint must still be present.
	zoneFound := false
	for _, term := range podWrapper.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
		for _, expr := range term.MatchExpressions {
			if expr.Key == "topology.kubernetes.io/zone" {
				zoneFound = true
			}
		}
	}
	if !zoneFound {
		t.Error("non-architecture zone constraint was removed — must be preserved")
	}

	// The architecture constraint must still point to ppc64le.
	archFound := false
	for _, term := range podWrapper.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
		for _, expr := range term.MatchExpressions {
			if expr.Key == utils.ArchLabel {
				archFound = true
				if len(expr.Values) != 1 || expr.Values[0] != "ppc64le" {
					t.Errorf("Expected architecture [ppc64le], got %v", expr.Values)
				}
			}
		}
	}
	if !archFound {
		t.Error("Architecture constraint not found after controller re-application")
	}
}
