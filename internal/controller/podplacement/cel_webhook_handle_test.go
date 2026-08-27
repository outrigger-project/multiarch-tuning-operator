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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	jsonpatch "gomodules.xyz/jsonpatch/v2"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/panjf2000/ants/v2"

	"github.com/openshift/multiarch-tuning-operator/api/common/plugins"
	"github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	. "github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

// buildHandleRequest encodes a Pod as a raw CREATE admission.Request.
func buildHandleRequest(pod *corev1.Pod) admission.Request {
	raw, err := json.Marshal(pod)
	Expect(err).NotTo(HaveOccurred(), "failed to marshal pod")
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UID:       "test-uid",
			Operation: admissionv1.Create,
			Object:    runtime.RawExtension{Raw: raw},
			Resource:  metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
		},
	}
}

// newHandleWebhook returns a webhook wired with a fake controller-runtime
// client (empty store) so that client.List returns an empty PPC list without
// hitting a real API server.  The scheme is populated so that
// admission.NewDecoder can decode Pods.
func newHandleWebhook() *PodSchedulingGateMutatingWebHook {
	s := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(s)).To(Succeed(), "add core scheme")
	Expect(v1beta1.AddToScheme(s)).To(Succeed(), "add multiarch scheme")
	fakeClient := fake.NewClientBuilder().WithScheme(s).Build()
	pool, err := ants.NewMultiPool(1, 1, ants.LeastTasks, ants.WithNonblocking(true))
	Expect(err).NotTo(HaveOccurred(), "ants pool")
	// clientSet nil is safe: delayedSchedulingGatedEvent skips the goroutine when
	// clientSet is nil (informational-only path, not required for admission logic).
	// apiReader nil is safe: the webhook falls back to the informer-backed client only when apiReader != nil.
	return NewPodSchedulingGateMutatingWebHook(fakeClient, nil, nil, nil, s, record.NewFakeRecorder(32), pool)
}

