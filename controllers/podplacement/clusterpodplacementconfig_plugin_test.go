package podplacement

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/common/plugins"
	v1alpha1 "github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1alpha1"
	v1beta1 "github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("ClusterPodPlacementConfig Conversion Tests", func() {
	var (
		ctx context.Context
	)
	BeforeEach(func() {
		ctx = context.TODO()
		err := v1alpha1.AddToScheme(scheme.Scheme)
		Expect(err).NotTo(HaveOccurred())
		err = v1beta1.AddToScheme(scheme.Scheme)
		Expect(err).NotTo(HaveOccurred())
	})
	Context("When a v1beta1 pod placement config is created", func() {
		It("should create a v1beta1 CR omitting the plugins key and successfully convert it to v1alpha1", func() {
			By("Creating a v1beta1 ClusterPodPlacementConfig")
			v1beta1Obj1 := &v1beta1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cppc",
					Namespace: "default",
				},
				Spec: v1beta1.ClusterPodPlacementConfigSpec{
					LogVerbosity: "Normal",
				},
			}
			err := k8sClient.Create(ctx, v1beta1Obj1)
			Expect(err).NotTo(HaveOccurred())
			Eventually(func(g Gomega) {
				v1alpha1Obj1 := &v1alpha1.ClusterPodPlacementConfig{}
				err := k8sClient.Get(ctx, runtimeclient.ObjectKey{Name: "test-cppc", Namespace: "default"}, v1alpha1Obj1)
				g.Expect(err).NotTo(HaveOccurred())
				// Verify the LogVerbosity field
				g.Expect(v1alpha1Obj1.Spec.LogVerbosity).NotTo(Equal("Normal"))
			}, time.Second*10, time.Millisecond*250).Should(Succeed())
			err = k8sClient.Delete(ctx, &v1beta1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cppc",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
	Context("When a v1beta1 ClusterPodPlacementConfig with NodeAffinityScoringPlugin is created", func() {
		It("should convert to v1beta1 and get rid of the NodeAffinityScoringPlugin configuration", func() {
			// Step 1: Create a v1alpha1 ClusterPodPlacementConfig object
			v1beta1Obj2 := &v1beta1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cppc-specs",
					Namespace: "default",
				},
				Spec: v1beta1.ClusterPodPlacementConfigSpec{
					LogVerbosity: "Normal",
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "test"},
					},
					Plugins: plugins.Plugins{
						NodeAffinityScoring: &plugins.NodeAffinityScoring{
							BasePlugin: plugins.BasePlugin{
								Enabled: true,
							},
							Platforms: []plugins.NodeAffinityScoringPlatformTerm{
								{Architecture: "ppc64le", Weight: 50},
							},
						},
					},
				},
			}
			err := k8sClient.Create(ctx, v1beta1Obj2)
			Expect(err).NotTo(HaveOccurred())

			// Step 2: Validate the conversion to v1beta1
			Eventually(func(g Gomega) {
				v1alpha1Obj2 := &v1alpha1.ClusterPodPlacementConfig{}
				err := k8sClient.Get(ctx, runtimeclient.ObjectKey{Name: "test-cppc-specs", Namespace: "default"}, v1alpha1Obj2)
				g.Expect(err).NotTo(HaveOccurred())

				// Verify the LogVerbosity field
				g.Expect(v1alpha1Obj2.Spec.LogVerbosity).NotTo(Equal("Normal"))

				// Verify the NamespaceSelector
				g.Expect(v1alpha1Obj2.Spec.NamespaceSelector.MatchLabels).To(Equal(map[string]string{"env": "test"}))

				// Verify the Plugins field
				g.Expect(v1alpha1Obj2.Spec.Plugins.NodeAffinityScoring).NotTo(BeNil())
				g.Expect(v1alpha1Obj2.Spec.Plugins.NodeAffinityScoring.Enabled).To(BeTrue())
				g.Expect(v1alpha1Obj2.Spec.Plugins.NodeAffinityScoring.Platforms).To(ConsistOf(
					plugins.NodeAffinityScoringPlatformTerm{Architecture: "ppc64le", Weight: 50},
				))
			}, time.Second*10, time.Millisecond*250).Should(Succeed())
			err = k8sClient.Delete(ctx, &v1beta1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cppc-specs",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
	Context("When a v1beta1 ClusterPodPlacementConfig without NodeAffinityScoringPlugin", func() {
		It("Should convert to v1beta1 and get rid of the NodeAffinityScoringPlugin configuration with no specs", func() {
			// Step 1: Create a v1alpha1 ClusterPodPlacementConfig object
			v1beta1Obj3 := &v1beta1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cppc-no-specs",
					Namespace: "default",
				},
				Spec: v1beta1.ClusterPodPlacementConfigSpec{
					LogVerbosity: "Normal",
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "test"},
					},
					Plugins: plugins.Plugins{
						NodeAffinityScoring: &plugins.NodeAffinityScoring{
							Platforms: []plugins.NodeAffinityScoringPlatformTerm{},
						},
					},
				},
			}
			err := k8sClient.Create(ctx, v1beta1Obj3)
			Expect(err).NotTo(HaveOccurred())

			// Step 2: Validate the conversion to v1beta1
			Eventually(func(g Gomega) {
				v1alpha1Obj3 := &v1alpha1.ClusterPodPlacementConfig{}
				err := k8sClient.Get(ctx, runtimeclient.ObjectKey{Name: "test-cppc-no-specs", Namespace: "default"}, v1alpha1Obj3)
				g.Expect(err).NotTo(HaveOccurred())

				// Verify the LogVerbosity field
				g.Expect(v1alpha1Obj3.Spec.LogVerbosity).NotTo(Equal("Normal"))

				// Verify the NamespaceSelector
				g.Expect(v1alpha1Obj3.Spec.NamespaceSelector.MatchLabels).To(Equal(map[string]string{"env": "test"}))

				// Verify the Plugins field
				g.Expect(v1alpha1Obj3.Spec.Plugins.NodeAffinityScoring).NotTo(BeNil())
			}, time.Second*10, time.Millisecond*250).Should(Succeed())
			err = k8sClient.Delete(ctx, &v1beta1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cppc-no-specs",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
	Context("When a v1beta1 ClusterPodPlacementConfig with NodeAffinityScoringPlugin is created without plugins", func() {
		It("should convert to v1beta1 and get rid of the NodeAffinityScoringPlugin configuration without plugins ", func() {
			// Step 1: Create a v1alpha1 ClusterPodPlacementConfig object
			v1beta1Obj4 := &v1beta1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cppc-no-plugin",
					Namespace: "default",
				},
				Spec: v1beta1.ClusterPodPlacementConfigSpec{
					LogVerbosity: "Normal",
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "test"},
					},
					Plugins: plugins.Plugins{
						NodeAffinityScoring: &plugins.NodeAffinityScoring{
							Platforms: []plugins.NodeAffinityScoringPlatformTerm{},
						},
					},
				},
			}
			err := k8sClient.Create(ctx, v1beta1Obj4)
			Expect(err).NotTo(HaveOccurred())

			// Step 2: Validate the conversion to v1beta1
			Eventually(func(g Gomega) {
				v1alpha1Obj4 := &v1alpha1.ClusterPodPlacementConfig{}
				err := k8sClient.Get(ctx, runtimeclient.ObjectKey{Name: "test-cppc-no-plugin", Namespace: "default"}, v1alpha1Obj4)
				g.Expect(err).NotTo(HaveOccurred())

				// Verify the LogVerbosity field
				g.Expect(v1alpha1Obj4.Spec.LogVerbosity).NotTo(Equal("Normal"))

				// Verify the NamespaceSelector
				g.Expect(v1alpha1Obj4.Spec.NamespaceSelector.MatchLabels).To(Equal(map[string]string{"env": "test"}))

				// Verify the Plugins field
				g.Expect(v1alpha1Obj4.Spec.Plugins.NodeAffinityScoring).NotTo(BeNil())
				g.Expect(v1alpha1Obj4.Spec.Plugins.NodeAffinityScoring.BasePlugin).NotTo(BeNil())

			}, time.Second*10, time.Millisecond*250).Should(Succeed())
			err = k8sClient.Delete(ctx, &v1beta1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cppc-no-plugin",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
	Context("When a v1beta1 ClusterPodPlacementConfig with NodeAffinityScoringPlugin is created with enable false", func() {
		It("should convert to v1beta1 and get rid of the NodeAffinityScoringPlugin configuration with enable false", func() {
			// Step 1: Create a v1alpha1 ClusterPodPlacementConfig object
			v1beta1Obj5 := &v1beta1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cppc-enable-false",
					Namespace: "default",
				},
				Spec: v1beta1.ClusterPodPlacementConfigSpec{
					LogVerbosity: "Normal",
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "test"},
					},
					Plugins: plugins.Plugins{
						NodeAffinityScoring: &plugins.NodeAffinityScoring{
							BasePlugin: plugins.BasePlugin{
								Enabled: false,
							},
							Platforms: []plugins.NodeAffinityScoringPlatformTerm{
								{Architecture: "ppc64le", Weight: 50},
							},
						},
					},
				},
			}
			err := k8sClient.Create(ctx, v1beta1Obj5)
			Expect(err).NotTo(HaveOccurred())

			// Step 2: Validate the conversion to v1beta1
			Eventually(func(g Gomega) {
				v1alpha1Obj5 := &v1alpha1.ClusterPodPlacementConfig{}
				err := k8sClient.Get(ctx, runtimeclient.ObjectKey{Name: "test-cppc-enable-false", Namespace: "default"}, v1alpha1Obj5)
				g.Expect(err).NotTo(HaveOccurred())

				// Verify the LogVerbosity field
				g.Expect(v1alpha1Obj5.Spec.LogVerbosity).NotTo(Equal("Normal"))

				// Verify the NamespaceSelector
				g.Expect(v1alpha1Obj5.Spec.NamespaceSelector.MatchLabels).To(Equal(map[string]string{"env": "test"}))

				// Verify the Plugins field
				g.Expect(v1alpha1Obj5.Spec.Plugins.NodeAffinityScoring).NotTo(BeNil())
				g.Expect(v1alpha1Obj5.Spec.Plugins.NodeAffinityScoring.Enabled).To(BeFalse())
				g.Expect(v1alpha1Obj5.Spec.Plugins.NodeAffinityScoring.Platforms).To(ConsistOf(
					plugins.NodeAffinityScoringPlatformTerm{Architecture: "ppc64le", Weight: 50},
				))
			}, time.Second*10, time.Millisecond*250).Should(Succeed())
			err = k8sClient.Delete(ctx, &v1beta1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cppc-enable-false",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
	Context("When a v1beta1 ClusterPodPlacementConfig with NodeAffinityScoringPlugin empty platform is created", func() {
		It("should convert to v1beta1 and get rid of the NodeAffinityScoringPlugin empty platform configuration", func() {
			// Step 1: Create a v1alpha1 ClusterPodPlacementConfig object
			v1beta1Obj6 := &v1beta1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cppc-no-platform",
					Namespace: "default",
				},
				Spec: v1beta1.ClusterPodPlacementConfigSpec{
					LogVerbosity: "Normal",
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "test"},
					},
					Plugins: plugins.Plugins{
						NodeAffinityScoring: &plugins.NodeAffinityScoring{
							BasePlugin: plugins.BasePlugin{
								Enabled: true,
							},
							Platforms: []plugins.NodeAffinityScoringPlatformTerm{},
						},
					},
				},
			}
			err := k8sClient.Create(ctx, v1beta1Obj6)
			Expect(err).NotTo(HaveOccurred())

			// Step 2: Validate the conversion to v1beta1
			Eventually(func(g Gomega) {
				v1alpha1Obj6 := &v1alpha1.ClusterPodPlacementConfig{}
				err := k8sClient.Get(ctx, runtimeclient.ObjectKey{Name: "test-cppc-no-platform", Namespace: "default"}, v1alpha1Obj6)
				g.Expect(err).NotTo(HaveOccurred())

				// Verify the LogVerbosity field
				g.Expect(v1alpha1Obj6.Spec.LogVerbosity).NotTo(Equal("Normal"))

				// Verify the NamespaceSelector
				g.Expect(v1alpha1Obj6.Spec.NamespaceSelector.MatchLabels).To(Equal(map[string]string{"env": "test"}))

				// Verify the Plugins field
				g.Expect(v1alpha1Obj6.Spec.Plugins.NodeAffinityScoring).NotTo(BeNil())
				g.Expect(v1alpha1Obj6.Spec.Plugins.NodeAffinityScoring.Enabled).To(BeTrue())
				g.Expect(v1alpha1Obj6.Spec.Plugins.NodeAffinityScoring.Platforms).NotTo(BeNil())
			}, time.Second*10, time.Millisecond*250).Should(Succeed())
			err = k8sClient.Delete(ctx, &v1beta1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cppc-no-platform",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

var _ = ginkgo.Describe("NodeAffinityScoring Validation", func() {
	tests := []struct {
		name       string
		platforms  []plugins.NodeAffinityScoringPlatformTerm
		shouldFail bool
	}{
		{
			name: "Valid Platforms",
			platforms: []plugins.NodeAffinityScoringPlatformTerm{
				{Architecture: "ppc64le", Weight: 50},
				{Architecture: "amd64", Weight: 30},
			},
			shouldFail: false,
		},
		{
			name: "Invalid Architecture",
			platforms: []plugins.NodeAffinityScoringPlatformTerm{
				{Architecture: "invalid_arch", Weight: 20},
			},
			shouldFail: true,
		},
		{
			name: "Invalid Weight",
			platforms: []plugins.NodeAffinityScoringPlatformTerm{
				{Architecture: "amd64", Weight: -10},
			},
			shouldFail: true,
		},
	}

	for _, tt := range tests {
		testCase := tt
		ginkgo.It(testCase.name, func() {
			scoring := &plugins.NodeAffinityScoring{
				BasePlugin: plugins.BasePlugin{Enabled: true},
				Platforms:  testCase.platforms,
			}

			err := validateNodeAffinityScoring(scoring)
			if testCase.shouldFail {
				gomega.Expect(err).To(gomega.HaveOccurred(), "Expected validation to fail but it passed")
			} else {
				gomega.Expect(err).NotTo(gomega.HaveOccurred(), "Expected validation to pass but it failed")
			}
		})
	}
})

func validateNodeAffinityScoring(scoring *plugins.NodeAffinityScoring) error {
	for _, platform := range scoring.Platforms {
		// Check if Architecture is non-empty.
		if len(platform.Architecture) == 0 {
			return fmt.Errorf("Architecture cannot be empty")
		}
		// Check if Weight is within range.
		if platform.Weight < 0 || platform.Weight > 100 {
			return fmt.Errorf("Weight must be between 0 and 100")
		}
		// Validate architecture value (simulate Enum validation).
		validArchitectures := map[string]bool{
			"arm64":   true,
			"amd64":   true,
			"ppc64le": true,
			"s390x":   true,
		}
		if !validArchitectures[platform.Architecture] {
			return fmt.Errorf("Invalid architecture: %s", platform.Architecture)
		}
	}
	return nil
}
