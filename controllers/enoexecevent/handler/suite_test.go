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

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	"go.uber.org/zap/zapcore"

	"github.com/openshift/multiarch-tuning-operator/pkg/e2e"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	testingutils "github.com/openshift/multiarch-tuning-operator/pkg/testing/framework"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"

	//+kubebuilder:scaffold:imports
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg          *rest.Config
	k8sClient    client.Client
	k8sClientSet *kubernetes.Clientset
	stopMgr      context.CancelFunc
	testEnv      *envtest.Environment
	ctx          context.Context
	suiteLog     = ctrl.Log.WithName("setup")
)

const (
	testNamespace     = "test-namespace"
	testContainerName = "test-container"
	testContainerID   = "cri-o://1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
	testNodeName      = "test-node"
	testNodeArch      = utils.ArchitecturePpc64le
	testCommand       = "foo-binary"
)

func init() {
	e2e.CommonInit()
}

func TestOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controllers Integration Suite", Label("integration", "integration-enoexec-handler"))
}

var _ = BeforeAll

var _ = SynchronizedBeforeSuite(func() []byte {
	suiteLog = zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true), zap.Level(zapcore.Level(-5)))
	ctx = context.TODO()
	logf.SetLogger(suiteLog)
	SetDefaultEventuallyPollingInterval(5 * time.Millisecond)
	SetDefaultEventuallyTimeout(5 * time.Second)
	startTestEnv()
	testingutils.EnsureNamespaces(ctx, k8sClient, testNamespace)
	node := builder.NewNodeBuilder().WithName(testNodeName).WithLabel(utils.ArchLabel, testNodeArch).Build()
	Expect(k8sClient.Create(ctx, node)).To(Succeed(), "failed to create test node")

	runManager()
	kc := testingutils.FromEnvTestConfig(cfg)
	data, err := json.Marshal(kc)
	Expect(err).NotTo(HaveOccurred(), "failed to marshal sharedData")
	return data
}, func(data []byte) {
	suiteLog = zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true), zap.Level(zapcore.Level(-5)))
	ctx = context.TODO()
	logf.SetLogger(suiteLog)
	SetDefaultEventuallyPollingInterval(5 * time.Millisecond)
	SetDefaultEventuallyTimeout(5 * time.Second)
	var err error
	var kc api.Config
	err = json.Unmarshal(data, &kc)
	Expect(err).NotTo(HaveOccurred(), "failed to unmarshal sharedData")
	// Sync test cluster environment
	ocg := clientcmd.NewDefaultClientConfig(kc, &clientcmd.ConfigOverrides{})
	cfg, err = ocg.ClientConfig()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	k8sClientSet, err = kubernetes.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClientSet).NotTo(BeNil())
})

var _ = SynchronizedAfterSuite(func() {}, func() {
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
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
	//+kubebuilder:scaffold:scheme

	k8sClientSet, err = kubernetes.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClientSet).NotTo(BeNil())

	klog.Info("Applying CRDs to the test environment")
	kustomizer := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	resMap, err := kustomizer.Run(filesys.MakeFsOnDisk(), filepath.Join("..", "..", "..", "config", "crd"))
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

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme.Scheme,
		HealthProbeBindAddress: ":4980",
		Logger:                 suiteLog,
	})
	Expect(err).NotTo(HaveOccurred())

	suiteLog.Info("Manager created")

	err = mgr.AddReadyzCheck("readyz", healthz.Ping)
	Expect(err).NotTo(HaveOccurred())

	reconciler := NewReconciler(mgr.GetClient(), k8sClientSet, mgr.GetScheme(), mgr.GetEventRecorderFor("enoexecevent-controller"))
	if err = reconciler.SetupWithManager(mgr); err != nil {
		suiteLog.Error(err, "unable to create controller", "controller", "ENoExecEvent")
	}
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
