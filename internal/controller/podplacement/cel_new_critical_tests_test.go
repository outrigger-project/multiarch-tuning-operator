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

// Package podplacement – critical missing test scenarios:
//
//  1. Plugin disabled (Enabled: false) — no mutation must occur.
//  2. Affinity-ordering assertions — unrelated terms keep their exact position.
//  3. Metadata preservation — labels, annotations, ownerReferences, and
//     finalizers must survive applyArchitectureConstraints unchanged.
//  4. Scheduling-gate idempotency — ensureSchedulingGate on a pod that already
//     carries the gate must not add a duplicate.
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

// ── 1. Plugin disabled ────────────────────────────────────────────────────────

// TestApplyCELInWebhook_PluginDisabled_NoModification verifies that a
// PodPlacementConfig whose CelArchitecturePlacement plugin is explicitly
// disabled (Enabled: false) does not modify the pod's affinity or nodeSelector.
func TestApplyCELInWebhook_PluginDisabled_NoModification(t *testing.T) {
	ctx := context.Background()
	recorder := record.NewFakeRecorder(8)

	original := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "c", Image: "nginx:latest"}},
		},
	}

	ppcs := []v1beta1.PodPlacementConfig{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "disabled-cel-ppc", Namespace: "default"},
			Spec: v1beta1.PodPlacementConfigSpec{
				Priority: 100,
				Plugins: &plugins.LocalPlugins{
					CelArchitecturePlacement: &plugins.CelArchitecturePlacement{
						// Plugin explicitly disabled.
						BasePlugin:            plugins.BasePlugin{Enabled: false},
						FallbackArchitectures: []string{utils.ArchitecturePpc64le},
						Rules: []plugins.ArchitectureRule{
							{
								Name:          "always-true",
								Expression:    `true`,
								Architectures: []string{utils.ArchitecturePpc64le},
							},
						},
					},
				},
			},
		},
	}

	wh := &PodSchedulingGateMutatingWebHook{}
	pod := newPod(original, ctx, recorder)
	wh.applyCELInWebhook(ctx, pod, ppcs)

	// Affinity must be nil — the disabled plugin must not have set anything.
	if pod.Spec.Affinity != nil {
		t.Errorf("expected pod.Spec.Affinity to be nil when CEL plugin is disabled, got: %+v", pod.Spec.Affinity)
	}
	if pod.Spec.NodeSelector != nil {
		if _, exists := pod.Spec.NodeSelector[utils.ArchLabel]; exists {
			t.Errorf("expected no arch nodeSelector when CEL plugin is disabled")
		}
	}
}

// TestApplyCELInWebhook_PluginDisabled_ExistingAffinityUnchanged verifies that
// a pod with user-defined node affinity is not touched when the plugin is off.
func TestApplyCELInWebhook_PluginDisabled_ExistingAffinityUnchanged(t *testing.T) {
	ctx := context.Background()
	recorder := record.NewFakeRecorder(8)

	existingTerm := corev1.NodeSelectorTerm{
		MatchExpressions: []corev1.NodeSelectorRequirement{
			{
				Key:      "topology.kubernetes.io/zone",
				Operator: corev1.NodeSelectorOpIn,
				Values:   []string{"us-east-1a"},
			},
		},
	}

	original := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-with-affinity", Namespace: "default"},
		Spec: corev1.PodSpec{
			Affinity: &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{existingTerm},
					},
				},
			},
		},
	}

	ppcs := []v1beta1.PodPlacementConfig{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "disabled-ppc", Namespace: "default"},
			Spec: v1beta1.PodPlacementConfigSpec{
				Plugins: &plugins.LocalPlugins{
					CelArchitecturePlacement: &plugins.CelArchitecturePlacement{
						BasePlugin:            plugins.BasePlugin{Enabled: false},
						FallbackArchitectures: []string{utils.ArchitecturePpc64le},
					},
				},
			},
		},
	}

	wh := &PodSchedulingGateMutatingWebHook{}
	pod := newPod(original, ctx, recorder)
	wh.applyCELInWebhook(ctx, pod, ppcs)

	// The existing zone term must be untouched.
	if pod.Spec.Affinity == nil ||
		pod.Spec.Affinity.NodeAffinity == nil ||
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		t.Fatal("pod affinity should not have been removed by a disabled CEL plugin")
	}
	terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 1 {
		t.Fatalf("expected 1 term, got %d", len(terms))
	}
	if len(terms[0].MatchExpressions) != 1 || terms[0].MatchExpressions[0].Key != "topology.kubernetes.io/zone" {
		t.Errorf("existing affinity term was modified by disabled plugin: %+v", terms[0])
	}
}

