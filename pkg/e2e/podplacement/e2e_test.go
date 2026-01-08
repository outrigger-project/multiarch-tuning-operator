package podplacement_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"log"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	ctrl "sigs.k8s.io/controller-runtime"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	ocpconfigv1 "github.com/openshift/api/config/v1"
	ocpoperatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"

	"github.com/openshift/multiarch-tuning-operator/api/common/plugins"
	"github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/pkg/e2e"
	. "github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/framework"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/registry"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

var (
	client    runtimeclient.Client
	clientset *kubernetes.Clientset
	ctx       context.Context
	suiteLog  = ctrl.Log.WithName("setup")
)

var (
	inSecureRegistryConfig   *registry.RegistryConfig
	notTrustedRegistryConfig *registry.RegistryConfig
	trustedRegistryConfig    *registry.RegistryConfig
	registryNS               *corev1.Namespace
	imageForRemove           *ocpconfigv1.Image
	masterNodes              *corev1.NodeList
	certConfigmapName        = "registry-config"
)

func init() {
	e2e.CommonInit()
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Multiarch Tuning Operator Suite (PodPlacementOperand E2E)", Label("e2e", "pod-placement-operand"))
}

var _ = SynchronizedBeforeSuite(func() []byte {
	var err error
	client, clientset, ctx, suiteLog = e2e.CommonBeforeSuite()
	err = client.Create(ctx, &v1beta1.ClusterPodPlacementConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Spec: v1beta1.ClusterPodPlacementConfigSpec{
			Plugins: &plugins.Plugins{
				NodeAffinityScoring: defaultNodeAffinityScoring(),
			},
		},
	})

	Expect(err).NotTo(HaveOccurred())
	Eventually(framework.ValidateCreation(client, ctx)).Should(Succeed())
	updateGlobalPullSecret("quay.io/multi-arch/tuning-test-global")
	masterNodes, err = framework.GetNodesWithLabel(ctx, client, "node-role.kubernetes.io/master", "")
	Expect(err).NotTo(HaveOccurred())
	if len(masterNodes.Items) == 0 {
		By("Skipping registry config setting because it is not supported on hosted clusters")
	} else {
		By("Prepare registry config test data")
		createRegistryConfigTestData()
		By("Wait for machineconfig finishing updating")
		framework.WaitForMCPComplete(ctx, client)
	}
	return nil
}, func(data []byte) {
	var err error
	client, clientset, ctx, suiteLog = e2e.CommonBeforeSuite()

	masterNodes, err = framework.GetNodesWithLabel(ctx, client, "node-role.kubernetes.io/master", "")
	Expect(err).NotTo(HaveOccurred())
})

var _ = SynchronizedAfterSuite(func() {}, func() {
	err := client.Delete(ctx, &v1beta1.ClusterPodPlacementConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
	})
	Expect(err).NotTo(HaveOccurred())
	Eventually(framework.ValidateDeletion(client, ctx)).Should(Succeed())
	if len(masterNodes.Items) == 0 {
		By("Skipping registry config clean up because it is not supported on hosted clusters")
	} else {
		By("Clean up registry config test data")
		deleteRegistryConfigTestData()
		By("Wait for machineconfig finishing updating")
		framework.WaitForMCPComplete(ctx, client)
	}
})

func defaultExpectedAffinityTerms() []corev1.PreferredSchedulingTerm {
	return NewPreferredSchedulingTerms().
		WithArchitectureWeight(utils.ArchitectureAmd64, 50).
		Build()
}

func defaultNodeAffinityScoring() *plugins.NodeAffinityScoring {
	ret := &plugins.NodeAffinityScoring{
		BasePlugin: plugins.BasePlugin{
			Enabled: true,
		},
	}
	for _, term := range defaultExpectedAffinityTerms() {
		ret.Platforms = append(ret.Platforms, plugins.NodeAffinityScoringPlatformTerm{
			Architecture: term.Preference.MatchExpressions[0].Values[0],
			Weight:       term.Weight,
		})
	}
	return ret
}

