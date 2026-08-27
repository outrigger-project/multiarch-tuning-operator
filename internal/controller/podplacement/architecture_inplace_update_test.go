/*
Copyright 2025 Red Hat, Inc.

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

// ApplyArchitectureNodeAffinityInPlaceUpdate verifies that architecture constraints
// are updated in-place without removing and re-adding NodeSelectorTerms, which would
// cause Kubernetes to reject the update with:
// "no additions/deletions to non-empty NodeSelectorTerms list are allowed"
var _ = Describe("ApplyArchitectureNodeAffinityInPlaceUpdate", func() {
	DescribeTable("should update architecture constraints in-place",
		func(pod *corev1.Pod, architectures []string, expectedTermCount int, expectedArchInEachTerm []string, verifyOtherConstraints bool) {
			// Store original term count to verify in-place update
			originalTermCount := 0
			if pod.Spec.Affinity != nil &&
				pod.Spec.Affinity.NodeAffinity != nil &&
				pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
				originalTermCount = len(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms)
			}

			applyArchitectureNodeAffinity(pod, architectures)

			// Verify the term count matches expected (should be preserved for in-place update)
			Expect(pod.Spec.Affinity).NotTo(BeNil(), "Expected affinity structure to be created")
			Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil(), "Expected affinity structure to be created")
			Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil(),
				"Expected affinity structure to be created")

			terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
			Expect(terms).To(HaveLen(expectedTermCount))

			// For in-place updates, term count should be preserved
			if originalTermCount > 0 {
				Expect(terms).To(HaveLen(originalTermCount),
					"Term count changed from %d to %d - this would cause Kubernetes API rejection", originalTermCount, len(terms))
			}

			// Verify each term has the correct architecture constraint
			for i, term := range terms {
				foundArch := false
				for _, expr := range term.MatchExpressions {
					if expr.Key == utils.ArchLabel {
						foundArch = true
						Expect(expr.Operator).To(Equal(corev1.NodeSelectorOpIn),
							"Term %d: Expected operator In", i)
						Expect(expr.Values).To(Equal(expectedArchInEachTerm),
							"Term %d: architecture values mismatch", i)
					}
				}
				Expect(foundArch).To(BeTrue(), "Term %d: Architecture constraint not found", i)

				// Verify other constraints are preserved
				if verifyOtherConstraints {
					nonArchCount := 0
					for _, expr := range term.MatchExpressions {
						if expr.Key != utils.ArchLabel {
							nonArchCount++
						}
					}
					if i == 0 && originalTermCount > 0 {
						Expect(nonArchCount).NotTo(BeZero(),
							"Term %d: Other constraints were removed but should be preserved", i)
					}
				}
			}
		},
		Entry("update existing arch constraint in-place",
			NewPod().WithName("test-pod").WithNodeSelectorTermsMatchExpressions(
				[]corev1.NodeSelectorRequirement{
					{
						Key:      "kubernetes.io/os",
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"linux"},
					},
					{
						Key:      utils.ArchLabel,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"amd64", "ppc64le", "s390x"},
					},
				},
			).Build(),
			[]string{"ppc64le"},
			1,
			[]string{"ppc64le"},
			true,
		),
		Entry("update multiple terms with arch constraints",
			NewPod().WithName("test-pod").WithNodeSelectorTermsMatchExpressions(
				[]corev1.NodeSelectorRequirement{
					{
						Key:      "kubernetes.io/os",
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"linux"},
					},
					{
						Key:      utils.ArchLabel,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"amd64"},
					},
				},
				[]corev1.NodeSelectorRequirement{
					{
						Key:      "node.kubernetes.io/instance-type",
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"m5.large"},
					},
					{
						Key:      utils.ArchLabel,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"amd64"},
					},
				},
			).Build(),
			[]string{"ppc64le"},
			2,
			[]string{"ppc64le"},
			true,
		),
		Entry("add arch to term without arch constraint",
			NewPod().WithName("test-pod").WithNodeSelectorTermsMatchExpressions(
				[]corev1.NodeSelectorRequirement{
					{
						Key:      "kubernetes.io/os",
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"linux"},
					},
				},
			).Build(),
			[]string{"ppc64le"},
			1,
			[]string{"ppc64le"},
			true,
		),
	)
})

// ApplyArchitectureConstraintsPreservesTermStructure verifies that the complete
// applyArchitectureConstraints function preserves the NodeSelectorTerms structure
var _ = Describe("ApplyArchitectureConstraintsPreservesTermStructure", func() {
	It("should preserve term structure during in-place update", func() {
		pod := NewPod().WithName("test-pod").WithNodeSelectors(utils.ArchLabel, "amd64").WithNodeSelectorTermsMatchExpressions(
			[]corev1.NodeSelectorRequirement{
				{
					Key:      "kubernetes.io/os",
					Operator: corev1.NodeSelectorOpIn,
					Values:   []string{"linux"},
				},
				{
					Key:      utils.ArchLabel,
					Operator: corev1.NodeSelectorOpIn,
					Values:   []string{"amd64", "ppc64le", "s390x"},
				},
			},
		).Build()

		originalTermCount := len(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms)

		modified := applyArchitectureConstraints(pod, []string{"ppc64le"})

		Expect(modified).To(BeTrue(), "Expected modified to be true")

		// Verify nodeSelector arch was removed
		_, exists := pod.Spec.NodeSelector[utils.ArchLabel]
		Expect(exists).To(BeFalse(), "Architecture constraint should be removed from nodeSelector")

		// Verify term count is preserved (in-place update)
		terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
		Expect(terms).To(HaveLen(originalTermCount),
			"Term count changed from %d to %d - this would cause Kubernetes API rejection", originalTermCount, len(terms))

		// Verify architecture was updated to ppc64le
		foundPpc64le := false
		for _, term := range terms {
			for _, expr := range term.MatchExpressions {
				if expr.Key == utils.ArchLabel {
					if len(expr.Values) == 1 && expr.Values[0] == "ppc64le" {
						foundPpc64le = true
					}
				}
			}
		}
		Expect(foundPpc64le).To(BeTrue(), "Expected architecture to be updated to ppc64le")

		// Verify os constraint is preserved
		foundOS := false
		for _, term := range terms {
			for _, expr := range term.MatchExpressions {
				if expr.Key == "kubernetes.io/os" {
					foundOS = true
				}
			}
		}
		Expect(foundOS).To(BeTrue(), "OS constraint should be preserved")
	})
})
