package framework

import (
	"context"
	"log"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	ocpmachineconfigurationv1 "github.com/openshift/api/machineconfiguration/v1"
)

func VerifyMCPsAreUpdating(g Gomega, ctx context.Context, client runtimeclient.Client) {
	var err error
	mcps := ocpmachineconfigurationv1.MachineConfigPoolList{}
	err = client.List(ctx, &mcps)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(mcps.Items).NotTo(BeEmpty())
	g.Expect(mcps.Items).Should(HaveEach(WithTransform(func(mcp ocpmachineconfigurationv1.MachineConfigPool) corev1.ConditionStatus {
		status := corev1.ConditionFalse
		for _, condition := range mcp.Status.Conditions {
			if condition.Type == "Updating" {
				status = condition.Status
				break
			}
		}
		return status
	}, Equal(corev1.ConditionTrue))))
}

func VerifyMCPsAreUpdated(g Gomega, ctx context.Context, client runtimeclient.Client) {
	var err error
	mcps := ocpmachineconfigurationv1.MachineConfigPoolList{}
	err = client.List(ctx, &mcps)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(mcps.Items).NotTo(BeEmpty())
	g.Expect(mcps.Items).Should(HaveEach(WithTransform(func(mcp ocpmachineconfigurationv1.MachineConfigPool) corev1.ConditionStatus {
		status := corev1.ConditionFalse
		for _, condition := range mcp.Status.Conditions {
			if condition.Type == "Updated" {
				status = condition.Status
				break
			}
		}
		return status
	}, Equal(corev1.ConditionTrue))))

}

func WaitForMCPComplete(ctx context.Context, client runtimeclient.Client) {
	log.Printf("Verifying machineconfig start updating")
	Eventually(func(g Gomega) {
		VerifyMCPsAreUpdating(g, ctx, client)
	}, 15*time.Minute, 1*time.Minute).Should(Succeed())
	log.Printf("Verifying machineconfig finish updating")
	Eventually(func(g Gomega) {
		VerifyMCPsAreUpdated(g, ctx, client)
	}, 15*time.Minute, 1*time.Minute).Should(Succeed())
}
