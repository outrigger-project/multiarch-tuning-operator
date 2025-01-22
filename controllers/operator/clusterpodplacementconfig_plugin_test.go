package operator

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/common"
	operatorplugins "github.com/openshift/multiarch-tuning-operator/apis/multiarch/common/plugins"
	baseplugins2 "github.com/openshift/multiarch-tuning-operator/apis/multiarch/common/plugins/base_plugin"

	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/common/plugins/nodeaffinityscoring"
	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1alpha1"
	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/framework"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("ClusterPodPlacementConfig Conversion Tests", func() {
	var (
		ctx    context.Context
		client runtimeclient.Client
	)
	BeforeEach(func() {
		By("Creating the ClusterPodPlacementConfig")
		err := k8sClient.Create(ctx, builder.NewClusterPodPlacementConfig().WithName(common.SingletonResourceObjectName).Build())
		Expect(err).NotTo(HaveOccurred(), "failed to create ClusterPodPlacementConfig", err)
		validateReconcile()
	})
	AfterEach(func() {
		By("Deleting the ClusterPodPlacementConfig")
		err := k8sClient.Delete(ctx, builder.NewClusterPodPlacementConfig().WithName(common.SingletonResourceObjectName).Build())
		Expect(err).NotTo(HaveOccurred(), "failed to delete ClusterPodPlacementConfig", err)
		Eventually(framework.ValidateDeletion(k8sClient, ctx)).Should(Succeed(), "the ClusterPodPlacementConfig should be deleted")
	})
	Context("When the operator is running and a pod placement config is created", func() {
		It("should create a v1beta1 CR and successfully convert it to v1alpha1", func() {
			By("Creating a v1beta1 ClusterPodPlacementConfig")
			err := client.Create(ctx, &v1beta1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cppc",
					Namespace: "default",
				},
				Spec: v1beta1.ClusterPodPlacementConfigSpec{
					LogVerbosity: "Normal",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				c := &v1alpha1.ClusterPodPlacementConfig{}
				err := client.Get(ctx, runtimeclient.ObjectKey{Name: "test-cppc", Namespace: "default"}, c)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(c.Spec.LogVerbosity).To(Equal("Normal"))
			}, time.Second*10, time.Millisecond*250).Should(Succeed())
		})

		It("should create a v1alpha1 CR and successfully convert it to v1beta1", func() {
			By("Creating a v1alpha1 ClusterPodPlacementConfig")
			err := client.Create(ctx, &v1alpha1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cppc",
					Namespace: "default",
				},
				Spec: v1alpha1.ClusterPodPlacementConfigSpec{
					LogVerbosity: "Normal",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				c := &v1beta1.ClusterPodPlacementConfig{}
				err := client.Get(ctx, runtimeclient.ObjectKey{Name: "test-cppc", Namespace: "default"}, c)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(c.Spec.LogVerbosity).To(Equal("Normal"))
			}, time.Second*10, time.Millisecond*250).Should(Succeed())
		})
	})
	Context("When a v1alpha1 ClusterPodPlacementConfig with NodeAffinityScoringPlugin is created", func() {
		It("should convert to v1beta1 and preserve NodeAffinityScoringPlugin configuration", func() {
			// Step 1: Create a v1alpha1 ClusterPodPlacementConfig object
			v1alpha1Obj := &v1alpha1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cppc-with-specs",
					Namespace: "default",
				},
				Spec: v1alpha1.ClusterPodPlacementConfigSpec{
					LogVerbosity: "Normal",
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "test"},
					},
					Plugins: operatorplugins.OperatorPlugins{
						NodeAffinityScoring: &nodeaffinityscoring.NodeAffinityScoring{
							BasePlugin: baseplugins2.BasePlugin{
								Enabled: true,
							},
							Platforms: []nodeaffinityscoring.NodeAffinityScoringPlatformTerm{
								{Architecture: "ppc64le", Weight: 50},
							},
						},
					},
				},
			}

			By("Creating the v1alpha1 ClusterPodPlacementConfig")
			err := client.Create(ctx, v1alpha1Obj)
			Expect(err).NotTo(HaveOccurred())

			// Step 2: Validate the conversion to v1beta1
			Eventually(func(g Gomega) {
				v1beta1Obj := &v1beta1.ClusterPodPlacementConfig{}
				err := client.Get(ctx, runtimeclient.ObjectKey{Name: "test-cppc-with-specs", Namespace: "default"}, v1beta1Obj)
				g.Expect(err).NotTo(HaveOccurred())

				// Verify the LogVerbosity field
				g.Expect(v1beta1Obj.Spec.LogVerbosity).To(Equal("Normal"))

				// Verify the NamespaceSelector
				g.Expect(v1beta1Obj.Spec.NamespaceSelector.MatchLabels).To(Equal(map[string]string{"env": "test"}))

				// Verify the Plugins field
				g.Expect(v1beta1Obj.Spec.Plugins.NodeAffinityScoring).NotTo(BeNil())
				g.Expect(v1beta1Obj.Spec.Plugins.NodeAffinityScoring.Enabled).To(BeTrue())
				g.Expect(v1beta1Obj.Spec.Plugins.NodeAffinityScoring.Platforms).To(ConsistOf(
					nodeaffinityscoring.NodeAffinityScoringPlatformTerm{Architecture: "ppc64le", Weight: 50},
				))
			}, time.Second*10, time.Millisecond*250).Should(Succeed())
		})
	})
})
