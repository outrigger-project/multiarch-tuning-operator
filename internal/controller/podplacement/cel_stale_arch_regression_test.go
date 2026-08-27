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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"

	. "github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

var _ = Describe("CEL Stale Architecture Regression", func() {

	// TestApplyArchitectureConstraintsRemovesStaleArchitectureValues
	It("should remove stale architecture values and apply only the new CEL-selected architecture (CPD field symptom regression)", func() {
		pod := NewPod().WithName("ibm-lh-lakehouse-ces-0").WithNamespace("cpd-instance").
			WithAffinity(&corev1.Affinity{
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
			}).Build()

		changed := applyArchitectureConstraints(pod, []string{utils.ArchitecturePpc64le})
		Expect(changed).To(BeTrue(), "expected architecture constraints application to report a change")

		Expect(pod.Spec.Affinity).NotTo(BeNil())
		Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil())
		Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())

		terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
		Expect(terms).To(HaveLen(1), "expected exactly 1 merged required node selector term")

		var (
			foundZoneConstraint bool
			archExpressions     []corev1.NodeSelectorRequirement
		)

		for _, expr := range terms[0].MatchExpressions {
			switch expr.Key {
			case "topology.kubernetes.io/zone":
				foundZoneConstraint = true
				Expect(expr.Values).To(ConsistOf("us-east-1a"), "zone constraint was modified unexpectedly")
			case utils.ArchLabel:
				archExpressions = append(archExpressions, expr)
			}
		}

		Expect(foundZoneConstraint).To(BeTrue(), "expected non-architecture zone constraint to be preserved")
		Expect(archExpressions).To(HaveLen(1),
			"expected exactly 1 architecture expression after cleanup and reapply")

		archExpr := archExpressions[0]
		Expect(archExpr.Operator).To(Equal(corev1.NodeSelectorOpIn))
		Expect(archExpr.Values).To(ConsistOf(utils.ArchitecturePpc64le),
			"expected stale architectures to be removed and replaced with only ppc64le")

		for _, staleArch := range []string{utils.ArchitectureAmd64, utils.ArchitectureS390x} {
			Expect(archExpr.Values).NotTo(ContainElement(staleArch),
				"stale architecture %q was preserved unexpectedly in final required affinity", staleArch)
		}
	})

	// TestApplyArchitectureConstraintsReplacesBroadFallbackWithMatchedRule
	It("should replace broad fallback architecture values with CEL-matched rule architecture", func() {
		pod := NewPod().WithName("lhconsole-api-v3-76575f8566-bbnvb").WithNamespace("cpd-instance").
			WithNodeSelectors(utils.ArchLabel, utils.ArchitectureAmd64).
			WithAffinity(&corev1.Affinity{
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
			}).Build()

		removed := removeAllArchitectureConstraints(pod)
		Expect(removed).To(BeTrue(), "expected stale architecture constraints to be removed")
		Expect(pod.Spec.NodeSelector).NotTo(HaveKey(utils.ArchLabel),
			"expected nodeSelector architecture key to be removed")

		terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
		Expect(terms).To(HaveLen(1),
			"expected one preserved non-architecture term after cleanup")

		for _, expr := range terms[0].MatchExpressions {
			Expect(expr.Key).NotTo(Equal(utils.ArchLabel),
				"expected no architecture expressions after cleanup")
		}

		applyArchitectureNodeAffinity(pod, []string{utils.ArchitecturePpc64le})

		terms = pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
		Expect(terms).To(HaveLen(1), "expected one merged required term after reapply")

		var foundExclusivePPC64LE bool
		var foundOSConstraint bool
		for _, expr := range terms[0].MatchExpressions {
			switch expr.Key {
			case utils.ArchLabel:
				Expect(expr.Values).To(ConsistOf(utils.ArchitecturePpc64le),
					"expected final architecture expression to be exclusive to ppc64le")
				foundExclusivePPC64LE = true
			case "kubernetes.io/os":
				if len(expr.Values) == 1 && expr.Values[0] == "linux" {
					foundOSConstraint = true
				}
			}
		}
		Expect(foundExclusivePPC64LE).To(BeTrue(),
			"expected to find an exclusive ppc64le architecture expression after reapply")
		Expect(foundOSConstraint).To(BeTrue(),
			"expected existing os constraint to be preserved in merged required term")
	})
})
