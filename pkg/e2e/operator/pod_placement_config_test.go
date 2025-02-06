package operator_test

import (
	"os"

	v1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

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
		Expect(err).NotTo(HaveOccurred())
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
		})
	})
	It("should reconcile the monitoring stack objects based on the namespace labels", func() {
		By("Creating the ClusterPodPlacementConfig")
		err := client.Create(ctx, NewClusterPodPlacementConfig().WithName("cluster").Build())
		Expect(err).NotTo(HaveOccurred())
		Eventually(framework.ValidateCreation(client, ctx)).Should(Succeed())
		By("Getting the namespace")
		ns := &corev1.Namespace{}
		err = client.Get(ctx, types.NamespacedName{
			Name: utils.Namespace(),
		}, ns)
		Expect(err).NotTo(HaveOccurred(), "failed to get namespace", err)
		Expect(ns.Labels).NotTo(HaveKey(utils.MonitoringLabelKey(ctx, dClient)),
			"the namespace should not have the cluster-monitoring label")
		sm := &v1.ServiceMonitor{}
		By("Getting the ServiceMonitor")
		err = client.Get(ctx, types.NamespacedName{
			Name:      utils.PodPlacementControllerName,
			Namespace: utils.Namespace(),
		}, sm)
		Expect(err).To(HaveOccurred(), "the ServiceMonitor should not be available", err)
		Expect(errors.IsNotFound(err)).To(BeTrue(), "the ServiceMonitor should not be available", err)
		By("Labeling the namespace")
		if ns.Labels == nil {
			ns.Labels = make(map[string]string)
		}
		ns.Labels[utils.MonitoringLabelKey(ctx, dClient)] = "true"
		err = client.Update(ctx, ns)
		Expect(err).NotTo(HaveOccurred(), "failed to update namespace", err)
		By("Verifying the service monitor is created")
		Eventually(func(g Gomega) {
			sm := &v1.ServiceMonitor{}
			err := client.Get(ctx, types.NamespacedName{
				Name:      utils.PodPlacementControllerName,
				Namespace: utils.Namespace(),
			}, sm)
			g.Expect(err).NotTo(HaveOccurred(), "failed to get ServiceMonitor", err)
		}).Should(Succeed(), "the ServiceMonitor should be created")
		By("Removing the label from the namespace")
		err = client.Get(ctx, types.NamespacedName{
			Name: utils.Namespace(),
		}, ns)
		Expect(err).NotTo(HaveOccurred(), "failed to get namespace", err)
		delete(ns.Labels, utils.MonitoringLabelKey(ctx, dClient))
		err = client.Update(ctx, ns)
		Expect(err).NotTo(HaveOccurred(), "failed to update namespace", err)
		By("Verifying the service monitor is deleted")
		Eventually(func(g Gomega) {
			sm := &v1.ServiceMonitor{}
			err := client.Get(ctx, types.NamespacedName{
				Name:      utils.PodPlacementControllerName,
				Namespace: utils.Namespace(),
			}, sm)
			g.Expect(err).To(HaveOccurred(), "the ServiceMonitor should not be available", err)
			g.Expect(errors.IsNotFound(err)).To(BeTrue(), "the ServiceMonitor should not be available", err)
		}, e2e.WaitMedium).Should(Succeed(), "the ServiceMonitor should be deleted")
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
})
