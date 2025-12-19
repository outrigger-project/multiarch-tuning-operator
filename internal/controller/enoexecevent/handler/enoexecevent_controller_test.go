package handler

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/pkg/e2e"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/framework"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

func defaultENoExecFormatError() *builder.ENoExecEventBuilder {
	return builder.NewENoExecEvent().
		WithNodeName(testNodeName).
		WithPodNamespace(testNamespace).
		WithNamespace(utils.Namespace()).WithContainerID(testContainerID)
}

func ensureEvent(podName string, message string) AsyncAssertion {
	// Wait for the event to be published
	return Eventually(func(g Gomega) {
		// Get the pod from the API server
		pod := &v1.Pod{}
		err := k8sClient.Get(ctx, crclient.ObjectKey{
			Name:      podName,
			Namespace: testNamespace,
		}, pod)
		g.Expect(err).NotTo(HaveOccurred(), "failed to get Pod", err)
		// Get the events
		events, err := framework.GetEventsForObject(ctx, k8sClientSet, podName, testNamespace)
		g.Expect(err).NotTo(HaveOccurred(), "failed to get events for Pod", err)
		// Check if the event is present
		found := false
		for _, event := range events {
			if event.Reason == utils.ExecFormatErrorEventReason &&
				event.Message == message &&
				event.InvolvedObject.Namespace == testNamespace &&
				event.InvolvedObject.Name == podName {
				found = true
				break
			}
		}
		g.Expect(found).To(BeTrue(), "Event not found in Pod events")
	}).WithPolling(e2e.PollingInterval).WithTimeout(e2e.WaitShort)
}

func ensureDeletion(eneeName string) {
	Eventually(func(g Gomega) {
		enee := &v1beta1.ENoExecEvent{}
		err := k8sClient.Get(ctx, crclient.ObjectKey{
			Name:      eneeName,
			Namespace: utils.Namespace(),
		}, enee)
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "ENoExecEvent should be deleted")
	}).WithPolling(e2e.PollingInterval).WithTimeout(e2e.WaitShort).Should(Succeed(), "failed to delete ENoExecEvent")
}

func ensureLabel(podName string) AsyncAssertion {
	return Eventually(func(g Gomega) {
		pod := &v1.Pod{}
		err := k8sClient.Get(ctx, crclient.ObjectKey{
			Name:      podName,
			Namespace: testNamespace,
		}, pod)
		g.Expect(err).NotTo(HaveOccurred(), "failed to get Pod", err)
		g.Expect(pod.Labels).To(HaveKeyWithValue(utils.ExecFormatErrorLabelKey, utils.True), "Pod should be labeled with ENoExecEvent label")
	})
}

func deletePod(podName string) {
	pod := &v1.Pod{}
	err := k8sClient.Get(ctx, crclient.ObjectKey{
		Name:      podName,
		Namespace: testNamespace,
	}, pod)
	if err != nil && !apierrors.IsNotFound(err) {
		Fail(fmt.Sprintf("failed to get Pod %s: %v", podName, err))
	}
	if err == nil {
		Expect(k8sClient.Delete(ctx, pod)).To(Succeed(), "failed to delete Pod")
	}
}

func createENEEAndUpdateStatus(enee *v1beta1.ENoExecEvent) {
	By("Creating the ENoExecEvent")
	Expect(k8sClient.Create(ctx, enee.DeepCopy())).To(Succeed(), "failed to create ENoExecEvent")
	var eneeToUpdate = &v1beta1.ENoExecEvent{}
	Expect(k8sClient.Get(ctx, crclient.ObjectKey{
		Name:      enee.Name,
		Namespace: enee.Namespace,
	}, eneeToUpdate)).To(Succeed(), "failed to get ENoExecEvent after creation")
	eneeToUpdate.Status = enee.Status
	By("Updating the ENoExecEvent status")
	Expect(k8sClient.Status().Update(ctx, eneeToUpdate)).To(Succeed(), "failed to update ENoExecEvent status")
}

func createPodAndUpdateStatus(pod *v1.Pod) {
	By("Creating the Pod")
	Expect(k8sClient.Create(ctx, pod.DeepCopy())).To(Succeed(), "failed to create Pod")
	var podToUpdate = &v1.Pod{}
	Expect(k8sClient.Get(ctx, crclient.ObjectKey{
		Name:      pod.Name,
		Namespace: pod.Namespace,
	}, podToUpdate)).To(Succeed(), "failed to get Pod after creation")
	podToUpdate.Status = pod.Status
	By("Updating the Pod status")
	Expect(k8sClient.Status().Update(ctx, podToUpdate)).To(Succeed(), "failed to update Pod status")
}

