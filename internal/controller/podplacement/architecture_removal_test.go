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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

var _ = Describe("RemoveArchitectureFromNodeSelector", func() {
	DescribeTable("should handle architecture removal from nodeSelector correctly",
		func(pod *corev1.Pod, expectedRemove bool) {
			removed := removeArchitectureFromNodeSelector(pod)
			Expect(removed).To(Equal(expectedRemove))
			// Verify arch label was actually removed
			if pod.Spec.NodeSelector != nil {
				_, exists := pod.Spec.NodeSelector[utils.ArchLabel]
				Expect(exists).To(BeFalse(), "Architecture label still exists in nodeSelector")
			}
		},
		Entry("remove arch from nodeSelector",
			NewPod().WithNodeSelectors(utils.ArchLabel, "amd64", "other-label", "value").Build(),
			true,
		),
		Entry("no arch in nodeSelector",
			NewPod().WithNodeSelectors("other-label", "value").Build(),
			false,
		),
		Entry("nil nodeSelector",
			NewPod().Build(),
			false,
		),
	)
})

var _ = Describe("RemoveArchitectureFromNodeAffinity", func() {
	DescribeTable("should handle architecture removal from nodeAffinity correctly",
		func(pod *corev1.Pod, expectedRemove bool, checkNil bool, checkPreferredPreserved bool) {
			removed := removeArchitectureFromNodeAffinity(pod)
			Expect(removed).To(Equal(expectedRemove))

			// Verify arch expressions were removed from required affinity
			if pod.Spec.Affinity != nil && pod.Spec.Affinity.NodeAffinity != nil {
				if pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
					for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
						for _, expr := range term.MatchExpressions {
							Expect(expr.Key).NotTo(Equal(utils.ArchLabel),
								"Architecture expression still exists in required affinity")
						}
					}
				}

				// Verify preferred affinity was preserved
				if checkPreferredPreserved {
					Expect(pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil(),
						"Preferred affinity was removed but should be preserved")
				}
			}

			// Check if structures were properly nil'd out
			if checkNil {
				if pod.Spec.Affinity != nil && pod.Spec.Affinity.NodeAffinity != nil &&
					pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
					Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms).
						To(BeEmpty(), "Expected empty node selector terms after cleanup")
				}
			}
		},
		Entry("remove arch from nodeAffinity",
			NewPod().WithNodeSelectorTermsMatchExpressions(
				[]corev1.NodeSelectorRequirement{
					{
						Key:      utils.ArchLabel,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"amd64"},
					},
				},
			).Build(),
			true, true, false,
		),
		Entry("remove arch but keep other expressions",
			NewPod().WithNodeSelectorTermsMatchExpressions(
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
			true, false, false,
		),
		Entry("preserve preferred affinity",
			NewPod().WithNodeSelectorTermsMatchExpressions(
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
			true, false, true,
		),
		Entry("nil affinity",
			NewPod().Build(),
			false, false, false,
		),
	)
})

var _ = Describe("RemoveAllArchitectureConstraints", func() {
	DescribeTable("should remove all architecture constraints correctly",
		func(pod *corev1.Pod, expectedRemove bool) {
			removed := removeAllArchitectureConstraints(pod)
			Expect(removed).To(Equal(expectedRemove))

			// Verify all architecture constraints were removed
			if pod.Spec.NodeSelector != nil {
				_, exists := pod.Spec.NodeSelector[utils.ArchLabel]
				Expect(exists).To(BeFalse(), "Architecture label still exists in nodeSelector")
			}

			if pod.Spec.Affinity != nil && pod.Spec.Affinity.NodeAffinity != nil {
				if pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
					for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
						for _, expr := range term.MatchExpressions {
							Expect(expr.Key).NotTo(Equal(utils.ArchLabel),
								"Architecture expression still exists in required affinity")
						}
					}
				}
			}
		},
		Entry("remove from both nodeSelector and nodeAffinity",
			NewPod().WithNodeSelectors(utils.ArchLabel, "amd64").WithNodeSelectorTermsMatchExpressions(
				[]corev1.NodeSelectorRequirement{
					{
						Key:      utils.ArchLabel,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"amd64"},
					},
				},
			).Build(),
			true,
		),
		Entry("remove from nodeSelector only",
			NewPod().WithNodeSelectors(utils.ArchLabel, "amd64").Build(),
			true,
		),
		Entry("remove from nodeAffinity only",
			NewPod().WithNodeSelectorTermsMatchExpressions(
				[]corev1.NodeSelectorRequirement{
					{
						Key:      utils.ArchLabel,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"amd64"},
					},
				},
			).Build(),
			true,
		),
		Entry("no architecture constraints",
			NewPod().WithNodeSelectors("other-label", "value").Build(),
			false,
		),
	)
})

var _ = Describe("RemoveArchitectureFromNodeAffinityEmptyTermCleanup", func() {
	It("should clean up empty RequiredDuringSchedulingIgnoredDuringExecution after removing all terms", func() {
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
		Expect(removed).To(BeTrue(), "Expected architecture to be removed")

		// Verify empty term was cleaned up - the entire RequiredDuringSchedulingIgnoredDuringExecution should be nil
		if pod.Spec.Affinity != nil && pod.Spec.Affinity.NodeAffinity != nil {
			Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).To(BeNil(),
				"Expected RequiredDuringSchedulingIgnoredDuringExecution to be nil after removing all terms")
		}
	})
})
