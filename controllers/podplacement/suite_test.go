/*
Copyright 2023 Red Hat, Inc.

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
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"go.uber.org/zap/zapcore"

	v1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	testingutils "github.com/openshift/multiarch-tuning-operator/pkg/testing/framework"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/image/fake/registry"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg             *rest.Config
	k8sClient       client.Client
	registryAddress string
	registryCert    []byte
	stopMgr         context.CancelFunc
	testEnv         *envtest.Environment

	suiteLog = ctrl.Log.WithName("setup")
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controllers Integration Suite", Label("integration"))
}

var _ = BeforeAll

var _ = BeforeSuite(func() {
	suiteLog = zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true), zap.Level(zapcore.Level(-5)))

	logf.SetLogger(suiteLog)
	SetDefaultEventuallyPollingInterval(5 * time.Millisecond)
	SetDefaultEventuallyTimeout(5 * time.Second)
	wg := &sync.WaitGroup{}
	testingutils.DecorateWithWaitGroup(wg, startTestEnv)
	testingutils.DecorateWithWaitGroup(wg, startRegistry)
	wg.Wait()
	testingutils.DecorateWithWaitGroup(wg, seedRegistry)
	testingutils.DecorateWithWaitGroup(wg, seedK8S)
	wg.Wait()
	// TODO: should we continue running the manager in the BeforeSuite node?
	runManager()
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	stopMgr()
	// TODO: we miss a way to gracefully shutdown the registry server in the AfterSuite fixture.
	// wait for the manager to stop. FIXME: this is a hack, not sure what is the right way to do it.
	time.Sleep(1 * time.Second)
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

func startTestEnv() {
	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			MutatingWebhooks: []*v1.MutatingWebhookConfiguration{getMutatingWebHook()},
		},
	}
	var err error

	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = corev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
	//+kubebuilder:scaffold:scheme
}

func startRegistry() {
	By("bootstrapping test registry environment")
	var (
		err          error
		registryChan = make(chan error)
	)
	registryAddress, registryCert, err = registry.RunRegistry(ctx, registryChan)
	Expect(err).NotTo(HaveOccurred(), "failed to start registry server")
	testingutils.StartChannelMonitor(registryChan, "registry server failed")

	// TODO: we miss a way to gracefully shutdown the registry server in the AfterSuite fixture.
	By("Waiting for the registry server to be ready...")
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(registryCert)
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: caCertPool,
			},
		},
	}
	Eventually(func(g Gomega) {
		// See https://github.com/distribution/distribution/blob/28c8bc6c0e4b5dfc380e0fa3058d4877fabdfa4a/registry/registry.go#L143
		resp, err := httpClient.Get(fmt.Sprintf("https://%s/", registryAddress))
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
	}).MustPassRepeatedly(3).Should(
		Succeed(), "registry server is not ready yet")
	suiteLog.Info("Registry server is ready", "registryAddress", registryAddress)
}

func seedRegistry() {
	By("Seeding the registry with images...")
	err := registry.SeedMockRegistry(ctx)
	Expect(err).NotTo(HaveOccurred())
	// Just print the seed images for debugging purposes.
	suiteLog.Info("Seed completed. The registry contains the following images:")
	for _, image := range registry.GetMockImages() {
		suiteLog.Info(image.GetUrl())
	}
}

func seedK8S() {
	var err error
	By("Create internal namespaces")
	testingutils.EnsureNamespaces(ctx, k8sClient, "openshift-image-registry", "openshift-config", "test-namespace")
	By("Create the global pull secret")
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pull-secret",
			Namespace: "openshift-config",
		},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			".dockerconfigjson": []byte(fmt.Sprintf(`{"auths":{"%s":{"auth":"%s"}}}`,
				registryAddress, base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf(
					"%s:%s", registry.GlobalPullSecretUser, registry.GlobalPullSecretPassword))))),
		},
	}
	err = k8sClient.Create(ctx, secret)
	Expect(err).NotTo(HaveOccurred())

	By("Add the image registry certificate to the image-registry-certificates configmap")
	registryCert := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "image-registry-certificates",
			Namespace: "openshift-image-registry",
		},
		Data: map[string]string{
			strings.Replace(registryAddress, ":", "..", 1): string(registryCert),
		},
	}
	err = k8sClient.Create(ctx, registryCert)
	Expect(err).NotTo(HaveOccurred())
}

func runManager() {
	By("Creating the manager")
	webhookServer := webhook.NewServer(webhook.Options{
		Port:    testEnv.WebhookInstallOptions.LocalServingPort,
		Host:    testEnv.WebhookInstallOptions.LocalServingHost,
		CertDir: testEnv.WebhookInstallOptions.LocalServingCertDir,
	})
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme.Scheme,
		HealthProbeBindAddress: ":4980",
		Logger:                 suiteLog,
		WebhookServer:          webhookServer,
	})
	Expect(err).NotTo(HaveOccurred())
	suiteLog.Info("Manager created")

	clientset := kubernetes.NewForConfigOrDie(cfg)

	By("Setting up PodPlacement controller")
	Expect((&PodReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		ClientSet: clientset,
	}).SetupWithManager(mgr)).NotTo(HaveOccurred())

	By("Setting up Namespace Cache")
	nscache := NewNamespaceCache(mgr)
	err = mgr.Add(nscache)
	Expect(err).NotTo(HaveOccurred())

	mgr.GetWebhookServer().Register("/add-pod-scheduling-gate", &webhook.Admission{
		Handler: &PodSchedulingGateMutatingWebHook{
			Client:  mgr.GetClient(),
			Scheme:  mgr.GetScheme(),
			NsCache: nscache,
		}})
	By("Setting up System Config Syncer")
	err = mgr.Add(NewConfigSyncerRunnable())
	Expect(err).NotTo(HaveOccurred())

	By("Setting up Registry Certificates Syncer")
	err = mgr.Add(NewRegistryCertificatesSyncer(clientset, "openshift-image-registry",
		"image-registry-certificates"))
	Expect(err).NotTo(HaveOccurred())

	By("Setting up Global Pull Secret Syncer")
	err = mgr.Add(NewGlobalPullSecretSyncer(clientset, "openshift-config",
		"pull-secret"))
	Expect(err).NotTo(HaveOccurred())

	err = mgr.AddReadyzCheck("readyz", healthz.Ping)
	Expect(err).NotTo(HaveOccurred())

	/***** TODO[OCP specific] ******
	err = mgr.Add(openshiftsysconfig.NewICSPSyncer(mgr))
	Expect(err).NotTo(HaveOccurred())

	err = mgr.Add(openshiftsysconfig.NewIDMSSyncer(mgr))
	 Expect(err).NotTo(HaveOccurred())

	err = mgr.Add(openshiftsysconfig.NewITMSSyncer(mgr))
	Expect(err).NotTo(HaveOccurred())

	err = mgr.Add(openshiftsysconfig.NewImageRegistryConfigSyncer(mgr))
	Expect(err).NotTo(HaveOccurred())
	********/

	By("Starting the manager")
	go func() {
		var mgrCtx context.Context
		mgrCtx, stopMgr = context.WithCancel(ctx)
		err = mgr.Start(mgrCtx)
		Expect(err).NotTo(HaveOccurred())
	}()

	By("Waiting for the manager to be ready...")
	Eventually(func(g Gomega) {
		resp, err := http.Get("http://127.0.0.1:4980/readyz")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
	}).MustPassRepeatedly(3).Should(
		Succeed(), "manager is not ready yet")
	suiteLog.Info("Manager is ready")
}

func getMutatingWebHook() *v1.MutatingWebhookConfiguration {
	return &v1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mutating-webhook-configuration",
		},
		Webhooks: []v1.MutatingWebhook{
			{
				Name:                    "pod-placement-scheduling-gate.multiarch.openshift.io",
				AdmissionReviewVersions: []string{"v1"},
				ClientConfig: v1.WebhookClientConfig{
					Service: &v1.ServiceReference{
						Name: "webhook-service",
						Path: utils.NewPtr("/add-pod-scheduling-gate"),
					},
				},
				FailurePolicy: utils.NewPtr(v1.Ignore),
				Rules: []v1.RuleWithOperations{
					{
						Operations: []v1.OperationType{"CREATE"},
						Rule: v1.Rule{
							APIGroups:   []string{""},
							APIVersions: []string{"v1"},
							Resources:   []string{"pods"},
						},
					},
				},
				SideEffects: utils.NewPtr(v1.SideEffectClassNone),
			},
		},
	}
}
