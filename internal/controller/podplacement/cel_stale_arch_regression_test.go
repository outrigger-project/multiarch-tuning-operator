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

// TestApplyArchitectureConstraintsRemovesStaleArchitectureValues reproduces the
// field symptom seen in CPD where a pod intended for ppc64le still ended up with
// a required architecture set containing amd64, ppc64le, and s390x.
// This test verifies the low-level mutation helper fully removes stale required
// architecture constraints before applying the new CEL-selected architecture.
func TestApplyArchitectureConstraintsRemovesStaleArchitectureValues(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ibm-lh-lakehouse-ces-0",
			Namespace: "cpd-instance",
		},
		Spec: corev1.PodSpec{
			Affinity: &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								// Simulates the stale architecture-only term observed in the field.
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
								// Simulates unrelated scheduling intent that must be preserved.
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
			},
		},
	}

	changed := applyArchitectureConstraints(pod, []string{utils.ArchitecturePpc64le})
	if !changed {
		t.Fatal("expected architecture constraints application to report a change")
	}

	if pod.Spec.Affinity == nil ||
		pod.Spec.Affinity.NodeAffinity == nil ||
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		t.Fatal("expected required node affinity to exist after applying architecture constraints")
	}

	terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 1 {
		t.Fatalf("expected exactly 1 merged required node selector term, got %d", len(terms))
	}

	var (
		foundZoneConstraint bool
		archExpressions     []corev1.NodeSelectorRequirement
	)

	for _, expr := range terms[0].MatchExpressions {
		switch expr.Key {
		case "topology.kubernetes.io/zone":
			foundZoneConstraint = true
			if len(expr.Values) != 1 || expr.Values[0] != "us-east-1a" {
				t.Fatalf("zone constraint was modified unexpectedly: %#v", expr)
			}
		case utils.ArchLabel:
			archExpressions = append(archExpressions, expr)
		}
	}

	if !foundZoneConstraint {
		t.Fatal("expected non-architecture zone constraint to be preserved")
	}

	if len(archExpressions) != 1 {
		t.Fatalf("expected exactly 1 architecture expression after cleanup and reapply, got %d", len(archExpressions))
	}

	archExpr := archExpressions[0]
	if archExpr.Operator != corev1.NodeSelectorOpIn {
		t.Fatalf("expected architecture operator %q, got %q", corev1.NodeSelectorOpIn, archExpr.Operator)
	}

	if len(archExpr.Values) != 1 || archExpr.Values[0] != utils.ArchitecturePpc64le {
		t.Fatalf("expected stale architectures to be removed and replaced with only %q, got %v",
			utils.ArchitecturePpc64le, archExpr.Values)
	}

	for _, staleArch := range []string{utils.ArchitectureAmd64, utils.ArchitectureS390x} {
		for _, actual := range archExpr.Values {
			if actual == staleArch {
				t.Fatalf("stale architecture %q was preserved unexpectedly in final required affinity: %v",
					staleArch, archExpr.Values)
			}
		}
	}
}

// TestApplyArchitectureConstraintsReplacesBroadFallbackWithMatchedRule verifies
// the exact CEL expectation for a matched rule: once CEL selects a specific
// architecture, previously broad required architecture values must not survive.
func TestApplyArchitectureConstraintsReplacesBroadFallbackWithMatchedRule(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lhconsole-api-v3-76575f8566-bbnvb",
			Namespace: "cpd-instance",
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				utils.ArchLabel: utils.ArchitectureAmd64,
			},
			Affinity: &corev1.Affinity{
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
			},
		},
	}

	removed := removeAllArchitectureConstraints(pod)
	if !removed {
		t.Fatal("expected stale architecture constraints to be removed")
	}

	if _, exists := pod.Spec.NodeSelector[utils.ArchLabel]; exists {
		t.Fatalf("expected nodeSelector architecture key %q to be removed", utils.ArchLabel)
	}

	terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 1 {
		t.Fatalf("expected one preserved non-architecture term after cleanup, got %d", len(terms))
	}

	for _, expr := range terms[0].MatchExpressions {
		if expr.Key == utils.ArchLabel {
			t.Fatalf("expected no architecture expressions after cleanup, found %#v", expr)
		}
	}

	applyArchitectureNodeAffinity(pod, []string{utils.ArchitecturePpc64le})

	terms = pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 1 {
		t.Fatalf("expected one merged required term after reapply, got %d", len(terms))
	}

	var foundExclusivePPC64LE bool
	var foundOSConstraint bool
	for _, expr := range terms[0].MatchExpressions {
		switch expr.Key {
		case utils.ArchLabel:
			if len(expr.Values) == 1 && expr.Values[0] == utils.ArchitecturePpc64le {
				foundExclusivePPC64LE = true
			} else {
				t.Fatalf("expected final architecture expression to be exclusive to %q, got %v",
					utils.ArchitecturePpc64le, expr.Values)
			}
		case "kubernetes.io/os":
			if len(expr.Values) == 1 && expr.Values[0] == "linux" {
				foundOSConstraint = true
			}
		}
	}

	if !foundExclusivePPC64LE {
		t.Fatalf("expected to find an exclusive %q architecture expression after reapply", utils.ArchitecturePpc64le)
	}
	if !foundOSConstraint {
		t.Fatal("expected existing os constraint to be preserved in merged required term")
	}
}

// Made with Bob
