package podplacement_test

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/openshift/multiarch-tuning-operator/pkg/e2e"
	. "github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/framework"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

const (
	helloOpenshiftPublicMultiarchImage        = "quay.io/openshifttest/hello-openshift:1.2.0"
	helloOpenshiftPublicArmImage              = "quay.io/openshifttest/hello-openshift:arm-1.2.0"
	helloOpenshiftPublicArmAmdImage           = "quay.io/openshifttest/hello-openshift:arm-amd-1.2.0"
	helloOpenshiftPublicArmPpcImage           = "quay.io/openshifttest/hello-openshift:arm-ppc64le-1.2.0"
	helloOpenshiftPrivateMultiarchImageGlobal = "quay.io/multi-arch/tuning-test-global:1.1.0"
	helloOpenshiftPrivateArmImageGlobal       = "quay.io/multi-arch/tuning-test-global:arm-1.1.0"
	helloOpenshiftPrivateArmPpcImageGlobal    = "quay.io/multi-arch/tuning-test-global:arm-ppc64le-1.1.0"
	helloOpenshiftPrivateMultiarchImageLocal  = "quay.io/multi-arch/tuning-test-local:1.1.0"
	helloOpenshiftPrivateArmImageLocal        = "quay.io/multi-arch/tuning-test-local:arm-1.1.0"
	helloOpenshiftPrivateArmPpcImageLocal     = "quay.io/multi-arch/tuning-test-local:arm-ppc64le-1.1.0"
	registryAddress                           = "quay.io/multi-arch/tuning-test-local"
	authUserLocal                             = "multi-arch+mto_testing_local_ps"
	authPassLocal                             = "R9ATA6ENZ7DRD6AFX2VRMK5TTWN8MAPZEHG5QYUUXM1AA8LV6Y02O9Y926T8V28M"
)

