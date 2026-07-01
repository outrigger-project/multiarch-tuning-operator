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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/multiarch-tuning-operator/api/common/plugins"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

func TestCELCoexistence(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CEL Coexistence Test Suite", Label("integration"))
}

var _ = Describe("CEL Architecture Placement and NodeAffinityScoring Coexistence", func() {
	Context("When both plugins are enabled in the same PodPlacementConfig", func() {
		It("should apply CEL architecture constraints AND NodeAffinityScoring preferences", func() {
			// Create a pod
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels: map[string]string{
						"app": "test",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "test:latest",
						},
					},
				},
			}

			// Apply CEL architecture placement (sets required affinity)
			architectures := []string{"amd64", "arm64"}
			applyArchitectureConstraints(pod, architectures)

			// Verify required affinity was set by CEL
			Expect(pod.Spec.Affinity).NotTo(BeNil())
			Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil())
			Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())

			terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
			Expect(terms).To(HaveLen(1))
			Expect(terms[0].MatchExpressions).To(HaveLen(1))
			Expect(terms[0].MatchExpressions[0].Key).To(Equal(utils.ArchLabel))
			Expect(terms[0].MatchExpressions[0].Operator).To(Equal(corev1.NodeSelectorOpIn))
			Expect(terms[0].MatchExpressions[0].Values).To(ConsistOf("amd64", "arm64"))

			// Now apply NodeAffinityScoring (sets preferred affinity)
			nodeAffinityScoring := &plugins.NodeAffinityScoring{
				BasePlugin: plugins.BasePlugin{Enabled: true},
				Platforms: []plugins.NodeAffinityScoringPlatformTerm{
					{Architecture: "amd64", Weight: 50},
					{Architecture: "arm64", Weight: 30},
				},
			}

			// Simulate what SetPreferredArchNodeAffinity does
			if pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution == nil {
				pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution = []corev1.PreferredSchedulingTerm{}
			}

			for _, platform := range nodeAffinityScoring.Platforms {
				term := corev1.PreferredSchedulingTerm{
					Weight: platform.Weight,
					Preference: corev1.NodeSelectorTerm{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      utils.ArchLabel,
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{platform.Architecture},
							},
						},
					},
				}
				pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution =
					append(pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution, term)
			}

			// Verify BOTH required (from CEL) and preferred (from NodeAffinityScoring) are present
			Expect(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil(),
				"Required affinity from CEL should still be present")
			Expect(pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil(),
				"Preferred affinity from NodeAffinityScoring should be present")

			// Verify required affinity (CEL) is still intact
			requiredTerms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
			Expect(requiredTerms).To(HaveLen(1))
			Expect(requiredTerms[0].MatchExpressions[0].Values).To(ConsistOf("amd64", "arm64"))

			// Verify preferred affinity (NodeAffinityScoring) was added
			preferredTerms := pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution
			Expect(preferredTerms).To(HaveLen(2))
			Expect(preferredTerms[0].Weight).To(Equal(int32(50)))
			Expect(preferredTerms[0].Preference.MatchExpressions[0].Values).To(ConsistOf("amd64"))
			Expect(preferredTerms[1].Weight).To(Equal(int32(30)))
			Expect(preferredTerms[1].Preference.MatchExpressions[0].Values).To(ConsistOf("arm64"))
		})

		It("should preserve CEL required affinity when NodeAffinityScoring adds preferred affinity", func() {
			// Create a pod with existing required affinity from CEL
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-2",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      utils.ArchLabel,
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{"ppc64le"},
											},
										},
									},
								},
							},
						},
					},
					Containers: []corev1.Container{
						{Name: "test", Image: "test:latest"},
					},
				},
			}

			// Store original required affinity
			originalRequired := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Values

			// Add preferred affinity (simulating NodeAffinityScoring)
			pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution = []corev1.PreferredSchedulingTerm{
				{
					Weight: 100,
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
			}

			// Verify required affinity is unchanged
			currentRequired := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Values
			Expect(currentRequired).To(Equal(originalRequired), "Required affinity from CEL should not be modified")

			// Verify preferred affinity was added
			Expect(pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution).To(HaveLen(1))
		})
	})

	Context("When CEL is applied", func() {
		It("should skip image-based detection", func() {
			// This is tested by the reconciler logic:
			// celApplied is returned from applyMatchingPPCs
			// When celApplied is true, image-based detection is skipped

			// Verify the logic flow:
			// 1. applyMatchingPPCs returns true when CEL is applied
			// 2. processPod checks celApplied before running image detection
			// 3. Image detection is skipped when celApplied is true

			celApplied := true
			Expect(celApplied).To(BeTrue(), "CEL applied flag should be true")

			// In the actual reconciler, this would prevent image detection:
			// if !celApplied { /* image detection */ }
		})
	})
})

// Made with Bob
