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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

var _ = Describe("ApplyArchitectureNodeAffinity", func() {
	DescribeTable("should apply architecture node affinity correctly",
		func(pod *corev1.Pod, architectures []string, expectAffinity bool) {
			applyArchitectureNodeAffinity(pod, architectures)

			if expectAffinity {
				// Verify affinity structure was created
				Expect(pod.Spec.Affinity).NotTo(BeNil(), "Expected affinity to be created")
				Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil(), "Expected node affinity to be created")
				Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil(),
					"Expected required node affinity to be created")

				// Verify architecture requirement was added
				found := false
				for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
					for _, expr := range term.MatchExpressions {
						if expr.Key == utils.ArchLabel && expr.Operator == corev1.NodeSelectorOpIn {
							found = true
							Expect(expr.Values).To(Equal(architectures))
						}
					}
				}
				Expect(found).To(BeTrue(), "Architecture requirement not found in node affinity")
			} else {
				// For empty architectures, affinity should not be modified
				if pod.Spec.Affinity != nil && pod.Spec.Affinity.NodeAffinity != nil &&
					pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
					Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms).
						To(BeEmpty(), "Expected no node selector terms for empty architectures")
				}
			}
		},
		Entry("apply single architecture",
			NewPod().WithName("test-pod").Build(),
			[]string{"ppc64le"},
			true,
		),
		Entry("apply multiple architectures",
			NewPod().WithName("test-pod").Build(),
			[]string{"amd64", "ppc64le"},
			true,
		),
		Entry("empty architectures list",
			NewPod().WithName("test-pod").Build(),
			[]string{},
			false,
		),
		Entry("apply to pod with existing affinity",
			NewPod().WithName("test-pod").WithAffinity(&corev1.Affinity{
				PodAffinity: &corev1.PodAffinity{},
			}).Build(),
			[]string{"ppc64le"},
			true,
		),
	)
})

var _ = Describe("ApplyArchitectureConstraints", func() {
	DescribeTable("should apply architecture constraints correctly",
		func(pod *corev1.Pod, architectures []string, expectModified bool) {
			modified := applyArchitectureConstraints(pod, architectures)

			Expect(modified).To(Equal(expectModified))

			if expectModified && len(architectures) > 0 {
				// Verify old constraints were removed
				if pod.Spec.NodeSelector != nil {
					_, exists := pod.Spec.NodeSelector[utils.ArchLabel]
					Expect(exists).To(BeFalse(), "Old architecture constraint still exists in nodeSelector")
				}

				// Verify new constraints were applied
				Expect(pod.Spec.Affinity).NotTo(BeNil(), "Expected node affinity to be created")
				Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil(), "Expected node affinity to be created")

				found := false
				if pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
					for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
						for _, expr := range term.MatchExpressions {
							if expr.Key == utils.ArchLabel {
								found = true
								Expect(expr.Values).To(HaveLen(len(architectures)))
							}
						}
					}
				}
				Expect(found).To(BeTrue(), "New architecture constraint not found")
			}
		},
		Entry("remove old and apply new",
			NewPod().WithName("test-pod").WithNodeSelectors(utils.ArchLabel, "amd64").Build(),
			[]string{"ppc64le"},
			true,
		),
		Entry("apply to clean pod",
			NewPod().WithName("test-pod").Build(),
			[]string{"ppc64le"},
			true,
		),
		Entry("empty architectures",
			NewPod().WithName("test-pod").Build(),
			[]string{},
			false,
		),
		Entry("remove from both nodeSelector and nodeAffinity",
			NewPod().WithName("test-pod").WithNodeSelectors(utils.ArchLabel, "amd64").WithNodeSelectorTermsMatchExpressions(
				[]corev1.NodeSelectorRequirement{
					{
						Key:      utils.ArchLabel,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"amd64"},
					},
				},
			).Build(),
			[]string{"ppc64le"},
			true,
		),
	)
})

// ApplyArchitectureConstraints_Idempotency verifies that applying the same
// architecture constraints twice produces identical NodeSelectorTerms, ensuring
// the reconciler can safely re-apply CEL constraints without triggering
// Kubernetes immutable field errors.
var _ = Describe("ApplyArchitectureConstraints_Idempotency", func() {
	DescribeTable("should be idempotent",
		func(pod *corev1.Pod, architectures []string) {
			// Apply architecture constraints first time
			applyArchitectureConstraints(pod, architectures)

			// Capture the state after first application
			firstTerms := captureNodeSelectorTerms(pod)

			// Apply the same architecture constraints second time
			applyArchitectureConstraints(pod, architectures)

			// Capture the state after second application
			secondTerms := captureNodeSelectorTerms(pod)

			// Verify that the NodeSelectorTerms are identical
			Expect(nodeSelectorsEqual(firstTerms, secondTerms)).To(BeTrue(),
				"NodeSelectorTerms changed after second application.\nFirst: %+v\nSecond: %+v", firstTerms, secondTerms)
		},
		Entry("idempotent for single architecture",
			NewPod().WithName("test-pod").WithNamespace("default").Build(),
			[]string{"ppc64le"},
		),
		Entry("idempotent for multiple architectures",
			NewPod().WithName("test-pod").WithNamespace("default").Build(),
			[]string{"amd64", "arm64"},
		),
		Entry("idempotent with existing affinity",
			NewPod().WithName("test-pod").WithNamespace("default").WithNodeSelectorTermsMatchExpressions(
				[]corev1.NodeSelectorRequirement{
					{
						Key:      "node-role.kubernetes.io/worker",
						Operator: corev1.NodeSelectorOpExists,
					},
				},
			).Build(),
			[]string{"ppc64le"},
		),
	)
})