var _ = Describe("The Pod Placement Operand", func() {
	var (
		podLabel                  = map[string]string{"app": "test"}
		schedulingGateLabel       = map[string]string{utils.SchedulingGateLabel: utils.SchedulingGateLabelValueRemoved}
		schedulingGateNotSetLabel = map[string]string{utils.SchedulingGateLabel: utils.LabelValueNotSet}
	)
	BeforeEach(func() {
		By("Verifying the operand is ready")
		Eventually(framework.ValidateCreation(client, ctx)).Should(Succeed(), "operand not ready before the test case execution")
		By("Operand ready. Executing the case")
	})
	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			By("The test case failed, get the podplacement and podplacement webhook logs for debug")
			// ignore err
			_ = framework.StorePodsLog(ctx, clientset, client, utils.Namespace(), "control-plane", "controller-manager", "manager", os.Getenv("ARTIFACT_DIR"))
			_ = framework.StorePodsLog(ctx, clientset, client, utils.Namespace(), "controller", utils.PodPlacementControllerName, utils.PodPlacementControllerName, os.Getenv("ARTIFACT_DIR"))
			_ = framework.StorePodsLog(ctx, clientset, client, utils.Namespace(), "controller", utils.PodPlacementWebhookName, utils.PodPlacementWebhookName, os.Getenv("ARTIFACT_DIR"))
		}
		By("Verify the operand is still ready after the case ran")
		Eventually(framework.ValidateCreation(client, ctx)).Should(Succeed(), "operand not ready after the test case execution")
		By("Operand ready after the case execution. Continuing")
	})
	Context("When a deployment is deployed with a single container and a public image", func() {
		It("should set the node affinity", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a deployment using the single container with a public multiarch image")
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
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify arch label are set")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.MultiArchLabel, "",
				utils.ArchLabelValue(utils.ArchitectureAmd64), "",
				utils.ArchLabelValue(utils.ArchitectureArm64), "",
				utils.ArchLabelValue(utils.ArchitectureS390x), "",
				utils.ArchLabelValue(utils.ArchitecturePpc64le), "",
			), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should have been set node affinity of arch info.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *expectedNSTs), e2e.WaitShort).Should(Succeed())
			By("The pod should have the expected preferred affinities")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
		})
		It("should set the node affinity on privileged deployments", func() {
			var err error
			By("Create an ephemeral namespace with security labels")
			ns := framework.NewEphemeralNamespace()
			ns.Labels = map[string]string{
				"pod-security.kubernetes.io/audit":               "privileged",
				"pod-security.kubernetes.io/warn":                "privileged",
				"pod-security.kubernetes.io/enforce":             "privileged",
				"security.openshift.io/scc.podSecurityLabelSync": "false",
			}
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			// Since the CRB to enable using SCCs is cluster-scoped, build its name based on the NS to
			// ensure concurrency safety with other possible test cases
			By("Create a clusterrolebinding to enable using scc")
			ephemeralCRBName := framework.NormalizeNameString("priv-scc-" + ns.Name)
			s := NewSubject().WithKind(rbacv1.ServiceAccountKind).WithName("default").WithNamespace(ns.Name).Build()
			crb := NewClusterRoleBinding().
				WithRoleRef(rbacv1.GroupName, "ClusterRole", "system:openshift:scc:privileged").
				WithSubjects(s).
				WithName(ephemeralCRBName).
				Build()
			err = client.Create(ctx, crb)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, crb)
			By("Create a deployment using the container with security context setting")
			sc := NewSecurityContext().WithPrivileged(utils.NewPtr(true)).
				WithRunAsGroup(utils.NewPtr(int64(0))).
				WithRunAsUSer(utils.NewPtr(int64(0))).
				Build()
			vm := NewVolumeMount().WithName("test-hostpath").WithMountPath("/mnt/hostpath").Build()
			c := NewContainer().WithImage(helloOpenshiftPublicMultiarchImage).
				WithSecurityContext(sc).
				WithVolumeMounts(vm).
				Build()
			v := NewVolume().WithName("test-hostpath").
				WithVolumeSourceHostPath("/var/lib/kubelet/config.json", utils.NewPtr(corev1.HostPathFile)).
				Build()
			ps := NewPodSpec().WithContainers(c).
				WithServiceAccountName("default").
				WithVolumes(v).
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
			By("Verify the pod has scc label")
			sccAnnotation := map[string]string{"openshift.io/scc": "privileged"}
			Eventually(framework.VerifyPodAnnotations(ctx, client, ns, "app", "test", sccAnnotation), e2e.WaitShort).Should(Succeed())
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureAmd64,
					utils.ArchitectureArm64, utils.ArchitectureS390x, utils.ArchitecturePpc64le).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify arch label are set")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.MultiArchLabel, "",
				utils.ArchLabelValue(utils.ArchitectureAmd64), "",
				utils.ArchLabelValue(utils.ArchitectureArm64), "",
				utils.ArchLabelValue(utils.ArchitectureS390x), "",
				utils.ArchLabelValue(utils.ArchitecturePpc64le), "",
			), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should have been set node affinity of arch info.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *expectedNSTs), e2e.WaitShort).Should(Succeed())
			By("The pod should have the expected preferred affinities")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
		})
		It("should set the node affinity when users node affinity do not conflict", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a deployment using the container with non-confliction node affinity")
			hostnameLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.HostnameLabel, corev1.NodeSelectorOpExists).
				Build()
			hostnameLabelNSTs := NewNodeSelectorTerm().WithMatchExpressions(hostnameLabelNSR).Build()
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				WithNodeSelectorTerms(*hostnameLabelNSTs).
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
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(hostnameLabelNSR, archLabelNSR).Build()
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify arch label are set")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.MultiArchLabel, "",
				utils.ArchLabelValue(utils.ArchitectureAmd64), "",
				utils.ArchLabelValue(utils.ArchitectureArm64), "",
				utils.ArchLabelValue(utils.ArchitectureS390x), "",
				utils.ArchLabelValue(utils.ArchitecturePpc64le), "",
			), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should have been set node affinity of arch info.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *expectedNSTs), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities set in the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
		})
		It("should append the preferred affinities in the ClusterPodPlacementConfig when user provided non-arch-related preferred affinities", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a deployment using the container with conflicting node affinity")
			archLabelPST := NewPreferredSchedulingTerm().WithCustomKeyValue("foo", "bar").WithWeight(64).Build()
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				WithPreferredNodeAffinities(
					*archLabelPST,
				).Build()
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
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify arch label are set")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.MultiArchLabel, "",
				utils.ArchLabelValue(utils.ArchitectureAmd64), "",
				utils.ArchLabelValue(utils.ArchitectureArm64), "",
				utils.ArchLabelValue(utils.ArchitectureS390x), "",
				utils.ArchLabelValue(utils.ArchitecturePpc64le), "",
			), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should have been set node affinity of arch info.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *expectedNSTs), e2e.WaitShort).Should(Succeed())
			expectedPSTs := defaultExpectedAffinityTerms()
			expectedPSTs = append(expectedPSTs, *archLabelPST)
			By("The pod should  have the preferred affinities provided by the CPPC and users")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				expectedPSTs), e2e.WaitShort).Should(Succeed())
		})
		It("should set the required node affinity when users provide only arch-related preferred affinities", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a deployment using the container with conflicting node affinity")
			archLabelPST := NewPreferredSchedulingTerm().WithArchitecture(utils.ArchitectureAmd64).WithWeight(1).Build()
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				WithPreferredNodeAffinities(
					*archLabelPST,
				).Build()
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
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify arch label are set")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.MultiArchLabel, "",
				utils.ArchLabelValue(utils.ArchitectureAmd64), "",
				utils.ArchLabelValue(utils.ArchitectureArm64), "",
				utils.ArchLabelValue(utils.ArchitectureS390x), "",
				utils.ArchLabelValue(utils.ArchitecturePpc64le), "",
			), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet,
				utils.PreferredNodeAffinityLabel, utils.LabelValueNotSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should have been set node affinity of arch info.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *expectedNSTs), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities set in the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				NewPreferredSchedulingTerms().WithPreferredSchedulingTerm(archLabelPST).Build()), e2e.WaitShort).Should(Succeed())
		})
		It("should set the preferred node affinity when users provide only arch-related required affinities", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a deployment using the container with conflicting node affinity")
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureArm64).
				Build()
			archLabelNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				WithNodeSelectorTerms(*archLabelNSTs).Build()
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("test-deployment").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.LabelValueNotSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should keep the same node affinity provided by the users. No node affinity is added by the controller.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *archLabelNSTs), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities provided by the CPPC")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
		})
		It("should not set the node affinities when users have already provided both required and preferred node affinities for architecture", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a deployment using the container with conflicting node affinity")
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureArm64).
				Build()
			archLabelNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			archLabelPST := NewPreferredSchedulingTerm().WithArchitecture(utils.ArchitectureArm64).WithWeight(1).Build()
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				WithNodeSelectorTerms(*archLabelNSTs).
				WithPreferredNodeAffinities(
					*archLabelPST,
				).Build()
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("test-deployment").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())
			By("The pod should not have been processed by the webhook and the scheduling gate label should be set as not-set")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateNotSetLabel), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.LabelValueNotSet,
				utils.PreferredNodeAffinityLabel, utils.LabelValueNotSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should keep the same node affinity provided by the users. No node affinity is added by the controller.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *archLabelNSTs), e2e.WaitShort).Should(Succeed())
			By("The pod should keep the same preferred affinities provided by the users")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				NewPreferredSchedulingTerms().WithPreferredSchedulingTerm(archLabelPST).Build()), e2e.WaitShort).Should(Succeed())
			By("The pod should not have the preferred affinities provided by the CPPC")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).ShouldNot(Succeed())
		})
		It("should check each matchExpressions when users node affinity has multiple matchExpressions", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a deployment using the container with multiple matchExpressions on required node affinity set")
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureArm64).
				Build()
			hostnameLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.HostnameLabel, corev1.NodeSelectorOpExists).
				Build()
			archLabelNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			hostnameLabelNSTs := NewNodeSelectorTerm().WithMatchExpressions(hostnameLabelNSR).Build()
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				WithNodeSelectorTerms(*archLabelNSTs, *hostnameLabelNSTs).Build()
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("test-deployment").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())
			expectedArchLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureAmd64,
					utils.ArchitectureArm64, utils.ArchitectureS390x, utils.ArchitecturePpc64le).
				Build()
			expectedHostnameNST := NewNodeSelectorTerm().WithMatchExpressions(hostnameLabelNSR, expectedArchLabelNSR).Build()
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify arch label are set")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.MultiArchLabel, "",
				utils.ArchLabelValue(utils.ArchitectureAmd64), "",
				utils.ArchLabelValue(utils.ArchitectureArm64), "",
				utils.ArchLabelValue(utils.ArchitectureS390x), "",
				utils.ArchLabelValue(utils.ArchitecturePpc64le), "",
			), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should have been set node affinity of arch info.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *expectedHostnameNST, *archLabelNSTs), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities set in the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
		})
		It("should not set the required node affinity when a nodeSelector exists for the architecture", func() {
			var err error
			var nodeSelectors = map[string]string{utils.ArchLabel: utils.ArchitectureAmd64}
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a deployment using the container with a preset nodeSelector")
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
			By("The pod should have been processed by the webhook and the scheduling gate label should be removed")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.LabelValueNotSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should not have been set node affinity of arch info.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test"), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities set in the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
		})
		It("should neither set the node affinity nor gate pods with nodeName set", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Get the name of a random worker node")
			workerNodeName, err := framework.GetRandomNodeName(ctx, client, "node-role.kubernetes.io/worker", "")
			Expect(workerNodeName).NotTo(BeEmpty())
			Expect(err).NotTo(HaveOccurred())
			By("Create a deployment using the container with a preset nodeName")
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				WithNodeName(workerNodeName).
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
			By("The pod should not have been processed by the webhook and the scheduling gate label should set as not-set")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateNotSetLabel), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.LabelValueNotSet,
				utils.PreferredNodeAffinityLabel, utils.LabelValueNotSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should keep the same node affinity provided by the users. No node affinity is added by the controller.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test"), e2e.WaitShort).Should(Succeed())
			By("The pod should be running and not gated")
			Eventually(framework.VerifyPodsAreRunning(ctx, client, ns, "app", "test"), e2e.WaitShort).Should(Succeed())
			By("The pod should not have preferred affinities from the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test", nil), e2e.WaitShort).Should(Succeed())
		})
	})
	Context("When a statefulset is deployed with single vs multi container images", func() {
		It("should set the node affinity when with a single container and a singlearch image", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a statefulset using the container with a singlearch image")
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicArmImage).
				Build()
			s := NewStatefulSet().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("test-statefulset").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, s)
			Expect(err).NotTo(HaveOccurred())
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureArm64).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify arch label are set")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.SingleArchLabel, "",
				utils.ArchLabelValue(utils.ArchitectureArm64), "",
			), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should have been set node affinity of arch info.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *expectedNSTs), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities set in the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
		})
		It("should set the node affinity when with a single container and a multiarch image", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a statefulset using the container with a multiarch image")
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				Build()
			s := NewStatefulSet().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("test-statefulset").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, s)
			Expect(err).NotTo(HaveOccurred())
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureAmd64,
					utils.ArchitectureArm64, utils.ArchitectureS390x, utils.ArchitecturePpc64le).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify arch label are set")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.MultiArchLabel, "",
				utils.ArchLabelValue(utils.ArchitectureAmd64), "",
				utils.ArchLabelValue(utils.ArchitectureArm64), "",
				utils.ArchLabelValue(utils.ArchitectureS390x), "",
				utils.ArchLabelValue(utils.ArchitecturePpc64le), "",
			), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should have been set node affinity of arch info.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *expectedNSTs), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities set in the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
		})
		It("should set the node affinity when with more containers some with singlearch image some with multiarch image", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a statefulset using more containers some with singlearch image some with multiarch image")
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage, helloOpenshiftPublicArmImage).
				Build()
			s := NewStatefulSet().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("test-statefulset").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, s)
			Expect(err).NotTo(HaveOccurred())
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureArm64).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify arch label are set")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.SingleArchLabel, "",
				utils.ArchLabelValue(utils.ArchitectureArm64), "",
			), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should have been set node affinity of arch info.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *expectedNSTs), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities set in the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
		})
		It("should not set the required node affinity when more multiarch image-based containers and users set node affinity", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a statefulset using the containers with conflicting node affinity")
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureArm64).
				Build()
			archLabelNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage, helloOpenshiftPublicArmPpcImage).
				WithNodeSelectorTerms(*archLabelNSTs).Build()
			s := NewStatefulSet().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("test-statefulset").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, s)
			Expect(err).NotTo(HaveOccurred())
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			By("The pod should have been processed by the webhook and the scheduling gate label should be set (the preferred node affinity is not set)")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.LabelValueNotSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should keep the same node affinity provided by the users. No node affinity is added by the controller.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *expectedNSTs), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities set in the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
		})
		It("should set the node affinity when with more containers all with multiarch image", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a statefulset using more containers all with multiarch image")
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage, helloOpenshiftPublicArmAmdImage, helloOpenshiftPublicArmPpcImage).
				Build()
			s := NewStatefulSet().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("test-statefulset").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, s)
			Expect(err).NotTo(HaveOccurred())
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureArm64).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify arch label are set")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.SingleArchLabel, "",
				utils.ArchLabelValue(utils.ArchitectureArm64), "",
			), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should have been set node affinity of arch info.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *expectedNSTs), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities set in the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
		})
	})
	Context("PodPlacementOperand works with several high-level resources owning pods", func() {
		It("should neither set the node affinity nor gate pod for DaemonSet owning pod", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a daemonset using the container with a multiarch image")
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				Build()
			d := NewDaemonSet().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithName("test-daemonset").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())
			By("The pod should not have been processed by the webhook and the scheduling gate label should set as not-set")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateNotSetLabel), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.LabelValueNotSet,
				utils.PreferredNodeAffinityLabel, utils.LabelValueNotSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should only have metadata.name provided by the DaemonSet. No node affinity is added by the controller.")
			Eventually(framework.VerifyDaemonSetPodNodeAffinity(ctx, client, ns, "app", "test", nil), e2e.WaitShort).Should(Succeed())
			Eventually(framework.VerifyDaemonSetPreferredPodNodeAffinity(ctx, client, ns, "app", "test", nil), e2e.WaitShort).Should(Succeed())
		})
		It("should set the node affinity on Job owning pod", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a job using the container with a multiarch image")
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				WithRestartPolicy("OnFailure").
				Build()
			j := NewJob().
				WithPodSpec(ps).
				WithPodLabels(podLabel).
				WithName("test-job").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, j)
			Expect(err).NotTo(HaveOccurred())
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureAmd64,
					utils.ArchitectureArm64, utils.ArchitectureS390x, utils.ArchitecturePpc64le).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify arch label are set")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.MultiArchLabel, "",
				utils.ArchLabelValue(utils.ArchitectureAmd64), "",
				utils.ArchLabelValue(utils.ArchitectureArm64), "",
				utils.ArchLabelValue(utils.ArchitectureS390x), "",
				utils.ArchLabelValue(utils.ArchitecturePpc64le), "",
			), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should have been set node affinity of arch info.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *expectedNSTs), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities set in the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
		})
		It("should set the node affinity on Build owning pod", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a build using the container with a multiarch image")
			b := NewBuild().
				WithDockerImage(helloOpenshiftPublicMultiarchImage).
				WithName("test-build").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, b)
			Expect(err).NotTo(HaveOccurred())
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "openshift.io/build.name", "test-build", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "openshift.io/build.name", "test-build",
				utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "openshift.io/build.name", "test-build",
				utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities set in the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "openshift.io/build.name", "test-build",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
		})
		It("should set the node affinity on DeploymentConfig owning pod", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a deploymentconfig using the container with a multiarch image")
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				Build()
			d := NewDeploymentConfig().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(int32(1)).
				WithName("test-deploymentconfig").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureAmd64,
					utils.ArchitectureArm64, utils.ArchitectureS390x, utils.ArchitecturePpc64le).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify arch label are set")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.MultiArchLabel, "",
				utils.ArchLabelValue(utils.ArchitectureAmd64), "",
				utils.ArchLabelValue(utils.ArchitectureArm64), "",
				utils.ArchLabelValue(utils.ArchitectureS390x), "",
				utils.ArchLabelValue(utils.ArchitecturePpc64le), "",
			), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should have been set node affinity of arch info.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *expectedNSTs), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities set in the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
		})
	})
	Context("When deploying workloads with public and private images", func() {
		It("should not set the required node affinity if missing a pull secret", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a deployment using the container with private images and do not configure pull secret for it")
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPrivateMultiarchImageLocal).
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
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.LabelValueNotSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should not have been set node affinity of arch info because pull secret is missing.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test"), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities set in the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
		})
		It("should set the node affinity in pods with images requiring credentials set in the global pull secret", func() {
			if len(masterNodes.Items) == 0 {
				Skip("The current cluster is a hosted cluster, skipping global pull secret tests")
			}
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a deployment using the container with private images requiring credentials set in the global pull secret")
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPrivateArmPpcImageGlobal).
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
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureArm64, utils.ArchitecturePpc64le).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify arch label are set")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.MultiArchLabel, "",
				utils.ArchLabelValue(utils.ArchitectureArm64), "",
				utils.ArchLabelValue(utils.ArchitecturePpc64le), "",
			), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should have been set node affinity of arch info.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *expectedNSTs), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities set in the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
		})
		It("should set the node affinity in pods with images requiring credentials set in pods imagePullSecrets", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a secret for local pull secret")
			secretData := map[string][]byte{
				".dockerconfigjson": []byte(fmt.Sprintf(`{"auths":{"%s":{"auth":"%s"}}}`,
					registryAddress, base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf(
						"%s:%s", authUserLocal, authPassLocal))))),
			}
			secret := NewSecret().
				WithData(secretData).
				WithDockerConfigJSONType().
				WithName("mto-testing-local-pull-secret").
				WithNameSpace(ns.Name).
				Build()
			err = client.Create(ctx, secret)
			Expect(err).NotTo(HaveOccurred())
			By("Create a deployment using the container with images that requiring credentials set in pod imagePullSecrets")
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPrivateMultiarchImageLocal).
				WithImagePullSecrets(secret.Name).
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
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify arch label are set")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.MultiArchLabel, "",
				utils.ArchLabelValue(utils.ArchitectureAmd64), "",
				utils.ArchLabelValue(utils.ArchitectureArm64), "",
				utils.ArchLabelValue(utils.ArchitectureS390x), "",
				utils.ArchLabelValue(utils.ArchitecturePpc64le), "",
			), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should have been set node affinity of arch info.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *expectedNSTs), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities set in the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
		})
		It("should set the node affinity in pods with images requiring credentials including globs set in pods imagePullSecrets", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			registryHostWithGlob := strings.Replace(registryAddress, "quay.io", "*.io", 1)
			By("Create a secret for the local pull secret for " + registryHostWithGlob)
			secretData := map[string][]byte{
				".dockerconfigjson": []byte(fmt.Sprintf(`{"auths":{"%s":{"auth":"%s"}}}`,
					registryHostWithGlob, base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf(
						"%s:%s", authUserLocal, authPassLocal))))),
			}
			secret := NewSecret().
				WithData(secretData).
				WithDockerConfigJSONType().
				WithName("mto-testing-local-pull-secret").
				WithNameSpace(ns.Name).
				Build()
			err = client.Create(ctx, secret)
			Expect(err).NotTo(HaveOccurred())
			By("Create a deployment using the container with images that requiring credentials set in pod imagePullSecrets")
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPrivateMultiarchImageLocal).
				WithImagePullSecrets(secret.Name).
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
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify arch label are set")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.MultiArchLabel, "",
				utils.ArchLabelValue(utils.ArchitectureAmd64), "",
				utils.ArchLabelValue(utils.ArchitectureArm64), "",
				utils.ArchLabelValue(utils.ArchitectureS390x), "",
				utils.ArchLabelValue(utils.ArchitecturePpc64le), "",
			), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should have been set node affinity of arch info.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *expectedNSTs), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities set in the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
		})
		It("should set the node affinity in pods with images that require both global and local pull secrets", func() {
			if len(masterNodes.Items) == 0 {
				Skip("The current cluster is a hosted cluster, skipping global pull secret tests")
			}
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create secret for local pull secret")
			secretData := map[string][]byte{
				".dockerconfigjson": []byte(fmt.Sprintf(`{"auths":{"%s":{"auth":"%s"}}}`,
					registryAddress, base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf(
						"%s:%s", authUserLocal, authPassLocal))))),
			}
			secret := NewSecret().
				WithData(secretData).
				WithDockerConfigJSONType().
				WithName("mto-testing-local-pull-secret").
				WithNameSpace(ns.Name).
				Build()
			err = client.Create(ctx, secret)
			Expect(err).NotTo(HaveOccurred())
			By("Create a deployment using the containers with images that require both global and local pull secrets")
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPrivateMultiarchImageGlobal, helloOpenshiftPrivateArmImageGlobal,
					helloOpenshiftPrivateArmPpcImageLocal, helloOpenshiftPrivateArmImageLocal).
				WithImagePullSecrets(secret.Name).
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
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureArm64).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			By("Verify arch label are set")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.SingleArchLabel, "",
				utils.ArchLabelValue(utils.ArchitectureArm64), "",
			), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("The pod should have been set node affinity of arch info.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *expectedNSTs), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities set in the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
		})
	})
	Context("When deploying workloads that registry of image uses a self-signed certificate", Serial, func() {
		BeforeEach(func() {
			if len(masterNodes.Items) == 0 {
				Skip("The current cluster is a hosted cluster, skipping image config tests")
			}
		})
		It("should set the node affinity when registry url added to insecureRegistries list", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a deployment using the container with image from registry added in insecure list")
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(
					NewPodSpec().
						WithContainersImages(framework.GetReplacedImageURI(helloOpenshiftPrivateMultiarchImageLocal, fmt.Sprintf("%s:%d", inSecureRegistryConfig.RegistryHost, inSecureRegistryConfig.Port))).
						Build()).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("test-deployment").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())

			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify arch label are set")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.MultiArchLabel, "",
				utils.ArchLabelValue(utils.ArchitectureAmd64), "",
				utils.ArchLabelValue(utils.ArchitectureArm64), "",
				utils.ArchLabelValue(utils.ArchitectureS390x), "",
				utils.ArchLabelValue(utils.ArchitecturePpc64le), "",
			), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should get node affinity of arch info beucase registry is added in insecure list.")
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureAmd64,
					utils.ArchitectureArm64, utils.ArchitectureS390x, utils.ArchitecturePpc64le).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(archLabelNSR).Build()
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *expectedNSTs), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities set in the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
		})
		It("should not set the node affinity when registry certificate is not in the trusted anchors", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a deployment using the container with image from an untrusted Registry")
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(
					NewPodSpec().
						WithContainersImages(framework.GetReplacedImageURI(helloOpenshiftPrivateMultiarchImageLocal, fmt.Sprintf("%s:%d", notTrustedRegistryConfig.RegistryHost, notTrustedRegistryConfig.Port))).
						Build()).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("test-deployment").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.LabelValueNotSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should not get node affinity of arch info beucase registry certificate is not in the trusted anchors.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test"), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities set in the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
		})
		It("should set the node affinity when registry certificate is added in the trusted anchors", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a deployment using the container with image from the registry its certificate added in the trusted Registry")
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(
					NewPodSpec().
						WithContainersImages(framework.GetReplacedImageURI(helloOpenshiftPrivateMultiarchImageLocal, fmt.Sprintf("%s:%d", trustedRegistryConfig.RegistryHost, trustedRegistryConfig.Port))).
						Build()).
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
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify arch label are set")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.MultiArchLabel, "",
				utils.ArchLabelValue(utils.ArchitectureAmd64), "",
				utils.ArchLabelValue(utils.ArchitectureArm64), "",
				utils.ArchLabelValue(utils.ArchitectureS390x), "",
				utils.ArchLabelValue(utils.ArchitecturePpc64le), "",
			), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should get node affinity of arch info because registry certificate is added in the trusted anchors.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *expectedNSTs), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities set in the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
		})
		It("should not set the node affinity when registry url added to blockedRegistries list", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			// Check ppo will block image from registry in blocked registry list
			By("Create a deployment using the container with image from registry which is added in blocked list")
			d := NewDeployment().
				WithSelectorAndPodLabels(map[string]string{"app": "test-block"}).
				WithPodSpec(
					NewPodSpec().
						WithContainersImages(e2e.PausePublicMultiarchImage).
						Build()).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("test-deployment-blocked").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test-block", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test-block",
				utils.NodeAffinityLabel, utils.LabelValueNotSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should not get node affinity of arch info because registry is in blocked list.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test-block"), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities set in the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test-block",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
		})
	})
	Context("When deploying workloads running images in registries that have mirrors configuration", func() {
		BeforeEach(func() {
			if len(masterNodes.Items) == 0 {
				Skip("The current cluster is a hosted cluster, skipping registry config tests")
			}
		})
		It("Should set node affinity when source registry is unavailable, mirrors working and AllowContactingSource enabled in a ImageContentSourcePolicy", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a deployment running image in source registry")
			ps := NewPodSpec().
				WithContainersImages(framework.GetReplacedImageURI(e2e.HelloopenshiftPublicMultiarchImageDigest, e2e.MyFakeICSPAllowContactSourceTestSourceRegistry)).
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
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify arch label are set")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.MultiArchLabel, "",
				utils.ArchLabelValue(utils.ArchitectureAmd64), "",
				utils.ArchLabelValue(utils.ArchitectureArm64), "",
				utils.ArchLabelValue(utils.ArchitectureS390x), "",
				utils.ArchLabelValue(utils.ArchitecturePpc64le), "",
			), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should get node affinity of arch info because the mirror registries are functional.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *expectedNSTs), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities set in the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
			By("The pod should be running")
			Eventually(framework.VerifyPodsAreRunning(ctx, client, ns, "app", "test"), e2e.WaitShort).Should(Succeed())
		})
		It("Should set node affinity when source registry is unavailable, mirrors are working and NeverContactingSource enabled in a ImageDigestMirrorSet", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a deployment running image in source registry")
			ps := NewPodSpec().
				WithContainersImages(framework.GetReplacedImageURI(e2e.HelloopenshiftPublicMultiarchImageDigest, e2e.MyFakeIDMSNeverContactSourceTestSourceRegistry)).
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
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify arch label are set")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.MultiArchLabel, "",
				utils.ArchLabelValue(utils.ArchitectureAmd64), "",
				utils.ArchLabelValue(utils.ArchitectureArm64), "",
				utils.ArchLabelValue(utils.ArchitectureS390x), "",
				utils.ArchLabelValue(utils.ArchitecturePpc64le), "",
			), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should get node affinity of arch info because the mirror registries are functional.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *expectedNSTs), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities set in the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
			By("The pod should be running")
			Eventually(framework.VerifyPodsAreRunning(ctx, client, ns, "app", "test"), e2e.WaitShort).Should(Succeed())
		})
		It("Should set node affinity when source registry is working, mirrors are unavailable and AllowContactingSource enabled in a ImageTagMirrorSet", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a deployment running image in source registry")
			ps := NewPodSpec().
				WithContainersImages(e2e.SleepPublicMultiarchImage).
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
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("The pod should get node affinity of arch info even if mirror registries down but AllowContactingSource enabled and source is functional.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test", *expectedNSTs), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities set in the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
			By("Verify arch label are set")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.MultiArchLabel, "",
				utils.ArchLabelValue(utils.ArchitectureAmd64), "",
				utils.ArchLabelValue(utils.ArchitectureArm64), "",
				utils.ArchLabelValue(utils.ArchitectureS390x), "",
				utils.ArchLabelValue(utils.ArchitecturePpc64le), "",
			), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should be running")
			Eventually(framework.VerifyPodsAreRunning(ctx, client, ns, "app", "test"), e2e.WaitShort).Should(Succeed())
		})
		It("Should not set node affinity when source registry is working, mirrors are unavailable and NeverContactingSource enabled in a ImageTagMirrorSet", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a deployment running image in source registry")
			ps := NewPodSpec().
				WithContainersImages(e2e.RedisPublicMultiarchImage).
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
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			Eventually(framework.VerifyPodLabels(ctx, client, ns, "app", "test", e2e.Present, schedulingGateLabel), e2e.WaitShort).Should(Succeed())
			By("Verify node affinity label are set correct")
			Eventually(framework.VerifyPodLabelsAreSet(ctx, client, ns, "app", "test",
				utils.NodeAffinityLabel, utils.LabelValueNotSet,
				utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet,
			), e2e.WaitShort).Should(Succeed())
			By("The pod should not get node affinity of arch info because mirror registries are down and NeverContactSource is enabled.")
			Eventually(framework.VerifyPodNodeAffinity(ctx, client, ns, "app", "test"), e2e.WaitShort).Should(Succeed())
			By("The pod should have the preferred affinities set in the ClusterPodPlacementConfig")
			Eventually(framework.VerifyPodPreferredNodeAffinity(ctx, client, ns, "app", "test",
				defaultExpectedAffinityTerms()), e2e.WaitShort).Should(Succeed())
		})
	})
})
