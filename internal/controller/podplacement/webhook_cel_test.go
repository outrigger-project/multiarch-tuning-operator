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
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"

	"github.com/openshift/multiarch-tuning-operator/api/common/plugins"
	"github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	. "github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

var _ = Describe("Webhook CEL applyCELInWebhook", func() {

	// TestApplyCELInWebhook_AppliesArchitecturesBeforePersistence
	Describe("applies architecture constraints from the matching PPC", func() {
		var (
			ctx      context.Context
			recorder *record.FakeRecorder
		)

		BeforeEach(func() {
			ctx = context.Background()
			recorder = record.NewFakeRecorder(10)
		})

		DescribeTable("should apply (or skip) architectures based on PPC configuration",
			func(
				pod *corev1.Pod,
				matchingPPCs []v1beta1.PodPlacementConfig,
				expectedArchitectures []string,
				expectModified bool,
			) {
				webhook := &PodSchedulingGateMutatingWebHook{}
				podWrapper := newPod(pod, ctx, recorder)
				webhook.applyCELInWebhook(ctx, podWrapper, matchingPPCs)

				if expectModified {
					Expect(podWrapper.Spec.Affinity).NotTo(BeNil(), "Expected node affinity to be set")
					Expect(podWrapper.Spec.Affinity.NodeAffinity).NotTo(BeNil())
					Expect(podWrapper.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())

					found := false
					for _, term := range podWrapper.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
						for _, expr := range term.MatchExpressions {
							if expr.Key == utils.ArchLabel && expr.Operator == corev1.NodeSelectorOpIn {
								found = true
								Expect(expr.Values).To(HaveLen(len(expectedArchitectures)))
								for i, arch := range expectedArchitectures {
									Expect(expr.Values[i]).To(Equal(arch))
								}
							}
						}
					}
					Expect(found).To(BeTrue(), "Architecture requirement not found in node affinity")
				} else {
					if podWrapper.Spec.Affinity != nil &&
						podWrapper.Spec.Affinity.NodeAffinity != nil &&
						podWrapper.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
						for _, term := range podWrapper.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
							for _, expr := range term.MatchExpressions {
								Expect(expr.Key).NotTo(Equal(utils.ArchLabel), "Unexpected architecture requirement found")
							}
						}
					}
				}
			},
			Entry("applies CEL architecture from highest priority PPC",
				NewPod().WithName("test-pod").WithNamespace("default").Build(),
				[]v1beta1.PodPlacementConfig{
					*NewPodPlacementConfig().WithName("ppc-high-priority").WithNamespace("default").WithPriority(100).
						WithCelArchitecturePlacement(true, []string{"amd64"}, []plugins.ArchitectureRule{
							NewRule("test-rule", "self.metadata.name == 'test-pod'", "ppc64le"),
						}).Build(),
				},
				[]string{"ppc64le"}, true,
			),
			Entry("no modification when no CEL plugin enabled",
				NewPod().WithName("test-pod").WithNamespace("default").Build(),
				[]v1beta1.PodPlacementConfig{
					*NewPodPlacementConfig().WithName("ppc-no-cel").WithNamespace("default").WithPriority(100).
						WithNodeAffinityScoring(true).Build(),
				},
				nil, false,
			),
			Entry("applies fallback when no rules match",
				NewPod().WithName("other-pod").WithNamespace("default").Build(),
				[]v1beta1.PodPlacementConfig{
					*NewPodPlacementConfig().WithName("ppc-with-fallback").WithNamespace("default").WithPriority(100).
						WithCelArchitecturePlacement(true, []string{"amd64", "arm64"}, []plugins.ArchitectureRule{
							NewRule("test-rule", "self.metadata.name == 'test-pod'", "ppc64le"),
						}).Build(),
				},
				[]string{"amd64", "arm64"}, true,
			),
		)
	})

	// TestApplyCELInWebhook_MalformedPPCDoesNotBlockLowerPriority
	It("should not block lower-priority PPCs when the higher-priority PPC has malformed CEL", func() {
		ctx := context.Background()
		recorder := record.NewFakeRecorder(10)

		pod := NewPod().WithName("test-pod").WithNamespace("default").Build()
		matchingPPCs := []v1beta1.PodPlacementConfig{
			*NewPodPlacementConfig().WithName("ppc-malformed-high-priority").WithNamespace("default").WithPriority(200).
				WithCelArchitecturePlacement(true, []string{"amd64"}, []plugins.ArchitectureRule{
					NewRule("malformed-rule", "self.metadata.name ==", "s390x"),
				}).Build(),
			*NewPodPlacementConfig().WithName("ppc-valid-low-priority").WithNamespace("default").WithPriority(100).
				WithCelArchitecturePlacement(true, []string{"amd64"}, []plugins.ArchitectureRule{
					NewRule("valid-rule", "self.metadata.name == 'test-pod'", "ppc64le"),
				}).Build(),
		}

		webhook := &PodSchedulingGateMutatingWebHook{}
		podWrapper := newPod(pod, ctx, recorder)
		webhook.applyCELInWebhook(ctx, podWrapper, matchingPPCs)

		Expect(podWrapper.Spec.Affinity).NotTo(BeNil(), "Expected node affinity to be set from lower-priority PPC")
		Expect(podWrapper.Spec.Affinity.NodeAffinity).NotTo(BeNil())
		Expect(podWrapper.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())

		found := false
		for _, term := range podWrapper.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
			for _, expr := range term.MatchExpressions {
				if expr.Key == utils.ArchLabel && expr.Operator == corev1.NodeSelectorOpIn {
					found = true
					Expect(expr.Values).To(ConsistOf("ppc64le"))
				}
			}
		}
		Expect(found).To(BeTrue(), "Architecture requirement from lower-priority PPC not found")
	})

	// TestApplyCELInWebhook_RespectsPPCPriority
	It("should sort PPCs by priority and apply only the highest-priority matching PPC", func() {
		ctx := context.Background()
		recorder := record.NewFakeRecorder(10)

		pod := NewPod().WithName("test-pod").WithNamespace("default").Build()
		matchingPPCs := []v1beta1.PodPlacementConfig{
			*NewPodPlacementConfig().WithName("ppc-low-priority").WithNamespace("default").WithPriority(50).
				WithCelArchitecturePlacement(true, []string{"arm64"}, nil).Build(),
			*NewPodPlacementConfig().WithName("ppc-high-priority").WithNamespace("default").WithPriority(150).
				WithCelArchitecturePlacement(true, []string{"ppc64le"}, nil).Build(),
			*NewPodPlacementConfig().WithName("ppc-medium-priority").WithNamespace("default").WithPriority(100).
				WithCelArchitecturePlacement(true, []string{"s390x"}, nil).Build(),
		}

		webhook := &PodSchedulingGateMutatingWebHook{}
		podWrapper := newPod(pod, ctx, recorder)
		webhook.applyCELInWebhook(ctx, podWrapper, matchingPPCs)

		Expect(podWrapper.Spec.Affinity).NotTo(BeNil())
		Expect(podWrapper.Spec.Affinity.NodeAffinity).NotTo(BeNil())
		Expect(podWrapper.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())

		found := false
		for _, term := range podWrapper.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
			for _, expr := range term.MatchExpressions {
				if expr.Key == utils.ArchLabel && expr.Operator == corev1.NodeSelectorOpIn {
					found = true
					Expect(expr.Values).To(ConsistOf("ppc64le"),
						"Expected architecture [ppc64le] from highest priority PPC, got %v", expr.Values)
				}
			}
		}
		Expect(found).To(BeTrue(), "Architecture requirement from highest priority PPC not found")
	})

	// TestApplyCELInWebhook_RemovesNodeSelectorArchBeforeAdmission
	It("should remove kubernetes.io/arch from nodeSelector and set NodeAffinity before admission", func() {
		ctx := context.Background()
		recorder := record.NewFakeRecorder(10)

		pod := NewPod().WithName("test-pod-with-nodeselector").WithNamespace("default").
			WithNodeSelectors(utils.ArchLabel, "amd64", "other-key", "other-value").Build()

		matchingPPCs := []v1beta1.PodPlacementConfig{
			*NewPodPlacementConfig().WithName("ppc-cel").WithNamespace("default").WithPriority(100).
				WithCelArchitecturePlacement(true, []string{"ppc64le"}, nil).Build(),
		}

		webhook := &PodSchedulingGateMutatingWebHook{}
		podWrapper := newPod(pod, ctx, recorder)
		webhook.applyCELInWebhook(ctx, podWrapper, matchingPPCs)

		Expect(podWrapper.Spec.NodeSelector).NotTo(HaveKey(utils.ArchLabel),
			"kubernetes.io/arch should have been removed from nodeSelector by the webhook")
		Expect(podWrapper.Spec.NodeSelector["other-key"]).To(Equal("other-value"),
			"non-arch nodeSelector key must be preserved")

		Expect(podWrapper.Spec.Affinity).NotTo(BeNil())
		Expect(podWrapper.Spec.Affinity.NodeAffinity).NotTo(BeNil())
		Expect(podWrapper.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())
		terms := podWrapper.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
		Expect(terms).NotTo(BeEmpty(), "Expected at least one NodeSelectorTerm after applyCELInWebhook")

		found := false
		for _, term := range terms {
			for _, expr := range term.MatchExpressions {
				if expr.Key == utils.ArchLabel && expr.Operator == corev1.NodeSelectorOpIn {
					found = true
					Expect(expr.Values).To(ConsistOf("ppc64le"))
				}
			}
		}
		Expect(found).To(BeTrue(), "Architecture requirement not found in NodeAffinity after applyCELInWebhook")
	})

	// TestApplyCELInWebhook_ControllerIdempotencyAfterWebhookMutation
	It("should not change NodeSelectorTerms count when re-applying the same constraints (KEP-3838 idempotency)", func() {
		ctx := context.Background()
		recorder := record.NewFakeRecorder(10)

		pod := NewPod().WithName("post-webhook-pod").WithNamespace("default").
			WithNodeSelectorTermsMatchExpressions(
				[]corev1.NodeSelectorRequirement{
					{Key: "topology.kubernetes.io/zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"us-east-1a"}},
					{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"ppc64le"}},
				},
			).Build()

		matchingPPCs := []v1beta1.PodPlacementConfig{
			*NewPodPlacementConfig().WithName("ppc-cel").WithNamespace("default").WithPriority(100).
				WithCelArchitecturePlacement(true, []string{"ppc64le"}, nil).Build(),
		}

		originalTermCount := len(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms)

		webhook := &PodSchedulingGateMutatingWebHook{}
		podWrapper := newPod(pod, ctx, recorder)
		webhook.applyCELInWebhook(ctx, podWrapper, matchingPPCs)

		finalTermCount := len(podWrapper.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms)
		Expect(finalTermCount).To(Equal(originalTermCount),
			"NodeSelectorTerms count changed from %d to %d — "+
				"this would cause Kubernetes to reject the controller update with HTTP 422 "+
				"(KEP-3838 immutability constraint)", originalTermCount, finalTermCount)

		zoneFound := false
		for _, term := range podWrapper.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
			for _, expr := range term.MatchExpressions {
				if expr.Key == "topology.kubernetes.io/zone" {
					zoneFound = true
				}
			}
		}
		Expect(zoneFound).To(BeTrue(), "non-architecture zone constraint was removed — must be preserved")

		archFound := false
		for _, term := range podWrapper.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
			for _, expr := range term.MatchExpressions {
				if expr.Key == utils.ArchLabel {
					archFound = true
					Expect(expr.Values).To(ConsistOf("ppc64le"))
				}
			}
		}
		Expect(archFound).To(BeTrue(), "Architecture constraint not found after controller re-application")
	})

	// TestApplyCELInWebhook_KEP3838_ArchOnlyTermPreserved
	// Regression test for the root cause of the integration failure:
	// a 2-term pod (arch-only term + zone term) must keep both terms after the
	// webhook fires so the controller's subsequent Update does not send fewer
	// terms than what was persisted, which would be rejected with HTTP 422
	// ("no additions/deletions to non-empty NodeSelectorTerms list are allowed",
	// KEP-3838 immutability requirement).
	It("should preserve both NodeSelectorTerms when one term is arch-only (KEP-3838 webhook regression)", func() {
		ctx := context.Background()
		recorder := record.NewFakeRecorder(10)

		// Pod with 2 terms: Term 1 = arch-only, Term 2 = zone-only.
		// This mirrors the exact scenario from the failing integration test.
		pod := NewPod().WithName("kep3838-webhook-pod").WithNamespace("default").
			WithNodeSelectorTermsMatchExpressions(
				// Term 1: arch-only — must NOT be dropped
				[]corev1.NodeSelectorRequirement{
					{Key: utils.ArchLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{utils.ArchitectureAmd64}},
				},
				// Term 2: zone constraint — must be preserved
				[]corev1.NodeSelectorRequirement{
					{Key: "topology.kubernetes.io/zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"us-east-1a"}},
				},
			).Build()

		matchingPPCs := []v1beta1.PodPlacementConfig{
			*NewPodPlacementConfig().WithName("kep3838-ppc").WithNamespace("default").WithPriority(100).
				WithCelArchitecturePlacement(true, []string{utils.ArchitecturePpc64le}, []plugins.ArchitectureRule{
					NewRule("always-true", `true`, utils.ArchitecturePpc64le),
				}).Build(),
		}

		wh := &PodSchedulingGateMutatingWebHook{}
		podWrapper := newPod(pod, ctx, recorder)
		wh.applyCELInWebhook(ctx, podWrapper, matchingPPCs)

		terms := podWrapper.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
		Expect(terms).To(HaveLen(2),
			"webhook must NOT shrink NodeSelectorTerms (KEP-3838): expected 2 terms, got %d — "+
				"this would cause a Kubernetes HTTP 422 on the controller Update", len(terms))

		// Verify architecture was updated to ppc64le in BOTH terms
		for i, term := range terms {
			found := false
			for _, expr := range term.MatchExpressions {
				if expr.Key == utils.ArchLabel {
					found = true
					Expect(expr.Values).To(ConsistOf(utils.ArchitecturePpc64le),
						"term[%d]: expected ppc64le architecture, got %v", i, expr.Values)
				}
			}
			Expect(found).To(BeTrue(), "term[%d]: architecture constraint missing after webhook applied", i)
		}

		// Verify zone is preserved in term 1 (index 1 = original zone-only term)
		zoneFound := false
		for _, expr := range terms[1].MatchExpressions {
			if expr.Key == "topology.kubernetes.io/zone" {
				zoneFound = true
				Expect(expr.Values).To(ConsistOf("us-east-1a"), "zone value was modified")
			}
		}
		Expect(zoneFound).To(BeTrue(), "zone constraint was removed from the zone term — must be preserved")
	})
})
