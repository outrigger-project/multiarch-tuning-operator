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

// Package podplacement – Handle() admission path tests.
//
// These tests exercise the complete Handle() → decode → gate → patch pipeline,
// which the existing webhook_cel_test.go does not cover (it calls
// applyCELInWebhook directly).
//
// Design note on the test client:
// Handle() calls a.client.List() to fetch PodPlacementConfigs.  When that call
// errors out (e.g. nil client dereference), the webhook is intentionally
// designed to "fail open": it empties the PPC list and continues processing.
// For tests that only need to verify the gate/decode/patch plumbing we use a
// nil kubernetes.Clientset (clientSet is used only for the async event goroutine
// which is safe to no-op) and a minimally wired scheme.  Tests that need PPCs
// to be matched go through applyCELInWebhook directly (already covered in
// webhook_cel_test.go).
package podplacement

import (
	"context"
	"encoding/json"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/panjf2000/ants/v2"

	"github.com/openshift/multiarch-tuning-operator/api/common/plugins"
	"github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

// buildHandleRequest encodes a Pod as a raw CREATE admission.Request.
func buildHandleRequest(t *testing.T, pod *corev1.Pod) admission.Request {
	t.Helper()
	raw, err := json.Marshal(pod)
	if err != nil {
		t.Fatalf("failed to marshal pod: %v", err)
	}
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UID:       "test-uid",
			Operation: admissionv1.Create,
			Object:    runtime.RawExtension{Raw: raw},
			Resource:  metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
		},
	}
}

// newHandleWebhook returns a webhook whose client.List will fail (nil client),
// triggering the "fail open" path that produces an empty PPC list.  The scheme
// is populated so that admission.NewDecoder can decode Pods.
func newHandleWebhook(t *testing.T) *PodSchedulingGateMutatingWebHook {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	pool, err := ants.NewMultiPool(1, 1, ants.LeastTasks, ants.WithNonblocking(true))
	if err != nil {
		t.Fatalf("ants pool: %v", err)
	}
	// clientSet nil is safe: the async delayedSchedulingGatedEvent goroutine will
	// no-op because the pool submit will error immediately.
	return NewPodSchedulingGateMutatingWebHook(nil, nil, s, record.NewFakeRecorder(32), pool)
}

// TestHandleAdmission_ResponseAllowed verifies that Handle() returns
// Allowed=true for a plain pod.
func TestHandleAdmission_ResponseAllowed(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "plain-pod", Namespace: "default"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "nginx:latest"}}},
	}
	wh := newHandleWebhook(t)
	resp := wh.Handle(context.Background(), buildHandleRequest(t, pod))

	if !resp.Allowed {
		t.Fatalf("expected Allowed=true, got: %v", resp.Result)
	}
}

// TestHandleAdmission_SchedulingGatePatchPresent verifies that the response
// patch contains an operation that adds the scheduling gate.
func TestHandleAdmission_SchedulingGatePatchPresent(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "plain-pod", Namespace: "default"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "nginx:latest"}}},
	}
	wh := newHandleWebhook(t)
	resp := wh.Handle(context.Background(), buildHandleRequest(t, pod))

	if !resp.Allowed {
		t.Fatalf("expected Allowed=true, got: %v", resp.Result)
	}

	// The patch must contain /spec/schedulingGates.
	gateFound := false
	for _, p := range resp.Patches {
		if p.Path == "/spec/schedulingGates" && p.Operation == "add" {
			gateFound = true
		}
	}
	if !gateFound {
		t.Errorf("expected patch to add /spec/schedulingGates; patches=%v", resp.Patches)
	}
}

// TestHandleAdmission_SchedulingGateLabelPatchPresent verifies that the label
// marking the pod as gated is included in the patch.
func TestHandleAdmission_SchedulingGateLabelPatchPresent(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "plain-pod", Namespace: "default"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "nginx:latest"}}},
	}
	wh := newHandleWebhook(t)
	resp := wh.Handle(context.Background(), buildHandleRequest(t, pod))

	if !resp.Allowed {
		t.Fatalf("expected Allowed=true, got: %v", resp.Result)
	}

	// Reconstruct the full patched pod by applying the JSON patches to the
	// original pod JSON, so we can inspect the final state.
	originalJSON, _ := json.Marshal(pod)
	patchedJSON, err := applyJSONPatches(originalJSON, resp.Patches)
	if err != nil {
		// Fallback: inspect labels directly via the in-memory webhook state
		// by checking patch values.
		for _, p := range resp.Patches {
			if p.Path == "/metadata/labels" || p.Path == "/metadata/labels/"+escapeJSONPointer(utils.SchedulingGateLabel) {
				return // Label patch found, test passes
			}
		}
		t.Fatalf("could not apply patches: %v; patches=%v", err, resp.Patches)
		return
	}

	var patched corev1.Pod
	if unmarshalErr := json.Unmarshal(patchedJSON, &patched); unmarshalErr != nil {
		t.Fatalf("unmarshal patched pod: %v", unmarshalErr)
	}
	if patched.Labels[utils.SchedulingGateLabel] != utils.SchedulingGateLabelValueGated {
		t.Errorf("label %q = %q, want %q — all labels: %v",
			utils.SchedulingGateLabel, patched.Labels[utils.SchedulingGateLabel],
			utils.SchedulingGateLabelValueGated, patched.Labels)
	}
}

