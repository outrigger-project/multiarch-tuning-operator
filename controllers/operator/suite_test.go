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

package operator

import (
	"context"
	"k8s.io/utils/clock"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"sigs.k8s.io/kustomize/api/resmap"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"

	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/dynamic"
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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"go.uber.org/zap/zapcore"

	"github.com/openshift/library-go/pkg/operator/events"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1alpha1"
	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1beta1"
	testingutils "github.com/openshift/multiarch-tuning-operator/pkg/testing/framework"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg       *rest.Config
	k8sClient client.Client
	stopMgr   context.CancelFunc
	testEnv   *envtest.Environment
	ctx       context.Context
	suiteLog  = ctrl.Log.WithName("setup")
)

func TestOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Operator Integration Suite", Label("integration", "operator"))
}

var _ = BeforeAll

var _ = BeforeSuite(func() {
	suiteLog = zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true), zap.Level(zapcore.Level(-5)))
	ctx = context.TODO()
	logf.SetLogger(suiteLog)
	SetDefaultEventuallyPollingInterval(5 * time.Millisecond)
	SetDefaultEventuallyTimeout(5 * time.Second)
	startTestEnv()
	testingutils.EnsureNamespaces(ctx, k8sClient, "test-namespace")
	runManager()
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	stopMgr()
	// wait for the manager to stop. FIXME: this is a hack, not sure what is the right way to do it.
	time.Sleep(5 * time.Second)
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

func startTestEnv() {
	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	var err error

	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())
	err = corev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = appsv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = v1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = v1beta1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = admissionv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = monitoringv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
	//+kubebuilder:scaffold:scheme

	klog.Info("Applying CRDs to the test environment")
	kustomizer := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	resMap, err := kustomizer.Run(filesys.MakeFsOnDisk(), filepath.Join("..", "..", "config", "crd"))
	Expect(err).NotTo(HaveOccurred())
	err = applyResources(resMap)
	Expect(err).NotTo(HaveOccurred())
}

func applyResources(resources resmap.ResMap) error {
	// Create a universal decoder for deserializing the resources
	decoder := scheme.Codecs.UniversalDeserializer()
	for _, res := range resources.Resources() {
		raw, err := res.AsYAML()
		Expect(err).NotTo(HaveOccurred())

		if len(raw) == 0 {
			return nil // Nothing to process
		}

		// Decode the resource from the buffer
		obj, _, err := decoder.Decode(raw, nil, nil)
		if err != nil {
			return err
		}

		// Check if the resource already exists
		existingObj := obj.DeepCopyObject().(client.Object)
		err = k8sClient.Get(context.TODO(), client.ObjectKey{
			Name:      existingObj.GetName(),
			Namespace: existingObj.GetNamespace(),
		}, existingObj)

		if err != nil && !errors.IsNotFound(err) {
			// Return error if it's not a "not found" error
			return err
		}
		if err == nil {
			// Resource exists, update it
			obj.(client.Object).SetResourceVersion(existingObj.GetResourceVersion())
			err = k8sClient.Update(ctx, obj.(client.Object))
			if err != nil {
				return err
			}
		} else {
			// Resource does not exist, create it
			err = k8sClient.Create(ctx, obj.(client.Object))
			if err != nil {
				return err
			}
		}
	}

	return nil
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

	By("Setting up ClusterPodPlacementConfig controller")
	ctrlref, err := events.GetControllerReferenceForCurrentPod(context.TODO(), clientset, utils.Namespace(), nil)
	if err != nil {
		suiteLog.Error(err, "unable to get controller reference for current pod (falling back to namespace)")
	}
	Expect((&ClusterPodPlacementConfigReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		ClientSet:     clientset,
		DynamicClient: dynamic.NewForConfigOrDie(cfg),
		Recorder:      events.NewKubeRecorder(clientset.CoreV1().Events(utils.Namespace()), utils.OperatorName, ctrlref, clock.RealClock{}),
	}).SetupWithManager(mgr)).NotTo(HaveOccurred())

	err = mgr.AddReadyzCheck("readyz", healthz.Ping)
	Expect(err).NotTo(HaveOccurred())

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
