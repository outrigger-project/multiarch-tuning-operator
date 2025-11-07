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
	"fmt"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/common"
	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/framework"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

var _ = Describe("Controllers/ClusterPodPlacementConfig/ClusterPodPlacementConfigReconciler", Serial, Ordered, func() {
	When("The ClusterPodPlacementConfig", func() {
		Context("is handling the lifecycle of the operand", func() {
			BeforeEach(func() {
				By("Creating the ClusterPodPlacementConfig")
				err := k8sClient.Create(ctx, builder.NewClusterPodPlacementConfig().WithName(common.SingletonResourceObjectName).Build())
				Expect(err).NotTo(HaveOccurred(), "failed to create ClusterPodPlacementConfig", err)
				validateReconcile()
			})
			AfterEach(func() {
				By("Deleting the ClusterPodPlacementConfig")
				err := k8sClient.Delete(ctx, builder.NewClusterPodPlacementConfig().WithName(common.SingletonResourceObjectName).Build())
				Expect(err).NotTo(HaveOccurred(), "failed to delete ClusterPodPlacementConfig", err)
				Eventually(framework.ValidateDeletion(k8sClient, ctx)).Should(Succeed(), "the ClusterPodPlacementConfig should be deleted")
			})
			It("should refuse to create a ClusterPodPlacementConfig with an invalid name", func() {
				By("Creating a ClusterPodPlacementConfig with an invalid name")
				ppc := builder.NewClusterPodPlacementConfig().WithName("invalid-name").Build()
				err := k8sClient.Create(ctx, ppc)
				Expect(err).To(HaveOccurred(), "The creation of the ClusterPodPlacementConfig with a wrong name did not fail", err)
			})
			It("should reconcile the deployment pod-placement-controller", func() {
				// get the deployment
				By("getting the deployment " + utils.PodPlacementControllerName)
				d := appsv1.Deployment{}
				err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      utils.PodPlacementControllerName,
						Namespace: utils.Namespace(),
					},
				}), &d)
				Expect(err).NotTo(HaveOccurred(), "failed to get deployment "+utils.PodPlacementControllerName, err)
				// change the deployment's replicas
				By("changing the deployment's replicas to 3")
				d.Spec.Replicas = utils.NewPtr(int32(3))
				err = k8sClient.Update(ctx, &d)
				Expect(err).NotTo(HaveOccurred(), "failed to update deployment "+utils.PodPlacementControllerName, err)
				By("verifying the conditions are correct")
				Eventually(framework.VerifyConditions(ctx, k8sClient,
					framework.NewConditionTypeStatusTuple(v1beta1.AvailableType, corev1.ConditionTrue),
					framework.NewConditionTypeStatusTuple(v1beta1.ProgressingType, corev1.ConditionTrue),
					framework.NewConditionTypeStatusTuple(v1beta1.DegradedType, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.PodPlacementControllerNotRolledOutType, corev1.ConditionTrue),
					framework.NewConditionTypeStatusTuple(v1beta1.PodPlacementWebhookNotRolledOutType, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.MutatingWebhookConfigurationNotAvailable, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.DeprovisioningType, corev1.ConditionFalse),
				)).Should(Succeed(), "the ClusterPodPlacementConfig should have the correct conditions")
				By("verifying the deployment's replicas are reconciled 2")
				Eventually(func(g Gomega) {
					d := appsv1.Deployment{}
					err = k8sClient.Get(ctx, crclient.ObjectKeyFromObject(&appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      utils.PodPlacementControllerName,
							Namespace: utils.Namespace(),
						},
					}), &d)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get deployment "+utils.PodPlacementControllerName, err)
					g.Expect(d.Spec.Replicas).To(Equal(utils.NewPtr(int32(2))), "the deployment's replicas should be 2")
				}).Should(Succeed(), "the deployment's replicas should be 2")
				setDeploymentReady(utils.PodPlacementControllerName, NewGomegaWithT(GinkgoT()))
				By("verifying the conditions are restored to normal")
				Eventually(framework.VerifyConditions(ctx, k8sClient,
					framework.NewConditionTypeStatusTuple(v1beta1.AvailableType, corev1.ConditionTrue),
					framework.NewConditionTypeStatusTuple(v1beta1.ProgressingType, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.DegradedType, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.PodPlacementControllerNotRolledOutType, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.PodPlacementWebhookNotRolledOutType, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.MutatingWebhookConfigurationNotAvailable, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.DeprovisioningType, corev1.ConditionFalse),
				))
			})
			DescribeTable("should reconcile the deployment when deleted", func(deployment string) {
				By("deleting the deployment " + deployment)
				err := k8sClient.Delete(ctx, &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      deployment,
						Namespace: utils.Namespace(),
					},
				})
				Expect(err).NotTo(HaveOccurred(), "failed to delete deployment "+utils.PodPlacementWebhookName, err)
				By("verifying the conditions are correct")
				Eventually(func(g Gomega) {
					err = k8sClient.Get(ctx, crclient.ObjectKey{
						Name: utils.PodMutatingWebhookConfigurationName,
					}, &admissionv1.MutatingWebhookConfiguration{})
					g.Expect(err).To(HaveOccurred(), "the mutating webhook configuration should not be available", err)
					g.Expect(errors.IsNotFound(err)).To(BeTrue(), "the mutating webhook configuration should not be available", err)
					framework.VerifyConditions(ctx, k8sClient,
						framework.NewConditionTypeStatusTuple(v1beta1.AvailableType, corev1.ConditionFalse),
						framework.NewConditionTypeStatusTuple(v1beta1.ProgressingType, corev1.ConditionTrue),
						framework.NewConditionTypeStatusTuple(v1beta1.DegradedType, corev1.ConditionTrue),
						framework.NewConditionTypeStatusTuple(v1beta1.MutatingWebhookConfigurationNotAvailable, corev1.ConditionTrue),
						framework.NewConditionTypeStatusTuple(v1beta1.DeprovisioningType, corev1.ConditionFalse),
					)(g)
				}).Should(Succeed(), "the ClusterPodPlacementConfig should have the correct conditions")

				By("Verify the deployment is recreated")
				validateReconcile()
			},
				Entry(utils.PodPlacementWebhookName, utils.PodPlacementWebhookName), Entry(utils.PodPlacementControllerName, utils.PodPlacementControllerName))
			It("should reconcile a service if deleted", func() {
				err := k8sClient.Delete(ctx, &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      utils.PodPlacementWebhookName,
						Namespace: utils.Namespace(),
					},
				}, crclient.PropagationPolicy(metav1.DeletePropagationBackground))
				Expect(err).NotTo(HaveOccurred(), "failed to delete service "+utils.PodPlacementWebhookName, err)
				Eventually(func(g Gomega) {
					s := &corev1.Service{}
					err = k8sClient.Get(ctx, crclient.ObjectKeyFromObject(&corev1.Service{
						ObjectMeta: metav1.ObjectMeta{
							Name:      utils.PodPlacementWebhookName,
							Namespace: utils.Namespace(),
						},
					}), s)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get service "+utils.PodPlacementWebhookName, err)
				}).Should(Succeed(), "the service "+utils.PodPlacementWebhookName+" should be recreated")
			})
			DescribeTable("should reconcile if deleted", func(object crclient.Object) {
				name := object.GetName()
				By("Deleting " + name)
				err := k8sClient.Delete(ctx, object)
				Expect(err).NotTo(HaveOccurred(), "failed to delete "+name, err)

				By("Looking for the object to be recreated")
				Eventually(func(g Gomega) {
					err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(object), object)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get "+name, err)
					g.Expect(object.GetDeletionTimestamp().IsZero()).To(BeTrue(), "the "+name+" is still pending deletion")
				}).Should(Succeed(), "the "+name+" should be recreated")
			},
				Entry("ClusterRole", builder.NewClusterRole().WithName(utils.PodPlacementWebhookName).Build()),
				Entry("ClusterRoleBinding", builder.NewClusterRoleBinding().WithName(utils.PodPlacementWebhookName).Build()),
				Entry("Role", builder.NewRole().WithName(utils.PodPlacementControllerName).WithNamespace(utils.Namespace()).Build()),
				Entry("RoleBinding", builder.NewRoleBinding().WithName(utils.PodPlacementControllerName).WithNamespace(utils.Namespace()).Build()),
				Entry("ServiceAccount", builder.NewServiceAccount().WithName(utils.PodPlacementWebhookName).WithNamespace(utils.Namespace()).Build()),
			)
			It("should reconcile a service if changed", func() {
				s := &corev1.Service{}
				err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      utils.PodPlacementWebhookName,
						Namespace: utils.Namespace(),
					},
				}), s)
				Expect(err).NotTo(HaveOccurred(), "failed to get service "+utils.PodPlacementWebhookName, err)
				By("changing the service's port")
				// change the service's port
				s.Spec.Ports[0].Port = 8080
				err = k8sClient.Update(ctx, s)
				Expect(err).NotTo(HaveOccurred(), "failed to update service "+utils.PodPlacementWebhookName, err)
				By("waiting for the service's port to be reconciled")
				Eventually(func(g Gomega) {
					s := &corev1.Service{}
					err = k8sClient.Get(ctx, crclient.ObjectKeyFromObject(&corev1.Service{
						ObjectMeta: metav1.ObjectMeta{
							Name:      utils.PodPlacementWebhookName,
							Namespace: utils.Namespace(),
						},
					}), s)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get service "+utils.PodPlacementWebhookName, err)
					g.Expect(s.Spec.Ports[0].Port).To(Equal(int32(443)), "the service's port should be 443")
				}).Should(Succeed(), "the service's port never reconciled to 443")
			})
			It("should reconcile a mutating webhook configuration if changed", func() {
				mwc := &admissionv1.MutatingWebhookConfiguration{}
				err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(&admissionv1.MutatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name: utils.PodMutatingWebhookConfigurationName,
					},
				}), mwc)
				Expect(err).NotTo(HaveOccurred(), "failed to get mutating webhook configuration "+utils.PodMutatingWebhookConfigurationName, err)
				// change the mutating webhook configuration's failure policy
				mwc.Webhooks[0].FailurePolicy = utils.NewPtr(admissionv1.Fail)
				err = k8sClient.Update(ctx, mwc)
				Expect(err).NotTo(HaveOccurred(), "failed to update mutating webhook configuration "+utils.PodMutatingWebhookConfigurationName, err)
				Eventually(func(g Gomega) {
					mwc := &admissionv1.MutatingWebhookConfiguration{}
					err = k8sClient.Get(ctx, crclient.ObjectKeyFromObject(&admissionv1.MutatingWebhookConfiguration{
						ObjectMeta: metav1.ObjectMeta{
							Name: utils.PodMutatingWebhookConfigurationName,
						},
					}), mwc)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get mutating webhook configuration "+utils.PodMutatingWebhookConfigurationName, err)
					g.Expect(mwc.Webhooks[0].FailurePolicy).To(Equal(utils.NewPtr(admissionv1.Ignore)), "the mutating webhook configuration's failure policy should be Ignore")
				}).Should(Succeed(), "the mutating webhook configuration's failure policy never reconciled to Ignore")
			})
			It("should sync the deployments' logLevel arguments", func() {
				By("Changing the logLevel")
				Eventually(func(g Gomega) {
					ppc := &v1beta1.ClusterPodPlacementConfig{}
					err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(&v1beta1.ClusterPodPlacementConfig{
						ObjectMeta: metav1.ObjectMeta{
							Name:      common.SingletonResourceObjectName,
							Namespace: utils.Namespace(),
						},
					}), ppc)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get ClusterPodPlacementConfig", err)
					// change the clusterpodplacementconfig's logLevel
					ppc.Spec.LogVerbosity = common.LogVerbosityLevelTraceAll
					err = k8sClient.Update(ctx, ppc)
					g.Expect(err).NotTo(HaveOccurred(), "failed to update ClusterPodPlacementConfig", err)
				}).Should(Succeed(), "the ClusterPodPlacementConfig should be updated")
				By("Verifying the conditions are correct: available but not rolled out")
				Eventually(framework.VerifyConditions(ctx, k8sClient,
					framework.NewConditionTypeStatusTuple(v1beta1.AvailableType, corev1.ConditionTrue),
					framework.NewConditionTypeStatusTuple(v1beta1.ProgressingType, corev1.ConditionTrue),
					framework.NewConditionTypeStatusTuple(v1beta1.DegradedType, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.PodPlacementControllerNotRolledOutType, corev1.ConditionTrue),
					framework.NewConditionTypeStatusTuple(v1beta1.PodPlacementWebhookNotRolledOutType, corev1.ConditionTrue),
					framework.NewConditionTypeStatusTuple(v1beta1.MutatingWebhookConfigurationNotAvailable, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.DeprovisioningType, corev1.ConditionFalse),
				)).Should(Succeed(), "the ClusterPodPlacementConfig should have the correct conditions")
				By("Verifying the deployments' logLevel arguments are updated")
				Eventually(func(g Gomega) {
					d := appsv1.Deployment{}
					err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(&appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      utils.PodPlacementControllerName,
							Namespace: utils.Namespace(),
						},
					}), &d)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get deployment "+utils.PodPlacementControllerName, err)
					g.Expect(d.Spec.Template.Spec.Containers[0].Args).To(ContainElement(
						fmt.Sprintf("--initial-log-level=%d", common.LogVerbosityLevelTraceAll.ToZapLevelInt())))
				}).Should(Succeed(), "the deployment "+utils.PodPlacementControllerName+" should be updated")
				Eventually(func(g Gomega) {
					d := appsv1.Deployment{}
					err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(&appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      utils.PodPlacementWebhookName,
							Namespace: utils.Namespace(),
						},
					}), &d)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get deployment "+utils.PodPlacementWebhookName, err)
					g.Expect(d.Spec.Template.Spec.Containers[0].Args).To(ContainElement(
						fmt.Sprintf("--initial-log-level=%d", common.LogVerbosityLevelTraceAll.ToZapLevelInt())))
				}).Should(Succeed(), "the deployment "+utils.PodPlacementWebhookName+" should be updated")
				setDeploymentReady(utils.PodPlacementControllerName, NewGomegaWithT(GinkgoT()))
				setDeploymentReady(utils.PodPlacementWebhookName, NewGomegaWithT(GinkgoT()))
				By("Verifying the conditions are restored to normal")
				Eventually(framework.VerifyConditions(ctx, k8sClient,
					framework.NewConditionTypeStatusTuple(v1beta1.AvailableType, corev1.ConditionTrue),
					framework.NewConditionTypeStatusTuple(v1beta1.ProgressingType, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.DegradedType, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.PodPlacementControllerNotRolledOutType, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.PodPlacementWebhookNotRolledOutType, corev1.ConditionFalse),
				)).Should(Succeed(), "the ClusterPodPlacementConfig should have the correct conditions")
			})
			It("Should sync the namespace selector", func() {
				// get the clusterpodplacementconfig
				ppc := &v1beta1.ClusterPodPlacementConfig{}
				Eventually(func(g Gomega) {
					err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(&v1beta1.ClusterPodPlacementConfig{
						ObjectMeta: metav1.ObjectMeta{
							Name:      common.SingletonResourceObjectName,
							Namespace: utils.Namespace(),
						},
					}), ppc)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get ClusterPodPlacementConfig", err)
					// change the clusterpodplacementconfig's namespace selector
					ppc.Spec.NamespaceSelector = &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"foo": "bar",
						},
					}
					err = k8sClient.Update(ctx, ppc)
					g.Expect(err).NotTo(HaveOccurred(), "failed to update ClusterPodPlacementConfig", err)
				}).Should(Succeed(), "the ClusterPodPlacementConfig should be updated")
				Eventually(func(g Gomega) {
					mw := &admissionv1.MutatingWebhookConfiguration{}
					err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(&admissionv1.MutatingWebhookConfiguration{
						ObjectMeta: metav1.ObjectMeta{
							Name: utils.PodMutatingWebhookConfigurationName,
						},
					}), mw)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get mutating webhook configuration "+utils.PodMutatingWebhookConfigurationName, err)
					g.Expect(mw.Webhooks[0].NamespaceSelector).To(Equal(ppc.Spec.NamespaceSelector))
				}).Should(Succeed(), "the deployment "+utils.PodPlacementControllerName+" should be updated")
			})
			It("Should have ClusterPodPlacementConfig finalizers", func() {
				ppc := &v1beta1.ClusterPodPlacementConfig{}
				err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(&v1beta1.ClusterPodPlacementConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      common.SingletonResourceObjectName,
						Namespace: utils.Namespace(),
					},
				}), ppc)
				Expect(err).NotTo(HaveOccurred(), "failed to get ClusterPodPlacementConfig", err)
				Eventually(func(g Gomega) {
					cppc := &v1beta1.ClusterPodPlacementConfig{}
					err := k8sClient.Get(ctx, crclient.ObjectKey{
						Name:      common.SingletonResourceObjectName,
						Namespace: utils.Namespace(),
					}, cppc)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get ClusterPodPlacementConfig", err)
					g.Expect(cppc.Finalizers).To(ContainElement(utils.PodPlacementFinalizerName))
				})
			})
		})
		Context("is handling the cleanup lifecycle of the ClusterPodPlacementConfig", func() {
			BeforeEach(func() {
				err := k8sClient.Create(ctx, builder.NewClusterPodPlacementConfig().WithName(common.SingletonResourceObjectName).Build())
				Expect(err).NotTo(HaveOccurred(), "failed to create ClusterPodPlacementConfig", err)
				validateReconcile()
			})
			It("Should remove finalizers and allow the collection of ClusterPodPlcementConfig and Pod placement controller deployment if no pods with our scheduling gates are present", func() {
				// add a pod with a different scheduling gate
				pod := builder.NewPod().
					WithContainersImages("nginx:latest").
					WithGenerateName("test-pod-").
					WithSchedulingGates("different-scheduling-gate").
					WithNamespace("test-namespace").
					Build()
				err := k8sClient.Create(ctx, pod)
				Expect(err).NotTo(HaveOccurred(), "failed to create pod", err)
				err = k8sClient.Delete(ctx, builder.NewClusterPodPlacementConfig().WithName(common.SingletonResourceObjectName).Build())
				Expect(err).NotTo(HaveOccurred(), "failed to delete ClusterPodPlacementConfig", err)
				Eventually(framework.ValidateDeletion(k8sClient, ctx)).Should(Succeed(), "the ClusterPodPlacementConfig should be deleted")
				err = k8sClient.Delete(ctx, pod)
				Expect(err).NotTo(HaveOccurred(), "failed to delete pod", err)
			})
			It("Should not remove finalizers and not allow the collection of ClusterPodPlcementConfig and Pod placement controller deployment until pods with our scheduling gates are present", func() {
				// add a pod with our scheduling gate
				pod := builder.NewPod().
					WithContainersImages("nginx:latest").
					WithGenerateName("test-pod-").
					WithSchedulingGates(utils.SchedulingGateName).
					WithNamespace("test-namespace").
					Build()
				err := k8sClient.Create(ctx, pod)
				Expect(err).NotTo(HaveOccurred(), "failed to create pod", err)
				By("The pod has been created with our scheduling gate (the pod reconciler is not running in the integration test, therefore the scheduling gate will not be removed)")
				err = k8sClient.Delete(ctx, builder.NewClusterPodPlacementConfig().WithName(common.SingletonResourceObjectName).Build())
				Expect(err).NotTo(HaveOccurred(), "failed to delete ClusterPodPlacementConfig", err)
				Consistently(func(g Gomega) {
					cppc := &v1beta1.ClusterPodPlacementConfig{}
					err := k8sClient.Get(ctx, crclient.ObjectKey{
						Name:      common.SingletonResourceObjectName,
						Namespace: utils.Namespace(),
					}, cppc)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get ClusterPodPlacementConfig", err)
					g.Expect(cppc.DeletionTimestamp.IsZero()).NotTo(BeTrue())
					g.Expect(cppc.Finalizers).To(ContainElement(utils.PodPlacementFinalizerName))
					framework.VerifyConditions(ctx, k8sClient,
						framework.NewConditionTypeStatusTuple(v1beta1.AvailableType, corev1.ConditionFalse),
						framework.NewConditionTypeStatusTuple(v1beta1.ProgressingType, corev1.ConditionFalse),
						framework.NewConditionTypeStatusTuple(v1beta1.DegradedType, corev1.ConditionFalse),
						framework.NewConditionTypeStatusTuple(v1beta1.PodPlacementControllerNotRolledOutType, corev1.ConditionFalse),
						framework.NewConditionTypeStatusTuple(v1beta1.PodPlacementWebhookNotRolledOutType, corev1.ConditionTrue),
						framework.NewConditionTypeStatusTuple(v1beta1.MutatingWebhookConfigurationNotAvailable, corev1.ConditionTrue),
						framework.NewConditionTypeStatusTuple(v1beta1.DeprovisioningType, corev1.ConditionTrue),
					)
				})
				By("Manually delete the gated pod")
				err = k8sClient.Delete(ctx, pod)
				Expect(err).NotTo(HaveOccurred(), "failed to delete pod", err)
				By("The pod has been deleted and the ClusterPodPlacementConfig should now be collected")
				Eventually(framework.ValidateDeletion(k8sClient, ctx)).Should(Succeed(), "the ClusterPodPlacementConfig should be deleted")
			})
		})
		Context("the ClusterPodPlacementConfig is deleted within 1s after creation", func() {
			It("Should cleanup all finalizers", func() {
				By("Creating the ClusterPodPlacementConfig")
				err := k8sClient.Create(ctx, builder.NewClusterPodPlacementConfig().WithName(common.SingletonResourceObjectName).Build())
				Expect(err).NotTo(HaveOccurred(), "failed to create ClusterPodPlacementConfig", err)
				By("imeditately deleting it after creation")
				err = k8sClient.Delete(ctx, builder.NewClusterPodPlacementConfig().WithName(common.SingletonResourceObjectName).Build())
				Expect(err).NotTo(HaveOccurred(), "failed to delete ClusterPodPlacementConfig", err)
				By("Verify all corresponding resources are deleted")
				Eventually(framework.ValidateDeletion(k8sClient, ctx)).Should(Succeed(), "the ClusterPodPlacementConfig should be deleted")
			})
		})
	})
	Context("the operand is deployed", func() {
		BeforeAll(func() {
			err := k8sClient.Create(ctx, builder.NewClusterPodPlacementConfig().WithName(common.SingletonResourceObjectName).Build())
			Expect(err).NotTo(HaveOccurred(), "failed to create ClusterPodPlacementConfig", err)
			validateReconcile()
		})
		AfterEach(func() {
			By("Restoring the status of the deployments")
			setDeploymentReady(utils.PodPlacementControllerName, NewGomegaWithT(GinkgoT()))
			setDeploymentReady(utils.PodPlacementWebhookName, NewGomegaWithT(GinkgoT()))
		})
		When("all components are available", func() {
			It("should be available", func() {
				Eventually(
					framework.VerifyConditions(ctx, k8sClient,
						framework.NewConditionTypeStatusTuple(v1beta1.AvailableType, corev1.ConditionTrue),
						framework.NewConditionTypeStatusTuple(v1beta1.ProgressingType, corev1.ConditionFalse),
						framework.NewConditionTypeStatusTuple(v1beta1.DegradedType, corev1.ConditionFalse),
						framework.NewConditionTypeStatusTuple(v1beta1.PodPlacementControllerNotRolledOutType, corev1.ConditionFalse),
						framework.NewConditionTypeStatusTuple(v1beta1.PodPlacementWebhookNotRolledOutType, corev1.ConditionFalse),
						framework.NewConditionTypeStatusTuple(v1beta1.MutatingWebhookConfigurationNotAvailable, corev1.ConditionFalse),
						framework.NewConditionTypeStatusTuple(v1beta1.DeprovisioningType, corev1.ConditionFalse),
					)).Should(Succeed(), "the ClusterPodPlacementConfig should have the correct conditions")
			})
		})
		When("the pod placement controller is not available", func() {
			It("should be degraded, progressing and no mutating webhook should be present", func() {
				patchDeploymentStatus(utils.PodPlacementControllerName, NewGomegaWithT(GinkgoT()), func(d *appsv1.Deployment) {
					d.Status.AvailableReplicas = 0
					d.Status.UpdatedReplicas = 0
					d.Status.ReadyReplicas = 0
					d.Status.Replicas = 0
				})
				Eventually(framework.VerifyConditions(ctx, k8sClient,
					framework.NewConditionTypeStatusTuple(v1beta1.AvailableType, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.ProgressingType, corev1.ConditionTrue),
					framework.NewConditionTypeStatusTuple(v1beta1.DegradedType, corev1.ConditionTrue),
					framework.NewConditionTypeStatusTuple(v1beta1.PodPlacementControllerNotRolledOutType, corev1.ConditionTrue),
					framework.NewConditionTypeStatusTuple(v1beta1.PodPlacementWebhookNotRolledOutType, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.MutatingWebhookConfigurationNotAvailable, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.DeprovisioningType, corev1.ConditionFalse),
				)).Should(Succeed(), "the ClusterPodPlacementConfig should have the correct conditions")
				By("Verify no mutating webhook configuration is not available")
				err := k8sClient.Get(ctx, crclient.ObjectKey{
					Name: utils.PodMutatingWebhookConfigurationName,
				}, &admissionv1.MutatingWebhookConfiguration{})
				Expect(err).To(HaveOccurred(), "the mutating webhook configuration should not be available", err)
				Expect(errors.IsNotFound(err)).To(BeTrue(), "the mutating webhook configuration should not be available", err)
			})
		})
		When("the pod placement webhook is not available", func() {
			It("should be unavailable and degraded", func() {
				By("Setting the deployment's available replicas to 0")
				patchDeploymentStatus(utils.PodPlacementWebhookName, NewGomegaWithT(GinkgoT()), func(d *appsv1.Deployment) {
					d.Status.AvailableReplicas = 0
					d.Status.UpdatedReplicas = 0
					d.Status.ReadyReplicas = 0
					d.Status.Replicas = 0
				})
				By("Verifying the conditions are correct")
				Eventually(framework.VerifyConditions(ctx, k8sClient,
					framework.NewConditionTypeStatusTuple(v1beta1.AvailableType, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.ProgressingType, corev1.ConditionTrue),
					framework.NewConditionTypeStatusTuple(v1beta1.DegradedType, corev1.ConditionTrue),
					framework.NewConditionTypeStatusTuple(v1beta1.PodPlacementControllerNotRolledOutType, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.PodPlacementWebhookNotRolledOutType, corev1.ConditionTrue),
					framework.NewConditionTypeStatusTuple(v1beta1.MutatingWebhookConfigurationNotAvailable, corev1.ConditionTrue),
					framework.NewConditionTypeStatusTuple(v1beta1.DeprovisioningType, corev1.ConditionFalse),
				)).Should(Succeed(), "the ClusterPodPlacementConfig should have the correct conditions")
				By("Verify no mutating webhook configuration is available")
				err := k8sClient.Get(ctx, crclient.ObjectKey{
					Name: utils.PodMutatingWebhookConfigurationName,
				}, &admissionv1.MutatingWebhookConfiguration{})
				Expect(err).To(HaveOccurred(), "the mutating webhook configuration should not be available", err)
				Expect(errors.IsNotFound(err)).To(BeTrue(), "the mutating webhook configuration should not be available", err)
			})
		})
		When("at least one replica is available for all components", func() {
			It("should be available, progressing", func() {
				By("Setting the deployment's available replicas to 1, minimum")
				patchDeploymentStatus(utils.PodPlacementControllerName, NewGomegaWithT(GinkgoT()), func(d *appsv1.Deployment) {
					d.Status.AvailableReplicas = 1
					d.Status.UpdatedReplicas = 1
					d.Status.ReadyReplicas = 1
					d.Status.Replicas = 1
				})
				patchDeploymentStatus(utils.PodPlacementWebhookName, NewGomegaWithT(GinkgoT()), func(d *appsv1.Deployment) {
					d.Status.AvailableReplicas = 1
					d.Status.UpdatedReplicas = 1
					d.Status.ReadyReplicas = 1
					d.Status.Replicas = 1
				})
				Eventually(framework.VerifyConditions(ctx, k8sClient,
					framework.NewConditionTypeStatusTuple(v1beta1.AvailableType, corev1.ConditionTrue),
					framework.NewConditionTypeStatusTuple(v1beta1.ProgressingType, corev1.ConditionTrue),
					framework.NewConditionTypeStatusTuple(v1beta1.DegradedType, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.PodPlacementControllerNotRolledOutType, corev1.ConditionTrue),
					framework.NewConditionTypeStatusTuple(v1beta1.PodPlacementWebhookNotRolledOutType, corev1.ConditionTrue),
					framework.NewConditionTypeStatusTuple(v1beta1.MutatingWebhookConfigurationNotAvailable, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.DeprovisioningType, corev1.ConditionFalse),
				)).Should(Succeed(), "the ClusterPodPlacementConfig should have the correct conditions")
				By("Verify the mutating webhook configuration is available")
				err := k8sClient.Get(ctx, crclient.ObjectKey{
					Name: utils.PodMutatingWebhookConfigurationName,
				}, &admissionv1.MutatingWebhookConfiguration{})
				Expect(err).NotTo(HaveOccurred(), "the mutating webhook configuration should be available", err)
			})
		})
		When("the cluster pod placement config is updated", func() {
			It("roll-out the deployment with changes", func() {
				By("Updating the ClusterPodPlacementConfig")
				ppc := &v1beta1.ClusterPodPlacementConfig{}
				err := k8sClient.Get(ctx, crclient.ObjectKey{
					Name: common.SingletonResourceObjectName,
				}, ppc)
				Expect(err).NotTo(HaveOccurred(), "failed to get ClusterPodPlacementConfig", err)
				ppc.Spec.LogVerbosity = common.LogVerbosityLevelTraceAll
				err = k8sClient.Update(ctx, ppc)
				Expect(err).NotTo(HaveOccurred(), "failed to update ClusterPodPlacementConfig", err)
				By("Verifying the deployments generation is different than the observed generation")
				for _, name := range []string{utils.PodPlacementControllerName, utils.PodPlacementWebhookName} {
					Eventually(func(g Gomega) {
						d := appsv1.Deployment{}
						err := k8sClient.Get(ctx, crclient.ObjectKey{
							Name:      name,
							Namespace: utils.Namespace(),
						}, &d)
						g.Expect(err).NotTo(HaveOccurred(), "failed to get deployment "+name, err)
						g.Expect(d.ObjectMeta.Generation).To(Not(Equal(d.Status.ObservedGeneration)), "the deployment's generation should be updated")
					}).Should(Succeed(), "the deployment "+name+" should be updated")
				}
				Eventually(framework.VerifyConditions(ctx, k8sClient,
					framework.NewConditionTypeStatusTuple(v1beta1.AvailableType, corev1.ConditionTrue),
					framework.NewConditionTypeStatusTuple(v1beta1.ProgressingType, corev1.ConditionTrue),
					framework.NewConditionTypeStatusTuple(v1beta1.DegradedType, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.PodPlacementControllerNotRolledOutType, corev1.ConditionTrue),
					framework.NewConditionTypeStatusTuple(v1beta1.PodPlacementWebhookNotRolledOutType, corev1.ConditionTrue),
					framework.NewConditionTypeStatusTuple(v1beta1.MutatingWebhookConfigurationNotAvailable, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.DeprovisioningType, corev1.ConditionFalse),
				)).Should(Succeed(), "the ClusterPodPlacementConfig should have the correct conditions")
			})
			It("Eventually, the deployments roll-out should complete and the conditions back to normal", func() {
				Eventually(framework.VerifyConditions(ctx, k8sClient,
					framework.NewConditionTypeStatusTuple(v1beta1.AvailableType, corev1.ConditionTrue),
					framework.NewConditionTypeStatusTuple(v1beta1.ProgressingType, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.DegradedType, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.PodPlacementControllerNotRolledOutType, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.PodPlacementWebhookNotRolledOutType, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.MutatingWebhookConfigurationNotAvailable, corev1.ConditionFalse),
					framework.NewConditionTypeStatusTuple(v1beta1.DeprovisioningType, corev1.ConditionFalse),
				)).Should(Succeed(), "the ClusterPodPlacementConfig should converge to normal conditions")
			})
		})
		AfterAll(func() {
			err := k8sClient.Delete(ctx, builder.NewClusterPodPlacementConfig().WithName(common.SingletonResourceObjectName).Build())
			Expect(crclient.IgnoreNotFound(err)).NotTo(HaveOccurred(), "failed to delete ClusterPodPlacementConfig", err)
			Eventually(framework.ValidateDeletion(k8sClient, ctx)).Should(Succeed(), "the ClusterPodPlacementConfig should be deleted")
		})
	})
	When("Creating a cluster pod placement config", func() {
		Context("with invalid values in the plugins.nodeAffinityScoring stanza", func() {
			DescribeTable("The request should fail with", func(object *v1beta1.ClusterPodPlacementConfig) {
				By("Ensure no ClusterPodPlacementConfig exists")
				cppc := &v1beta1.ClusterPodPlacementConfig{}
				err := k8sClient.Get(ctx, crclient.ObjectKey{
					Name: common.SingletonResourceObjectName,
				}, cppc)
				Expect(errors.IsNotFound(err)).To(BeTrue(), "The ClusterPodPlacementConfig should not exist")
				// Expect(errors.IsNotFound(err)).To(BeTrue(), "The ClusterPodPlacementConfig should not exist")
				By("Create the ClusterPodPlacementConfig")
				err = k8sClient.Create(ctx, object)
				By(fmt.Sprintf("The error is: %+v", err))
				By("Verify the ClusterPodPlacementConfig is not created")
				Expect(err).To(HaveOccurred(), "The create ClusterPodPlacementConfig should not be accepted")
				By("Verify the error is 'invalid'")
				Expect(errors.IsInvalid(err)).To(BeTrue(), "The invalid ClusterPodPlacementConfig should not be accepted")
			},
				Entry("Negative weight", builder.NewClusterPodPlacementConfig().
					WithName(common.SingletonResourceObjectName).
					WithPlugins().
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, -100).
					Build()),
				Entry("Zero weight", builder.NewClusterPodPlacementConfig().
					WithName(common.SingletonResourceObjectName).
					WithPlugins().
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 0).
					Build()),
				Entry("Excessive weight", builder.NewClusterPodPlacementConfig().
					WithName(common.SingletonResourceObjectName).
					WithPlugins().
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 200).
					Build()),
				Entry("Wrong architecture", builder.NewClusterPodPlacementConfig().
					WithName(common.SingletonResourceObjectName).
					WithPlugins().
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm("Wrong", 200).
					Build()),
				Entry("No terms", builder.NewClusterPodPlacementConfig().
					WithName(common.SingletonResourceObjectName).
					WithPlugins().
					WithNodeAffinityScoring(true).
					Build()),
				Entry("Missing architecture in a term", builder.NewClusterPodPlacementConfig().
					WithName(common.SingletonResourceObjectName).
					WithPlugins().
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm("", 5).
					Build()),
			)
			AfterEach(func() {
				By("Ensure the ClusterPodPlacementConfig is deleted")
				err := k8sClient.Delete(ctx, builder.NewClusterPodPlacementConfig().WithName(common.SingletonResourceObjectName).Build())
				Expect(crclient.IgnoreNotFound(err)).NotTo(HaveOccurred(), "failed to delete ClusterPodPlacementConfig", err)
			})
		})
	})
	Context("is handling the cleanup lifecycle of the ClusterPodPlacementConfig with the ExecFormatErrorMonitor enabled", func() {
		BeforeEach(func() {
			By("Creating the ClusterPodPlacementConfig with ExecFormatErrorMonitor enabled")
			err := k8sClient.Create(ctx, builder.NewClusterPodPlacementConfig().WithName(common.SingletonResourceObjectName).
				WithExecFormatErrorMonitor(true).Build())
			Expect(err).NotTo(HaveOccurred(), "failed to create ClusterPodPlacementConfig", err)
			validateReconcile(framework.MainPlugin, framework.ENoExecPlugin)
		})
		AfterEach(func() {
			By("Deleting the ClusterPodPlacementConfig")
			err := k8sClient.Delete(ctx, builder.NewClusterPodPlacementConfig().WithName(common.SingletonResourceObjectName).Build())
			Expect(err).NotTo(HaveOccurred(), "failed to delete ClusterPodPlacementConfig", err)
			Eventually(framework.ValidateDeletion(k8sClient, ctx, framework.MainPlugin, framework.ENoExecPlugin)).Should(Succeed(), "the ClusterPodPlacementConfig should be deleted")
		})
		It("ensure the finalizers exist", func() {
			By("Verifying the cppc has the correct finalizer")
			cppc := &v1beta1.ClusterPodPlacementConfig{}
			err := k8sClient.Get(ctx, crclient.ObjectKey{
				Name: common.SingletonResourceObjectName,
			}, cppc)
			Expect(err).NotTo(HaveOccurred())
			Expect(cppc.Finalizers).To(ContainElement(utils.ExecFormatErrorFinalizerName))
			By("Verifying the enoexec Deployment has the correct finalizer")
			d := appsv1.Deployment{}
			err = k8sClient.Get(ctx, crclient.ObjectKeyFromObject(&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      utils.EnoexecControllerName,
					Namespace: utils.Namespace(),
				},
			}), &d)
			Expect(err).NotTo(HaveOccurred(), "failed to get deployment "+utils.EnoexecControllerName, err)
			Expect(d.Finalizers).To(ContainElement(utils.ExecFormatErrorFinalizerName))
		})
	})
})

