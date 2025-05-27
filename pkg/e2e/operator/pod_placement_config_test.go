package operator_test

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/common"
	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1alpha1"
	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1beta1"

	"github.com/openshift/multiarch-tuning-operator/pkg/e2e"
	. "github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/framework"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

const (
	helloOpenshiftPublicMultiarchImage = "quay.io/openshifttest/hello-openshift:1.2.0"
)

var _ = Describe("The Multiarch Tuning Operator", Serial, func() {
	var (
		podLabel                  = map[string]string{"app": "test"}
		schedulingGateLabel       = map[string]string{utils.SchedulingGateLabel: utils.SchedulingGateLabelValueRemoved}
		schedulingGateNotSetLabel = map[string]string{utils.SchedulingGateLabel: utils.LabelValueNotSet}
	)
	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			By("The test case failed, get the podplacement and podplacement webhook logs for debug")
			// ignore err
			_ = framework.StorePodsLog(ctx, clientset, client, utils.Namespace(), "control-plane", "controller-manager", "manager", os.Getenv("ARTIFACT_DIR"))
			_ = framework.StorePodsLog(ctx, clientset, client, utils.Namespace(), "controller", utils.PodPlacementControllerName, utils.PodPlacementControllerName, os.Getenv("ARTIFACT_DIR"))
			_ = framework.StorePodsLog(ctx, clientset, client, utils.Namespace(), "controller", utils.PodPlacementWebhookName, utils.PodPlacementWebhookName, os.Getenv("ARTIFACT_DIR"))
		}
		err := client.Delete(ctx, &v1beta1.ClusterPodPlacementConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster",
			},
		})
		Expect(runtimeclient.IgnoreNotFound(err)).NotTo(HaveOccurred())
		Eventually(framework.ValidateDeletion(client, ctx)).Should(Succeed())
	})
	Context("When the operator is running and a pod placement config is created", func() {
		It("should deploy the operands with v1beta1 API", func() {
			err := client.Create(ctx, &v1beta1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Eventually(framework.ValidateCreation(client, ctx)).Should(Succeed())
			By("convert the v1beta1 CR to v1alpha1 should succeed")
			c := &v1alpha1.ClusterPodPlacementConfig{}
			err = client.Get(ctx, runtimeclient.ObjectKey{Name: "cluster"}, c)
			Expect(err).NotTo(HaveOccurred())
		})
		It("should deploy the operands with v1alpha1 API", func() {
			err := client.Create(ctx, &v1alpha1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Eventually(framework.ValidateCreation(client, ctx)).Should(Succeed())
			By("convert the v1alpha1 CR to v1beta1 should succeed")
			c := &v1beta1.ClusterPodPlacementConfig{}
			err = client.Get(ctx, runtimeclient.ObjectKey{Name: "cluster"}, c)
			Expect(err).NotTo(HaveOccurred())
		})
	})
	Context("The webhook should get requests only for pods matching the namespaceSelector in the ClusterPodPlacementConfig CR", func() {
		BeforeEach(func() {
			By("set opt-out namespaceSelector for ClusterPodPlacementConfig")
			err := client.Create(ctx, &v1beta1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Spec: v1beta1.ClusterPodPlacementConfigSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "multiarch.openshift.io/exclude-pod-placement",
								Operator: "DoesNotExist",
							},
						}}}})
			Expect(err).NotTo(HaveOccurred())
			Eventually(framework.ValidateCreation(client, ctx)).Should(Succeed())
		})
		It("should exclude namespaces that have the opt-out label", func() {
			var err error
			By("create namespace with opt-out label")
			ns := framework.NewEphemeralNamespace()
			ns.Labels = map[string]string{
				"multiarch.openshift.io/exclude-pod-placement": "",
			}
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				Build()
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("test-deployment").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())
			//should exclude the namespace
			By("The pod should not have been processed by the webhook and the scheduling gate label should not be set")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Absent, schedulingGateNotSetLabel), e2e.WaitShort).Should(Succeed())
			By("The pod should not have been set node affinity of arch info.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test"), e2e.WaitShort).Should(Succeed())
			By("The pod should not have preferred affinities")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				nil), e2e.WaitShort).Should(Succeed())
		})
		It("should handle namespaces that do not have the opt-out label", func() {
			var err error
			By("create namespace without opt-out label")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				Build()
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("test-deployment").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureAmd64,
					utils.ArchitectureArm64, utils.ArchitectureS390x, utils.ArchitecturePpc64le).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			//should handle the namespace
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("The pod should have been set architecture label")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.MultiArchLabel, "",
				utils.ArchLabelValue(utils.ArchitectureAmd64), "",
				utils.ArchLabelValue(utils.ArchitectureArm64), "",
				utils.ArchLabelValue(utils.ArchitectureS390x), "",
				utils.ArchLabelValue(utils.ArchitecturePpc64le), "",
			), e2e.WaitShort).Should(Succeed())
			By("The pod should have been set node affinity of arch info.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *expectedNSTs), e2e.WaitShort).Should(Succeed())
		})
	})
	Context("The operator should respect to an opt-in namespaceSelector in ClusterPodPlacementConfig CR", func() {
		BeforeEach(func() {
			By("set opt-in namespaceSelector for ClusterPodPlacementConfig")
			err := client.Create(ctx, &v1beta1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Spec: v1beta1.ClusterPodPlacementConfigSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "multiarch.openshift.io/include-pod-placement",
								Operator: "Exists",
							},
						}}}})
			Expect(err).NotTo(HaveOccurred())
			Eventually(framework.ValidateCreation(client, ctx)).Should(Succeed())
		})
		It("should exclude namespaces that do not match the opt-in configuration", func() {
			var err error
			By("create namespace without opt-in label")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				Build()
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("test-deployment").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())
			//should exclude the namespace
			By("The pod should not have been processed by the webhook and the scheduling gate label should not be set")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Absent, schedulingGateNotSetLabel), e2e.WaitShort).Should(Succeed())
			By("The pod should not have been set node affinity of arch info.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test"), e2e.WaitShort).Should(Succeed())
		})
		It("should handle namespaces that match the opt-in configuration", func() {
			var err error
			By("create namespace with opt-in label")
			ns := framework.NewEphemeralNamespace()
			ns.Labels = map[string]string{
				"multiarch.openshift.io/include-pod-placement": "",
			}
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				Build()
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("test-deployment").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureAmd64,
					utils.ArchitectureArm64, utils.ArchitectureS390x, utils.ArchitecturePpc64le).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			//should handle the namespace
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("The pod should have been set architecture label")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.MultiArchLabel, "",
				utils.ArchLabelValue(utils.ArchitectureAmd64), "",
				utils.ArchLabelValue(utils.ArchitectureArm64), "",
				utils.ArchLabelValue(utils.ArchitectureS390x), "",
				utils.ArchLabelValue(utils.ArchitecturePpc64le), "",
			), e2e.WaitShort).Should(Succeed())
			By("The pod should have been set node affinity of arch info.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *expectedNSTs), e2e.WaitShort).Should(Succeed())
			By("No preferred affinities should be set (the plugin is not enabled)")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				nil), e2e.WaitShort).Should(Succeed())
		})
	})
	Context("The webhook should not gate pods with node selectors that pin them to the control plane", func() {
		BeforeEach(func() {
			err := client.Create(ctx, &v1beta1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Spec: v1beta1.ClusterPodPlacementConfigSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "multiarch.openshift.io/exclude-pod-placement",
								Operator: "DoesNotExist",
							},
						}}}})
			Expect(err).NotTo(HaveOccurred())
			Eventually(framework.ValidateCreation(client, ctx)).Should(Succeed())
		})
		DescribeTable("should not gate pods to schedule in control plane nodes", func(selector string) {
			var err error
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			var nodeSelectors = map[string]string{selector: ""}
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				WithNodeSelectors(nodeSelectors).
				Build()
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("test-deployment").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())
			//should exclude the namespace
			By("The pod should not have been processed by the webhook and the scheduling gate label should be set as not-set")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateNotSetLabel), e2e.WaitShort).Should(Succeed())
			By("The pod should not have been set node affinity of arch info.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test"), e2e.WaitShort).Should(Succeed())
		},
			Entry(utils.ControlPlaneNodeSelectorLabel, utils.ControlPlaneNodeSelectorLabel),
			Entry(utils.MasterNodeSelectorLabel, utils.MasterNodeSelectorLabel),
		)
	})
	Context("When a pod placement config is created", func() {
		It("should create a v1beta1 CPPC with plugins and succeed getting the v1alpha1 version of the CPPC", func() {
			By("Creating the ClusterPodPlacementConfig")
			err := client.Create(ctx,
				NewClusterPodPlacementConfig().
					WithName(common.SingletonResourceObjectName).
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm(utils.ArchitectureArm64, 50).
					Build(),
			)
			Expect(err).NotTo(HaveOccurred(), "failed to create the v1beta1 ClusterPodPlacementConfig", err)
			By("Get the v1beta1 version of the CPPC")
			ppc := &v1beta1.ClusterPodPlacementConfig{}
			err = client.Get(ctx, runtimeclient.ObjectKeyFromObject(&v1beta1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.SingletonResourceObjectName,
				},
			}), ppc)
			Expect(err).NotTo(HaveOccurred(), "failed to get the v1beta1 ClusterPodPlacementConfig", err)
			Eventually(framework.ValidateCreation(client, ctx)).Should(Succeed())
			By("Validate the plugins stanza is set")
			Expect(ppc.Spec.Plugins).NotTo(BeNil())
			Expect(ppc.Spec.Plugins.NodeAffinityScoring.IsEnabled()).To(BeTrue())
			// Get v1alpha1 ClusterPodPlacementConfig
			By("Get the v1alpha1 version of the ClusterPodPlacementConfig")
			v1alpha1obj := &v1alpha1.ClusterPodPlacementConfig{}
			err = client.Get(ctx, runtimeclient.ObjectKey{
				Name: common.SingletonResourceObjectName,
			}, v1alpha1obj)
			Expect(err).NotTo(HaveOccurred(), "failed to get the v1alpha1 version of the ClusterPodPlacementConfig", err)
			Expect(v1alpha1obj.Spec).To(Equal(v1alpha1.ClusterPodPlacementConfigSpec{
				LogVerbosity:      "Normal",
				NamespaceSelector: nil,
			}))
		})
		It("should succeed creating a v1alpha1 CPPC and get the v1beta1 version with no plugins field", func() {
			By("Creating a v1alpha1 ClusterPodPlacementConfig")
			err := client.Create(ctx, &v1alpha1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.SingletonResourceObjectName,
				},
			})
			Expect(err).NotTo(HaveOccurred(), "failed to create the v1alpha1 version of the ClusterPodPlacementConfig", err)
			Eventually(framework.ValidateCreation(client, ctx)).Should(Succeed())
			// Get the details
			By("Get the v1beta1 version of the ClusterPodPlacementConfig")
			ppc := &v1beta1.ClusterPodPlacementConfig{}
			err = client.Get(ctx, runtimeclient.ObjectKeyFromObject(&v1beta1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.SingletonResourceObjectName,
				},
			}), ppc)
			Expect(err).NotTo(HaveOccurred(), "failed to get the v1beta1 version of the ClusterPodPlacementConfig", err)
			By("Validate a v1beta1 ClusterPodPlacementConfig plugins ommit empty")
			Expect(ppc.Spec.Plugins).To(BeNil())
		})
		It("should fail creating the CPPC with multiple items for the same architecture in the plugins.nodeAffinityScoring.Platforms list", func() {
			By("Creating a v1beta1 ClusterPodPlacementConfig")
			err := client.Create(ctx,
				NewClusterPodPlacementConfig().
					WithName(common.SingletonResourceObjectName).
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 50).
					WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 100).
					Build(),
			)
			Expect(err).To(HaveOccurred(), "the ClusterPodPlacementConfig should not be accepted", err)
		})
		It("Should ignore pods with already set required node affinity when the nodeAffinityScoring plugin is disabled", func() {
			var err error
			By("Creating a v1beta1 ClusterPodPlacementConfig")
			err = client.Create(ctx,
				NewClusterPodPlacementConfig().
					WithName(common.SingletonResourceObjectName).
					WithNodeAffinityScoring(false).
					WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 24).
					Build(),
			)
			Expect(err).NotTo(HaveOccurred(), "failed to create the ClusterPodPlacementConfig", err)
			Eventually(framework.ValidateCreation(client, ctx)).Should(Succeed())
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a deployment using the container with conflicting node affinity")
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureAmd64).
				Build()
			archLabelNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			By("Create a deployment using the container")
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				WithNodeSelectorTerms(*archLabelNSTs).
				Build()
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("test-deployment").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())
			By("Verify node affinity and scheduling gate label are set")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.SchedulingGateLabel, utils.LabelValueNotSet,
				utils.NodeAffinityLabel, utils.LabelValueNotSet,
			), e2e.WaitShort).Should(Succeed())
			By("Verify preferred node affinity label is not present")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Absent,
				map[string]string{utils.PreferredNodeAffinityLabel: utils.LabelValueNotSet}), e2e.WaitShort).Should(Succeed())
			By("The pod should keep the same node affinity provided by the users. No node affinity is added by the controller.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *archLabelNSTs), e2e.WaitShort).Should(Succeed())
			By("The pod should not have any preferred affinities")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test", nil), e2e.WaitShort).Should(Succeed())
		})
	})
	Context("the ClusterPodPlacementConfig is deleted within 1s after creation", func() {
		It("Should cleanup all finalizers", func() {
			err := client.Create(ctx, &v1beta1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			By("imeditately deleting the clusterpodplacementconfig after creation")
			err = client.Delete(ctx, &v1beta1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			By("Verify all corresponding resources are deleted")
			Eventually(framework.ValidateDeletion(client, ctx)).Should(Succeed())
		})
	})
})