// Reconciler_CELReapplication_NoMutation verifies that when the reconciler
// re-applies CEL architecture placement after the webhook has already applied it,
// the pod object remains unchanged and no Kubernetes API error occurs.
var _ = Describe("Reconciler_CELReapplication_NoMutation", func() {
	DescribeTable("should not mutate pod on reapplication",
		func(architectures []string) {
			pod := NewPod().WithName("test-pod").WithNamespace("default").Build()

			// Simulate webhook applying CEL constraints
			applyArchitectureConstraints(pod, architectures)
			webhookTerms := captureNodeSelectorTerms(pod)

			// Simulate reconciler re-applying the same CEL constraints
			applyArchitectureConstraints(pod, architectures)
			reconcilerTerms := captureNodeSelectorTerms(pod)

			// Verify that the pod object is unchanged
			Expect(nodeSelectorsEqual(webhookTerms, reconcilerTerms)).To(BeTrue(),
				"Pod object changed after reconciler re-application.\nWebhook: %+v\nReconciler: %+v", webhookTerms, reconcilerTerms)

			// Verify the architectures are still correct
			found := false
			for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
				for _, expr := range term.MatchExpressions {
					if expr.Key == utils.ArchLabel && expr.Operator == corev1.NodeSelectorOpIn {
						found = true
						Expect(expr.Values).To(Equal(architectures))
					}
				}
			}
			Expect(found).To(BeTrue(), "Architecture requirement not found after reconciler re-application")
		},
		Entry("single architecture", []string{"ppc64le"}),
		Entry("multiple architectures", []string{"amd64", "arm64", "ppc64le"}),
	)
})

// captureNodeSelectorTerms creates a deep copy of NodeSelectorTerms for comparison
func captureNodeSelectorTerms(pod *corev1.Pod) []corev1.NodeSelectorTerm {
	if pod.Spec.Affinity == nil ||
		pod.Spec.Affinity.NodeAffinity == nil ||
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		return nil
	}

	terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	captured := make([]corev1.NodeSelectorTerm, len(terms))
	for i, term := range terms {
		captured[i] = corev1.NodeSelectorTerm{
			MatchExpressions: make([]corev1.NodeSelectorRequirement, len(term.MatchExpressions)),
			MatchFields:      make([]corev1.NodeSelectorRequirement, len(term.MatchFields)),
		}
		copy(captured[i].MatchExpressions, term.MatchExpressions)
		copy(captured[i].MatchFields, term.MatchFields)
	}
	return captured
}

// nodeSelectorsEqual compares two slices of NodeSelectorTerms for equality
func nodeSelectorsEqual(a, b []corev1.NodeSelectorTerm) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if len(a[i].MatchExpressions) != len(b[i].MatchExpressions) {
			return false
		}
		if len(a[i].MatchFields) != len(b[i].MatchFields) {
			return false
		}

		for j := range a[i].MatchExpressions {
			if a[i].MatchExpressions[j].Key != b[i].MatchExpressions[j].Key {
				return false
			}
			if a[i].MatchExpressions[j].Operator != b[i].MatchExpressions[j].Operator {
				return false
			}
			if len(a[i].MatchExpressions[j].Values) != len(b[i].MatchExpressions[j].Values) {
				return false
			}
			for k := range a[i].MatchExpressions[j].Values {
				if a[i].MatchExpressions[j].Values[k] != b[i].MatchExpressions[j].Values[k] {
					return false
				}
			}
		}

		for j := range a[i].MatchFields {
			if a[i].MatchFields[j].Key != b[i].MatchFields[j].Key {
				return false
			}
			if a[i].MatchFields[j].Operator != b[i].MatchFields[j].Operator {
				return false
			}
			if len(a[i].MatchFields[j].Values) != len(b[i].MatchFields[j].Values) {
				return false
			}
			for k := range a[i].MatchFields[j].Values {
				if a[i].MatchFields[j].Values[k] != b[i].MatchFields[j].Values[k] {
					return false
				}
			}
		}
	}

	return true
}

var _ = Describe("ApplyArchitectureNodeAffinityPreservesOtherAffinity", func() {
	It("should preserve existing pod affinity while adding node affinity", func() {
		pod := NewPod().WithName("test-pod").WithAffinity(&corev1.Affinity{
			PodAffinity: &corev1.PodAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app": "test",
							},
						},
					},
				},
			},
		}).Build()

		applyArchitectureNodeAffinity(pod, []string{"ppc64le"})

		// Verify pod affinity was preserved
		Expect(pod.Spec.Affinity.PodAffinity).NotTo(BeNil(), "Pod affinity was removed but should be preserved")
		// Verify node affinity was added
		Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil(), "Node affinity was not added")
	})
})
