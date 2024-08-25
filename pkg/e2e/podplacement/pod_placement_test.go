package podplacement_test

import (
	"context"
	"encoding/base64"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	ocpconfigv1 "github.com/openshift/api/config/v1"

	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/multiarch-tuning-operator/pkg/e2e"
	. "github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/framework"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/registry"
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
	auth_user_local                           = "multi-arch+mto_testing_local_ps"
	auth_pass_local                           = "R9ATA6ENZ7DRD6AFX2VRMK5TTWN8MAPZEHG5QYUUXM1AA8LV6Y02O9Y926T8V28M"
)

var _ = Describe("The Pod Placement Operand", func() {
	var (
		podLabel            = map[string]string{"app": "test"}
		schedulingGateLabel = map[string]string{utils.SchedulingGateLabel: utils.SchedulingGateLabelValueRemoved}
	)
	BeforeEach(func() {
		By("Verifying the operand is ready")
		Eventually(framework.ValidateCreation(client, ctx)).Should(Succeed(), "operand not ready before the test case execution")
		By("Operand ready. Executing the case")
	})
	AfterEach(func() {
		By("Verify the operand is still ready after the case ran")
		Eventually(framework.ValidateCreation(client, ctx)).Should(Succeed(), "operand not ready after the test case execution")
		By("Operand ready after the case execution. Continuing")
	})
	Context("When a deployment is deployed with a single container and a public image", func() {
		It("should set the node affinity", func() {
			var err error
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
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(&archLabelNSR).Build()
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
			verifyPodNodeAffinity(ns, "app", "test", expectedNSTs)
		})
		It("should set the node affinity on privileged deployments", func() {
			var err error
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
			sc := NewSecurityContext().WithPrivileged(utils.NewPtr(true)).
				WithRunAsGroup(utils.NewPtr(int64(0))).
				WithRunAsUSer(utils.NewPtr(int64(0))).
				Build()
			vm := NewVolumeMount().WithName("test-hostpath").WithMountPath("/mnt/hostpath").Build()
			c := NewContainer().WithImage(helloOpenshiftPublicMultiarchImage).
				WithSecurityContext(&sc).
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
			// TODO: verify the pod has some scc label
			sccAnnotation := map[string]string{"openshift.io/scc": "privileged"}
			verifyPodAnnotations(ns, "app", "test", sccAnnotation)
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureAmd64,
					utils.ArchitectureArm64, utils.ArchitectureS390x, utils.ArchitecturePpc64le).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(&archLabelNSR).Build()
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
			verifyPodNodeAffinity(ns, "app", "test", expectedNSTs)
		})
		It("should set the node affinity when users node affinity do not conflict", func() {
			var err error
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			hostnameLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.HostnameLabel, corev1.NodeSelectorOpExists).
				Build()
			hostnameLabelNSTs := NewNodeSelectorTerm().WithMatchExpressions(&hostnameLabelNSR).Build()
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				WithNodeSelectorTerms(hostnameLabelNSTs).
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
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(&hostnameLabelNSR, &archLabelNSR).Build()
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
			verifyPodNodeAffinity(ns, "app", "test", expectedNSTs)
		})
		It("should not set the node affinity when users node affinity conflicts", func() {
			var err error
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureArm64).
				Build()
			archLabelNSTs := NewNodeSelectorTerm().WithMatchExpressions(&archLabelNSR).Build()
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				WithNodeSelectorTerms(archLabelNSTs).Build()
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("test-deployment").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
			verifyPodNodeAffinity(ns, "app", "test", archLabelNSTs)
		})
		It("should check each matchExpressions when users node affinity has multiple matchExpressions", func() {
			var err error
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureArm64).
				Build()
			hostnameLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.HostnameLabel, corev1.NodeSelectorOpExists).
				Build()
			archLabelNSTs := NewNodeSelectorTerm().WithMatchExpressions(&archLabelNSR).Build()
			hostnameLabelNSTs := NewNodeSelectorTerm().WithMatchExpressions(&hostnameLabelNSR).Build()
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				WithNodeSelectorTerms(archLabelNSTs, hostnameLabelNSTs).Build()
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
			expectedHostnameNST := NewNodeSelectorTerm().WithMatchExpressions(&hostnameLabelNSR, &expectedArchLabelNSR).Build()
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
			verifyPodNodeAffinity(ns, "app", "test", expectedHostnameNST, archLabelNSTs)
		})
		It("should not set the node affinity when nodeSelector exist", func() {
			var err error
			var nodeSelectors = map[string]string{utils.ArchLabel: utils.ArchitectureAmd64}
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
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
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
			verifyPodNodeAffinity(ns, "app", "test")
		})
		It("should not set the node affinity when nodeName exist", func() {
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
			By("The pod should not have been processed by the webhook and the scheduling gate label should not be added")
			verifyPodLabels(ns, "app", "test", e2e.Absent, schedulingGateLabel)
			By("The pod should keep the same node affinity provided by the users. No node affinity is added by the controller.")
			verifyPodNodeAffinity(ns, "app", "test")
			By("The pod should be running and not gated")
			Eventually(func(g Gomega) {
				framework.VerifyPodsAreRunning(g, ctx, client, ns, "app", "test")
			}, e2e.WaitShort).Should(Succeed())
		})
	})
	Context("When a statefulset is deployed with single vs multi container images", func() {
		It("should set the node affinity when with a single container and a singlearch image", func() {
			var err error
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
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
			err = client.Create(ctx, &s)
			Expect(err).NotTo(HaveOccurred())
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureArm64).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(&archLabelNSR).Build()
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
			verifyPodNodeAffinity(ns, "app", "test", expectedNSTs)
		})
		It("should set the node affinity when with a single container and a multiarch image", func() {
			var err error
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
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
			err = client.Create(ctx, &s)
			Expect(err).NotTo(HaveOccurred())
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureAmd64,
					utils.ArchitectureArm64, utils.ArchitectureS390x, utils.ArchitecturePpc64le).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(&archLabelNSR).Build()
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
			verifyPodNodeAffinity(ns, "app", "test", expectedNSTs)
		})
		It("should set the node affinity when with more containers some with singlearch image some with multiarch image", func() {
			var err error
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
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
			err = client.Create(ctx, &s)
			Expect(err).NotTo(HaveOccurred())
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureArm64).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(&archLabelNSR).Build()
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
			verifyPodNodeAffinity(ns, "app", "test", expectedNSTs)
		})
		It("should not set the node affinity when with more containers all with multiarch image but users node affinity conflicts", func() {
			var err error
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureArm64).
				Build()
			archLabelNSTs := NewNodeSelectorTerm().WithMatchExpressions(&archLabelNSR).Build()
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage, helloOpenshiftPublicArmPpcImage).
				WithNodeSelectorTerms(archLabelNSTs).Build()
			s := NewStatefulSet().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("test-statefulset").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, &s)
			Expect(err).NotTo(HaveOccurred())
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(&archLabelNSR).Build()
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
			verifyPodNodeAffinity(ns, "app", "test", expectedNSTs)
		})
		It("should set the node affinity when with more containers all with multiarch image", func() {
			var err error
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
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
			err = client.Create(ctx, &s)
			Expect(err).NotTo(HaveOccurred())
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureArm64).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(&archLabelNSR).Build()
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
			verifyPodNodeAffinity(ns, "app", "test", expectedNSTs)
		})
	})
	Context("PodPlacementOperand works with several high-level resources owning pods", func() {
		It("should set the node affinity on DaemonSet owning pod", func() {
			var err error
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			ps := NewPodSpec().
				WithContainersImages(helloOpenshiftPublicMultiarchImage).
				Build()
			d := NewDaemonSet().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(ps).
				WithName("test-daemonset").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, &d)
			Expect(err).NotTo(HaveOccurred())
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureAmd64,
					utils.ArchitectureArm64, utils.ArchitectureS390x, utils.ArchitecturePpc64le).
				Build()
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
			verifyDaemonSetPodNodeAffinity(ns, "app", "test", archLabelNSR)
		})
		It("should set the node affinity on Job owning pod", func() {
			var err error
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
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
			err = client.Create(ctx, &j)
			Expect(err).NotTo(HaveOccurred())
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureAmd64,
					utils.ArchitectureArm64, utils.ArchitectureS390x, utils.ArchitecturePpc64le).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(&archLabelNSR).Build()
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
			verifyPodNodeAffinity(ns, "app", "test", expectedNSTs)
		})
		It("should set the node affinity on Build owning pod", func() {
			var err error
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			b := NewBuild().
				WithDockerImage(helloOpenshiftPublicMultiarchImage).
				WithName("test-build").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, &b)
			Expect(err).NotTo(HaveOccurred())
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureAmd64,
					utils.ArchitectureArm64, utils.ArchitectureS390x, utils.ArchitecturePpc64le).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(&archLabelNSR).Build()
			verifyPodLabels(ns, "openshift.io/build.name", "test-build", e2e.Present, schedulingGateLabel)
			verifyPodNodeAffinity(ns, "openshift.io/build.name", "test-build", expectedNSTs)
		})
		It("should set the node affinity on DeploymentConfig owning pod", func() {
			var err error
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
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
			err = client.Create(ctx, &d)
			Expect(err).NotTo(HaveOccurred())
			archLabelNSR := NewNodeSelectorRequirement().
				WithKeyAndValues(utils.ArchLabel, corev1.NodeSelectorOpIn, utils.ArchitectureAmd64,
					utils.ArchitectureArm64, utils.ArchitectureS390x, utils.ArchitecturePpc64le).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(&archLabelNSR).Build()
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
			verifyPodNodeAffinity(ns, "app", "test", expectedNSTs)
		})
	})
	Context("When deploying workloads with public and private images", func() {
		It("should not set the node affinity if missing a pull secret", func() {
			var err error
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
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
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
			verifyPodNodeAffinity(ns, "app", "test")
		})
		It("should set the node affinity in pods with images requiring credentials set in the global pull secret", func() {
			var err error
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
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
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(&archLabelNSR).Build()
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
			verifyPodNodeAffinity(ns, "app", "test", expectedNSTs)
		})
		It("should set the node affinity in pods with images requiring credentials set in pods imagePullSecrets", func() {
			var err error
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			secretData := map[string][]byte{
				".dockerconfigjson": []byte(fmt.Sprintf(`{"auths":{"%s":{"auth":"%s"}}}`,
					registryAddress, base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf(
						"%s:%s", auth_user_local, auth_pass_local))))),
			}
			secret := NewSecret().
				WithData(secretData).
				WithDockerConfigJsonType().
				WithName("mto-testing-local-pull-secret").
				WithNameSpace(ns.Name).
				Build()
			err = client.Create(ctx, &secret)
			Expect(err).NotTo(HaveOccurred())
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
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(&archLabelNSR).Build()
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
			verifyPodNodeAffinity(ns, "app", "test", expectedNSTs)
		})
		It("should set the node affinity in pods with images that require both global and local pull secrets", func() {
			var err error
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			secretData := map[string][]byte{
				".dockerconfigjson": []byte(fmt.Sprintf(`{"auths":{"%s":{"auth":"%s"}}}`,
					registryAddress, base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf(
						"%s:%s", auth_user_local, auth_pass_local))))),
			}
			secret := NewSecret().
				WithData(secretData).
				WithDockerConfigJsonType().
				WithName("mto-testing-local-pull-secret").
				WithNameSpace(ns.Name).
				Build()
			err = client.Create(ctx, &secret)
			Expect(err).NotTo(HaveOccurred())
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
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(&archLabelNSR).Build()
			verifyPodNodeAffinity(ns, "app", "test", expectedNSTs)
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
		})
	})
	Context("When deploying workloads that registry of image uses a self-signed certificate", Serial, func() {
		It("should set the node affinity when registry url added to insecureRegistries list", func() {
			var (
				registryName string = "insecure-registry"
				err          error
			)
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			// Create registry
			registryConfig, err := registry.NewRegistry(ns, registryName, dns.Spec.BaseDomain, "https://quay.io", auth_user_local, auth_pass_local)
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				err = registry.RemoveCertificateFiles(registryConfig.KeyPath)
				Expect(err).NotTo(HaveOccurred())
			}()

			err = registry.Deploy(ctx, client, registryConfig)
			Expect(err).NotTo(HaveOccurred())
			// Update image.config
			// todo consider a retry mechanism such as exponentialBackOff to make concurrent running cases safe
			// now we can't make sure timeout for mcp updating when cases running in Parallel
			image := getImageConfig(ctx, client)
			imageForRemove := image
			image.Spec.RegistrySources.InsecureRegistries = append(image.Spec.RegistrySources.InsecureRegistries, registryConfig.RegistryHost)
			err = client.Update(ctx, &image)
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				image := getImageConfig(ctx, client)
				image.Spec = imageForRemove.Spec
				err = client.Update(ctx, &image)
				Expect(err).NotTo(HaveOccurred())
				framework.WaitForMCPComplete(ctx, client)
			}()
			framework.WaitForMCPComplete(ctx, client)
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(
					NewPodSpec().
						WithContainersImages(getReplacedImageURI(helloOpenshiftPrivateMultiarchImageLocal, registryConfig.RegistryHost)).
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
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(&archLabelNSR).Build()
			verifyPodNodeAffinity(ns, "app", "test", expectedNSTs)
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
		})
		It("should not set the node affinity when registry certificate is not in the trusted anchors", func() {
			var (
				registryName string = "untrusted-registry"
				err          error
			)
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			// Create registry
			registryConfig, err := registry.NewRegistry(ns, registryName, dns.Spec.BaseDomain, "https://quay.io", auth_user_local, auth_pass_local)
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				err = registry.RemoveCertificateFiles(registryConfig.KeyPath)
				Expect(err).NotTo(HaveOccurred())
			}()
			err = registry.Deploy(ctx, client, registryConfig)
			Expect(err).NotTo(HaveOccurred())
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(
					NewPodSpec().
						WithContainersImages(getReplacedImageURI(helloOpenshiftPrivateMultiarchImageLocal, registryConfig.RegistryHost)).
						Build()).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("test-deployment").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())
			verifyPodNodeAffinity(ns, "app", "test")
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
		})
		It("should set the node affinity when registry certificate is added in the trusted anchors", func() {
			var (
				registryName string = "trusted-registry"
				err          error
			)
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			// Create registry
			registryConfig, err := registry.NewRegistry(ns, registryName, dns.Spec.BaseDomain, "https://quay.io", auth_user_local, auth_pass_local)
			Expect(err).NotTo(HaveOccurred())
			// remove Certificate dir
			defer func() {
				err = registry.RemoveCertificateFiles(registryConfig.KeyPath)
				Expect(err).NotTo(HaveOccurred())
			}()
			err = registry.Deploy(ctx, client, registryConfig)
			Expect(err).NotTo(HaveOccurred())
			// Create configmap for registry Certificate
			err = registry.AddCertificateToConfigmap(ctx, client, registryConfig)
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				err = registry.RemoveCertificateFromConfigmap(ctx, client, registryConfig)
				Expect(err).NotTo(HaveOccurred())
			}()
			// Update image.config
			image := getImageConfig(ctx, client)
			imageForRemove := image
			image.Spec.AdditionalTrustedCA = ocpconfigv1.ConfigMapNameReference{
				Name: "registry-cas",
			}
			err = client.Update(ctx, &image)
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				image := getImageConfig(ctx, client)
				image.Spec = imageForRemove.Spec
				err = client.Update(ctx, &image)
				Expect(err).NotTo(HaveOccurred())
				waitForCOComplete()
			}()
			waitForCOComplete()
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(
					NewPodSpec().
						WithContainersImages(getReplacedImageURI(helloOpenshiftPrivateMultiarchImageLocal, registryConfig.RegistryHost)).
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
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(&archLabelNSR).Build()
			verifyPodNodeAffinity(ns, "app", "test", expectedNSTs)
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
		})
		It("should set the node affinity when registry certificate added in the trusted anchors and registry url added to allowed list", func() {
			var (
				registryName string = "allowed-registry"
				err          error
			)
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			// Create registry
			registryConfig, err := registry.NewRegistry(ns, registryName, dns.Spec.BaseDomain, "https://quay.io", auth_user_local, auth_pass_local)
			Expect(err).NotTo(HaveOccurred())
			// remove Certificate dir
			defer func() {
				err = registry.RemoveCertificateFiles(registryConfig.KeyPath)
				Expect(err).NotTo(HaveOccurred())
			}()
			err = registry.Deploy(ctx, client, registryConfig)
			Expect(err).NotTo(HaveOccurred())
			// Create configmap for registry Certificate
			err = registry.AddCertificateToConfigmap(ctx, client, registryConfig)
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				err = registry.RemoveCertificateFromConfigmap(ctx, client, registryConfig)
				Expect(err).NotTo(HaveOccurred())
			}()
			// Update image.config
			// Add registry-cas configmap and registry urls to image.config
			image := getImageConfig(ctx, client)
			imageForRemove := image
			image.Spec.AdditionalTrustedCA = ocpconfigv1.ConfigMapNameReference{
				Name: "registry-cas",
			}
			image.Spec.RegistrySources.AllowedRegistries = append(image.Spec.RegistrySources.AllowedRegistries, registryConfig.RegistryHost, "quay.io",
				"registry.redhat.io", "image-registry.openshift-image-registry.svc:5000", "registry.build10.ci.openshift.org")
			err = client.Update(ctx, &image)
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				image := getImageConfig(ctx, client)
				image.Spec = imageForRemove.Spec
				err = client.Update(ctx, &image)
				Expect(err).NotTo(HaveOccurred())
				framework.WaitForMCPComplete(ctx, client)
			}()
			framework.WaitForMCPComplete(ctx, client)
			d := NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(
					NewPodSpec().
						WithContainersImages(getReplacedImageURI(helloOpenshiftPrivateMultiarchImageLocal, registryConfig.RegistryHost)).
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
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(&archLabelNSR).Build()
			verifyPodNodeAffinity(ns, "app", "test", expectedNSTs)
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
		})
		It("should not set the node affinity when registry url added to blockedRegistries list", func() {
			var err error
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			// Update image.config
			image := getImageConfig(ctx, client)
			imageForRemove := image
			blockedRegistry := "gcr.io"
			image.Spec.RegistrySources.BlockedRegistries = append(image.Spec.RegistrySources.BlockedRegistries, blockedRegistry)
			err = client.Update(ctx, &image)
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				image := getImageConfig(ctx, client)
				image.Spec = imageForRemove.Spec
				err = client.Update(ctx, &image)
				Expect(err).NotTo(HaveOccurred())
				framework.WaitForMCPComplete(ctx, client)
			}()
			framework.WaitForMCPComplete(ctx, client)
			// Check ppo will block image from registry in blocked registry list
			d := NewDeployment().
				WithSelectorAndPodLabels(map[string]string{"app": "test-block"}).
				WithPodSpec(
					NewPodSpec().
						WithContainersImages("gcr.io/google_containers/pause:3.2").
						Build()).
				WithReplicas(utils.NewPtr(int32(1))).
				WithName("test-deployment-blocked").
				WithNamespace(ns.Name).
				Build()
			err = client.Create(ctx, d)
			Expect(err).NotTo(HaveOccurred())
			verifyPodLabels(ns, "app", "test-block", e2e.Present, schedulingGateLabel)
			verifyPodNodeAffinity(ns, "app", "test-block")
			// Check ppo will allow image from registry not in blocked registry list
			d = NewDeployment().
				WithSelectorAndPodLabels(podLabel).
				WithPodSpec(NewPodSpec().
					WithContainersImages(helloOpenshiftPublicMultiarchImage).
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
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(&archLabelNSR).Build()
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
			verifyPodNodeAffinity(ns, "app", "test", expectedNSTs)
		})
	})
	Context("When deploying workloads running images in registries that have mirrors configuration", func() {
		It("Should set node affinity for images from a fake registry with working mirrors configured in ImageTagMirrorSet and AllowContactingSource enabled", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a deployment running image from the fake registry, with working mirrors set in ImageTagMirrorSet")
			ps := NewPodSpec().
				WithContainersImages(getReplacedImageURI(helloOpenshiftPublicMultiarchImage, e2e.MyFakeITMSAllowContactSourceTestSourceRegistry)).
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
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(&archLabelNSR).Build()
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
			By("The pod should be running")
			Eventually(func(g Gomega) {
				framework.VerifyPodsAreRunning(g, ctx, client, ns, "app", "test")
			}, e2e.WaitShort).Should(Succeed())
			By("The pod should get node affinity of arch info because the mirror registries are functional.")
			verifyPodNodeAffinity(ns, "app", "test", expectedNSTs)
		})
		It("Should set node affinity for images from a working registry with fake mirrors configured in ImageTagMirrorSet and AllowContactingSource enabled", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a deployment running image from the working registry, with fake mirrors set in ImageTagMirrorSet")
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
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(&archLabelNSR).Build()
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
			By("The pod should get node affinity of arch info even if mirror registries down but AllowContactingSource enabled and source is functional.")
			verifyPodNodeAffinity(ns, "app", "test", expectedNSTs)
			By("The pod should be running")
			Eventually(func(g Gomega) {
				framework.VerifyPodsAreRunning(g, ctx, client, ns, "app", "test")
			}, e2e.WaitShort).Should(Succeed())
		})
		It("Should set node affinity for images from a fake registry with working mirrors configured in ImageTagMirrorSet and NeverContactSource enabled", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a deployment running image from the fake registry, with working mirrors set in ImageTagMirrorSet")
			ps := NewPodSpec().
				WithContainersImages(getReplacedImageURI(e2e.OpenshifttestPublicMultiarchImage, e2e.MyFakeITMSNeverContactSourceTestSourceRegistry)).
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
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(&archLabelNSR).Build()
			By("The pod should have been processed by the webhook and the scheduling gate label should be added")
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
			By("The pod should be running")
			Eventually(func(g Gomega) {
				framework.VerifyPodsAreRunning(g, ctx, client, ns, "app", "test")
			}, e2e.WaitShort).Should(Succeed())
			By("The pod should get node affinity of arch info because the mirror registries are functional.")
			verifyPodNodeAffinity(ns, "app", "test", expectedNSTs)
		})
		It("Should not set node affinity for images from a working registry with fake mirrors configured in ImageTagMirrorSet and NeverContactSource enabled", func() {
			var err error
			By("Create an ephemeral namespace")
			ns := framework.NewEphemeralNamespace()
			err = client.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			//nolint:errcheck
			defer client.Delete(ctx, ns)
			By("Create a deployment running image from the working registry, with fake mirrors set in ImageTagMirrorSet")
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
			verifyPodLabels(ns, "app", "test", e2e.Present, schedulingGateLabel)
			By("The pod should not get node affinity of arch info because mirror registries are down and NeverContactSource is enabled.")
			verifyPodNodeAffinity(ns, "app", "test")
		})
	})
})