func patchDeploymentStatus(name string, g Gomega, patch func(*appsv1.Deployment)) {
	d := appsv1.Deployment{}
	err := k8sClient.Get(ctx, crclient.ObjectKey{
		Name:      name,
		Namespace: utils.Namespace(),
	}, &d)
	g.Expect(err).NotTo(HaveOccurred(), "failed to get deployment "+name, err)
	patch(&d)
	err = k8sClient.Status().Update(ctx, &d)
	g.Expect(err).NotTo(HaveOccurred(), "failed to update deployment "+name, err)
}

func setDeploymentReady(name string, g Gomega) {
	patchDeploymentStatus(name, g, func(deployment *appsv1.Deployment) {
		// This will simulate the deployment being available for the integration tests, letting the
		// deployment controller to reconcile the deployment
		deployment.Status.AvailableReplicas = *deployment.Spec.Replicas
		deployment.Status.UpdatedReplicas = *deployment.Spec.Replicas
		deployment.Status.ReadyReplicas = *deployment.Spec.Replicas
		deployment.Status.Replicas = *deployment.Spec.Replicas
		deployment.Status.ObservedGeneration = deployment.Generation
	})
}

// validateReconcile
// NOTE: this can be used only in integratoin tests as it changes the status of deployments
func validateReconcile(pluginObjectsSet ...framework.PluginObjectsSet) {
	for _, name := range []string{utils.PodPlacementControllerName, utils.PodPlacementWebhookName} {
		Eventually(func(g Gomega) {
			setDeploymentReady(name, g)
		}).Should(Succeed(), "the deployment "+name+" should be ready")
	}
	Eventually(framework.ValidateCreation(k8sClient, ctx, pluginObjectsSet...)).Should(Succeed(), "the ClusterPodPlacementConfig should be created")
	By("The ClusterPodPlacementConfig is ready")
}