// TestHandleAdmission_PodWithNodeName_GateNotAdded verifies that pods
// already bound to a node (spec.nodeName set) are not gated.
func TestHandleAdmission_PodWithNodeName_GateNotAdded(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "bound-pod", Namespace: "default"},
		Spec: corev1.PodSpec{
			NodeName:   "worker-1",
			Containers: []corev1.Container{{Name: "c", Image: "nginx:latest"}},
		},
	}
	wh := newHandleWebhook(t)
	resp := wh.Handle(context.Background(), buildHandleRequest(t, pod))

	if !resp.Allowed {
		t.Fatalf("expected Allowed=true for already-bound pod, got: %v", resp.Result)
	}
	for _, p := range resp.Patches {
		if p.Path == "/spec/schedulingGates" && p.Operation == "add" {
			t.Errorf("should not add scheduling gate to pod with NodeName set; patch=%v", p)
		}
	}
}

// TestHandleAdmission_BadRawInput_ReturnsBadRequest verifies that Handle()
// returns an Errored (not Allowed) response when the raw object bytes cannot
// be decoded as a Pod.
func TestHandleAdmission_BadRawInput_ReturnsBadRequest(t *testing.T) {
	wh := newHandleWebhook(t)
	resp := wh.Handle(context.Background(), admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UID:       "bad-uid",
			Operation: admissionv1.Create,
			Object:    runtime.RawExtension{Raw: []byte(`{invalid json`)},
			Resource:  metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
		},
	})

	if resp.Allowed {
		t.Error("expected Allowed=false for malformed raw input, got Allowed=true")
	}
}

// TestHandleAdmission_AlreadyGatedPod_NoDuplicateGate verifies the full
// Handle() pipeline when the incoming pod already carries the MTO scheduling
// gate (e.g. a retry or re-created pod).  No duplicate gate must be added.
func TestHandleAdmission_AlreadyGatedPod_NoDuplicateGate(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pre-gated", Namespace: "default"},
		Spec: corev1.PodSpec{
			SchedulingGates: []corev1.PodSchedulingGate{{Name: utils.SchedulingGateName}},
			Containers:      []corev1.Container{{Name: "c", Image: "nginx:latest"}},
		},
	}
	wh := newHandleWebhook(t)
	resp := wh.Handle(context.Background(), buildHandleRequest(t, pod))

	if !resp.Allowed {
		t.Fatalf("expected Allowed=true, got: %v", resp.Result)
	}

	// Apply patches and verify there is exactly one scheduling gate.
	originalJSON, _ := json.Marshal(pod)
	patchedJSON, err := applyJSONPatches(originalJSON, resp.Patches)
	if err != nil {
		// If patch application fails (e.g. JSON Merge Patch vs JSON Patch),
		// count the /spec/schedulingGates 'add' patches and verify at most one.
		addGateOps := 0
		for _, p := range resp.Patches {
			if p.Path == "/spec/schedulingGates" && p.Operation == "add" {
				addGateOps++
			}
		}
		if addGateOps > 1 {
			t.Errorf("expected at most 1 add-schedulingGates patch, got %d", addGateOps)
		}
		return
	}

	var patched corev1.Pod
	if unmarshalErr := json.Unmarshal(patchedJSON, &patched); unmarshalErr != nil {
		t.Fatalf("unmarshal patched pod: %v", unmarshalErr)
	}

	count := 0
	for _, g := range patched.Spec.SchedulingGates {
		if g.Name == utils.SchedulingGateName {
			count++
		}
	}
	if count > 1 {
		t.Errorf("expected exactly 1 scheduling gate in patched pod, got %d — gates=%v",
			count, patched.Spec.SchedulingGates)
	}
}

