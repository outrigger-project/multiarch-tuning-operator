package podplacementconfig_test

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/multiarch-tuning-operator/api/common"
	"github.com/openshift/multiarch-tuning-operator/pkg/e2e"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/framework"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

var (
	client    runtimeclient.Client
	clientset *kubernetes.Clientset
	ctx       context.Context
	suiteLog  = ctrl.Log.WithName("setup")
)

func init() {
	e2e.CommonInit()
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Multiarch Tuning Operator Suite (podplacementconfig E2E)", Label("e2e", "podplacementconfig"))
}

var _ = SynchronizedBeforeSuite(func() []byte {
	client, clientset, ctx, suiteLog = e2e.CommonBeforeSuite()
	err := client.Create(ctx,
		builder.NewClusterPodPlacementConfig().
			WithName(common.SingletonResourceObjectName).
			WithNodeAffinityScoring(true).
			WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 50).
			Build(),
	)
	Expect(err).NotTo(HaveOccurred())
	Eventually(framework.ValidateCreation(client, ctx)).Should(Succeed())
	return nil
}, func(data []byte) {
	client, clientset, ctx, suiteLog = e2e.CommonBeforeSuite()
})

var _ = SynchronizedAfterSuite(func() {}, func() {
	err := client.Delete(ctx, builder.NewClusterPodPlacementConfig().
		WithName(common.SingletonResourceObjectName).Build())
	Expect(runtimeclient.IgnoreNotFound(err)).NotTo(HaveOccurred())
	Eventually(framework.ValidateDeletion(client, ctx)).Should(Succeed())
})