func getImageConfig(ctx context.Context, client runtimeclient.Client) ocpconfigv1.Image {
	image := ocpconfigv1.Image{}
	err := client.Get(ctx, runtimeclient.ObjectKeyFromObject(&ocpconfigv1.Image{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
	}), &image)
	Expect(err).NotTo(HaveOccurred(), "failed to get image.config cluster", err)
	return image
}

func verifyPodNodeAffinity(ns *corev1.Namespace, labelKey string, labelInValue string, nodeSelectorTerms ...corev1.NodeSelectorTerm) {
	r, err := labels.NewRequirement(labelKey, "in", []string{labelInValue})
	labelSelector := labels.NewSelector().Add(*r)
	Expect(err).NotTo(HaveOccurred())
	Eventually(func(g Gomega) {
		pods := &corev1.PodList{}
		err := client.List(ctx, pods, &runtimeclient.ListOptions{
			Namespace:     ns.Name,
			LabelSelector: labelSelector,
		})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(pods.Items).NotTo(BeEmpty())
		if len(nodeSelectorTerms) == 0 {
			g.Expect(pods.Items).To(HaveEach(WithTransform(func(p corev1.Pod) *corev1.Affinity {
				return p.Spec.Affinity
			}, BeNil())))
		} else {
			g.Expect(pods.Items).To(HaveEach(framework.HaveEquivalentNodeAffinity(
				&corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: nodeSelectorTerms,
					},
				})))
		}
	}, e2e.WaitShort).Should(Succeed())
}

