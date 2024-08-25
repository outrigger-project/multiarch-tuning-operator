package podplacement_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	ocpappsv1 "github.com/openshift/api/apps/v1"
	ocpbuildv1 "github.com/openshift/api/build/v1"
	ocpconfigv1 "github.com/openshift/api/config/v1"
	ocpmachineconfigurationv1 "github.com/openshift/api/machineconfiguration/v1"

	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/pkg/e2e"

	. "github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/framework"
)

var (
	cfg       *rest.Config
	client    runtimeclient.Client
	clientset *kubernetes.Clientset
	ctx       context.Context
	dns       = ocpconfigv1.DNS{}
	suiteLog  = ctrl.Log.WithName("setup")
)

func init() {
	e2e.CommonInit()
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Multiarch Tuning Operator Suite (PodPlacementOperand E2E)", Label("e2e", "pod-placement-operand"))
}

var _ = BeforeSuite(func() {
	client, clientset, ctx, suiteLog = e2e.CommonBeforeSuite()
	err := ocpappsv1.Install(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = ocpbuildv1.Install(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = ocpconfigv1.Install(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = ocpmachineconfigurationv1.Install(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	cfg, err = config.GetConfig()
	Expect(err).NotTo(HaveOccurred())

	err = client.Create(ctx, &v1beta1.ClusterPodPlacementConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
	})
	Expect(err).NotTo(HaveOccurred())
	Eventually(framework.ValidateCreation(client, ctx)).Should(Succeed())
	updateGlobalPullSecret()

	err = client.Get(ctx, runtimeclient.ObjectKeyFromObject(&ocpconfigv1.DNS{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
	}), &dns)
	Expect(err).NotTo(HaveOccurred())
	By("Create ImageTagMirrorSet for mirrors testing, with configurations enabling either the AllowContactSource policy or the NeverContactSource policy")
	itms := NewImageTagMirrorSet().
		WithImageTagMirrors(
			NewImageTagMirrors().
				WithMirrors(ocpconfigv1.ImageMirror(getImageRepository(e2e.OpenshifttestPublicMultiarchImage))).
				WithSource(getReplacedImageURI(getImageRepository(e2e.OpenshifttestPublicMultiarchImage), e2e.MyFakeITMSAllowContactSourceTestSourceRegistry)).
				WithMirrorAllowContactingSource().
				Build(),
			NewImageTagMirrors().
				WithMirrors(ocpconfigv1.ImageMirror(getReplacedImageURI(getImageRepository(e2e.SleepPublicMultiarchImage), e2e.MyFakeITMSAllowContactSourceTestMirrorRegistry))).
				WithSource(getImageRepository(e2e.SleepPublicMultiarchImage)).
				WithMirrorAllowContactingSource().
				Build(),
			NewImageTagMirrors().
				WithMirrors(ocpconfigv1.ImageMirror(getImageRepository(e2e.OpenshifttestPublicMultiarchImage))).
				WithSource(getReplacedImageURI(getImageRepository(e2e.OpenshifttestPublicMultiarchImage), e2e.MyFakeITMSNeverContactSourceTestSourceRegistry)).
				WithMirrorNeverContactSource().
				Build(),
			NewImageTagMirrors().
				WithMirrors(ocpconfigv1.ImageMirror(getReplacedImageURI(getImageRepository(e2e.RedisPublicMultiarchImage), e2e.MyFakeITMSNeverContactSourceTestMirrorRegistry))).
				WithSource(getImageRepository(e2e.RedisPublicMultiarchImage)).
				WithMirrorNeverContactSource().
				Build()).
		WithName(e2e.ITMSName).
		Build()
	err = client.Create(ctx, itms)
	Expect(err).NotTo(HaveOccurred())
	By("Wait for machineconfig finishing updating")
	framework.WaitForMCPComplete(ctx, client)
})

var _ = AfterSuite(func() {
	err := client.Delete(ctx, &v1beta1.ClusterPodPlacementConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
	})
	Expect(err).NotTo(HaveOccurred())
	Eventually(framework.ValidateDeletion(client, ctx)).Should(Succeed())
	deleteCertificatesConfigmap(ctx, client)
	By("Deleting ImageTagMirrorSet after testing and wait for machineconfig finishing updating")
	itms := NewImageTagMirrorSet().WithName(e2e.ITMSName).Build()
	err = client.Delete(ctx, itms)
	Expect(err).NotTo(HaveOccurred())
	framework.WaitForMCPComplete(ctx, client)
})

// updateGlobalPullSecret patches the global pull secret to onboard the
// read-only credentials of the quay.io org. for testing images stored
// in a repo for which credentials are expected to stay in the global pull secret.
// NOTE: TODO: do we need to change the location of the secrets even here for testing non-OCP distributions?
func updateGlobalPullSecret() {
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
	registry := "quay.io/multi-arch/tuning-test-global"
	auth := map[string]string{
		"auth": base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s",
			"multi-arch+mto_testing_global_ps", "NELK81COHVFAZHY49MXK9XJ02U7A85V0HY3NS14O4K2AFRN3EY39SH64MFU3U90W"))),
	}
	auths[registry] = auth
	dockerConfigJSON["auths"] = auths
	newDockerConfigJSONBytes, err := json.Marshal(dockerConfigJSON)
	Expect(err).NotTo(HaveOccurred(), "failed to marshal dockerconfigjson", err)
	// Update secret
	secret.Data[".dockerconfigjson"] = []byte(newDockerConfigJSONBytes)
	err = client.Update(ctx, &secret)
	Expect(err).NotTo(HaveOccurred())
}

func deleteCertificatesConfigmap(ctx context.Context, client runtimeclient.Client) {
	configmap := v1.ConfigMap{}
	err := client.Get(ctx, runtimeclient.ObjectKeyFromObject(&v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "registry-cas",
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

func getImageRepository(image string) string {
	colonIndex := strings.LastIndex(image, ":")
	if colonIndex != -1 {
		image = image[:colonIndex]
	}
	return image
}

func getReplacedImageURI(image, replacedRegistry string) string {
	colonIndex := strings.Index(image, "/")
	if colonIndex == -1 {
		return image
	}
	return replacedRegistry + image[colonIndex:]
}