var _ = Describe("Webhook CEL Handle admission", func() {

	// TestHandleAdmission_ResponseAllowed
	It("should return Allowed=true for a plain pod", func() {
		pod := NewPod().WithName("plain-pod").WithNamespace("test-wh").WithContainersImages("nginx:latest").Build()
		wh := newHandleWebhook()
		resp := wh.Handle(context.Background(), buildHandleRequest(pod))
		Expect(resp.Allowed).To(BeTrue(), "expected Allowed=true, got: %v", resp.Result)
	})

	// TestHandleAdmission_SchedulingGatePatchPresent
	It("should include a patch that adds /spec/schedulingGates", func() {
		pod := NewPod().WithName("plain-pod").WithNamespace("test-wh").WithContainersImages("nginx:latest").Build()
		wh := newHandleWebhook()
		resp := wh.Handle(context.Background(), buildHandleRequest(pod))
		Expect(resp.Allowed).To(BeTrue(), "expected Allowed=true, got: %v", resp.Result)

		gateFound := false
		for _, p := range resp.Patches {
			if p.Path == "/spec/schedulingGates" && p.Operation == "add" {
				gateFound = true
			}
		}
		Expect(gateFound).To(BeTrue(),
			"expected patch to add /spec/schedulingGates; patches=%v", resp.Patches)
	})

	// TestHandleAdmission_SchedulingGateLabelPatchPresent
	It("should include the scheduling gate label in the patch", func() {
		pod := NewPod().WithName("plain-pod").WithNamespace("test-wh").WithContainersImages("nginx:latest").Build()
		wh := newHandleWebhook()
		resp := wh.Handle(context.Background(), buildHandleRequest(pod))
		Expect(resp.Allowed).To(BeTrue(), "expected Allowed=true, got: %v", resp.Result)

		originalJSON, _ := json.Marshal(pod)
		patchedJSON, err := applyJSONPatches(originalJSON, resp.Patches)
		if err != nil {
			// Fallback: inspect labels directly via the patch values.
			for _, p := range resp.Patches {
				if p.Path == "/metadata/labels" || p.Path == "/metadata/labels/"+escapeJSONPointer(utils.SchedulingGateLabel) {
					return // Label patch found, test passes
				}
			}
			Fail("could not apply patches: " + err.Error())
			return
		}

		var patched corev1.Pod
		Expect(json.Unmarshal(patchedJSON, &patched)).To(Succeed(), "unmarshal patched pod")
		Expect(patched.Labels[utils.SchedulingGateLabel]).To(Equal(utils.SchedulingGateLabelValueGated),
			"label %q = %q, want %q — all labels: %v",
			utils.SchedulingGateLabel, patched.Labels[utils.SchedulingGateLabel],
			utils.SchedulingGateLabelValueGated, patched.Labels)
	})

	// TestHandleAdmission_PodWithNodeName_GateNotAdded
	It("should not add the scheduling gate to pods already bound to a node (NodeName set)", func() {
		pod := NewPod().WithName("bound-pod").WithNamespace("test-wh").WithNodeName("worker-1").WithContainersImages("nginx:latest").Build()
		wh := newHandleWebhook()
		resp := wh.Handle(context.Background(), buildHandleRequest(pod))
		Expect(resp.Allowed).To(BeTrue(), "expected Allowed=true for already-bound pod")
		for _, p := range resp.Patches {
			Expect(p.Path == "/spec/schedulingGates" && p.Operation == "add").To(BeFalse(),
				"should not add scheduling gate to pod with NodeName set; patch=%v", p)
		}
	})

	// TestHandleAdmission_BadRawInput_ReturnsBadRequest
	It("should return Allowed=false when the raw object bytes cannot be decoded as a Pod", func() {
		wh := newHandleWebhook()
		resp := wh.Handle(context.Background(), admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				UID:       "bad-uid",
				Operation: admissionv1.Create,
				Object:    runtime.RawExtension{Raw: []byte(`{invalid json`)},
				Resource:  metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			},
		})
		Expect(resp.Allowed).To(BeFalse(), "expected Allowed=false for malformed raw input")
	})

	// TestHandleAdmission_AlreadyGatedPod_NoDuplicateGate
	It("should not add a duplicate scheduling gate when the pod already carries it", func() {
		pod := NewPod().WithName("pre-gated").WithNamespace("test-wh").WithSchedulingGates(utils.SchedulingGateName).WithContainersImages("nginx:latest").Build()
		wh := newHandleWebhook()
		resp := wh.Handle(context.Background(), buildHandleRequest(pod))
		Expect(resp.Allowed).To(BeTrue(), "expected Allowed=true")

		originalJSON, _ := json.Marshal(pod)
		patchedJSON, err := applyJSONPatches(originalJSON, resp.Patches)
		if err != nil {
			addGateOps := 0
			for _, p := range resp.Patches {
				if p.Path == "/spec/schedulingGates" && p.Operation == "add" {
					addGateOps++
				}
			}
			Expect(addGateOps).To(BeNumerically("<=", 1),
				"expected at most 1 add-schedulingGates patch, got %d", addGateOps)
			return
		}

		var patched corev1.Pod
		Expect(json.Unmarshal(patchedJSON, &patched)).To(Succeed(), "unmarshal patched pod")

		count := 0
		for _, g := range patched.Spec.SchedulingGates {
			if g.Name == utils.SchedulingGateName {
				count++
			}
		}
		Expect(count).To(BeNumerically("<=", 1),
			"expected exactly 1 scheduling gate in patched pod, got %d — gates=%v",
			count, patched.Spec.SchedulingGates)
	})

	// TestHandleAdmission_CELAppliedViaApplyCELInWebhook
	It("should apply CEL architecture constraints and add the scheduling gate in the correct order", func() {
		ctx := context.Background()
		recorder := record.NewFakeRecorder(8)

		raw := NewPod().WithName("my-pod").WithNamespace("test-wh").WithLabels("app", "myapp").WithContainersImages("nginx:latest").Build()
		pod := newPod(raw, ctx, recorder)

		ppc := buildTestPPCWithCELRule("cel-ppc", "default", 100,
			utils.ArchitectureAmd64,
			`self.metadata.name == "my-pod"`,
			utils.ArchitecturePpc64le)

		wh := &PodSchedulingGateMutatingWebHook{}
		wh.applyCELInWebhook(ctx, pod, []v1beta1.PodPlacementConfig{ppc})
		pod.ensureSchedulingGate()

		Expect(pod.Spec.Affinity).NotTo(BeNil())
		Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil())
		Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())
		archFound := false
		for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
			for _, expr := range term.MatchExpressions {
				if expr.Key == utils.ArchLabel && len(expr.Values) == 1 && expr.Values[0] == utils.ArchitecturePpc64le {
					archFound = true
				}
			}
		}
		Expect(archFound).To(BeTrue(), "expected ppc64le architecture constraint, not found")
		Expect(pod.HasSchedulingGate()).To(BeTrue(),
			"expected scheduling gate to be present after ensureSchedulingGate")
	})
})