// updateGlobalPullSecret patches the global pull secret to onboard the
// read-only credentials of the quay.io org. for testing images stored
// in a repo for which credentials are expected to stay in the global pull secret.
// NOTE: TODO: do we need to change the location of the secrets even here for testing non-OCP distributions?
func updateGlobalPullSecret(registry string, isdelete ...bool) {
	secret := corev1.Secret{}
	err := client.Get(ctx, runtimeclient.ObjectKeyFromObject(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pull-secret",
			Namespace: "openshift-config",
		},
	}), &secret)
	Expect(err).NotTo(HaveOccurred(), "failed to get secret/pull-secret in namespace openshift-config", err)
	var dockerConfigJSON map[string]interface{}
	err = json.Unmarshal(secret.Data[".dockerconfigjson"], &dockerConfigJSON)
	Expect(err).NotTo(HaveOccurred(), "failed to unmarshal dockerconfigjson", err)
	auths := dockerConfigJSON["auths"].(map[string]interface{})
	// Add new auth for quay.io/multi-arch/tuning-test-global to global pull secret
	Expect(registry).NotTo(BeEmpty(), "registry string should not be empty")
	if len(isdelete) > 0 && isdelete[0] {
		// Delete the auth for quay.io/multi-arch/tuning-test-global from global pull secret
		delete(auths, registry)
	} else {
		auth := map[string]string{}
		switch registry {
		case "quay.io/multi-arch/tuning-test-global":
			auth["auth"] = base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s",
				"multi-arch+mto_testing_global_ps",
				"NELK81COHVFAZHY49MXK9XJ02U7A85V0HY3NS14O4K2AFRN3EY39SH64MFU3U90W",
			)))
		case "quay.io/multi-arch/tuning-test-global-2":
			auth["auth"] = base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s",
				"multi-arch+mto_testing_global_ps_2",
				"T6VH4IEMN2N8JKWW0ZFY3372Y5BLVAH9Z9ASVWZZPG2RIDSV7UVMQT5MPFJEV59T",
			)))
		default:
			Expect(false).To(BeTrue(), "registry name is invalid: %s", registry)
		}
		auths[registry] = auth
	}
	dockerConfigJSON["auths"] = auths
	newDockerConfigJSONBytes, err := json.Marshal(dockerConfigJSON)
	Expect(err).NotTo(HaveOccurred(), "failed to marshal dockerconfigjson", err)
	// Update secret
	secret.Data[".dockerconfigjson"] = []byte(newDockerConfigJSONBytes)
	err = client.Update(ctx, &secret)
	Expect(err).NotTo(HaveOccurred())
}

func createRegistryConfigTestData() {
	By("Creating registry configuration custom resources")
	createTestICSP(ctx, client, e2e.ICSPName)
	createTestIDMS(ctx, client, e2e.IDMSName)
	createTestITMS(ctx, client, e2e.ITMSName)
	By("Getting image.config")
	image, err := framework.GetImageConfig(ctx, client)
	Expect(err).NotTo(HaveOccurred())
	imageForRemove = image.DeepCopy()
	By("Creating namespace for registry set up")
	registryNS = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: e2e.RegistryNamespace,
		},
	}
	err = client.Create(ctx, registryNS)
	Expect(err).NotTo(HaveOccurred())
	inSecureRegistryConfig = runRegistry(ctx, client, registryNS, e2e.InsecureRegistryName, false)
	notTrustedRegistryConfig = runRegistry(ctx, client, registryNS, e2e.NotTrustedRegistryName, false)
	trustedRegistryConfig = runRegistry(ctx, client, registryNS, e2e.TrustedRegistryName, true)
	By("Updating image.config")
	image.Spec.RegistrySources.InsecureRegistries = append(image.Spec.RegistrySources.InsecureRegistries, inSecureRegistryConfig.RegistryHost)
	image.Spec.AdditionalTrustedCA = ocpconfigv1.ConfigMapNameReference{
		Name: trustedRegistryConfig.CertConfigmapName,
	}
	image.Spec.RegistrySources.BlockedRegistries = append(image.Spec.RegistrySources.BlockedRegistries, framework.GetImageRepository(e2e.PausePublicMultiarchImage))
	By("Configuring containerRuntimeSearchRegistries in image.config")
	image.Spec.RegistrySources.ContainerRuntimeSearchRegistries = append(image.Spec.RegistrySources.ContainerRuntimeSearchRegistries,
		"quay.io")
	err = client.Update(ctx, image)
	Expect(err).NotTo(HaveOccurred())
}

