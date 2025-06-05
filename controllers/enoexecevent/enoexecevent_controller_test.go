package enoexecevent

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/multiarch-tuning-operator/pkg/testing/framework"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"

	"github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
)

var _ = Describe("Controllers/ENoExecEvent/ENoExecEventReconciler", func() {
	When("The ENoExecEvent", func() {
		Context("is handling the lifecycle of the operand", func() {
			It("should create a ENoExecEvent CR", func() {
				By("Creating the ENoExecEvent")
				enee := builder.NewENoExecEvent().WithName("test").WithNamespace(utils.Namespace()).Build()
				err := k8sClient.Create(ctx, enee)
				Expect(err).NotTo(HaveOccurred(), "failed to create ENoExecEvent", err)
				By("Deleting the ENoExecEvent")
				err = k8sClient.Delete(ctx, enee)
				Expect(err).NotTo(HaveOccurred(), "failed to delete ENoExecEvent", err)
				Eventually(framework.ValidateDeletion(k8sClient, ctx)).Should(Succeed(), "the ENoExecEvent should be deleted")
			})
			It("should create a ENoExecEvent CR with set fields", func() {
				By("Creating the ENoExecEvent")
				enee := builder.NewENoExecEvent().WithName("test-name").WithNamespace(utils.Namespace()).Build()
				err := k8sClient.Create(ctx, enee)
				Expect(err).NotTo(HaveOccurred(), "failed to create ENoExecEvent", err)

				// Set status manually (after creation)
				enee.Status = v1beta1.ENoExecEventStatus{
					NodeName:     "test-node",
					PodName:      "test-pod",
					PodNamespace: "test-namespace",
					ContainerID:  "docker://d34db33fd34db33fd34db33fa34db33fd34db33fd34db33fd34db33fd34db3d3",
					Command:      "foo",
				}
				err = k8sClient.Status().Update(ctx, enee)
				Expect(err).NotTo(HaveOccurred())
				Eventually(func(g Gomega) {
					// Get enee from the API server
					By("Ensure ENoExecEvent exists")
					enee = &v1beta1.ENoExecEvent{}
					err := k8sClient.Get(ctx, crclient.ObjectKey{
						Name:      "test-name",
						Namespace: utils.Namespace(),
					}, enee)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get enee", err)
					g.Expect(enee.Status.NodeName).To(Equal("test-node"))
					g.Expect(enee.Status.PodName).To(Equal("test-pod"))
					g.Expect(enee.Status.PodNamespace).To(Equal("test-namespace"))
					g.Expect(enee.Status.ContainerID).To(Equal("docker://d34db33fd34db33fd34db33fa34db33fd34db33fd34db33fd34db33fd34db3d3"))
					g.Expect(enee.Status.Command).To(Equal("foo"))
				}).Should(Succeed(), "failed to get enee")
				By("Deleting the ENoExecEvent")
				err = k8sClient.Delete(ctx, enee)
				Expect(err).NotTo(HaveOccurred(), "failed to delete ENoExecEvent", err)
				Eventually(framework.ValidateDeletion(k8sClient, ctx)).Should(Succeed(), "the ENoExecEvent should be deleted")
			})
		})
		Context("is validating the fields of the ENoExecEvent CR", func() {
			var enee *v1beta1.ENoExecEvent
			var eneeName string
			BeforeEach(func() {
				// Use a unique name based on the Ginkgo test node ID
				eneeName = fmt.Sprintf("uuid-%d", GinkgoParallelProcess())
				By("Creating the ENoExecEvent")
				enee = builder.NewENoExecEvent().
					WithName(eneeName).
					WithNamespace(utils.Namespace()).
					Build()
				err := k8sClient.Create(ctx, enee)
				Expect(err).NotTo(HaveOccurred())
			})
			AfterEach(func() {
				By("Deleting the ENoExecEvent")
				err := k8sClient.Delete(ctx, enee)
				if err != nil && !apierrors.IsNotFound(err) {
					// Only fail if it's an error *other* than NotFound
					Fail(fmt.Sprintf("failed to delete ENoExecEvent: %v", err))
				}
				// Wait for deletion to be fully propagated
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{
						Name:      eneeName,
						Namespace: utils.Namespace(),
					}, &v1beta1.ENoExecEvent{})
				}).Should(MatchError(ContainSubstring("not found")), "the ENoExecEvent should be deleted")
			})
			It("should reject a NodeName that has an invalid character", func() {
				By("Updating the ENoExecEvent")
				enee.Status = v1beta1.ENoExecEventStatus{
					NodeName: "test!-node-name",
				}
				err := k8sClient.Status().Update(ctx, enee)
				Expect(err).To(HaveOccurred(), "Should not update enne status with nodeName that contains an invalid character", err)
				enee.Status = v1beta1.ENoExecEventStatus{
					NodeName: "test.node-name",
				}
				err = k8sClient.Status().Update(ctx, enee)
				Expect(err).To(HaveOccurred(), "Should not update enne status with nodeName that contains an invalid character", err)
			})
			It("should reject a NodeName that does not start or end with alphanumeric character", func() {
				By("Updating the ENoExecEvent")
				enee.Status = v1beta1.ENoExecEventStatus{
					NodeName: "-test-node-name",
				}
				err := k8sClient.Status().Update(ctx, enee)
				Expect(err).To(HaveOccurred(), "Should not update enne status with nodeName that starts with an invalid character", err)
				By("Updating the ENoExecEvent")
				enee.Status = v1beta1.ENoExecEventStatus{
					NodeName: "test-node-name-",
				}
				err = k8sClient.Status().Update(ctx, enee)
				Expect(err).To(HaveOccurred(), "Should not update enne status with nodeName that ends with an invalid character", err)
			})
			It("should reject a PodName that exceeds 253 character", func() {
				By("Updating the ENoExecEvent")
				enee.Status = v1beta1.ENoExecEventStatus{
					PodName: "a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890",
				}
				err := k8sClient.Status().Update(ctx, enee)
				Expect(err).To(HaveOccurred(), "Should not update enne status with podName that is longer that 253 characters", err)
			})
			It("should reject invalid PodName", func() {
				By("Updating the ENoExecEvent")
				enee.Status = v1beta1.ENoExecEventStatus{
					PodName: "test!-pod-name",
				}
				err := k8sClient.Status().Update(ctx, enee)
				Expect(err).To(HaveOccurred(), "Should not update enne status with podName that contains an invalid character", err)
				By("Updating the ENoExecEvent")
				enee.Status = v1beta1.ENoExecEventStatus{
					PodName: "test-POD-name",
				}
				err = k8sClient.Status().Update(ctx, enee)
				Expect(err).To(HaveOccurred(), "Should not update enne status with podName that contains an uppercase character", err)
			})
			It("should accept valid PodName", func() {
				By("Updating the ENoExecEvent")
				enee.Status = v1beta1.ENoExecEventStatus{
					PodName: "valid.pod-name-26",
				}
				err := k8sClient.Status().Update(ctx, enee)
				Expect(err).NotTo(HaveOccurred(), "Should update enne status with valid podName", err)
			})
			It("should reject a PodName that does not start or end with alphanumeric character", func() {
				By("Updating the ENoExecEvent")
				enee.Status = v1beta1.ENoExecEventStatus{
					PodName: "-test-pod-name",
				}
				err := k8sClient.Status().Update(ctx, enee)
				Expect(err).To(HaveOccurred(), "Should not update enne status with podName that starts with invalid character", err)
				By("Updating the ENoExecEvent")
				enee.Status = v1beta1.ENoExecEventStatus{
					PodName: "test-pod-name.",
				}
				err = k8sClient.Status().Update(ctx, enee)
				Expect(err).To(HaveOccurred(), "Should not update enne status with podName that ends with invalid character", err)
			})
			It("should reject a PodNamespace that exceeds 253 character", func() {
				By("Updating the ENoExecEvent")
				enee.Status = v1beta1.ENoExecEventStatus{
					PodNamespace: "a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890",
				}
				err := k8sClient.Status().Update(ctx, enee)
				Expect(err).To(HaveOccurred(), "Should not update enne status with podNamespace that is longer that 253 characters", err)
			})
			It("should reject invalid PodNamespace", func() {
				By("Updating the ENoExecEvent")
				enee.Status = v1beta1.ENoExecEventStatus{
					PodNamespace: "test@pod-name",
				}
				err := k8sClient.Status().Update(ctx, enee)
				Expect(err).To(HaveOccurred(), "Should not update enne status with PodNamespace that contains an invalid character", err)
				By("Updating the ENoExecEvent")
				enee.Status = v1beta1.ENoExecEventStatus{
					PodNamespace: "test-POD-name",
				}
				err = k8sClient.Status().Update(ctx, enee)
				Expect(err).To(HaveOccurred(), "Should not update enne status with PodNamespace that contains an uppercase character", err)
			})
			It("should accept valid PodNamespace", func() {
				By("Updating the ENoExecEvent")
				enee.Status = v1beta1.ENoExecEventStatus{
					PodNamespace: "valid.pod-namespace-26",
				}
				err := k8sClient.Status().Update(ctx, enee)
				Expect(err).NotTo(HaveOccurred(), "Should update enne status with valid podNamespace", err)
			})
			It("should reject a PodNamespace that does not start or end with alphanumeric character", func() {
				By("Updating the ENoExecEvent")
				enee.Status = v1beta1.ENoExecEventStatus{
					PodNamespace: "-test-pod-name",
				}
				err := k8sClient.Status().Update(ctx, enee)
				Expect(err).To(HaveOccurred(), "Should not update enne status with podNamespace that starts with invalid character", err)
				By("Updating the ENoExecEvent")
				enee.Status = v1beta1.ENoExecEventStatus{
					PodNamespace: "test-pod-name.",
				}
				err = k8sClient.Status().Update(ctx, enee)
				Expect(err).To(HaveOccurred(), "Should not update enne status with podNamespace that ends with invalid character", err)
			})
			It("should reject ContainerID name that has an invalid length", func() {
				By("Updating the ENoExecEvent")
				enee.Status = v1beta1.ENoExecEventStatus{
					ContainerID: "docker://d34db33fd34db33fd34db33fd34db33fd34db33fd34db33fd34db33fd34db33fcc",
				}
				err := k8sClient.Status().Update(ctx, enee)
				Expect(err).To(HaveOccurred(), "Should not update enne status with containerID that has a hash longer than 64 characters", err)
				By("Updating the ENoExecEvent")
				enee.Status = v1beta1.ENoExecEventStatus{
					ContainerID: "docker://d34db33fd34db33fd34db33fd34d",
				}
				err = k8sClient.Status().Update(ctx, enee)
				Expect(err).To(HaveOccurred(), "Should not update enne status with containerID that has a hash shorter than 64 characters", err)
			})
			It("should reject ContainerID name that has an invalid format", func() {
				By("Updating the ENoExecEvent")
				enee.Status = v1beta1.ENoExecEventStatus{
					ContainerID: "docker://d34db33fd34db33fd34db33fd34d-33fd34db33fd34db33fd34db33fd34db33f",
				}
				err := k8sClient.Status().Update(ctx, enee)
				Expect(err).To(HaveOccurred(), "Should not update enne status with containerID that has invalid characters", err)
				By("Updating the ENoExecEvent")
				enee.Status = v1beta1.ENoExecEventStatus{
					ContainerID: "docker://d34db33fd34db33fd34db33fd34db33fd34db33fd34db33fd34db33fd34db33z",
				}
				err = k8sClient.Status().Update(ctx, enee)
				Expect(err).To(HaveOccurred(), "Should not update enne status with containerID hash that has letters after f", err)
				By("Updating the ENoExecEvent")
				enee.Status = v1beta1.ENoExecEventStatus{
					ContainerID: "docker:/d34db33fd34db33fd34db33fd34db33fd34db33fd34db33fd34db33fd34db33",
				}
				err = k8sClient.Status().Update(ctx, enee)
				Expect(err).To(HaveOccurred(), "Should not update enne status with containerID that has invalid format", err)
			})
		})
	})
})