// ── 2. Affinity-ordering assertions ──────────────────────────────────────────

// TestApplyArchitectureConstraints_TermOrderPreserved verifies that
// applyArchitectureConstraints does not change the relative order of
// NodeSelectorTerms that it updates in-place.
//
// Given three terms [T1-zone, T2-instance-type, T3-os] all containing an
// existing arch expression, after applying new arch constraints the order
// must remain T1, T2, T3 and no term must be dropped.
func TestApplyArchitectureConstraints_TermOrderPreserved(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "ordered-pod"},
		Spec: corev1.PodSpec{
			Affinity: &corev1.Affinity{
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
									{Key: "kubernetes.io/os", Operator: corev1.NodeSelectorOpIn, Values: []string{"linux"}},
									{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"amd64"}},
								},
							},
						},
					},
				},
			},
		},
	}

	applyArchitectureConstraints(pod, []string{utils.ArchitecturePpc64le})

	terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 3 {
		t.Fatalf("expected 3 terms after in-place update, got %d", len(terms))
	}

	// Verify positional identity: each term must retain its original non-arch key.
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
		if !found {
			t.Errorf("term[%d] lost its non-arch key %q after applyArchitectureConstraints", i, key)
		}
	}

	// Verify each term now has the new arch value.
	for i, term := range terms {
		archFound := false
		for _, expr := range term.MatchExpressions {
			if expr.Key == utils.ArchLabel {
				archFound = true
				if len(expr.Values) != 1 || expr.Values[0] != utils.ArchitecturePpc64le {
					t.Errorf("term[%d] arch value = %v, want [ppc64le]", i, expr.Values)
				}
			}
		}
		if !archFound {
			t.Errorf("term[%d] is missing arch expression after applyArchitectureConstraints", i)
		}
	}
}

// TestRemoveArchitectureFromNodeAffinity_MatchExpressionsOrderPreserved checks
// that non-arch MatchExpressions remain in the same relative order after
// removeArchitectureFromNodeAffinity removes the arch expression from a term.
func TestRemoveArchitectureFromNodeAffinity_MatchExpressionsOrderPreserved(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "order-test"},
		Spec: corev1.PodSpec{
			Affinity: &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{Key: "alpha", Operator: corev1.NodeSelectorOpIn, Values: []string{"1"}},
									{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"amd64"}},
									{Key: "beta", Operator: corev1.NodeSelectorOpIn, Values: []string{"2"}},
									{Key: "gamma", Operator: corev1.NodeSelectorOpIn, Values: []string{"3"}},
								},
							},
						},
					},
				},
			},
		},
	}

	removeArchitectureFromNodeAffinity(pod)

	if pod.Spec.Affinity == nil || pod.Spec.Affinity.NodeAffinity == nil ||
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		t.Fatal("required affinity should not be nil after removing arch from a term with other keys")
	}

	terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 1 {
		t.Fatalf("expected 1 term, got %d", len(terms))
	}

	// The three non-arch expressions must remain in their original relative order.
	exprs := terms[0].MatchExpressions
	if len(exprs) != 3 {
		t.Fatalf("expected 3 MatchExpressions after arch removal, got %d: %v", len(exprs), exprs)
	}
	expectedOrder := []string{"alpha", "beta", "gamma"}
	for i, want := range expectedOrder {
		if exprs[i].Key != want {
			t.Errorf("MatchExpressions[%d].Key = %q, want %q (order changed)", i, exprs[i].Key, want)
		}
	}
}

// TestApplyArchitectureConstraints_MatchFieldsPositionUnchanged verifies that
// MatchFields entries are not reordered or removed during an in-place update.
func TestApplyArchitectureConstraints_MatchFieldsPositionUnchanged(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "matchfields-pod"},
		Spec: corev1.PodSpec{
			Affinity: &corev1.Affinity{
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
			},
		},
	}

	applyArchitectureConstraints(pod, []string{utils.ArchitecturePpc64le})

	terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 1 {
		t.Fatalf("expected 1 term, got %d", len(terms))
	}
	if len(terms[0].MatchFields) != 1 {
		t.Fatalf("expected 1 MatchFields entry, got %d", len(terms[0].MatchFields))
	}
	if terms[0].MatchFields[0].Key != "metadata.name" {
		t.Errorf("MatchFields[0].Key changed: got %q", terms[0].MatchFields[0].Key)
	}
	if len(terms[0].MatchFields[0].Values) != 2 {
		t.Errorf("MatchFields[0].Values changed: got %v", terms[0].MatchFields[0].Values)
	}
}

// ── 3. Metadata preservation ─────────────────────────────────────────────────