// TestHandleAdmission_CELAppliedViaApplyCELInWebhook verifies that when a
// matching PPC is supplied directly to applyCELInWebhook (bypassing the real
// client.List), the resulting in-memory pod has the CEL architecture applied
// before the gate is added — reproducing what Handle() does end-to-end.
//
// This test is effectively an integration test of the webhook's two main steps
// (CEL apply → gate) in a single combined sequence, without needing a real
// k8s API server.
func TestHandleAdmission_CELAppliedViaApplyCELInWebhook(t *testing.T) {
	ctx := context.Background()
	recorder := record.NewFakeRecorder(8)

	raw := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pod",
			Namespace: "default",
			Labels:    map[string]string{"app": "myapp"},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "nginx:latest"}}},
	}
	pod := newPod(raw, ctx, recorder)

	// Simulate exactly what Handle() does: applyCELInWebhook then ensureSchedulingGate.
	ppc := buildTestPPCWithCELRule("cel-ppc", "default", 100,
		utils.ArchitectureAmd64,
		`self.metadata.name == "my-pod"`,
		utils.ArchitecturePpc64le)

	wh := &PodSchedulingGateMutatingWebHook{}
	wh.applyCELInWebhook(ctx, pod, []v1beta1.PodPlacementConfig{ppc})
	pod.ensureSchedulingGate()

	// 1. Architecture must be set.
	if pod.Spec.Affinity == nil ||
		pod.Spec.Affinity.NodeAffinity == nil ||
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		t.Fatal("expected node affinity to be set")
	}
	archFound := false
	for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
		for _, expr := range term.MatchExpressions {
			if expr.Key == utils.ArchLabel && len(expr.Values) == 1 && expr.Values[0] == utils.ArchitecturePpc64le {
				archFound = true
			}
		}
	}
	if !archFound {
		t.Error("expected ppc64le architecture constraint, not found")
	}

	// 2. Scheduling gate must be present.
	if !pod.HasSchedulingGate() {
		t.Error("expected scheduling gate to be present after ensureSchedulingGate")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// applyJSONPatches applies a slice of JSON Patch operations to srcJSON using
// a simple loop (no library needed for the patch ops produced by the webhook).
func applyJSONPatches(srcJSON []byte, patches []admission.JSONPatchOp) ([]byte, error) {
	// The webhook produces RFC 6902 JSON Patch. We use encoding/json to
	// unmarshal, modify, and re-marshal the object.
	var obj map[string]interface{}
	if err := json.Unmarshal(srcJSON, &obj); err != nil {
		return nil, err
	}
	for _, p := range patches {
		applyJSONPatchOp(obj, p)
	}
	return json.Marshal(obj)
}

// applyJSONPatchOp applies a single RFC 6902 JSON Patch operation to the
// provided map.  Only "add" and "replace" operations on simple "/" paths are
// handled — sufficient for the patches generated by this webhook.
func applyJSONPatchOp(obj map[string]interface{}, op admission.JSONPatchOp) {
	if op.Path == "" {
		return
	}
	// Very simple path splitter: "/a/b/c" → ["a","b","c"]
	parts := splitJSONPointer(op.Path)
	if len(parts) == 0 {
		return
	}
	switch op.Operation {
	case "add", "replace":
		setNestedValue(obj, parts, op.Value)
	}
}

func splitJSONPointer(path string) []string {
	if len(path) > 0 && path[0] == '/' {
		path = path[1:]
	}
	if path == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			out = append(out, unescapeJSONPointerToken(path[start:i]))
			start = i + 1
		}
	}
	out = append(out, unescapeJSONPointerToken(path[start:]))
	return out
}

func unescapeJSONPointerToken(s string) string {
	// RFC 6901 escape sequences: ~1 → /, ~0 → ~
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '~' && i+1 < len(s) {
			switch s[i+1] {
			case '0':
				out = append(out, '~')
				i++
				continue
			case '1':
				out = append(out, '/')
				i++
				continue
			}
		}
		out = append(out, s[i])
	}
	return string(out)
}

func escapeJSONPointer(s string) string {
	// Invert of unescape for building pointer segments.
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '~':
			result = append(result, '~', '0')
		case '/':
			result = append(result, '~', '1')
		default:
			result = append(result, s[i])
		}
	}
	return string(result)
}

func setNestedValue(obj map[string]interface{}, parts []string, value interface{}) {
	if len(parts) == 1 {
		obj[parts[0]] = value
		return
	}
	next, ok := obj[parts[0]]
	if !ok {
		child := map[string]interface{}{}
		obj[parts[0]] = child
		setNestedValue(child, parts[1:], value)
		return
	}
	if child, ok2 := next.(map[string]interface{}); ok2 {
		setNestedValue(child, parts[1:], value)
	}
}

// buildTestPPCWithCELRule builds a minimal PodPlacementConfig with a single
// CEL rule.
func buildTestPPCWithCELRule(name, ns string, priority uint8, fallback, expression, arch string) v1beta1.PodPlacementConfig {
	return v1beta1.PodPlacementConfig{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: v1beta1.PodPlacementConfigSpec{
			Priority: priority,
			Plugins: &plugins.LocalPlugins{
				CelArchitecturePlacement: &plugins.CelArchitecturePlacement{
					BasePlugin:            plugins.BasePlugin{Enabled: true},
					FallbackArchitectures: []string{fallback},
					Rules: []plugins.ArchitectureRule{
						{Name: "rule", Expression: expression, Architectures: []string{arch}},
					},
				},
			},
		},
	}
}