func verifyDaemonSetPodNodeAffinity(ns *corev1.Namespace, labelKey string, labelInValue string, nodeSelectorRequirement corev1.NodeSelectorRequirement) {
	r, err := labels.NewRequirement(labelKey, "in", []string{labelInValue})
	labelSelector := labels.NewSelector().Add(*r)
	Expect(err).NotTo(HaveOccurred())
	Eventually(func(g Gomega) {
		pods := &corev1.PodList{}
		err := client.List(ctx, pods, &runtimeclient.ListOptions{
			Namespace:     ns.Name,
			LabelSelector: labelSelector,
		})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(pods.Items).NotTo(BeEmpty())
		for i := 0; i < len(pods.Items); i++ {
			pod := pods.Items[i]
			nodename := pod.Spec.NodeName
			nodenameNSR := NewNodeSelectorRequirement().
				WithKeyAndValues("metadata.name", corev1.NodeSelectorOpIn, nodename).
				Build()
			expectedNSTs := NewNodeSelectorTerm().WithMatchExpressions(&nodeSelectorRequirement).WithMatchFields(&nodenameNSR).Build()
			g.Expect([]corev1.Pod{pod}).To(HaveEach(framework.HaveEquivalentNodeAffinity(
				&corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{expectedNSTs},
					},
				})))
		}
	}, e2e.WaitShort).Should(Succeed())
}