// TestApplyArchitectureConstraints_LabelsAndAnnotationsPreserved verifies that
// a pod's labels and annotations are completely unchanged after
// applyArchitectureConstraints runs.
func TestApplyArchitectureConstraints_LabelsAndAnnotationsPreserved(t *testing.T) {
	originalLabels := map[string]string{
		"app":        "database",
		"tier":       "backend",
		"managed-by": "helm",
		"version":    "1.2.3",
	}
	originalAnnotations := map[string]string{
		"kubectl.kubernetes.io/last-applied-configuration": `{"some":"json"}`,
		"custom-annotation": "custom-value",
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "metadata-pod",
			Namespace:   "prod",
			Labels:      copyStringMap(originalLabels),
			Annotations: copyStringMap(originalAnnotations),
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				utils.ArchLabel: "amd64",
				"zone":          "us-east-1",
			},
		},
	}

	applyArchitectureConstraints(pod, []string{utils.ArchitecturePpc64le})

	// Labels must be unchanged.
	if len(pod.Labels) != len(originalLabels) {
		t.Errorf("label count changed: want %d, got %d — labels=%v", len(originalLabels), len(pod.Labels), pod.Labels)
	}
	for k, wantV := range originalLabels {
		if gotV := pod.Labels[k]; gotV != wantV {
			t.Errorf("label %q: want %q, got %q", k, wantV, gotV)
		}
	}

	// Annotations must be unchanged.
	if len(pod.Annotations) != len(originalAnnotations) {
		t.Errorf("annotation count changed: want %d, got %d", len(originalAnnotations), len(pod.Annotations))
	}
	for k, wantV := range originalAnnotations {
		if gotV := pod.Annotations[k]; gotV != wantV {
			t.Errorf("annotation %q: want %q, got %q", k, wantV, gotV)
		}
	}

	// The non-arch nodeSelector key must survive.
	if pod.Spec.NodeSelector["zone"] != "us-east-1" {
		t.Errorf("non-arch nodeSelector key 'zone' was modified or removed")
	}
}

// TestApplyArchitectureConstraints_OwnerReferencesPreserved verifies that
// OwnerReferences are untouched after applyArchitectureConstraints.
func TestApplyArchitectureConstraints_OwnerReferencesPreserved(t *testing.T) {
	truePtr := true
	ownerRef := metav1.OwnerReference{
		APIVersion:         "apps/v1",
		Kind:               "Deployment",
		Name:               "my-deploy",
		UID:                "uid-12345",
		Controller:         &truePtr,
		BlockOwnerDeletion: &truePtr,
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "owned-pod",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
	}

	applyArchitectureConstraints(pod, []string{utils.ArchitecturePpc64le})

	if len(pod.OwnerReferences) != 1 {
		t.Fatalf("OwnerReferences count changed: want 1, got %d", len(pod.OwnerReferences))
	}
	got := pod.OwnerReferences[0]
	if got.Name != ownerRef.Name || got.UID != ownerRef.UID || got.Kind != ownerRef.Kind {
		t.Errorf("OwnerReference modified: got %+v", got)
	}
}

// TestApplyArchitectureConstraints_FinalizersPreserved verifies that finalizers
// are untouched after applyArchitectureConstraints.
func TestApplyArchitectureConstraints_FinalizersPreserved(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "finalized-pod",
			Namespace:  "default",
			Finalizers: []string{"example.com/my-finalizer", "storage.kubernetes.io/finalizer"},
		},
	}

	applyArchitectureConstraints(pod, []string{utils.ArchitecturePpc64le})

	if len(pod.Finalizers) != 2 {
		t.Errorf("Finalizers count changed: want 2, got %d — finalizers=%v", len(pod.Finalizers), pod.Finalizers)
	}
	for i, f := range []string{"example.com/my-finalizer", "storage.kubernetes.io/finalizer"} {
		if pod.Finalizers[i] != f {
			t.Errorf("Finalizers[%d] changed: want %q, got %q", i, f, pod.Finalizers[i])
		}
	}
}

// ── 4. Scheduling-gate idempotency ───────────────────────────────────────────