func createTestICSP(ctx context.Context, client runtimeclient.Client, name string) {
	By(fmt.Sprintf("Creating ImageContentSourcePolicy %s", name))
	icsp := NewImageContentSourcePolicy().
		WithRepositoryDigestMirrors(
			// source repository unavailable, mirror repository available, AllowContactingSource enabled
			NewRepositoryDigestMirrors().
				WithMirrors(framework.GetImageRepository(e2e.HelloopenshiftPublicMultiarchImage)).
				WithSource(framework.GetReplacedImageURI(framework.GetImageRepository(e2e.HelloopenshiftPublicMultiarchImage), e2e.MyFakeICSPAllowContactSourceTestSourceRegistry)).
				Build()).
		WithName(name).
		Build()
	err := client.Create(ctx, icsp)
	Expect(err).NotTo(HaveOccurred())
}

func createTestIDMS(ctx context.Context, client runtimeclient.Client, name string) {
	By(fmt.Sprintf("Creating ImageDigestMirrorSet %s", name))
	idms := NewImageDigestMirrorSet().
		WithImageDigestMirrors(
			// source repository unavailable, mirror repository available, NeverContactingSource enabled
			NewImageDigestMirrors().
				WithMirrors(ocpconfigv1.ImageMirror(framework.GetImageRepository(e2e.HelloopenshiftPublicMultiarchImage))).
				WithSource(framework.GetReplacedImageURI(framework.GetImageRepository(e2e.HelloopenshiftPublicMultiarchImage), e2e.MyFakeIDMSNeverContactSourceTestSourceRegistry)).
				WithMirrorNeverContactSource().
				Build()).
		WithName(name).
		Build()
	err := client.Create(ctx, idms)
	Expect(err).NotTo(HaveOccurred())
}

func createTestITMS(ctx context.Context, client runtimeclient.Client, name string) {
	By(fmt.Sprintf("Creating ImageTagMirrorSet %s", name))
	itms := NewImageTagMirrorSet().
		WithImageTagMirrors(
			// source repository available, mirror repository unavailable, AllowContactingSource enabled
			NewImageTagMirrors().
				WithMirrors(ocpconfigv1.ImageMirror(framework.GetReplacedImageURI(framework.GetImageRepository(e2e.SleepPublicMultiarchImage), e2e.MyFakeITMSAllowContactSourceTestMirrorRegistry))).
				WithSource(framework.GetImageRepository(e2e.SleepPublicMultiarchImage)).
				WithMirrorAllowContactingSource().
				Build(),
			// source repository available, mirror repository unavailable, NeverContactingSource enabled
			NewImageTagMirrors().
				WithMirrors(ocpconfigv1.ImageMirror(framework.GetReplacedImageURI(framework.GetImageRepository(e2e.RedisPublicMultiarchImage), e2e.MyFakeITMSNeverContactSourceTestMirrorRegistry))).
				WithSource(framework.GetImageRepository(e2e.RedisPublicMultiarchImage)).
				WithMirrorNeverContactSource().
				Build()).
		WithName(name).
		Build()
	err := client.Create(ctx, itms)
	Expect(err).NotTo(HaveOccurred())
}