var _ = Describe("internal/Controller/ENoExecEvent/Reconciler", func() {
	When("The operand", func() {
		Context("reconciles an ENoExecEvent object", func() {
			It("should publish an event to the pod", func() {
				// Create the pod
				podName := framework.GenerateName()
				eneeName := framework.GenerateName()
				pod := builder.NewPod().WithNamespace(testNamespace).WithName(podName).WithNodeName(testNodeName).
					WithContainer("test-image", v1.PullAlways).
					WithContainerStatuses(builder.NewContainerStatus().WithName(testContainerName).WithID(testContainerID).Build()).
					Build()
				createPodAndUpdateStatus(pod)
				// Create the ENoExecEvent object
				enee := defaultENoExecFormatError().WithPodName(podName).WithName(eneeName).Build()
				createENEEAndUpdateStatus(enee)
				By("Ensuring the event is published")
				ensureEvent(podName, utils.ExecFormatErrorEventMessage(testContainerName, testNodeArch)).
					Should(Succeed(), "failed to get event for Pod")
				By("Ensuring the ENoExecEvent is deleted")
				ensureDeletion(eneeName)
				By("Deleting pod")
				deletePod(podName)
			})
			It("should label the pod with the ENoExecEvent label", func() {
				// Create the pod
				podName := framework.GenerateName()
				eneeName := framework.GenerateName()
				pod := builder.NewPod().WithNamespace(testNamespace).WithName(podName).WithNodeName(testNodeName).
					WithContainer("test-image", v1.PullAlways).
					WithContainerStatuses(builder.NewContainerStatus().WithName(testContainerName).WithID(testContainerID).Build()).
					Build()
				createPodAndUpdateStatus(pod)
				enee := defaultENoExecFormatError().WithPodName(podName).WithName(eneeName).Build()
				createENEEAndUpdateStatus(enee)
				By("Ensuring the pod is labeled with ENoExecEvent label")
				ensureLabel(podName).Should(Succeed(), "failed to label Pod with ENoExecEvent label")
				By("Ensuring the ENoExecEvent is deleted")
				ensureDeletion(eneeName)
				By("Deleting pod")
				deletePod(podName)
			})
			It("should delete the ENoExecEvent object if the pod is not found", func() {
				// Create the ENoExecEvent object
				eneeName := framework.GenerateName()
				enee := defaultENoExecFormatError().WithPodName("non-existing-pod").WithName(eneeName).Build()
				createENEEAndUpdateStatus(enee)
				// Ensure the ENoExecEvent is deleted
				By("Ensuring the ENoExecEvent is deleted")
				ensureDeletion(eneeName)
			})
			It("should delete the ENoExecEvent object if the node is not found", func() {
				// Create the pod
				podName := framework.GenerateName()
				eneeName := framework.GenerateName()
				pod := builder.NewPod().WithNamespace(testNamespace).WithName(podName).WithNodeName(testNodeName).
					WithContainer("test-image", v1.PullAlways).
					WithContainerStatuses(builder.NewContainerStatus().WithName(testContainerName).WithID(testContainerID).Build()).
					Build()
				createPodAndUpdateStatus(pod)
				// Create the ENoExecEvent object
				enee := defaultENoExecFormatError().WithPodName(podName).WithNodeName("non-existing-node").WithName(eneeName).Build()
				createENEEAndUpdateStatus(enee)
				By("Ensuring the ENoExecEvent is deleted")
				ensureDeletion(eneeName)
				By("Ensuring the pod is not labeled with ENoExecEvent label")
				ensureLabel(podName).ShouldNot(Succeed(), "the pod should not have the ENoExecEvent label if the node is not found")
				By("Ensuring the pod does not have an event published")
				ensureEvent(podName, utils.ExecFormatErrorEventMessage(testContainerName, testNodeArch)).
					ShouldNot(Succeed(), "the pod should not have an event published if the node is not found")
				By("Deleting pod")
				deletePod(podName)
			})
			It("should publish the event to the pod with a wrong container ID", func() {
				// Create the pod
				podName := framework.GenerateName()
				eneeName := framework.GenerateName()
				pod := builder.NewPod().WithNamespace(testNamespace).WithName(podName).WithNodeName(testNodeName).
					WithContainer("test-image", v1.PullAlways).
					WithContainerStatuses(builder.NewContainerStatus().WithName(testContainerName).WithID("wrong-container-id").Build()).
					Build()
				createPodAndUpdateStatus(pod)
				// Create the ENoExecEvent object
				enee := defaultENoExecFormatError().WithPodName(podName).WithName(eneeName).Build()
				createENEEAndUpdateStatus(enee)
				// Ensure the ENoExecEvent is deleted
				ensureDeletion(eneeName)
				// Ensure the event is published
				ensureEvent(podName, utils.ExecFormatErrorEventMessage(utils.UnknownContainer, testNodeArch)).
					Should(Succeed(), "failed to get event for Pod with wrong container ID")
				ensureLabel(podName).Should(Succeed(), "failed to label Pod with ENoExecEvent label for wrong container ID")

			})
			It("should delete the ENoExecEvent object if the node is not the same as the podSpec.NodeName value", func() {
				// Create the pod
				podName := framework.GenerateName()
				eneeName := framework.GenerateName()
				pod := builder.NewPod().WithNamespace(testNamespace).WithName(podName).WithNodeName("other-node").
					WithContainer("test-image", v1.PullAlways).
					WithContainerStatuses(builder.NewContainerStatus().WithName(testContainerName).WithID(testContainerID).Build()).
					Build()
				createPodAndUpdateStatus(pod)
				// Create the ENoExecEvent object
				enee := defaultENoExecFormatError().WithPodName(podName).WithNodeName(testNodeName).WithName(eneeName).Build()
				createENEEAndUpdateStatus(enee)
				ensureDeletion(eneeName)
				By("Ensuring the pod is not labeled with ENoExecEvent label")
				ensureLabel(podName).ShouldNot(Succeed(), "the pod should not have the ENoExecEvent label if the node is not found")
				By("Ensuring the pod does not have an event published")
				ensureEvent(podName, utils.ExecFormatErrorEventMessage(testContainerName, testNodeArch)).
					ShouldNot(Succeed(), "the pod should not have an event published if the node is not found")
				By("Deleting pod")
				deletePod(podName)
			})
		})
		Context("basic creation", func() {
			It("should create, delete an ENoExecEvent CR", func() {
				By("Creating the ENoExecEvent")
				enee := builder.NewENoExecEvent().WithName("test").WithNamespace(testNamespace).Build()
				err := k8sClient.Create(ctx, enee)
				Expect(err).NotTo(HaveOccurred(), "failed to create ENoExecEvent", err)
				By("Deleting the ENoExecEvent")
				err = k8sClient.Delete(ctx, enee)
				Expect(err).NotTo(HaveOccurred(), "failed to delete ENoExecEvent", err)
			})
			It("should create a ENoExecEvent CR with set fields", func() {
				By("Creating the ENoExecEvent")
				enee := builder.NewENoExecEvent().WithName("test-name").WithNamespace(testNamespace).Build()
				err := k8sClient.Create(ctx, enee)
				Expect(err).NotTo(HaveOccurred(), "failed to create ENoExecEvent", err)

				// Set status manually (after creation)
				enee.Status = v1beta1.ENoExecEventStatus{
					NodeName:     "test-node",
					PodName:      "test-pod",
					PodNamespace: "test-namespace",
					ContainerID:  "docker://d34db33fd34db33fd34db33fa34db33fd34db33fd34db33fd34db33fd34db3d3",
				}
				err = k8sClient.Status().Update(ctx, enee)
				Expect(err).NotTo(HaveOccurred())
				Eventually(func(g Gomega) {
					// Get enee from the API server
					By("Ensure ENoExecEvent exists")
					enee = &v1beta1.ENoExecEvent{}
					err := k8sClient.Get(ctx, crclient.ObjectKey{
						Name:      "test-name",
						Namespace: testNamespace,
					}, enee)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get enee", err)
					g.Expect(enee.Status.NodeName).To(Equal("test-node"))
					g.Expect(enee.Status.PodName).To(Equal("test-pod"))
					g.Expect(enee.Status.PodNamespace).To(Equal("test-namespace"))
					g.Expect(enee.Status.ContainerID).To(Equal("docker://d34db33fd34db33fd34db33fa34db33fd34db33fd34db33fd34db33fd34db3d3"))
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
					WithNamespace(testNamespace).
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
						Namespace: testNamespace,
					}, &v1beta1.ENoExecEvent{})
				}).Should(MatchError(ContainSubstring("not found")), "the ENoExecEvent should be deleted")
			})
			It("should reject a NodeName that exceeds 253 character", func() {
				By("Updating the ENoExecEvent")
				enee.Status = v1beta1.ENoExecEventStatus{
					NodeName: "a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a234567890",
				}
				err := k8sClient.Status().Update(ctx, enee)
				Expect(err).To(HaveOccurred(), "Should not update enne status with NodeName that is longer that 253 characters", err)
			})
			It("should reject a NodeName that has an invalid character", func() {
				By("Updating the ENoExecEvent")
				enee.Status = v1beta1.ENoExecEventStatus{
					NodeName: "test!-node-name",
				}
				err := k8sClient.Status().Update(ctx, enee)
				Expect(err).To(HaveOccurred(), "Should not update enne status with nodeName that contains an invalid character", err)
				enee.Status = v1beta1.ENoExecEventStatus{
					NodeName: "test?node-name",
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
			It("should accept a NodeName that has valid character", func() {
				By("Updating the ENoExecEvent")
				enee.Status = v1beta1.ENoExecEventStatus{
					NodeName: "test.node.name",
				}
				err := k8sClient.Status().Update(ctx, enee)
				Expect(err).NotTo(HaveOccurred(), "Should update enne status with nodeName that starts with an valid character", err)
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
					PodNamespace: "a234567890.a234567890.a234567890.a234567890.a234567890.a234567890.a23456789",
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
				By("Updating the ENoExecEvent")
				enee.Status = v1beta1.ENoExecEventStatus{
					PodNamespace: "test.pod.name",
				}
				err = k8sClient.Status().Update(ctx, enee)
				Expect(err).To(HaveOccurred(), "Should not update enne status with PodNamespace that contains an invalid character", err)
			})
			It("should accept valid PodNamespace", func() {
				By("Updating the ENoExecEvent")
				enee.Status = v1beta1.ENoExecEventStatus{
					PodNamespace: "valid-pod-namespace-26",
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
					PodNamespace: "test-pod-name-",
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