// TestEnsureSchedulingGate_NoDuplicateWhenAlreadyPresent verifies that calling
// ensureSchedulingGate on a pod that already carries the MTO scheduling gate
// does not add a second copy of it.
func TestEnsureSchedulingGate_NoDuplicateWhenAlreadyPresent(t *testing.T) {
	ctx := context.Background()
	recorder := record.NewFakeRecorder(8)

	// Build a pod that already has the scheduling gate.
	raw := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pre-gated", Namespace: "default"},
		Spec: corev1.PodSpec{
			SchedulingGates: []corev1.PodSchedulingGate{
				{Name: utils.SchedulingGateName},
			},
		},
	}

	pod := newPod(raw, ctx, recorder)

	// Call ensureSchedulingGate (the private method used by Handle()).
	pod.ensureSchedulingGate()

	count := 0
	for _, g := range pod.Spec.SchedulingGates {
		if g.Name == utils.SchedulingGateName {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 instance of scheduling gate %q, got %d — gates=%v",
			utils.SchedulingGateName, count, pod.Spec.SchedulingGates)
	}
}

// TestEnsureSchedulingGate_AddedWhenAbsent verifies that ensureSchedulingGate
// adds the gate when the pod does not yet have it.
func TestEnsureSchedulingGate_AddedWhenAbsent(t *testing.T) {
	ctx := context.Background()
	recorder := record.NewFakeRecorder(8)

	raw := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "ungated", Namespace: "default"},
		Spec:       corev1.PodSpec{},
	}
	pod := newPod(raw, ctx, recorder)

	pod.ensureSchedulingGate()

	found := false
	for _, g := range pod.Spec.SchedulingGates {
		if g.Name == utils.SchedulingGateName {
			found = true
		}
	}
	if !found {
		t.Errorf("expected scheduling gate %q to be added, got gates=%v",
			utils.SchedulingGateName, pod.Spec.SchedulingGates)
	}
}

// TestEnsureSchedulingGate_ExistingOtherGatesUnchanged verifies that
// ensureSchedulingGate preserves pre-existing gates from other controllers.
func TestEnsureSchedulingGate_ExistingOtherGatesUnchanged(t *testing.T) {
	ctx := context.Background()
	recorder := record.NewFakeRecorder(8)

	raw := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "multi-gated", Namespace: "default"},
		Spec: corev1.PodSpec{
			SchedulingGates: []corev1.PodSchedulingGate{
				{Name: "other-controller.example.com/my-gate"},
			},
		},
	}
	pod := newPod(raw, ctx, recorder)

	pod.ensureSchedulingGate()

	if len(pod.Spec.SchedulingGates) < 2 {
		t.Fatalf("expected at least 2 gates after ensureSchedulingGate, got %v", pod.Spec.SchedulingGates)
	}

	mtoGateFound := false
	otherGateFound := false
	for _, g := range pod.Spec.SchedulingGates {
		switch g.Name {
		case utils.SchedulingGateName:
			mtoGateFound = true
		case "other-controller.example.com/my-gate":
			otherGateFound = true
		}
	}
	if !mtoGateFound {
		t.Errorf("MTO gate not found after ensureSchedulingGate; gates=%v", pod.Spec.SchedulingGates)
	}
	if !otherGateFound {
		t.Errorf("pre-existing gate was removed by ensureSchedulingGate; gates=%v", pod.Spec.SchedulingGates)
	}
}

// TestApplyCELInWebhook_PluginDisabled_GateStillAdded verifies the complete
// webhook path: even when the CEL plugin is disabled, the scheduling gate
// addition (separate from architecture application) is not blocked.
// This test calls applyCELInWebhook directly and then checks the pod's gate
// separately by calling ensureSchedulingGate — confirming the two operations
// are independent.
func TestApplyCELInWebhook_PluginDisabled_GateStillAdded(t *testing.T) {
	ctx := context.Background()
	recorder := record.NewFakeRecorder(8)

	raw := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "disabled-gate-test", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "c", Image: "nginx:latest"}},
		},
	}
	pod := newPod(raw, ctx, recorder)

	ppcs := []v1beta1.PodPlacementConfig{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "disabled-ppc", Namespace: "default"},
			Spec: v1beta1.PodPlacementConfigSpec{
				Plugins: &plugins.LocalPlugins{
					CelArchitecturePlacement: &plugins.CelArchitecturePlacement{
						BasePlugin:            plugins.BasePlugin{Enabled: false},
						FallbackArchitectures: []string{utils.ArchitecturePpc64le},
					},
				},
			},
		},
	}

	// CEL step (disabled) must not touch affinity.
	wh := &PodSchedulingGateMutatingWebHook{}
	wh.applyCELInWebhook(ctx, pod, ppcs)

	if pod.Spec.Affinity != nil {
		t.Errorf("disabled CEL plugin must not set affinity; got: %+v", pod.Spec.Affinity)
	}

	// Gate step (independent of CEL) must still work.
	pod.ensureSchedulingGate()
	if !pod.HasSchedulingGate() {
		t.Errorf("scheduling gate should be present even when CEL plugin is disabled")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func copyStringMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
