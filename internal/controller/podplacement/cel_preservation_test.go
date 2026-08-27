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

var _ = Describe("CEL Architecture Constraints Preservation", func() {

	// TestNonArchitectureRequiredSchedulingPreserved
	It("should preserve non-architecture required scheduling constraints (zone) when applying new arch", func() {
		pod := NewPod().WithName("test-pod").
			WithAffinity(&corev1.Affinity{
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
										Values:   []string{"amd64"},
									},
								},
							},
						},
					},
				},
			}).Build()

		applyArchitectureConstraints(pod, []string{"ppc64le"})

		terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
		Expect(len(terms)).To(BeNumerically(">=", 1), "Expected at least 1 term after applying constraints")

		zoneFound := false
		for _, term := range terms {
			for _, expr := range term.MatchExpressions {
				if expr.Key == "topology.kubernetes.io/zone" {
					zoneFound = true
					Expect(expr.Operator).To(Equal(corev1.NodeSelectorOpIn), "Zone operator was modified")
					Expect(expr.Values).To(ConsistOf("us-east-1a"), "Zone values were modified")
				}
			}
		}
		Expect(zoneFound).To(BeTrue(), "CRITICAL: Zone constraint was removed! Non-architecture required scheduling was destroyed!")

		archFound := false
		for _, term := range terms {
			for _, expr := range term.MatchExpressions {
				if expr.Key == utils.ArchLabel {
					archFound = true
					Expect(expr.Values).To(ConsistOf("ppc64le"), "Architecture constraint not applied correctly")
				}
			}
		}
		Expect(archFound).To(BeTrue(), "Architecture constraint was not applied")
	})

	// TestMultipleNonArchRequiredConstraintsPreserved
	It("should preserve multiple non-architecture required constraints when applying new arch", func() {
		pod := NewPod().WithName("test-pod").
			WithAffinity(&corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{Key: "topology.kubernetes.io/zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"us-east-1a", "us-east-1b"}},
									{Key: "node.kubernetes.io/instance-type", Operator: corev1.NodeSelectorOpIn, Values: []string{"m5.large", "m5.xlarge"}},
									{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"amd64"}},
									{Key: "kubernetes.io/os", Operator: corev1.NodeSelectorOpIn, Values: []string{"linux"}},
								},
								MatchFields: []corev1.NodeSelectorRequirement{
									{Key: "metadata.name", Operator: corev1.NodeSelectorOpIn, Values: []string{"node-1", "node-2"}},
								},
							},
						},
					},
				},
			}).Build()

		applyArchitectureConstraints(pod, []string{"ppc64le", "arm64"})

		terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
		requiredConstraints := map[string]bool{
			"topology.kubernetes.io/zone":      false,
			"node.kubernetes.io/instance-type": false,
			"kubernetes.io/os":                 false,
		}
		for _, term := range terms {
			for _, expr := range term.MatchExpressions {
				if _, exists := requiredConstraints[expr.Key]; exists {
					requiredConstraints[expr.Key] = true
				}
			}
		}
		for key, found := range requiredConstraints {
			Expect(found).To(BeTrue(), "CRITICAL: Required constraint %s was removed!", key)
		}

		matchFieldsFound := false
		for _, term := range terms {
			if len(term.MatchFields) > 0 {
				matchFieldsFound = true
				Expect(term.MatchFields[0].Key).To(Equal("metadata.name"), "MatchFields was modified")
			}
		}
		Expect(matchFieldsFound).To(BeTrue(), "CRITICAL: MatchFields was removed!")
	})

	// TestMultipleTermsWithMixedConstraintsPreserved
	It("should handle multiple terms with mixed architecture and non-architecture constraints", func() {
		pod := NewPod().WithName("test-pod").
			WithAffinity(&corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								// Term 1: Zone + Arch
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{Key: "topology.kubernetes.io/zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"us-east-1a"}},
									{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"amd64"}},
								},
							},
							{
								// Term 2: Instance type + Arch
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{Key: "node.kubernetes.io/instance-type", Operator: corev1.NodeSelectorOpIn, Values: []string{"m5.large"}},
									{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"amd64"}},
								},
							},
							{
								// Term 3: Only arch (should be removed after cleanup)
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"arm64"}},
								},
							},
						},
					},
				},
			}).Build()

		applyArchitectureConstraints(pod, []string{"ppc64le"})

		terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms

		zoneFound := false
		for _, term := range terms {
			for _, expr := range term.MatchExpressions {
				if expr.Key == "topology.kubernetes.io/zone" {
					zoneFound = true
				}
			}
		}
		Expect(zoneFound).To(BeTrue(), "CRITICAL: Zone constraint from term 1 was removed!")

		instanceTypeFound := false
		for _, term := range terms {
			for _, expr := range term.MatchExpressions {
				if expr.Key == "node.kubernetes.io/instance-type" {
					instanceTypeFound = true
				}
			}
		}
		Expect(instanceTypeFound).To(BeTrue(), "CRITICAL: Instance-type constraint from term 2 was removed!")

		newArchFound := false
		for _, term := range terms {
			for _, expr := range term.MatchExpressions {
				if expr.Key == utils.ArchLabel && len(expr.Values) == 1 && expr.Values[0] == "ppc64le" {
					newArchFound = true
				}
			}
		}
		Expect(newArchFound).To(BeTrue(), "New architecture constraint was not applied")
	})
})
