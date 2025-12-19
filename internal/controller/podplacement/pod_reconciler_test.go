package podplacement

import (
	"fmt"
	"strconv"
	"time"

	"github.com/openshift/multiarch-tuning-operator/pkg/e2e"

	"k8s.io/apimachinery/pkg/util/sets"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/openshift/multiarch-tuning-operator/pkg/utils"

	. "github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	. "github.com/openshift/multiarch-tuning-operator/pkg/testing/framework"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/image/fake/registry"
)

var _ = Describe("Internal/Controller/Podplacement/PodReconciler", func() {
	When("Handling Single-container Pods", func() {
		Context("with different image types", func() {
			DescribeTable("handles correctly", func(imageType string, supportedArchitectures ...string) {
				pod := NewPod().
					WithContainersImages(fmt.Sprintf("%s/%s/%s:latest", registryAddress,
						registry.PublicRepo, registry.ComputeNameByMediaType(imageType))).
					WithGenerateName("test-pod-").
					WithNamespace("test-namespace").
					Build()
				err := k8sClient.Create(ctx, pod)
				Expect(err).NotTo(HaveOccurred(), "failed to create pod", err)
				// Test the removal of the scheduling gate. However, since the pod is mutated and the reconciler
				// removes the scheduling gate concurrently, we cannot ensure that the scheduling gate is added
				// and that the following Eventually works on a pod with the scheduling gate.
				// Waiting for the pod to be mutated is not enough, as the pod could be mutated and the reconciler
				// could have removed the scheduling gate before our check.
				Eventually(func(g Gomega) {
					// Get pod from the API server
					err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get pod", err)
					g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
						Name: utils.SchedulingGateName,
					}), "scheduling gate not removed")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.SchedulingGateLabel, utils.SchedulingGateLabelValueRemoved),
						"scheduling gate annotation not found")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet),
						"preferred node affinity label not found")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet),
						"node affinity label not found")
				}).Should(Succeed(), "failed to remove scheduling gate from pod")
				Eventually(func(g Gomega) {
					g.Expect(*pod).To(HaveEquivalentNodeAffinity(
						&corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      utils.ArchLabel,
												Operator: corev1.NodeSelectorOpIn,
												Values:   supportedArchitectures,
											},
										},
									},
								},
							},
						}), "unexpected node affinity")
				}).Should(Succeed(), "failed to set node affinity to pod")
			},
				Entry("OCI Index Images", imgspecv1.MediaTypeImageIndex, utils.ArchitectureAmd64, utils.ArchitectureArm64),
				Entry("Docker images", imgspecv1.MediaTypeImageManifest, utils.ArchitecturePpc64le),
			)
		})
		Context("with an image that cannot be inspected", func() {
			It("remove the scheduling gate after the max retries count", func() {
				pod := NewPod().
					WithContainersImages("quay.io/non-existing/image:latest").
					WithGenerateName("test-pod-").
					WithNamespace("test-namespace").
					Build()
				err := k8sClient.Create(ctx, pod)
				Expect(err).NotTo(HaveOccurred(), "failed to create pod", err)
				// Test the removal of the scheduling gate. However, since the pod is mutated and the reconciler
				// removes the scheduling gate concurrently, we cannot ensure that the scheduling gate is added
				// and that the following Eventually works on a pod with the scheduling gate.
				// Waiting for the pod to be mutated is not enough, as the pod could be mutated and the reconciler
				// could have removed the scheduling gate before our check.
				Eventually(func(g Gomega) {
					// Get pod from the API server
					err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get pod", err)
					By(fmt.Sprintf("Error count is set to '%s'", pod.Labels[utils.ImageInspectionErrorCountLabel]))
					g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
						Name: utils.SchedulingGateName,
					}), "scheduling gate not removed")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.SchedulingGateLabel, utils.SchedulingGateLabelValueRemoved),
						"scheduling gate annotation not found")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.ImageInspectionErrorCountLabel, strconv.Itoa(MaxRetryCount)), "image inspection error count not found")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet),
						"preferred node affinity label not found")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.NodeAffinityLabel, utils.LabelValueNotSet),
						"node affinity label not found")
				}).WithTimeout(e2e.WaitShort).WithPolling(time.Millisecond*250).Should(Succeed(), "failed to remove scheduling gate from pod")
				// Polling set to 250ms such that the error count is shown in the logs at each update.
			})
		})
		Context("with different pull secrets", func() {
			It("handles images with global pull secrets correctly", func() {
				// TODO: Test logic for handling a Pod with one container and image using global pull secret
			})

			It("handles images with local pull secrets correctly", func() {
				// TODO: Test logic for handling a Pod with one container and image using local pull secret
			})
		})
	})
	When("Reconciling a pod", func() {
		Context("with cache enabled and an image that changes after the first time it is used", func() {
			It("does not query the remote registry until the cache expires", func() {
				imageName := registry.ComputeNameByMediaType(imgspecv1.MediaTypeImageIndex, "custom-image-that-will-change-supported-architectures")
				By("Pushing a custom image to the registry")
				supportedArchitectures := sets.New[string](utils.ArchitectureArm64, utils.ArchitectureAmd64)
				err := registry.PushMockImage(ctx,
					&registry.MockImage{
						Architectures: supportedArchitectures,
						Repository:    registry.PublicRepo,
						Name:          imageName,
						MediaType:     imgspecv1.MediaTypeImageIndex,
						Tag:           "latest",
					})
				Expect(err).NotTo(HaveOccurred(), "failed push custom image to registry, err")
				By("Creating a pod with the custom image [the cache will be populated with info about that image and its supported architectures]")
				pod := NewPod().
					WithContainersImages(fmt.Sprintf("%s/%s/%s:latest", registryAddress,
						registry.PublicRepo, imageName)).
					WithGenerateName("test-pod-").
					WithNamespace("test-namespace").
					Build()
				err = k8sClient.Create(ctx, pod)
				Expect(err).NotTo(HaveOccurred(), "failed to create pod", err)
				By("Waiting for the pod to be mutated and the scheduling gate to be removed")
				Eventually(func(g Gomega) {
					// Get pod from the API server
					err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get pod", err)
					g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
						Name: utils.SchedulingGateName,
					}), "scheduling gate not removed")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.SchedulingGateLabel, utils.SchedulingGateLabelValueRemoved),
						"scheduling gate annotation not found")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet),
						"preferred node affinity label not found")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet),
						"node affinity label not found")
				}).Should(Succeed(), "failed to remove scheduling gate from pod")
				By("Checking that the pod has the correct node affinity")
				Expect(*pod).To(HaveEquivalentNodeAffinity(
					&corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      utils.ArchLabel,
											Operator: corev1.NodeSelectorOpIn,
											Values:   sets.List(supportedArchitectures),
										},
									},
								},
							},
						},
					}), "unexpected node affinity")
				By("Replace the image with a new one having a different set of supported architectures [the cache will not be invalidated]")
				supportedArchitectures = sets.New[string](utils.ArchitectureArm64, utils.ArchitectureAmd64, utils.ArchitecturePpc64le)
				err = registry.PushMockImage(ctx,
					&registry.MockImage{
						Repository:    registry.PublicRepo,
						Name:          imageName,
						Architectures: supportedArchitectures,
						MediaType:     imgspecv1.MediaTypeImageIndex,
						Tag:           "latest",
					})
				Expect(err).NotTo(HaveOccurred(), "failed push custom image to registry, err")
				By("Creating a new pod with the custom image [the cache will be used to set the node affinity]")
				pod = NewPod().
					WithContainersImages(fmt.Sprintf("%s/%s/%s:latest", registryAddress,
						registry.PublicRepo, imageName)).
					WithGenerateName("test-pod-").
					WithNamespace("test-namespace").
					Build()
				err = k8sClient.Create(ctx, pod)
				Expect(err).NotTo(HaveOccurred(), "failed to create pod", err)
				By("Waiting for the pod to be mutated and the scheduling gate to be removed")
				Eventually(func(g Gomega) {
					// Get pod from the API server
					err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get pod", err)
					g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
						Name: utils.SchedulingGateName,
					}), "scheduling gate not removed")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.SchedulingGateLabel, utils.SchedulingGateLabelValueRemoved),
						"scheduling gate annotation not found")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet),
						"preferred node affinity label not found")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet),
						"node affinity label not found")
				}).Should(Succeed(), "failed to remove scheduling gate from pod")
				By("Checking that the pod has the wrong, cached, node affinity [this proves we are not querying the remote registry]")
				Expect(*pod).To(HaveEquivalentNodeAffinity(
					&corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      utils.ArchLabel,
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{utils.ArchitectureArm64, utils.ArchitectureAmd64},
										},
									},
								},
							},
						},
					}), "unexpected node affinity")
			})
			It("always queries the remote registry when the container uses imagePullPolicy 'Always'", func() {
				imageName := registry.ComputeNameByMediaType(imgspecv1.MediaTypeImageIndex, "custom-image-that-will-change-supported-architectures-2")
				By("Pushing a custom image to the registry")
				supportedArchitectures := sets.New[string](utils.ArchitectureArm64, utils.ArchitectureAmd64)
				err := registry.PushMockImage(ctx,
					&registry.MockImage{
						Architectures: supportedArchitectures,
						Repository:    registry.PublicRepo,
						Name:          imageName,
						MediaType:     imgspecv1.MediaTypeImageIndex,
						Tag:           "latest",
					})
				Expect(err).NotTo(HaveOccurred(), "failed push custom image to registry, err")
				By("Creating a pod with the custom image [the cache will be populated with info about that image and its supported architectures]")
				pod := NewPod().
					WithContainerImagePullAlways(fmt.Sprintf("%s/%s/%s:latest", registryAddress,
						registry.PublicRepo, imageName)).
					WithGenerateName("test-pod-").
					WithNamespace("test-namespace").
					Build()
				err = k8sClient.Create(ctx, pod)
				Expect(err).NotTo(HaveOccurred(), "failed to create pod", err)
				By("Waiting for the pod to be mutated and the scheduling gate to be removed")
				Eventually(func(g Gomega) {
					// Get pod from the API server
					err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get pod", err)
					g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
						Name: utils.SchedulingGateName,
					}), "scheduling gate not removed")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.SchedulingGateLabel, utils.SchedulingGateLabelValueRemoved),
						"scheduling gate annotation not found")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet),
						"preferred node affinity label not found")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet),
						"node affinity label not found")
				}).Should(Succeed(), "failed to remove scheduling gate from pod")
				By("Checking that the pod has the correct node affinity")
				Expect(*pod).To(HaveEquivalentNodeAffinity(
					&corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      utils.ArchLabel,
											Operator: corev1.NodeSelectorOpIn,
											Values:   sets.List(supportedArchitectures),
										},
									},
								},
							},
						},
					}), "unexpected node affinity")
				By("Replace the image with a new one having a different set of supported architectures")
				supportedArchitectures = sets.New[string](utils.ArchitectureArm64, utils.ArchitectureAmd64, utils.ArchitecturePpc64le)
				err = registry.PushMockImage(ctx,
					&registry.MockImage{
						Repository:    registry.PublicRepo,
						Name:          imageName,
						Architectures: supportedArchitectures,
						MediaType:     imgspecv1.MediaTypeImageIndex,
						Tag:           "latest",
					})
				Expect(err).NotTo(HaveOccurred(), "failed push custom image to registry, err")
				By("Creating a new pod with the custom image")
				pod = NewPod().
					WithContainerImagePullAlways(fmt.Sprintf("%s/%s/%s:latest", registryAddress,
						registry.PublicRepo, imageName)).
					WithGenerateName("test-pod-").
					WithNamespace("test-namespace").
					Build()
				err = k8sClient.Create(ctx, pod)
				Expect(err).NotTo(HaveOccurred(), "failed to create pod", err)
				By("Waiting for the pod to be mutated and the scheduling gate to be removed")
				Eventually(func(g Gomega) {
					// Get pod from the API server
					err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get pod", err)
					g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
						Name: utils.SchedulingGateName,
					}), "scheduling gate not removed")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.SchedulingGateLabel, utils.SchedulingGateLabelValueRemoved),
						"scheduling gate annotation not found")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet),
						"preferred node affinity label not found")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet),
						"node affinity label not found")
				}).Should(Succeed(), "failed to remove scheduling gate from pod")
				By("Checking that the pod has the wrong, cached, node affinity [this proves we are not querying the remote registry]")
				Expect(*pod).To(HaveEquivalentNodeAffinity(
					&corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      utils.ArchLabel,
											Operator: corev1.NodeSelectorOpIn,
											Values:   sets.List(supportedArchitectures),
										},
									},
								},
							},
						},
					}), "unexpected node affinity")
			})
		})
	})
	When("Handling Multi-container Pods", func() {
		Context("with different image types and different auth credentials sources", func() {
			It("handles node affinity as the intersection of the compatible architectures of each multi-arch image", func() {
				// TODO: Test logic for handling a Pod with multiple multi-arch image-based containers
			})

			It("handles node affinity of multi-arch images and single-arch image setting the only one possible", func() {
				// TODO: Test logic for handling a Pod with multiple multi-arch image-based containers
			})

		})
	})

	When("Handling Pod with Operator Bundle Images", func() {
		Context("with different image types", func() {
			DescribeTable("handles correctly", func(bundleImageType string, secondImageType string, supportedArchitectures ...string) {
				pod := NewPod().
					WithContainersImages(
						fmt.Sprintf("%s/%s/%s:latest", registryAddress,
							registry.PublicRepo, registry.ComputeNameByMediaType(bundleImageType, "bundle")),
						fmt.Sprintf("%s/%s/%s:latest", registryAddress,
							registry.PublicRepo, registry.ComputeNameByMediaType(secondImageType, "ppc64le-s390x"))).
					WithGenerateName("test-pod-").
					WithNamespace("test-namespace").Build()
				err := k8sClient.Create(ctx, pod)
				Expect(err).NotTo(HaveOccurred(), "failed to create pod", err)
				// Test the removal of the scheduling gate. However, since the pod is mutated and the reconciler
				// removes the scheduling gate concurrently, we cannot ensure that the scheduling gate is added
				// and that the following Eventually works on a pod with the scheduling gate.
				// Waiting for the pod to be mutated is not enough, as the pod could be mutated and the reconciler
				// could have removed the scheduling gate before our check.
				Eventually(func(g Gomega) {
					// Get pod from the API server
					err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get pod", err)
					g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
						Name: utils.SchedulingGateName,
					}), "scheduling gate not removed")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.SchedulingGateLabel, utils.SchedulingGateLabelValueRemoved),
						"scheduling gate annotation not found")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet),
						"preferred node affinity label not found")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet),
						"node affinity label not found")
				}).Should(Succeed(), "failed to remove scheduling gate from pod")
				Eventually(func(g Gomega) {
					if len(supportedArchitectures) == 0 {
						g.Expect(pod.Spec.Affinity.NodeAffinity).To(BeNil(), "unexpected node affinity")
						return
					}
					g.Expect(*pod).To(HaveEquivalentNodeAffinity(
						&corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      utils.ArchLabel,
												Operator: corev1.NodeSelectorOpIn,
												Values:   supportedArchitectures,
											},
										},
									},
								},
							},
						}), "unexpected node affinity")
				}).Should(Succeed(), "failed to set node affinity to pod")
			},
				Entry("OCI Index bundles + OCI index image", imgspecv1.MediaTypeImageIndex, imgspecv1.MediaTypeImageIndex, utils.ArchitecturePpc64le, utils.ArchitectureS390x),
				Entry("Docker manifest bundles + OCI index image", imgspecv1.MediaTypeImageManifest, imgspecv1.MediaTypeImageIndex, utils.ArchitecturePpc64le, utils.ArchitectureS390x),
			)
		})
	})
	When("The node affinity scoring plugin is enabled", func() {
		Context("with different types of preferred affinities", func() {
			It("appends ClusterPodPlacementConfig node affinity to nil preferred affinities", func() {
				pod := NewPod().
					WithContainersImages(fmt.Sprintf("%s/%s/%s:latest", registryAddress,
						registry.PublicRepo, registry.ComputeNameByMediaType(imgspecv1.MediaTypeImageManifest))).
					WithGenerateName("test-pod-").
					WithNamespace("test-namespace").
					Build()
				err := k8sClient.Create(ctx, pod)
				Expect(err).NotTo(HaveOccurred(), "failed to create pod", err)
				Eventually(func(g Gomega) {
					// Get pod from the API server
					err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get pod", err)
					g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
						Name: utils.SchedulingGateName,
					}), "scheduling gate not removed")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.SchedulingGateLabel, utils.SchedulingGateLabelValueRemoved),
						"scheduling gate annotation not found")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet),
						"preferred node affinity label not found")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet),
						"node affinity label not found")
				}).Should(Succeed(), "failed to remove scheduling gate from pod")

				Eventually(func(g Gomega) {
					g.Expect(pod.Spec.Affinity).NotTo(BeNil(), "pod.Spec.Affinity should not be nil")
					g.Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil(), "pod.Spec.Affinity.NodeAffinity should not be nil")
					g.Expect(pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil(),
						"PreferredDuringSchedulingIgnoredDuringExecution should not be nil")
					g.Expect(len(pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution)).To(BeNumerically(">", 0),
						"PreferredDuringSchedulingIgnoredDuringExecution should have at least one entry")

					g.Expect(*pod).To(HaveEquivalentPreferredNodeAffinity(
						&corev1.NodeAffinity{
							PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
								{
									Weight: int32(50),
									Preference: corev1.NodeSelectorTerm{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      utils.ArchLabel,
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{utils.ArchitectureArm64},
											},
										},
									},
								},
							},
						}),
						"unexpected preferred node affinity")
				}).Should(Succeed(), "failed to set preferred node affinity in pod")
			})
			It("appends ClusterPodPlacementConfig node affinity to predefined preferred affinities", func() {
				preferredSchedulingTerm := corev1.PreferredSchedulingTerm{
					Weight: int32(20),
					Preference: corev1.NodeSelectorTerm{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "foo",
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{"bar"},
							},
						},
					},
				}
				pod := NewPod().
					WithContainersImages(fmt.Sprintf("%s/%s/%s:latest", registryAddress,
						registry.PublicRepo, registry.ComputeNameByMediaType(imgspecv1.MediaTypeImageManifest))).
					WithGenerateName("test-pod-").
					WithNamespace("test-namespace").
					WithPreferredDuringSchedulingIgnoredDuringExecution(&preferredSchedulingTerm).
					Build()
				err := k8sClient.Create(ctx, pod)
				Expect(err).NotTo(HaveOccurred(), "failed to create pod", err)
				Eventually(func(g Gomega) {
					// Get pod from the API server
					err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get pod")
					g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
						Name: utils.SchedulingGateName,
					}), "scheduling gate not removed")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.SchedulingGateLabel, utils.SchedulingGateLabelValueRemoved),
						"scheduling gate annotation not found")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet),
						"preferred node affinity label not found")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet),
						"node affinity label not found")
				}).Should(Succeed(), "failed to remove scheduling gate from pod")
				Eventually(func(g Gomega) {
					g.Expect(pod.Spec.Affinity).NotTo(BeNil(), "pod.Spec.Affinity should not be nil")
					g.Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil(), "pod.Spec.Affinity.NodeAffinity should not be nil")
					g.Expect(pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil(),
						"PreferredDuringSchedulingIgnoredDuringExecution should not be nil")
					g.Expect(len(pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution)).To(BeNumerically(">", 0),
						"PreferredDuringSchedulingIgnoredDuringExecution should have at least one entry")
					g.Expect(*pod).To(HaveEquivalentPreferredNodeAffinity(
						&corev1.NodeAffinity{
							PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
								{
									Weight: int32(20),
									Preference: corev1.NodeSelectorTerm{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      "foo",
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{"bar"},
											},
										},
									},
								},
								{
									Weight: int32(50),
									Preference: corev1.NodeSelectorTerm{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      utils.ArchLabel,
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{utils.ArchitectureArm64},
											},
										},
									},
								},
							},
						}),
						"unexpected preferred node affinity")
				}).Should(Succeed(), "failed to set preferred node affinity in pod")
			})
			It("skips appending the ClusterPodPlacementConfig node affinity to the predefined preferred affinities", func() {
				preferredSchedulingTerm := corev1.PreferredSchedulingTerm{
					Weight: int32(10),
					Preference: corev1.NodeSelectorTerm{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      utils.ArchLabel,
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{utils.ArchitectureArm64},
							},
						},
					},
				}
				pod := NewPod().
					WithContainersImages(fmt.Sprintf("%s/%s/%s:latest", registryAddress,
						registry.PublicRepo, registry.ComputeNameByMediaType(imgspecv1.MediaTypeImageManifest))).
					WithGenerateName("test-pod-").
					WithNamespace("test-namespace").
					WithPreferredDuringSchedulingIgnoredDuringExecution(&preferredSchedulingTerm).
					Build()
				err := k8sClient.Create(ctx, pod)
				Expect(err).NotTo(HaveOccurred(), "failed to create pod")
				Eventually(func(g Gomega) {
					// Get pod from the API server
					err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get pod", err)
					g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
						Name: utils.SchedulingGateName,
					}), "scheduling gate not removed")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.SchedulingGateLabel, utils.SchedulingGateLabelValueRemoved),
						"scheduling gate annotation not found")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.PreferredNodeAffinityLabel, utils.LabelValueNotSet),
						"preferred node affinity label not found")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet),
						"node affinity label not found")
				}).Should(Succeed(), "failed to remove scheduling gate from pod")
				Eventually(func(g Gomega) {
					g.Expect(pod.Spec.Affinity).NotTo(BeNil(), "pod.Spec.Affinity should not be nil")
					g.Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil(), "pod.Spec.Affinity.NodeAffinity should not be nil")
					g.Expect(pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil(),
						"PreferredDuringSchedulingIgnoredDuringExecution should not be nil")
					g.Expect(len(pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution)).To(BeNumerically(">", 0),
						"PreferredDuringSchedulingIgnoredDuringExecution should have at least one entry")
					g.Expect(*pod).To(HaveEquivalentPreferredNodeAffinity(
						&corev1.NodeAffinity{
							PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
								{
									Weight: int32(10),
									Preference: corev1.NodeSelectorTerm{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      utils.ArchLabel,
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{utils.ArchitectureArm64},
											},
										},
									},
								},
							},
						}),
						"unexpected preferred node affinity")
				}).Should(Succeed(), "failed to set preferred node affinity in pod")
			})
			It("sets the preferred node affinity and label when there is an error setting the required affinity", func() {
				pod := NewPod().
					WithContainersImages("quay.io/non-existing/image:latest").
					WithGenerateName("test-pod-").
					WithNamespace("test-namespace").
					Build()
				err := k8sClient.Create(ctx, pod)
				Expect(err).NotTo(HaveOccurred(), "failed to create pod", err)
				// Test the removal of the scheduling gate. However, since the pod is mutated and the reconciler
				// removes the scheduling gate concurrently, we cannot ensure that the scheduling gate is added
				// and that the following Eventually works on a pod with the scheduling gate.
				// Waiting for the pod to be mutated is not enough, as the pod could be mutated and the reconciler
				// could have removed the scheduling gate before our check.
				Eventually(func(g Gomega) {
					// Get pod from the API server
					err := k8sClient.Get(ctx, crclient.ObjectKeyFromObject(pod), pod)
					g.Expect(err).NotTo(HaveOccurred(), "failed to get pod", err)
					By(fmt.Sprintf("Error count is set to '%s'", pod.Labels[utils.ImageInspectionErrorCountLabel]))
					g.Expect(pod.Spec.SchedulingGates).NotTo(ContainElement(corev1.PodSchedulingGate{
						Name: utils.SchedulingGateName,
					}), "scheduling gate not removed")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.SchedulingGateLabel, utils.SchedulingGateLabelValueRemoved),
						"scheduling gate annotation not found")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.ImageInspectionErrorCountLabel, strconv.Itoa(MaxRetryCount)), "image inspection error count not found")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet),
						"preferred node affinity label not found")
					g.Expect(pod.Labels).To(HaveKeyWithValue(utils.NodeAffinityLabel, utils.LabelValueNotSet),
						"node affinity label not found")
				}).WithTimeout(e2e.WaitShort).Should(Succeed(), "failed to remove scheduling gate from pod")
				// Polling set to 250ms such that the error count is shown in the logs at each update.

			})
		})
	})
})
