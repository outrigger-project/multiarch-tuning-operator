package operator_test

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/multiarch-tuning-operator/pkg/e2e"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/framework"
)

var (
	client    runtimeclient.Client
	clientset *kubernetes.Clientset
	dClient   *dynamic.DynamicClient
	ctx       context.Context
	suiteLog  = ctrl.Log.WithName("setup")
)

func init() {
	e2e.CommonInit()
	var err error
	dClient, err = framework.LoadDynamicClient()
	Expect(err).ToNot(HaveOccurred())
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Multiarch Tuning Operator Suite (Operator E2E)", Label("e2e", "operator"))
}

var _ = BeforeSuite(func() {
	client, clientset, ctx, suiteLog = e2e.CommonBeforeSuite()
})