func deleteRegistryConfigTestData() {
	By("Deleting registry configuration custom resources")
	deleteTestRegistryConfigObject(ctx, client, e2e.ICSPName, &ocpoperatorv1alpha1.ImageContentSourcePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: e2e.ICSPName},
	}, "ImageContentSourcePolicy")
	deleteTestRegistryConfigObject(ctx, client, e2e.IDMSName, &ocpconfigv1.ImageDigestMirrorSet{
		ObjectMeta: metav1.ObjectMeta{Name: e2e.IDMSName},
	}, "ImageDigestMirrorSet")
	deleteTestRegistryConfigObject(ctx, client, e2e.ITMSName, &ocpconfigv1.ImageTagMirrorSet{
		ObjectMeta: metav1.ObjectMeta{Name: e2e.ITMSName},
	}, "ImageTagMirrorSet")
	By("Restoring image.config")
	image, err := framework.GetImageConfig(ctx, client)
	Expect(err).NotTo(HaveOccurred())
	image.Spec = imageForRemove.Spec
	err = client.Update(ctx, image)
	Expect(err).NotTo(HaveOccurred())
	By("Deleting registry namespace")
	err = client.Delete(ctx, registryNS)
	Expect(err).NotTo(HaveOccurred())
	By("Cleaning up certificate files for registry")
	deleteTestRegistry(ctx, client, inSecureRegistryConfig)
	deleteTestRegistry(ctx, client, notTrustedRegistryConfig)
	deleteTestRegistry(ctx, client, trustedRegistryConfig)
	By("Deleting Certificates configmap if spec.data is null")
	deleteCertificatesConfigmap(ctx, client, certConfigmapName)
}

func deleteCertificatesConfigmap(ctx context.Context, client runtimeclient.Client, configmapName string) {
	configmap := corev1.ConfigMap{}
	err := client.Get(ctx, runtimeclient.ObjectKeyFromObject(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configmapName,
			Namespace: "openshift-config",
		},
	}), &configmap)
	if err != nil && runtimeclient.IgnoreNotFound(err) == nil {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	if configmap.Data == nil {
		err = client.Delete(ctx, &configmap)
		Expect(err).NotTo(HaveOccurred())
	}
}

func deleteTestRegistry(ctx context.Context, client runtimeclient.Client, registryConfig *registry.RegistryConfig) {
	By(fmt.Sprintf("Cleaning up created resources for %s registry test", registryConfig.Name))
	err := registry.RemoveCertificateFiles(registryConfig.KeyPath)
	Expect(err).NotTo(HaveOccurred())
	err = registry.RemoveCertificateFromConfigmap(ctx, client, registryConfig)
	Expect(err).NotTo(HaveOccurred())
}

func deleteTestRegistryConfigObject(ctx context.Context, client runtimeclient.Client, name string, obj runtimeclient.Object, objType string) {
	By(fmt.Sprintf("Deleting test %s", objType))
	err := client.Get(ctx, runtimeclient.ObjectKeyFromObject(obj), obj)
	if err != nil && runtimeclient.IgnoreNotFound(err) == nil {
		log.Printf("test %s %s does not exist, skip", objType, name)
		return
	}
	Expect(err).NotTo(HaveOccurred())
	err = client.Delete(ctx, obj)
	Expect(err).NotTo(HaveOccurred())
}

func runRegistry(ctx context.Context, client runtimeclient.Client, ns *corev1.Namespace, name string, ifAddCertificateToConfigmap bool) *registry.RegistryConfig {
	By(fmt.Sprintf("Runing registry for %s test", name))
	registryConfig, err := registry.NewRegistry(ns, name, certConfigmapName, "https://quay.io", authUserLocal, authPassLocal)
	Expect(err).NotTo(HaveOccurred())
	err = registry.Deploy(ctx, client, registryConfig)
	Expect(err).NotTo(HaveOccurred())
	if ifAddCertificateToConfigmap {
		err = registry.AddCertificateToConfigmap(ctx, client, registryConfig)
		Expect(err).NotTo(HaveOccurred())
	}
	return registryConfig
}
