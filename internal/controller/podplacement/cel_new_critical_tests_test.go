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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	"github.com/openshift/multiarch-tuning-operator/api/common/plugins"
	"github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	. "github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

var _ = Describe("CEL New Critical Tests", func() {

	// ── 1. Plugin disabled ────────────────────────────────────────────────────────

	// TestApplyCELInWebhook_PluginDisabled_NoModification
	It("should not modify pod affinity or nodeSelector when the CEL plugin is disabled (Enabled: false)", func() {
		ctx := context.Background()
		recorder := record.NewFakeRecorder(8)

		original := NewPod().
			WithName("test-pod").
			WithNamespace("default").
			WithContainersImages("nginx:latest").
			Build()

		ppcs := []v1beta1.PodPlacementConfig{
			*NewPodPlacementConfig().
				WithName("disabled-cel-ppc").
				WithNamespace("default").
				WithPriority(100).
				WithCelArchitecturePlacement(false, []string{utils.ArchitecturePpc64le},
					[]plugins.ArchitectureRule{
						{Name: "always-true", Expression: `true`, Architectures: []string{utils.ArchitecturePpc64le}},
					}).
				Build(),
		}

		wh := &PodSchedulingGateMutatingWebHook{}
		pod := newPod(original, ctx, recorder)
		wh.applyCELInWebhook(ctx, pod, ppcs)

		Expect(pod.Spec.Affinity).To(BeNil(),
			"expected pod.Spec.Affinity to be nil when CEL plugin is disabled")
		if pod.Spec.NodeSelector != nil {
			Expect(pod.Spec.NodeSelector).NotTo(HaveKey(utils.ArchLabel),
				"expected no arch nodeSelector when CEL plugin is disabled")
		}
	})

	// TestApplyCELInWebhook_PluginDisabled_ExistingAffinityUnchanged
	It("should not touch existing pod affinity when the CEL plugin is disabled", func() {
		ctx := context.Background()
		recorder := record.NewFakeRecorder(8)

		existingTerm := corev1.NodeSelectorTerm{
			MatchExpressions: []corev1.NodeSelectorRequirement{
				{Key: "topology.kubernetes.io/zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"us-east-1a"}},
			},
		}

		original := NewPod().WithName("pod-with-affinity").WithNamespace("default").
			WithAffinity(&corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{existingTerm},
					},
				},
			}).Build()

		ppcs := []v1beta1.PodPlacementConfig{
			*NewPodPlacementConfig().
				WithName("disabled-ppc").
				WithNamespace("default").
				WithCelArchitecturePlacement(false, []string{utils.ArchitecturePpc64le}, nil).
				Build(),
		}

		wh := &PodSchedulingGateMutatingWebHook{}
		pod := newPod(original, ctx, recorder)
		wh.applyCELInWebhook(ctx, pod, ppcs)

		Expect(pod.Spec.Affinity).NotTo(BeNil(),
			"pod affinity should not have been removed by a disabled CEL plugin")
		Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil())
		Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())
		terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
		Expect(terms).To(HaveLen(1))
		Expect(terms[0].MatchExpressions).To(HaveLen(1))
		Expect(terms[0].MatchExpressions[0].Key).To(Equal("topology.kubernetes.io/zone"),
			"existing affinity term was modified by disabled plugin")
	})

	// ── 2. Affinity-ordering assertions ──────────────────────────────────────────

	// TestApplyArchitectureConstraints_TermOrderPreserved
	It("should preserve the relative order of NodeSelectorTerms after applying architecture constraints", func() {
		pod := NewPod().WithName("ordered-pod").
			WithAffinity(&corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{MatchExpressions: []corev1.NodeSelectorRequirement{
								{Key: "topology.kubernetes.io/zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"us-east-1a"}},
								{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"amd64"}},
							}},
							{MatchExpressions: []corev1.NodeSelectorRequirement{
								{Key: "node.kubernetes.io/instance-type", Operator: corev1.NodeSelectorOpIn, Values: []string{"m5.large"}},
								{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"amd64"}},
							}},
							{MatchExpressions: []corev1.NodeSelectorRequirement{
								{Key: "kubernetes.io/os", Operator: corev1.NodeSelectorOpIn, Values: []string{"linux"}},
								{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"amd64"}},
							}},
						},
					},
				},
			}).Build()

		applyArchitectureConstraints(pod, []string{utils.ArchitecturePpc64le})

		terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
		Expect(terms).To(HaveLen(3), "expected 3 terms after in-place update")

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
			Expect(found).To(BeTrue(), "term[%d] lost its non-arch key %q after applyArchitectureConstraints", i, key)
		}

		for i, term := range terms {
			archFound := false
			for _, expr := range term.MatchExpressions {
				if expr.Key == utils.ArchLabel {
					archFound = true
					Expect(expr.Values).To(ConsistOf(utils.ArchitecturePpc64le),
						"term[%d] arch value = %v, want [ppc64le]", i, expr.Values)
				}
			}
			Expect(archFound).To(BeTrue(), "term[%d] is missing arch expression after applyArchitectureConstraints", i)
		}
	})

	// TestRemoveArchitectureFromNodeAffinity_MatchExpressionsOrderPreserved
	It("should preserve the relative order of non-arch MatchExpressions after removal", func() {
		pod := NewPod().WithName("order-test").
			WithAffinity(&corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{MatchExpressions: []corev1.NodeSelectorRequirement{
								{Key: "alpha", Operator: corev1.NodeSelectorOpIn, Values: []string{"1"}},
								{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"amd64"}},
								{Key: "beta", Operator: corev1.NodeSelectorOpIn, Values: []string{"2"}},
								{Key: "gamma", Operator: corev1.NodeSelectorOpIn, Values: []string{"3"}},
							}},
						},
					},
				},
			}).Build()

		removeArchitectureFromNodeAffinity(pod)

		Expect(pod.Spec.Affinity).NotTo(BeNil())
		Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil())
		Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil(),
			"required affinity should not be nil after removing arch from a term with other keys")

		terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
		Expect(terms).To(HaveLen(1))

		exprs := terms[0].MatchExpressions
		Expect(exprs).To(HaveLen(3), "expected 3 MatchExpressions after arch removal")
		expectedOrder := []string{"alpha", "beta", "gamma"}
		for i, want := range expectedOrder {
			Expect(exprs[i].Key).To(Equal(want),
				"MatchExpressions[%d].Key = %q, want %q (order changed)", i, exprs[i].Key, want)
		}
	})

	// TestApplyArchitectureConstraints_MatchFieldsPositionUnchanged
	// Left as explicit struct: PodBuilder.WithNodeSelectorTermsMatchExpressions only sets
	// MatchExpressions; it cannot represent the MatchFields field.
	It("should not reorder or remove MatchFields entries during in-place update", func() {
		pod := NewPod().WithName("matchfields-pod").
			WithAffinity(&corev1.Affinity{
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
			}).Build()

		applyArchitectureConstraints(pod, []string{utils.ArchitecturePpc64le})

		terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
		Expect(terms).To(HaveLen(1))
		Expect(terms[0].MatchFields).To(HaveLen(1), "expected 1 MatchFields entry")
		Expect(terms[0].MatchFields[0].Key).To(Equal("metadata.name"), "MatchFields[0].Key changed")
		Expect(terms[0].MatchFields[0].Values).To(HaveLen(2), "MatchFields[0].Values changed")
	})

	// ── 3. Metadata preservation ─────────────────────────────────────────────────

	// TestApplyArchitectureConstraints_LabelsAndAnnotationsPreserved
	It("should leave pod labels and annotations completely unchanged after applyArchitectureConstraints", func() {
		originalLabels := map[string]string{
			"app": "database", "tier": "backend", "managed-by": "helm", "version": "1.2.3",
		}
		originalAnnotations := map[string]string{
			"kubectl.kubernetes.io/last-applied-configuration": `{"some":"json"}`,
			"custom-annotation": "custom-value",
		}

		pod := NewPod().
			WithName("metadata-pod").
			WithNamespace("prod").
			WithLabels("app", "database", "tier", "backend", "managed-by", "helm", "version", "1.2.3").
			WithAnnotations(copyStringMap(originalAnnotations)).
			WithNodeSelectors(utils.ArchLabel, "amd64", "zone", "us-east-1").
			Build()

		applyArchitectureConstraints(pod, []string{utils.ArchitecturePpc64le})

		Expect(pod.Labels).To(HaveLen(len(originalLabels)),
			"label count changed — labels=%v", pod.Labels)
		for k, wantV := range originalLabels {
			Expect(pod.Labels[k]).To(Equal(wantV), "label %q changed", k)
		}

		Expect(pod.Annotations).To(HaveLen(len(originalAnnotations)),
			"annotation count changed")
		for k, wantV := range originalAnnotations {
			Expect(pod.Annotations[k]).To(Equal(wantV), "annotation %q changed", k)
		}

		Expect(pod.Spec.NodeSelector["zone"]).To(Equal("us-east-1"),
			"non-arch nodeSelector key 'zone' was modified or removed")
	})

	// TestApplyArchitectureConstraints_OwnerReferencesPreserved
	It("should leave OwnerReferences untouched after applyArchitectureConstraints", func() {
		truePtr := true
		ownerRef := metav1.OwnerReference{
			APIVersion: "apps/v1", Kind: "Deployment", Name: "my-deploy",
			UID: "uid-12345", Controller: &truePtr, BlockOwnerDeletion: &truePtr,
		}
		pod := NewPod().
			WithName("owned-pod").
			WithNamespace("default").
			WithOwnerReference(ownerRef).
			Build()

		applyArchitectureConstraints(pod, []string{utils.ArchitecturePpc64le})

		Expect(pod.OwnerReferences).To(HaveLen(1), "OwnerReferences count changed")
		got := pod.OwnerReferences[0]
		Expect(got.Name).To(Equal(ownerRef.Name))
		Expect(got.UID).To(Equal(ownerRef.UID))
		Expect(got.Kind).To(Equal(ownerRef.Kind))
	})

	It("should leave finalizers untouched after applyArchitectureConstraints", func() {
		pod := NewPod().WithName("finalized-pod").WithNamespace("default").
			WithFinalizers("example.com/my-finalizer", "storage.kubernetes.io/finalizer").Build()

		applyArchitectureConstraints(pod, []string{utils.ArchitecturePpc64le})

		Expect(pod.Finalizers).To(HaveLen(2),
			"Finalizers count changed — finalizers=%v", pod.Finalizers)
		for i, f := range []string{"example.com/my-finalizer", "storage.kubernetes.io/finalizer"} {
			Expect(pod.Finalizers[i]).To(Equal(f), "Finalizers[%d] changed", i)
		}
	})

	// ── 4. Scheduling-gate idempotency ───────────────────────────────────────────

	// TestEnsureSchedulingGate_NoDuplicateWhenAlreadyPresent
	It("should not add a duplicate scheduling gate when it is already present", func() {
		ctx := context.Background()
		recorder := record.NewFakeRecorder(8)

		raw := NewPod().
			WithName("pre-gated").
			WithNamespace("default").
			WithSchedulingGates(utils.SchedulingGateName).
			Build()
		pod := newPod(raw, ctx, recorder)
		pod.ensureSchedulingGate()

		count := 0
		for _, g := range pod.Spec.SchedulingGates {
			if g.Name == utils.SchedulingGateName {
				count++
			}
		}
		Expect(count).To(Equal(1),
			"expected exactly 1 instance of scheduling gate %q, got %d — gates=%v",
			utils.SchedulingGateName, count, pod.Spec.SchedulingGates)
	})

	// TestEnsureSchedulingGate_AddedWhenAbsent
	It("should add the scheduling gate when the pod does not have it", func() {
		ctx := context.Background()
		recorder := record.NewFakeRecorder(8)

		raw := NewPod().
			WithName("ungated").
			WithNamespace("default").
			Build()
		pod := newPod(raw, ctx, recorder)
		pod.ensureSchedulingGate()

		found := false
		for _, g := range pod.Spec.SchedulingGates {
			if g.Name == utils.SchedulingGateName {
				found = true
			}
		}
		Expect(found).To(BeTrue(),
			"expected scheduling gate %q to be added, got gates=%v",
			utils.SchedulingGateName, pod.Spec.SchedulingGates)
	})

	// TestEnsureSchedulingGate_ExistingOtherGatesUnchanged
	It("should preserve pre-existing gates from other controllers when adding the MTO gate", func() {
		ctx := context.Background()
		recorder := record.NewFakeRecorder(8)

		raw := NewPod().
			WithName("multi-gated").
			WithNamespace("default").
			WithSchedulingGates("other-controller.example.com/my-gate").
			Build()
		pod := newPod(raw, ctx, recorder)
		pod.ensureSchedulingGate()

		Expect(pod.Spec.SchedulingGates).To(HaveLen(2),
			"expected at least 2 gates after ensureSchedulingGate")

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
		Expect(mtoGateFound).To(BeTrue(),
			"MTO gate not found after ensureSchedulingGate; gates=%v", pod.Spec.SchedulingGates)
		Expect(otherGateFound).To(BeTrue(),
			"pre-existing gate was removed by ensureSchedulingGate; gates=%v", pod.Spec.SchedulingGates)
	})

	// TestApplyCELInWebhook_PluginDisabled_GateStillAdded
	It("should still add the scheduling gate even when the CEL plugin is disabled", func() {
		ctx := context.Background()
		recorder := record.NewFakeRecorder(8)

		raw := NewPod().
			WithName("disabled-gate-test").
			WithNamespace("default").
			WithContainersImages("nginx:latest").
			Build()
		pod := newPod(raw, ctx, recorder)

		ppcs := []v1beta1.PodPlacementConfig{
			*NewPodPlacementConfig().
				WithName("disabled-ppc").
				WithNamespace("default").
				WithCelArchitecturePlacement(false, []string{utils.ArchitecturePpc64le}, nil).
				Build(),
		}

		wh := &PodSchedulingGateMutatingWebHook{}
		wh.applyCELInWebhook(ctx, pod, ppcs)

		Expect(pod.Spec.Affinity).To(BeNil(),
			"disabled CEL plugin must not set affinity; got: %+v", pod.Spec.Affinity)

		pod.ensureSchedulingGate()
		Expect(pod.HasSchedulingGate()).To(BeTrue(),
			"scheduling gate should be present even when CEL plugin is disabled")
	})
})

// ── helpers ───────────────────────────────────────────────────────────────────

func copyStringMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