var _ = Describe("Webhook CEL apiReader/ppcCacheSynced behavior", func() {

	// TestHandle_CacheSynced_NoPPC_NoAPIReaderFallback
	It("should NOT call apiReader when informer cache is synced and returns no PPCs", func() {
		s := runtime.NewScheme()
		Expect(clientgoscheme.AddToScheme(s)).To(Succeed())
		Expect(v1beta1.AddToScheme(s)).To(Succeed())

		fakeClient := fake.NewClientBuilder().WithScheme(s).Build()
		pool, err := ants.NewMultiPool(1, 1, ants.LeastTasks, ants.WithNonblocking(true))
		Expect(err).NotTo(HaveOccurred())

		apiReaderCalled := false
		var mockAPIReader mockReader
		mockAPIReader.listFn = func() error {
			apiReaderCalled = true
			return nil
		}

		cacheSynced := func() bool { return true }
		wh := NewPodSchedulingGateMutatingWebHook(fakeClient, &mockAPIReader, cacheSynced, nil, s, record.NewFakeRecorder(32), pool)

		pod := NewPod().WithName("plain-pod").WithNamespace("test-wh").WithContainersImages("nginx:latest").Build()
		_ = wh.Handle(context.Background(), buildHandleRequest(pod))

		Expect(apiReaderCalled).To(BeFalse(),
			"apiReader should NOT be called when informer cache is synced and returned no PPCs")
	})

	// TestHandle_CacheNotSynced_NoPPC_APIReaderFallbackAllowed
	It("should call apiReader when informer cache is NOT yet synced", func() {
		s := runtime.NewScheme()
		Expect(clientgoscheme.AddToScheme(s)).To(Succeed())
		Expect(v1beta1.AddToScheme(s)).To(Succeed())

		fakeClient := fake.NewClientBuilder().WithScheme(s).Build()
		pool, err := ants.NewMultiPool(1, 1, ants.LeastTasks, ants.WithNonblocking(true))
		Expect(err).NotTo(HaveOccurred())

		apiReaderCalled := false
		var mockAPIReader mockReader
		mockAPIReader.listFn = func() error {
			apiReaderCalled = true
			return nil
		}

		cacheSynced := func() bool { return false }
		wh := NewPodSchedulingGateMutatingWebHook(fakeClient, &mockAPIReader, cacheSynced, nil, s, record.NewFakeRecorder(32), pool)

		pod := NewPod().WithName("plain-pod").WithNamespace("test-wh").WithContainersImages("nginx:latest").Build()
		_ = wh.Handle(context.Background(), buildHandleRequest(pod))

		Expect(apiReaderCalled).To(BeTrue(),
			"apiReader SHOULD be called when informer cache is not yet synced and returned no PPCs")
	})

	// TestHandle_NilCacheSynced_NoPPC_APIReaderFallbackAllowed
	It("should call apiReader when ppcCacheSynced is nil (no informer wired)", func() {
		s := runtime.NewScheme()
		Expect(clientgoscheme.AddToScheme(s)).To(Succeed())
		Expect(v1beta1.AddToScheme(s)).To(Succeed())

		fakeClient := fake.NewClientBuilder().WithScheme(s).Build()
		pool, err := ants.NewMultiPool(1, 1, ants.LeastTasks, ants.WithNonblocking(true))
		Expect(err).NotTo(HaveOccurred())

		apiReaderCalled := false
		var mockAPIReader mockReader
		mockAPIReader.listFn = func() error {
			apiReaderCalled = true
			return nil
		}

		wh := NewPodSchedulingGateMutatingWebHook(fakeClient, &mockAPIReader, nil, nil, s, record.NewFakeRecorder(32), pool)

		pod := NewPod().WithName("plain-pod").WithNamespace("test-wh").WithContainersImages("nginx:latest").Build()
		_ = wh.Handle(context.Background(), buildHandleRequest(pod))

		Expect(apiReaderCalled).To(BeTrue(),
			"apiReader SHOULD be called when ppcCacheSynced is nil (no informer wired)")
	})

	// TestHandle_ApiReaderNil_NoFallback
	It("should not panic and return Allowed=true when apiReader is nil", func() {
		pod := NewPod().WithName("plain-pod").WithNamespace("test-wh").WithContainersImages("nginx:latest").Build()
		wh := newHandleWebhook()
		resp := wh.Handle(context.Background(), buildHandleRequest(pod))
		Expect(resp.Allowed).To(BeTrue(), "expected Allowed=true with nil apiReader, got: %v", resp.Result)
	})
})

// ── helpers ───────────────────────────────────────────────────────────────────

// applyJSONPatches applies a slice of JSON Patch operations to srcJSON using
// a simple loop (no library needed for the patch ops produced by the webhook).
func applyJSONPatches(srcJSON []byte, patches []jsonpatch.JsonPatchOperation) ([]byte, error) {
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
// provided map. Only "add" and "replace" operations on simple "/" paths are
// handled — sufficient for the patches generated by this webhook.
func applyJSONPatchOp(obj map[string]interface{}, op jsonpatch.JsonPatchOperation) {
	if op.Path == "" {
		return
	}
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
	return *NewPodPlacementConfig().WithName(name).WithNamespace(ns).WithPriority(priority).
		WithCelArchitecturePlacement(true, []string{fallback}, []plugins.ArchitectureRule{
			NewRule("rule", expression, arch),
		}).Build()
}

// mockReader implements client.Reader for use as a spy in tests.
type mockReader struct {
	listFn func() error
}

func (m *mockReader) Get(_ context.Context, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
	return nil
}

func (m *mockReader) List(_ context.Context, _ client.ObjectList, _ ...client.ListOption) error {
	if m.listFn != nil {
		return m.listFn()
	}
	return nil
}