func verifyPodAnnotations(ns *corev1.Namespace, labelKey string, labelInValue string, entries map[string]string) {
	r, err := labels.NewRequirement(labelKey, "in", []string{labelInValue})
	labelSelector := labels.NewSelector().Add(*r)
	Expect(err).NotTo(HaveOccurred())
	Eventually(func(g Gomega) {
		pods := &corev1.PodList{}
		err := client.List(ctx, pods, &runtimeclient.ListOptions{
			Namespace:     ns.Name,
			LabelSelector: labelSelector,
		})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(pods.Items).NotTo(BeEmpty())
		for k, v := range entries {
			g.Expect(pods.Items).Should(HaveEach(WithTransform(func(p corev1.Pod) map[string]string {
				return p.Annotations
			}, And(Not(BeEmpty()), HaveKeyWithValue(k, v)))))
		}
	}, e2e.WaitShort).Should(Succeed())
}

func verifyPodLabels(ns *corev1.Namespace, labelKey string, labelInValue string, ifPresent bool, entries map[string]string) {
	r, err := labels.NewRequirement(labelKey, "in", []string{labelInValue})
	labelSelector := labels.NewSelector().Add(*r)
	Expect(err).NotTo(HaveOccurred())
	Eventually(func(g Gomega) {
		pods := &corev1.PodList{}
		err := client.List(ctx, pods, &runtimeclient.ListOptions{
			Namespace:     ns.Name,
			LabelSelector: labelSelector,
		})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(pods.Items).NotTo(BeEmpty())
		for k, v := range entries {
			if ifPresent {
				g.Expect(pods.Items).Should(HaveEach(WithTransform(func(p corev1.Pod) map[string]string {
					return p.Labels
				}, And(Not(BeEmpty()), HaveKeyWithValue(k, v)))))
			} else {
				g.Expect(pods.Items).Should(HaveEach(WithTransform(func(p corev1.Pod) map[string]string {
					return p.Labels
				}, Not(HaveKey(k)))))
			}
		}
	}, e2e.WaitShort).Should(Succeed())
}

