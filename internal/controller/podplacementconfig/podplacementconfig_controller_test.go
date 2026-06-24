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

package podplacementconfig

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift/multiarch-tuning-operator/api/common"
	"github.com/openshift/multiarch-tuning-operator/api/common/plugins"
	"github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/framework"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

var _ = Describe("Internal/Controller/PodPlacementConfig/PodPlacementConfigReconciler", Serial, func() {
	When("Creating a local podplacementconfig", func() {
		Context("with invalid values in the plugins.nodeAffinityScoring and invalid priority stanza", func() {
			DescribeTable("The request should fail with", func(object *v1beta1.PodPlacementConfig) {
				By("Ensure no PodPlacementConfig exists")
				ppc := &v1beta1.PodPlacementConfig{}
				err := k8sClient.Get(ctx, crclient.ObjectKey{
					Name:      common.SingletonResourceObjectName,
					Namespace: testNamespace,
				}, ppc)
				Expect(errors.IsNotFound(err)).To(BeTrue(), "The PodPlacementConfig should not exist")
				// Expect(errors.IsNotFound(err)).To(BeTrue(), "The PodPlacementConfig should not exist")
				By("Create the PodPlacementConfig")
				err = k8sClient.Create(ctx, object)
				By(fmt.Sprintf("The error is: %+v", err))
				By("Verify the PodPlacementConfig is not created")
				Expect(err).To(HaveOccurred(), "The create PodPlacementConfig should not be accepted")
				By("Verify the error is 'invalid'")
				Expect(errors.IsInvalid(err)).To(BeTrue(), "The invalid PodPlacementConfig should not be accepted")
			},
				Entry("Negative weight", builder.NewPodPlacementConfig().
					WithName(common.SingletonResourceObjectName).
					WithNamespace(testNamespace).
					WithPlugins().
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, -100).
					Build()),
				Entry("Zero weight", builder.NewPodPlacementConfig().
					WithName(common.SingletonResourceObjectName).
					WithNamespace(testNamespace).
					WithPlugins().
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 0).
					Build()),
				Entry("Excessive weight", builder.NewPodPlacementConfig().
					WithName(common.SingletonResourceObjectName).
					WithNamespace(testNamespace).
					WithPlugins().
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 200).
					Build()),
				Entry("Wrong architecture", builder.NewPodPlacementConfig().
					WithName(common.SingletonResourceObjectName).
					WithNamespace(testNamespace).
					WithPlugins().
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm("Wrong", 200).
					Build()),
				Entry("No terms", builder.NewPodPlacementConfig().
					WithName(common.SingletonResourceObjectName).
					WithNamespace(testNamespace).
					WithPlugins().
					WithNodeAffinityScoring(true).
					Build()),
				Entry("Missing architecture in a term", builder.NewPodPlacementConfig().
					WithName(common.SingletonResourceObjectName).
					WithNamespace(testNamespace).
					WithPlugins().
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm("", 5).
					Build()),
			)
			AfterEach(func() {
				By("Ensure the PodPlacementConfig is deleted")
				err := k8sClient.Delete(ctx, builder.NewPodPlacementConfig().WithName(common.SingletonResourceObjectName).WithNamespace(testNamespace).Build())
				Expect(crclient.IgnoreNotFound(err)).NotTo(HaveOccurred(), "failed to delete PodPlacementConfig", err)
			})
		})
		Context("the weebhook shoud deny creation", func() {
			It("when multiple items for the same architecture in the plugins.nodeAffinityScoring.Platforms list", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()
				By("Creating a local PodPlacementConfig with the same architecture")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-ppc").
						WithNamespace(ns.Name).
						WithPlugins().
						WithNodeAffinityScoring(true).
						WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 50).
						WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 100).
						Build(),
				)
				Expect(err).To(HaveOccurred(), "the PodPlacementConfig should not be accepted", err)
			})
			It("when there is an existing ppc with the same priority in the same namespace", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()
				By("Creating a local PodPlacementConfig with a Priority setting")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-ppc").
						WithNamespace(ns.Name).
						WithPriority(50).
						WithPlugins().
						WithNodeAffinityScoring(true).
						WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 50).
						Build(),
				)
				Expect(err).NotTo(HaveOccurred(), "the PodPlacementConfig should be accepted", err)
				By("Check the PodPlacementConfig is created and priority is 50")
				Eventually(func(g Gomega) {
					ppc := &v1beta1.PodPlacementConfig{}
					err := k8sClient.Get(ctx, crclient.ObjectKey{
						Name:      "test-ppc",
						Namespace: ns.Name,
					}, ppc)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get podplacementconfig", err)
					g.Expect(ppc.Spec.Priority).To(Equal(uint8(50)), "the ppc Priority should equal 50")
				}).Should(Succeed(), "the PodPlacementConfig should be created")
				By("Creating another local PodPlacementConfig with the same Priority")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-ppc-2").
						WithNamespace(ns.Name).
						WithPriority(50).
						WithPlugins().
						WithNodeAffinityScoring(true).
						WithNodeAffinityScoringTerm(utils.ArchitectureArm64, 50).
						Build(),
				)
				Expect(err).To(HaveOccurred(), "the PodPlacementConfig should not be accepted", err)
			})
			It("when update a local ppc priority to an existing one", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()
				By("Creating a local PodPlacementConfig with priority 30")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-ppc").
						WithNamespace(ns.Name).
						WithPriority(30).
						WithPlugins().
						WithNodeAffinityScoring(true).
						WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 50).
						Build(),
				)
				Expect(err).NotTo(HaveOccurred(), "the PodPlacementConfig should be accepted", err)
				By("Creating another local PodPlacementConfig with priority 50")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-ppc-2").
						WithNamespace(ns.Name).
						WithPriority(50).
						WithPlugins().
						WithNodeAffinityScoring(true).
						WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 50).
						Build(),
				)
				Expect(err).NotTo(HaveOccurred(), "the PodPlacementConfig should be accepted", err)
				By("Check the PodPlacementConfig is created and priority is 50")
				Eventually(func(g Gomega) {
					ppc2 := &v1beta1.PodPlacementConfig{}
					err := k8sClient.Get(ctx, crclient.ObjectKey{
						Name:      "test-ppc-2",
						Namespace: ns.Name,
					}, ppc2)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get podplacementconfig", err)
					g.Expect(ppc2.Spec.Priority).To(Equal(uint8(50)), "the ppc Priority should equal 50")
				}).Should(Succeed(), "the PodPlacementConfig should be created")
				By("Update the first local PodPlacementConfig priority to 50")
				ppc1 := &v1beta1.PodPlacementConfig{}
				err = k8sClient.Get(ctx, crclient.ObjectKey{
					Name:      "test-ppc",
					Namespace: ns.Name,
				}, ppc1)
				Expect(err).NotTo(HaveOccurred())
				ppc1.Spec.Priority = 50
				err = k8sClient.Update(ctx, ppc1)
				Expect(err).To(HaveOccurred(), "the PodPlacementConfig update should not be accepted", err)
			})
		})
		Context("the weebhook shoud allow creation", func() {
			It("when the ppc is recreated with the same priority after delation", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()
				By("Creating a local PodPlacementConfig with priority 50")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-ppc").
						WithNamespace(ns.Name).
						WithPriority(50).
						WithPlugins().
						WithNodeAffinityScoring(true).
						WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 50).
						Build(),
				)
				By("Check it can be created")
				Expect(err).NotTo(HaveOccurred(), "the PodPlacementConfig should be accepted", err)
				By("Deleting above created PodPlacementConfig")
				err = k8sClient.Delete(ctx, builder.NewPodPlacementConfig().
					WithName("test-ppc").
					WithNamespace(ns.Name).Build())
				Expect(err).NotTo(HaveOccurred())
				By("Check the PodPlacementConfig is deleted")
				Eventually(func(g Gomega) {
					ppc := &v1beta1.PodPlacementConfig{}
					err := k8sClient.Get(ctx, crclient.ObjectKey{
						Name:      "test-ppc",
						Namespace: ns.Name,
					}, ppc)
					Expect(errors.IsNotFound(err)).To(BeTrue(), "failed to delete podplacementconfig", err)
				}).Should(Succeed(), "the PodPlacementConfig should be deleted")
				By("Creating the PodPlacementConfig with the same priority 50 again")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-ppc").
						WithNamespace(ns.Name).
						WithPriority(50).
						WithPlugins().
						WithNodeAffinityScoring(true).
						WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 50).
						Build(),
				)
				By("Check it can be created again")
				Expect(err).NotTo(HaveOccurred(), "the PodPlacementConfig should be accepted", err)
			})
		})
	})

	// CEL Architecture Placement Plugin Webhook Validation Tests
	When("Creating a PodPlacementConfig with celArchitecturePlacement plugin", func() {
		Context("with valid CEL expressions", func() {
			It("should accept valid boolean CEL expression", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()

				By("Creating PodPlacementConfig with valid CEL expression")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-cel-valid").
						WithNamespace(ns.Name).
						WithCelArchitecturePlacement(
							true,
							[]string{"amd64"},
							[]plugins.ArchitectureRule{
								{
									Name:          "test-rule",
									Expression:    "has(self.metadata.labels.app)",
									Architectures: []string{"arm64"},
								},
							},
						).
						Build(),
				)
				Expect(err).NotTo(HaveOccurred(), "valid CEL expression should be accepted")
			})

			It("should accept valid namespace-based CEL expression", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()

				By("Creating PodPlacementConfig with namespace CEL expression")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-cel-namespace").
						WithNamespace(ns.Name).
						WithCelArchitecturePlacement(
							true,
							[]string{"amd64"},
							[]plugins.ArchitectureRule{
								{
									Name:          "namespace-rule",
									Expression:    "self.metadata.namespace == 'production'",
									Architectures: []string{"ppc64le"},
								},
							},
						).
						Build(),
				)
				Expect(err).NotTo(HaveOccurred(), "namespace-based CEL expression should be accepted")
			})

			It("should accept valid annotation-based CEL expression", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()

				By("Creating PodPlacementConfig with annotation CEL expression")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-cel-annotation").
						WithNamespace(ns.Name).
						WithCelArchitecturePlacement(
							true,
							[]string{"amd64"},
							[]plugins.ArchitectureRule{
								{
									Name:          "annotation-rule",
									Expression:    "has(self.metadata.annotations.arch)",
									Architectures: []string{"s390x"},
								},
							},
						).
						Build(),
				)
				Expect(err).NotTo(HaveOccurred(), "annotation-based CEL expression should be accepted")
			})

			It("should accept valid compound CEL expression with AND operator", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()

				By("Creating PodPlacementConfig with compound AND expression")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-cel-and").
						WithNamespace(ns.Name).
						WithCelArchitecturePlacement(
							true,
							[]string{"amd64"},
							[]plugins.ArchitectureRule{
								{
									Name:          "compound-and-rule",
									Expression:    "has(self.metadata.labels.app) && self.metadata.namespace == 'prod'",
									Architectures: []string{"arm64"},
								},
							},
						).
						Build(),
				)
				Expect(err).NotTo(HaveOccurred(), "compound AND CEL expression should be accepted")
			})

			It("should accept valid compound CEL expression with OR operator", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()

				By("Creating PodPlacementConfig with compound OR expression")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-cel-or").
						WithNamespace(ns.Name).
						WithCelArchitecturePlacement(
							true,
							[]string{"amd64"},
							[]plugins.ArchitectureRule{
								{
									Name:          "compound-or-rule",
									Expression:    "self.metadata.namespace == 'prod' || self.metadata.namespace == 'staging'",
									Architectures: []string{"ppc64le"},
								},
							},
						).
						Build(),
				)
				Expect(err).NotTo(HaveOccurred(), "compound OR CEL expression should be accepted")
			})

			It("should accept fallback-only configuration without rules", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()

				By("Creating PodPlacementConfig with fallback-only")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-cel-fallback-only").
						WithNamespace(ns.Name).
						WithCelArchitecturePlacement(
							true,
							[]string{"amd64", "arm64"},
							nil,
						).
						Build(),
				)
				Expect(err).NotTo(HaveOccurred(), "fallback-only configuration should be accepted")
			})

			It("should accept multiple valid rules", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()

				By("Creating PodPlacementConfig with multiple valid rules")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-cel-multiple").
						WithNamespace(ns.Name).
						WithCelArchitecturePlacement(
							true,
							[]string{"amd64"},
							[]plugins.ArchitectureRule{
								{
									Name:          "rule1",
									Expression:    "has(self.metadata.labels.tier) && self.metadata.labels.tier == 'frontend'",
									Architectures: []string{"arm64"},
								},
								{
									Name:          "rule2",
									Expression:    "self.metadata.namespace == 'backend'",
									Architectures: []string{"ppc64le"},
								},
							},
						).
						Build(),
				)
				Expect(err).NotTo(HaveOccurred(), "multiple valid rules should be accepted")
			})
		})

		Context("with invalid CEL expressions", func() {
			It("should reject malformed CEL syntax", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()

				By("Creating PodPlacementConfig with malformed CEL expression")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-cel-malformed").
						WithNamespace(ns.Name).
						WithCelArchitecturePlacement(
							true,
							[]string{"amd64"},
							[]plugins.ArchitectureRule{
								{
									Name:          "malformed-rule",
									Expression:    "self.metadata.labels[",
									Architectures: []string{"arm64"},
								},
							},
						).
						Build(),
				)
				Expect(err).To(HaveOccurred(), "malformed CEL expression should be rejected")
				Expect(err.Error()).To(ContainSubstring("invalid CEL expression"), "error should mention invalid CEL expression")
				Expect(err.Error()).To(ContainSubstring("malformed-rule"), "error should mention rule name")
			})

			It("should reject incomplete CEL expression", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()

				By("Creating PodPlacementConfig with incomplete expression")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-cel-incomplete").
						WithNamespace(ns.Name).
						WithCelArchitecturePlacement(
							true,
							[]string{"amd64"},
							[]plugins.ArchitectureRule{
								{
									Name:          "incomplete-rule",
									Expression:    "self.metadata.labels.app ==",
									Architectures: []string{"arm64"},
								},
							},
						).
						Build(),
				)
				Expect(err).To(HaveOccurred(), "incomplete CEL expression should be rejected")
				Expect(err.Error()).To(ContainSubstring("invalid CEL expression"), "error should mention invalid CEL expression")
			})

			It("should reject CEL expression with invalid operators", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()

				By("Creating PodPlacementConfig with invalid operator")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-cel-invalid-op").
						WithNamespace(ns.Name).
						WithCelArchitecturePlacement(
							true,
							[]string{"amd64"},
							[]plugins.ArchitectureRule{
								{
									Name:          "invalid-operator-rule",
									Expression:    "self.metadata.name === 'test'",
									Architectures: []string{"arm64"},
								},
							},
						).
						Build(),
				)
				Expect(err).To(HaveOccurred(), "invalid operator should be rejected")
				Expect(err.Error()).To(ContainSubstring("invalid CEL expression"), "error should mention invalid CEL expression")
			})

			It("should reject CEL expression returning string instead of boolean", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()

				By("Creating PodPlacementConfig with string-returning expression")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-cel-string").
						WithNamespace(ns.Name).
						WithCelArchitecturePlacement(
							true,
							[]string{"amd64"},
							[]plugins.ArchitectureRule{
								{
									Name:          "string-return-rule",
									Expression:    "self.metadata.name",
									Architectures: []string{"arm64"},
								},
							},
						).
						Build(),
				)
				Expect(err).To(HaveOccurred(), "string-returning expression should be rejected")
				Expect(err.Error()).To(ContainSubstring("must return a boolean"), "error should mention boolean return type requirement")
			})

			It("should reject CEL expression returning integer instead of boolean", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()

				By("Creating PodPlacementConfig with integer-returning expression")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-cel-int").
						WithNamespace(ns.Name).
						WithCelArchitecturePlacement(
							true,
							[]string{"amd64"},
							[]plugins.ArchitectureRule{
								{
									Name:          "int-return-rule",
									Expression:    "1 + 2",
									Architectures: []string{"arm64"},
								},
							},
						).
						Build(),
				)
				Expect(err).To(HaveOccurred(), "integer-returning expression should be rejected")
				Expect(err.Error()).To(ContainSubstring("must return a boolean"), "error should mention boolean return type requirement")
			})

			It("should reject CEL expression with invalid field access", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()

				By("Creating PodPlacementConfig with invalid field access")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-cel-invalid-field").
						WithNamespace(ns.Name).
						WithCelArchitecturePlacement(
							true,
							[]string{"amd64"},
							[]plugins.ArchitectureRule{
								{
									Name:          "invalid-field-rule",
									Expression:    "self.metadata.nonexistent.field == 'value'",
									Architectures: []string{"arm64"},
								},
							},
						).
						Build(),
				)
				Expect(err).To(HaveOccurred(), "invalid field access should be rejected")
				Expect(err.Error()).To(ContainSubstring("invalid CEL expression"), "error should mention invalid CEL expression")
			})
		})

		Context("with invalid architectures", func() {
			It("should reject invalid architecture name in fallback", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()

				By("Creating PodPlacementConfig with invalid fallback architecture")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-cel-invalid-arch").
						WithNamespace(ns.Name).
						WithCelArchitecturePlacement(
							true,
							[]string{"invalid-arch"},
							nil,
						).
						Build(),
				)
				Expect(err).To(HaveOccurred(), "invalid architecture should be rejected")
				Expect(err.Error()).To(ContainSubstring("invalid architectures"), "error should mention invalid architectures")
				Expect(err.Error()).To(ContainSubstring("invalid-arch"), "error should mention the invalid architecture name")
			})

			It("should reject invalid architecture name in rule", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()

				By("Creating PodPlacementConfig with invalid rule architecture")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-cel-invalid-rule-arch").
						WithNamespace(ns.Name).
						WithCelArchitecturePlacement(
							true,
							[]string{"amd64"},
							[]plugins.ArchitectureRule{
								{
									Name:          "invalid-arch-rule",
									Expression:    "true",
									Architectures: []string{"bad-arch"},
								},
							},
						).
						Build(),
				)
				Expect(err).To(HaveOccurred(), "invalid rule architecture should be rejected")
				Expect(err.Error()).To(ContainSubstring("invalid architectures"), "error should mention invalid architectures")
				Expect(err.Error()).To(ContainSubstring("bad-arch"), "error should mention the invalid architecture name")
			})

			It("should accept all valid architectures", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()

				By("Creating PodPlacementConfig with all valid architectures")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-cel-all-valid-archs").
						WithNamespace(ns.Name).
						WithCelArchitecturePlacement(
							true,
							[]string{"amd64", "arm64", "ppc64le", "s390x"},
							[]plugins.ArchitectureRule{
								{
									Name:          "all-archs-rule",
									Expression:    "true",
									Architectures: []string{"amd64", "arm64", "ppc64le", "s390x"},
								},
							},
						).
						Build(),
				)
				Expect(err).NotTo(HaveOccurred(), "all valid architectures should be accepted")
			})

			It("should reject unsupported architecture", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()

				By("Creating PodPlacementConfig with unsupported architecture")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-cel-unsupported").
						WithNamespace(ns.Name).
						WithCelArchitecturePlacement(
							true,
							[]string{"riscv64"},
							nil,
						).
						Build(),
				)
				Expect(err).To(HaveOccurred(), "unsupported architecture should be rejected")
				Expect(err.Error()).To(ContainSubstring("invalid architectures"), "error should mention invalid architectures")
			})
		})

		Context("with edge cases", func() {
			It("should reject empty CEL expression", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()

				By("Creating PodPlacementConfig with empty expression")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-cel-empty").
						WithNamespace(ns.Name).
						WithCelArchitecturePlacement(
							true,
							[]string{"amd64"},
							[]plugins.ArchitectureRule{
								{
									Name:          "empty-rule",
									Expression:    "",
									Architectures: []string{"arm64"},
								},
							},
						).
						Build(),
				)
				Expect(err).To(HaveOccurred(), "empty expression should be rejected")
			})

			It("should reject whitespace-only CEL expression", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()

				By("Creating PodPlacementConfig with whitespace-only expression")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-cel-whitespace").
						WithNamespace(ns.Name).
						WithCelArchitecturePlacement(
							true,
							[]string{"amd64"},
							[]plugins.ArchitectureRule{
								{
									Name:          "whitespace-rule",
									Expression:    "   ",
									Architectures: []string{"arm64"},
								},
							},
						).
						Build(),
				)
				Expect(err).To(HaveOccurred(), "whitespace-only expression should be rejected")
			})

			It("should accept CEL expression with unicode labels", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()

				By("Creating PodPlacementConfig with unicode label expression")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-cel-unicode").
						WithNamespace(ns.Name).
						WithCelArchitecturePlacement(
							true,
							[]string{"amd64"},
							[]plugins.ArchitectureRule{
								{
									Name:          "unicode-rule",
									Expression:    "has(self.metadata.labels['app.kubernetes.io/name'])",
									Architectures: []string{"arm64"},
								},
							},
						).
						Build(),
				)
				Expect(err).NotTo(HaveOccurred(), "unicode label expression should be accepted")
			})

			It("should accept long but valid CEL expression", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()

				By("Creating PodPlacementConfig with long expression")
				longExpr := "has(self.metadata.labels.app) && has(self.metadata.labels.tier) && " +
					"has(self.metadata.labels.env) && self.metadata.namespace == 'production' && " +
					"has(self.metadata.annotations.version)"
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-cel-long").
						WithNamespace(ns.Name).
						WithCelArchitecturePlacement(
							true,
							[]string{"amd64"},
							[]plugins.ArchitectureRule{
								{
									Name:          "long-rule",
									Expression:    longExpr,
									Architectures: []string{"arm64"},
								},
							},
						).
						Build(),
				)
				Expect(err).NotTo(HaveOccurred(), "long valid expression should be accepted")
			})

			It("should reject multiple rules when one is invalid", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()

				By("Creating PodPlacementConfig with one invalid rule among valid ones")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-cel-mixed").
						WithNamespace(ns.Name).
						WithCelArchitecturePlacement(
							true,
							[]string{"amd64"},
							[]plugins.ArchitectureRule{
								{
									Name:          "valid-rule",
									Expression:    "has(self.metadata.labels.app)",
									Architectures: []string{"arm64"},
								},
								{
									Name:          "invalid-rule",
									Expression:    "self.metadata.labels[",
									Architectures: []string{"ppc64le"},
								},
							},
						).
						Build(),
				)
				Expect(err).To(HaveOccurred(), "configuration with one invalid rule should be rejected")
				Expect(err.Error()).To(ContainSubstring("invalid-rule"), "error should mention the invalid rule name")
			})
		})

		Context("webhook integration with existing behavior", func() {
			It("should not validate CEL when plugin is disabled", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()

				By("Creating PodPlacementConfig with disabled CEL plugin and invalid expression")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-cel-disabled").
						WithNamespace(ns.Name).
						WithCelArchitecturePlacement(
							false, // Plugin disabled
							[]string{"amd64"},
							[]plugins.ArchitectureRule{
								{
									Name:          "should-not-validate",
									Expression:    "invalid[[[",
									Architectures: []string{"arm64"},
								},
							},
						).
						Build(),
				)
				Expect(err).NotTo(HaveOccurred(), "disabled plugin should not trigger validation")
			})

			It("should allow PodPlacementConfig without CEL plugin", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()

				By("Creating PodPlacementConfig without CEL plugin")
				err = k8sClient.Create(ctx,
					builder.NewPodPlacementConfig().
						WithName("test-no-cel").
						WithNamespace(ns.Name).
						WithPlugins().
						WithNodeAffinityScoring(true).
						WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 50).
						Build(),
				)
				Expect(err).NotTo(HaveOccurred(), "PodPlacementConfig without CEL plugin should be accepted")
			})

			It("should validate both CEL and NodeAffinityScoring when both enabled", func() {
				By("Create an ephemeral namespace")
				ns := framework.NewEphemeralNamespace()
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = k8sClient.Delete(ctx, ns)
				}()

				By("Creating PodPlacementConfig with both plugins")
				ppc := builder.NewPodPlacementConfig().
					WithName("test-both-plugins").
					WithNamespace(ns.Name).
					WithPlugins().
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 50).
					Build()

				ppc.Spec.Plugins.CelArchitecturePlacement = &plugins.CelArchitecturePlacement{
					BasePlugin: plugins.BasePlugin{
						Enabled: true,
					},
					FallbackArchitectures: []string{"amd64"},
					Rules: []plugins.ArchitectureRule{
						{
							Name:          "test-rule",
							Expression:    "has(self.metadata.labels.app)",
							Architectures: []string{"arm64"},
						},
					},
				}

				err = k8sClient.Create(ctx, ppc)
				Expect(err).NotTo(HaveOccurred(), "both plugins should be validated and accepted")
			})

			Context("with MaxItems validation for rules", func() {
				It("should accept exactly 1000 CEL rules", func() {
					By("Create an ephemeral namespace")
					ns := framework.NewEphemeralNamespace()
					err := k8sClient.Create(ctx, ns)
					Expect(err).NotTo(HaveOccurred())
					defer func() {
						_ = k8sClient.Delete(ctx, ns)
					}()

					By("Generating exactly 1000 rules programmatically")
					rules := make([]plugins.ArchitectureRule, 1000)
					for i := 0; i < 1000; i++ {
						rules[i] = plugins.ArchitectureRule{
							Name:          fmt.Sprintf("rule-%d", i),
							Expression:    "has(self.metadata.labels.app)",
							Architectures: []string{"amd64"},
						}
					}

					By("Creating PodPlacementConfig with exactly 1000 rules")
					err = k8sClient.Create(ctx,
						builder.NewPodPlacementConfig().
							WithName("test-cel-1000-rules").
							WithNamespace(ns.Name).
							WithCelArchitecturePlacement(
								true,
								[]string{"amd64"},
								rules,
							).
							Build(),
					)
					Expect(err).NotTo(HaveOccurred(), "exactly 1000 rules should be accepted")

					By("Verify the PodPlacementConfig was created successfully")
					Eventually(func(g Gomega) {
						ppc := &v1beta1.PodPlacementConfig{}
						err := k8sClient.Get(ctx, crclient.ObjectKey{
							Name:      "test-cel-1000-rules",
							Namespace: ns.Name,
						}, ppc)
						g.Expect(err).NotTo(HaveOccurred(), "PodPlacementConfig should exist")
						g.Expect(ppc.Spec.Plugins.CelArchitecturePlacement.Rules).To(HaveLen(1000), "should have exactly 1000 rules")
					}).Should(Succeed(), "PodPlacementConfig with 1000 rules should be created")
				})

				It("should reject 1001 CEL rules", func() {
					By("Create an ephemeral namespace")
					ns := framework.NewEphemeralNamespace()
					err := k8sClient.Create(ctx, ns)
					Expect(err).NotTo(HaveOccurred())
					defer func() {
						_ = k8sClient.Delete(ctx, ns)
					}()

					By("Generating 1001 rules programmatically")
					rules := make([]plugins.ArchitectureRule, 1001)
					for i := 0; i < 1001; i++ {
						rules[i] = plugins.ArchitectureRule{
							Name:          fmt.Sprintf("rule-%d", i),
							Expression:    "has(self.metadata.labels.app)",
							Architectures: []string{"amd64"},
						}
					}

					By("Creating PodPlacementConfig with 1001 rules")
					err = k8sClient.Create(ctx,
						builder.NewPodPlacementConfig().
							WithName("test-cel-1001-rules").
							WithNamespace(ns.Name).
							WithCelArchitecturePlacement(
								true,
								[]string{"amd64"},
								rules,
							).
							Build(),
					)
					Expect(err).To(HaveOccurred(), "1001 rules should be rejected")
					Expect(err.Error()).To(Or(
						ContainSubstring("too many"),
						ContainSubstring("maximum"),
						ContainSubstring("1000"),
						ContainSubstring("maxItems"),
					), "error should reference the rule limit")

					By("Verify the PodPlacementConfig was not created")
					ppc := &v1beta1.PodPlacementConfig{}
					err = k8sClient.Get(ctx, crclient.ObjectKey{
						Name:      "test-cel-1001-rules",
						Namespace: ns.Name,
					}, ppc)
					Expect(errors.IsNotFound(err)).To(BeTrue(), "PodPlacementConfig should not exist")
				})

				It("should handle validation performance with 1000 rules without panic", func() {
					By("Create an ephemeral namespace")
					ns := framework.NewEphemeralNamespace()
					err := k8sClient.Create(ctx, ns)
					Expect(err).NotTo(HaveOccurred())
					defer func() {
						_ = k8sClient.Delete(ctx, ns)
					}()

					By("Generating 1000 rules with varying complexity")
					rules := make([]plugins.ArchitectureRule, 1000)
					for i := 0; i < 1000; i++ {
						// Vary expression complexity to test validation performance
						var expr string
						switch i % 3 {
						case 0:
							expr = "has(self.metadata.labels.app)"
						case 1:
							expr = "self.metadata.namespace == 'production'"
						case 2:
							expr = "has(self.metadata.labels.tier) && self.metadata.namespace == 'prod'"
						}
						rules[i] = plugins.ArchitectureRule{
							Name:          fmt.Sprintf("perf-rule-%d", i),
							Expression:    expr,
							Architectures: []string{"amd64", "arm64"},
						}
					}

					By("Creating PodPlacementConfig and measuring validation doesn't panic")
					err = k8sClient.Create(ctx,
						builder.NewPodPlacementConfig().
							WithName("test-cel-perf").
							WithNamespace(ns.Name).
							WithCelArchitecturePlacement(
								true,
								[]string{"amd64"},
								rules,
							).
							Build(),
					)
					Expect(err).NotTo(HaveOccurred(), "validation should complete without panic")

					By("Verify admission validation processed all rules")
					Eventually(func(g Gomega) {
						ppc := &v1beta1.PodPlacementConfig{}
						err := k8sClient.Get(ctx, crclient.ObjectKey{
							Name:      "test-cel-perf",
							Namespace: ns.Name,
						}, ppc)
						g.Expect(err).NotTo(HaveOccurred(), "PodPlacementConfig should exist")
						g.Expect(ppc.Spec.Plugins.CelArchitecturePlacement.Rules).To(HaveLen(1000), "all 1000 rules should be present")
					}).Should(Succeed(), "validation should handle 1000 rules efficiently")
				})

				It("should verify no off-by-one error at boundary (999 rules)", func() {
					By("Create an ephemeral namespace")
					ns := framework.NewEphemeralNamespace()
					err := k8sClient.Create(ctx, ns)
					Expect(err).NotTo(HaveOccurred())
					defer func() {
						_ = k8sClient.Delete(ctx, ns)
					}()

					By("Generating exactly 999 rules")
					rules := make([]plugins.ArchitectureRule, 999)
					for i := 0; i < 999; i++ {
						rules[i] = plugins.ArchitectureRule{
							Name:          fmt.Sprintf("boundary-rule-%d", i),
							Expression:    "has(self.metadata.labels.app)",
							Architectures: []string{"amd64"},
						}
					}

					By("Creating PodPlacementConfig with 999 rules")
					err = k8sClient.Create(ctx,
						builder.NewPodPlacementConfig().
							WithName("test-cel-999-rules").
							WithNamespace(ns.Name).
							WithCelArchitecturePlacement(
								true,
								[]string{"amd64"},
								rules,
							).
							Build(),
					)
					Expect(err).NotTo(HaveOccurred(), "999 rules should be accepted (no off-by-one error)")

					By("Verify the PodPlacementConfig was created")
					Eventually(func(g Gomega) {
						ppc := &v1beta1.PodPlacementConfig{}
						err := k8sClient.Get(ctx, crclient.ObjectKey{
							Name:      "test-cel-999-rules",
							Namespace: ns.Name,
						}, ppc)
						g.Expect(err).NotTo(HaveOccurred(), "PodPlacementConfig should exist")
						g.Expect(ppc.Spec.Plugins.CelArchitecturePlacement.Rules).To(HaveLen(999), "should have exactly 999 rules")
					}).Should(Succeed(), "999 rules should be within limit")
				})

				It("should reject rules exceeding limit with one invalid rule among many", func() {
					By("Create an ephemeral namespace")
					ns := framework.NewEphemeralNamespace()
					err := k8sClient.Create(ctx, ns)
					Expect(err).NotTo(HaveOccurred())
					defer func() {
						_ = k8sClient.Delete(ctx, ns)
					}()

					By("Generating 500 valid rules and 1 invalid rule")
					rules := make([]plugins.ArchitectureRule, 501)
					for i := 0; i < 500; i++ {
						rules[i] = plugins.ArchitectureRule{
							Name:          fmt.Sprintf("valid-rule-%d", i),
							Expression:    "has(self.metadata.labels.app)",
							Architectures: []string{"amd64"},
						}
					}
					// Add one invalid rule
					rules[500] = plugins.ArchitectureRule{
						Name:          "invalid-expression-rule",
						Expression:    "self.metadata.labels[",
						Architectures: []string{"amd64"},
					}

					By("Creating PodPlacementConfig with invalid rule among valid ones")
					err = k8sClient.Create(ctx,
						builder.NewPodPlacementConfig().
							WithName("test-cel-invalid-among-many").
							WithNamespace(ns.Name).
							WithCelArchitecturePlacement(
								true,
								[]string{"amd64"},
								rules,
							).
							Build(),
					)
					Expect(err).To(HaveOccurred(), "invalid rule should be caught even with many rules")
					Expect(err.Error()).To(ContainSubstring("invalid CEL expression"), "error should mention invalid CEL expression")
					Expect(err.Error()).To(ContainSubstring("invalid-expression-rule"), "error should identify the specific invalid rule")
				})
			})
		})
	})
})
