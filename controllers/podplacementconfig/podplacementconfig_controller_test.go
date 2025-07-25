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

	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/common"
	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

var _ = Describe("Controllers/PodPlacementConfig/PodPlacementConfigReconciler", Serial, Ordered, func() {
	When("Creating a cluster pod placement config", func() {
		Context("with invalid values in the plugins.nodeAffinityScoring and invalid priority stanza", func() {
			DescribeTable("The request should fail with", func(object *v1beta1.PodPlacementConfig) {
				By("Ensure no PodPlacementConfig exists")
				cppc := &v1beta1.PodPlacementConfig{}
				err := k8sClient.Get(ctx, crclient.ObjectKey{
					Name:      common.SingletonResourceObjectName,
					Namespace: utils.Namespace(),
				}, cppc)
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
					WithNamespace(utils.Namespace()).
					WithPlugins().
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, -100).
					Build()),
				Entry("Zero weight", builder.NewPodPlacementConfig().
					WithName(common.SingletonResourceObjectName).
					WithNamespace(utils.Namespace()).
					WithPlugins().
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 0).
					Build()),
				Entry("Excessive weight", builder.NewPodPlacementConfig().
					WithName(common.SingletonResourceObjectName).
					WithNamespace(utils.Namespace()).
					WithPlugins().
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 200).
					Build()),
				Entry("Wrong architecture", builder.NewPodPlacementConfig().
					WithName(common.SingletonResourceObjectName).
					WithNamespace(utils.Namespace()).
					WithPlugins().
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm("Wrong", 200).
					Build()),
				Entry("No terms", builder.NewPodPlacementConfig().
					WithName(common.SingletonResourceObjectName).
					WithNamespace(utils.Namespace()).
					WithPlugins().
					WithNodeAffinityScoring(true).
					Build()),
				Entry("Missing architecture in a term", builder.NewPodPlacementConfig().
					WithName(common.SingletonResourceObjectName).
					WithNamespace(utils.Namespace()).
					WithPlugins().
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm("", 5).
					Build()),
			)
			AfterEach(func() {
				By("Ensure the PodPlacementConfig is deleted")
				err := k8sClient.Delete(ctx, builder.NewPodPlacementConfig().WithName(common.SingletonResourceObjectName).WithNamespace(utils.Namespace()).Build())
				Expect(crclient.IgnoreNotFound(err)).NotTo(HaveOccurred(), "failed to delete PodPlacementConfig", err)
			})
		})
	})
})