func waitForCOComplete() {
	var err error
	// wait to co openshift-apiserver start updating
	Eventually(func(g Gomega) {
		coApiserver := ocpconfigv1.ClusterOperator{}
		err = client.Get(ctx, runtimeclient.ObjectKeyFromObject(&ocpconfigv1.ClusterOperator{
			ObjectMeta: metav1.ObjectMeta{
				Name: "openshift-apiserver",
			},
		}), &coApiserver)
		Expect(err).NotTo(HaveOccurred())
		progressingStatus := ocpconfigv1.ConditionFalse
		for _, condition := range coApiserver.Status.Conditions {
			if condition.Type == "Progressing" {
				progressingStatus = condition.Status
				break
			}
		}
		g.Expect(progressingStatus).To(Equal(ocpconfigv1.ConditionTrue))
	}, e2e.WaitShort).Should(Succeed())
	// wait to co openshift-apiserver finish updating
	Eventually(func(g Gomega) {
		coApiserver := ocpconfigv1.ClusterOperator{}
		err = client.Get(ctx, runtimeclient.ObjectKeyFromObject(&ocpconfigv1.ClusterOperator{
			ObjectMeta: metav1.ObjectMeta{
				Name: "openshift-apiserver",
			},
		}), &coApiserver)
		Expect(err).NotTo(HaveOccurred())
		progressingStatus := ocpconfigv1.ConditionTrue
		for _, condition := range coApiserver.Status.Conditions {
			if condition.Type == "Progressing" {
				progressingStatus = condition.Status
				break
			}
		}
		g.Expect(progressingStatus).To(Equal(ocpconfigv1.ConditionFalse))
	}, e2e.WaitOverMedium, e2e.WaitShort).Should(Succeed())
}
