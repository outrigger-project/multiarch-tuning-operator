package podplacement

import (
	"context"
	"reflect"
	"sort"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/record"

	. "github.com/onsi/gomega"

	"github.com/openshift/multiarch-tuning-operator/api/common"
	"github.com/openshift/multiarch-tuning-operator/api/common/plugins"
	"github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/internal/controller/podplacement/metrics"
	mmoimage "github.com/openshift/multiarch-tuning-operator/pkg/image"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/image/fake"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"

	. "github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
)

var ctx context.Context

func init() {
	ctx = context.TODO()
}

func TestPod_GetPodImagePullSecrets(t *testing.T) {
	tests := []struct {
		name string
		pod  *v1.Pod
		want []string
	}{
		{
			name: "pod with no imagePullSecrets",
			pod:  NewPod().Build(),
			want: []string{},
		},
		{
			name: "pod with imagePullSecrets",
			pod:  NewPod().WithImagePullSecrets("my-secret").Build(),
			want: []string{"my-secret"},
		},
		{
			name: "pod with empty imagePullSecrets",
			pod:  NewPod().WithImagePullSecrets().Build(),
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := newPod(tt.pod, ctx, nil)
			g := NewGomegaWithT(t)
			g.Expect(pod.getPodImagePullSecrets()).To(Equal(tt.want))
		})
	}
}

func TestPod_HasSchedulingGate(t *testing.T) {
	tests := []struct {
		name string
		pod  *v1.Pod
		want bool
	}{
		{
			name: "pod with no scheduling gates",
			pod:  NewPod().Build(),
			want: false,
		},
		{
			name: "pod with empty scheduling gates",
			pod:  NewPod().WithSchedulingGates().Build(),
			want: false,
		},
		{
			name: "pod with the multiarch-tuning-operator scheduling gate",
			pod:  NewPod().WithSchedulingGates(utils.SchedulingGateName).Build(),
			want: true,
		},
		{
			name: "pod with scheduling gates and NO multiarch-tuning-operator scheduling gate",
			pod:  NewPod().WithSchedulingGates("some-other-scheduling-gate").Build(),
			want: false,
		},
		{
			name: "pod with scheduling gates and the multiarch-tuning-operator scheduling gate",
			pod: NewPod().WithSchedulingGates(
				"some-other-scheduling-gate-bar", utils.SchedulingGateName, "some-other-scheduling-gate-foo").Build(),
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := newPod(tt.pod, ctx, nil)
			g := NewGomegaWithT(t)
			g.Expect(pod.HasSchedulingGate()).To(Equal(tt.want))
		})
	}
}

func TestPod_RemoveSchedulingGate(t *testing.T) {
	tests := []struct {
		name string
		pod  *v1.Pod
		want []v1.PodSchedulingGate
	}{
		{
			name: "pod with no scheduling gates",
			pod:  NewPod().Build(),
			want: nil,
		},
		{
			name: "pod with empty scheduling gates",
			pod:  NewPod().WithSchedulingGates().Build(),
			want: []v1.PodSchedulingGate{},
		},
		{
			name: "pod with the multiarch-tuning-operator scheduling gate",
			pod:  NewPod().WithSchedulingGates(utils.SchedulingGateName).Build(),
			want: []v1.PodSchedulingGate{},
		},
		{
			name: "pod with scheduling gates and NO multiarch-tuning-operator scheduling gate",
			pod:  NewPod().WithSchedulingGates("some-other-scheduling-gate").Build(),
			want: []v1.PodSchedulingGate{
				{
					Name: "some-other-scheduling-gate",
				},
			},
		},
		{
			name: "pod with scheduling gates and the multiarch-tuning-operator scheduling gate",
			pod: NewPod().WithSchedulingGates(
				"some-other-scheduling-gate-bar", utils.SchedulingGateName,
				"some-other-scheduling-gate-foo").Build(),
			want: []v1.PodSchedulingGate{
				{
					Name: "some-other-scheduling-gate-bar",
				},
				{
					Name: "some-other-scheduling-gate-foo",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := newPod(tt.pod, ctx, nil)
			pod.RemoveSchedulingGate()
			g := NewGomegaWithT(t)
			g.Expect(pod.Spec.SchedulingGates).To(Equal(tt.want))
		})
	}
}

func TestPod_imagesNamesSet(t *testing.T) {
	tests := []struct {
		name string
		pod  *v1.Pod
		want sets.Set[containerImage]
	}{
		{
			name: "pod with a single container",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Image: "bar/foo:latest",
						},
					},
				},
			},
			want: sets.New[containerImage](containerImage{
				imageName: "//bar/foo:latest",
				skipCache: false,
			}),
		},
		{
			name: "pod with multiple containers, some with the same image",
			pod:  NewPod().WithContainersImages("bar/foo:latest", "bar/baz:latest", "bar/foo:latest").Build(),
			want: sets.New[containerImage](containerImage{
				imageName: "//bar/foo:latest",
				skipCache: false,
			}, containerImage{
				imageName: "//bar/baz:latest",
				skipCache: false,
			}),
		},
		{
			name: "pod with multiple containers and init containers",
			pod: NewPod().WithInitContainersImages("foo/bar:latest").WithContainersImages(
				"bar/foo:latest", "bar/baz:latest", "bar/foo:latest").Build(),
			want: sets.New[containerImage](
				containerImage{imageName: "//bar/foo:latest"},
				containerImage{imageName: "//bar/baz:latest"},
				containerImage{imageName: "//foo/bar:latest"}),
		},
		{
			name: "pod with multiple containers, init containers, one image with imagePullPolicy Always",
			pod: NewPod().WithInitContainersImages("foo/bar:latest").WithContainersImages(
				"bar/foo:latest", "bar/baz:latest", "bar/foo:latest").
				WithContainerImagePullAlways("foo/pull:always").Build(),
			want: sets.New[containerImage](
				containerImage{imageName: "//bar/foo:latest"},
				containerImage{imageName: "//bar/baz:latest"},
				containerImage{imageName: "//foo/bar:latest"},
				containerImage{imageName: "//foo/pull:always", skipCache: true},
			),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := newPod(tt.pod, ctx, nil)
			g := NewGomegaWithT(t)
			g.Expect(pod.imagesNamesSet()).To(Equal(tt.want))
		})
	}
}

func TestPod_intersectImagesArchitecture(t *testing.T) {
	tests := []struct {
		name string
		pod  *v1.Pod
		// pullSecretDataList is a list of pull secrets in the form of a slice of bytes. It is not used in the unit
		// tests. It is used in the integration tests.
		pullSecretDataList         [][]byte
		wantSupportedArchitectures sets.Set[string]
		wantErr                    bool
	}{
		{
			name:                       "pod with a single container and multi-arch image",
			pod:                        NewPod().WithContainersImages(fake.MultiArchImage).Build(),
			wantSupportedArchitectures: sets.New[string](utils.ArchitectureAmd64, utils.ArchitectureArm64),
		},
		{
			name:                       "pod with a single container and single-arch image",
			pod:                        NewPod().WithContainersImages(fake.SingleArchArm64Image).Build(),
			wantSupportedArchitectures: sets.New[string](utils.ArchitectureArm64),
		},
		{
			name:                       "pod with multiple containers and same image",
			pod:                        NewPod().WithContainersImages(fake.MultiArchImage, fake.MultiArchImage).Build(),
			wantSupportedArchitectures: sets.New[string](utils.ArchitectureAmd64, utils.ArchitectureArm64),
		},
		{
			name:                       "pod with multiple containers, single-arch image and multi-arch image",
			pod:                        NewPod().WithContainersImages(fake.MultiArchImage, fake.SingleArchArm64Image).Build(),
			wantSupportedArchitectures: sets.New[string](utils.ArchitectureArm64),
		},
		{
			name:                       "pod with multiple containers, two multi-arch images",
			pod:                        NewPod().WithContainersImages(fake.MultiArchImage, fake.MultiArchImage2).Build(),
			wantSupportedArchitectures: sets.New[string](utils.ArchitectureAmd64, utils.ArchitectureArm64),
		},
		{
			name:                       "pod with multiple containers, one non-existing image",
			pod:                        NewPod().WithContainersImages(fake.MultiArchImage, "non-existing-image").Build(),
			wantErr:                    true,
			wantSupportedArchitectures: nil,
		},
	}
	metrics.InitPodPlacementControllerMetrics()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imageInspectionCache = fake.FacadeSingleton()
			pod := newPod(tt.pod, ctx, nil)
			gotSupportedArchitectures, err := pod.intersectImagesArchitecture(tt.pullSecretDataList)
			g := NewGomegaWithT(t)
			g.Expect(err).Should(WithTransform(func(err error) bool { return err != nil }, Equal(tt.wantErr)),
				"error expectation failed")
			g.Expect(gotSupportedArchitectures).Should(WithTransform(func(arches []string) sets.Set[string] {
				if arches == nil {
					return nil
				}
				return sets.New[string](arches...)
			}, Equal(tt.wantSupportedArchitectures)),
				"the set in gotSupportedArchitectures is not equal to the expected one")
			imageInspectionCache = mmoimage.FacadeSingleton()
		})
	}
}

func TestPod_getArchitecturePredicate(t *testing.T) {
	tests := []struct {
		name               string
		pod                *v1.Pod
		pullSecretDataList [][]byte
		// Be aware that the values in the want.Values slice must be sorted alphabetically
		want    v1.NodeSelectorRequirement
		wantErr bool
	}{
		{
			name: "pod with several containers using multi-arch images",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Image: fake.MultiArchImage,
						},
					},
					InitContainers: []v1.Container{
						{
							Image: fake.MultiArchImage2,
						},
					},
				},
			},
			want: v1.NodeSelectorRequirement{
				Key:      utils.ArchLabel,
				Operator: v1.NodeSelectorOpIn,
				Values:   []string{utils.ArchitectureAmd64, utils.ArchitectureArm64},
			},
		},
		{
			name: "pod with non-existing image",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Image: fake.MultiArchImage,
						},
						{
							Image: "non-existing-image",
						},
					},
				},
			},
			wantErr: true,
			want:    v1.NodeSelectorRequirement{},
		},
		{
			name: "pod with conflicting architectures",
			pod:  NewPod().WithContainersImages(fake.SingleArchAmd64Image, fake.SingleArchArm64Image).Build(),
			want: v1.NodeSelectorRequirement{
				Key:      utils.NoSupportedArchLabel,
				Operator: v1.NodeSelectorOpExists,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imageInspectionCache = fake.FacadeSingleton()
			pod := newPod(tt.pod, ctx, nil)
			got, err := pod.getArchitecturePredicate(tt.pullSecretDataList)
			g := NewGomegaWithT(t)
			g.Expect(err).Should(WithTransform(func(err error) bool { return err != nil }, Equal(tt.wantErr)),
				"error expectation failed")
			// sort the architectures to make the comparison easier
			sort.Strings(got.Values)
			g.Expect(got).To(Equal(tt.want))
			imageInspectionCache = mmoimage.FacadeSingleton()
		})
	}
}

func TestPod_setArchNodeAffinity(t *testing.T) {
	tests := []struct {
		name string
		pod  *v1.Pod
		want *v1.Pod
	}{
		{
			name: "pod with empty node selector terms",
			pod:  NewPod().WithContainersImages(fake.MultiArchImage).WithNodeSelectorTermsMatchExpressions().Build(),
			want: NewPod().WithContainersImages(fake.MultiArchImage).WithNodeSelectorTermsMatchExpressions(
				[]v1.NodeSelectorRequirement{
					{
						Key:      utils.ArchLabel,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{utils.ArchitectureAmd64, utils.ArchitectureArm64},
					},
				},
			).Build(),
		},
		{
			name: "pod with node selector terms and nil match expressions",
			pod: NewPod().WithContainersImages(fake.SingleArchAmd64Image).WithNodeSelectorTermsMatchExpressions(
				nil).Build(),
			want: NewPod().WithContainersImages(fake.SingleArchAmd64Image).WithNodeSelectorTermsMatchExpressions(
				[]v1.NodeSelectorRequirement{
					{
						Key:      utils.ArchLabel,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{utils.ArchitectureAmd64},
					},
				},
			).Build(),
		},
		{
			name: "pod with node selector terms and empty match expressions",
			pod: NewPod().WithContainersImages(fake.SingleArchArm64Image).WithNodeSelectorTermsMatchExpressions(
				[]v1.NodeSelectorRequirement{}).Build(),
			want: NewPod().WithContainersImages(fake.SingleArchArm64Image).WithNodeSelectorTermsMatchExpressions(
				[]v1.NodeSelectorRequirement{
					{
						Key:      utils.ArchLabel,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{utils.ArchitectureArm64},
					},
				},
			).Build(),
		},
		{
			name: "pod with node selector terms and match expressions",
			pod: NewPod().WithContainersImages(fake.MultiArchImage).WithNodeSelectorTermsMatchExpressions(
				[]v1.NodeSelectorRequirement{
					{
						Key:      "foo",
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{"bar"},
					},
				}, []v1.NodeSelectorRequirement{
					{
						Key:      "baz",
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{"foo"},
					},
				}).Build(),
			want: NewPod().WithContainersImages(fake.MultiArchImage).WithNodeSelectorTermsMatchExpressions(
				[]v1.NodeSelectorRequirement{
					{
						Key:      "foo",
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{"bar"},
					},
					{
						Key:      utils.ArchLabel,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{utils.ArchitectureAmd64, utils.ArchitectureArm64},
					},
				}, []v1.NodeSelectorRequirement{
					{
						Key:      "baz",
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{"foo"},
					},
					{
						Key:      utils.ArchLabel,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{utils.ArchitectureAmd64, utils.ArchitectureArm64},
					},
				},
			).Build(),
		},
		{
			name: "pod with node selector terms and match expressions and an architecture requirement",
			pod: NewPod().WithContainersImages(fake.MultiArchImage).WithNodeSelectorTermsMatchExpressions(
				[]v1.NodeSelectorRequirement{
					{
						Key:      "foo",
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{"bar"},
					},
					{
						Key:      utils.ArchLabel,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{utils.ArchitectureS390x},
					},
				}, []v1.NodeSelectorRequirement{
					{
						Key:      "baz",
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{"foo"},
					},
				}).Build(),
			want: NewPod().WithContainersImages(fake.MultiArchImage).WithNodeSelectorTermsMatchExpressions(
				[]v1.NodeSelectorRequirement{
					{
						Key:      "foo",
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{"bar"},
					},
					{
						Key:      utils.ArchLabel,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{utils.ArchitectureS390x},
					},
				}, []v1.NodeSelectorRequirement{
					{
						Key:      "baz",
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{"foo"},
					}, {
						Key:      utils.ArchLabel,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{utils.ArchitectureAmd64, utils.ArchitectureArm64},
					},
				}).Build(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imageInspectionCache = fake.FacadeSingleton()
			pod := newPod(tt.pod, ctx, nil)
			g := NewGomegaWithT(t)
			pred, err := pod.getArchitecturePredicate(nil)
			g.Expect(err).ShouldNot(HaveOccurred())
			pod.setRequiredArchNodeAffinity(pred)
			g.Expect(pod.Spec.Affinity).Should(Equal(tt.want.Spec.Affinity))
			imageInspectionCache = mmoimage.FacadeSingleton()
		})
	}
}

func TestPod_SetPreferredArchNodeAffinityWithCPPC(t *testing.T) {
	tests := []struct {
		name string
		pod  *v1.Pod
		want *v1.Pod
	}{
		{
			name: "pod with no predefined preferred affinity",
			pod:  NewPod().WithContainersImages(fake.SingleArchAmd64Image).Build(),
			want: NewPod().WithContainersImages(fake.SingleArchAmd64Image).WithPreferredDuringSchedulingIgnoredDuringExecution(
				NewPreferredSchedulingTerm().WithArchitecture(utils.ArchitectureAmd64).WithWeight(1).Build(),
			).Build(),
		},
		{
			name: "pod with predefined preferred node affinity",
			pod: NewPod().WithContainersImages(fake.SingleArchAmd64Image).WithPreferredDuringSchedulingIgnoredDuringExecution(
				NewPreferredSchedulingTerm().WithCustomKeyValue("foo", "bar").WithWeight(50).Build(),
			).Build(),
			want: NewPod().WithContainersImages(fake.SingleArchAmd64Image).WithPreferredDuringSchedulingIgnoredDuringExecution(
				NewPreferredSchedulingTerm().WithCustomKeyValue("foo", "bar").WithWeight(50).Build(),
				NewPreferredSchedulingTerm().WithArchitecture(utils.ArchitectureAmd64).WithWeight(1).Build(),
			).Build(),
		},
		{
			name: "pod with predefined preferred node affinity with arch label set",
			pod: NewPod().WithContainersImages(fake.SingleArchAmd64Image).WithPreferredDuringSchedulingIgnoredDuringExecution(
				NewPreferredSchedulingTerm().WithArchitecture(utils.ArchitectureAmd64).WithWeight(30).Build(),
			).Build(),
			want: NewPod().WithContainersImages(fake.SingleArchAmd64Image).WithPreferredDuringSchedulingIgnoredDuringExecution(
				NewPreferredSchedulingTerm().WithArchitecture(utils.ArchitectureAmd64).WithWeight(30).Build(),
			).Build(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imageInspectionCache = fake.FacadeSingleton()
			pod := newPod(tt.pod, ctx, nil)
			g := NewGomegaWithT(t)
			pod.SetPreferredArchNodeAffinity(
				NewClusterPodPlacementConfig().
					WithName(common.SingletonResourceObjectName).
					WithNodeAffinityScoring(true).
					WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 1).Build())
			g.Expect(pod.Spec.Affinity).Should(Equal(tt.want.Spec.Affinity))
			imageInspectionCache = mmoimage.FacadeSingleton()
		})
	}
}

func TestPod_SetPreferredArchNodeAffinity(t *testing.T) {
	tests := []struct {
		name string
		pod  *v1.Pod
		want *v1.Pod
	}{
		{
			name: "pod with empty preferred node affinity",
			pod:  NewPod().WithContainersImages(fake.MultiArchImage).WithPreferredDuringSchedulingIgnoredDuringExecution().Build(),
			want: NewPod().WithContainersImages(fake.MultiArchImage).WithPreferredDuringSchedulingIgnoredDuringExecution().Build(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imageInspectionCache = fake.FacadeSingleton()
			pod := newPod(tt.pod, ctx, nil)
			g := NewGomegaWithT(t)
			pod.SetPreferredArchNodeAffinity(&v1beta1.ClusterPodPlacementConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Spec: v1beta1.ClusterPodPlacementConfigSpec{
					Plugins: &plugins.Plugins{
						NodeAffinityScoring: &plugins.NodeAffinityScoring{
							BasePlugin: plugins.BasePlugin{
								Enabled: true, // Enable the plugin
							},
							Platforms: []plugins.NodeAffinityScoringPlatformTerm{},
						},
					},
				},
			})
			g.Expect(pod.Spec.Affinity).Should(Equal(tt.want.Spec.Affinity))
			imageInspectionCache = mmoimage.FacadeSingleton()
		})
	}
}

func TestPod_SetNodeAffinityArchRequirement(t *testing.T) {
	tests := []struct {
		name               string
		pullSecretDataList [][]byte
		pod                *v1.Pod
		want               *v1.Pod
		expectErr          bool
	}{
		{
			name: "pod with no node selector terms",
			pod:  NewPod().WithContainersImages(fake.MultiArchImage).WithAffinity(nil).Build(),
			want: NewPod().WithContainersImages(fake.MultiArchImage).WithNodeSelectorTermsMatchExpressions(
				[]v1.NodeSelectorRequirement{
					{
						Key:      utils.ArchLabel,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{utils.ArchitectureAmd64, utils.ArchitectureArm64},
					},
				},
			).Build(),
		},
		{
			name: "pod with node selector and no architecture requirement",
			pod:  NewPod().WithContainersImages(fake.MultiArchImage).WithNodeSelectors("foo", "bar").Build(),
			want: NewPod().WithContainersImages(fake.MultiArchImage).WithNodeSelectors(
				"foo", "bar").WithNodeSelectorTermsMatchExpressions(
				[]v1.NodeSelectorRequirement{
					{
						Key:      utils.ArchLabel,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{utils.ArchitectureAmd64, utils.ArchitectureArm64},
					},
				}).Build(),
		},
		/*{ // This test is not valid anymore after 300e719608271b5c9baa6ecfd845c24c2a71eec8:
		    // We now check the predicates are set earlier in the process.
			name: "pod with node selector and architecture requirement",
			pod: NewPod().WithContainersImages(fake.MultiArchImage).WithNodeSelectors("foo", "bar",
				utils.ArchLabel, utils.ArchitectureArm64).Build(),
			want: NewPod().WithContainersImages(fake.MultiArchImage).WithNodeSelectors("foo", "bar",
				utils.ArchLabel, utils.ArchitectureArm64).Build(),
		},*/
		{
			name: "pod with no affinity",
			pod:  NewPod().WithContainersImages(fake.MultiArchImage).Build(),
			want: NewPod().WithContainersImages(fake.MultiArchImage).WithNodeSelectorTermsMatchExpressions(
				[]v1.NodeSelectorRequirement{
					{
						Key:      utils.ArchLabel,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{utils.ArchitectureAmd64, utils.ArchitectureArm64},
					},
				}).Build(),
		},
		{
			name: "pod with no node affinity",
			pod:  NewPod().WithContainersImages(fake.MultiArchImage).Build(),
			want: NewPod().WithContainersImages(fake.MultiArchImage).WithNodeSelectorTermsMatchExpressions(
				[]v1.NodeSelectorRequirement{
					{
						Key:      utils.ArchLabel,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{utils.ArchitectureAmd64, utils.ArchitectureArm64},
					},
				}).Build(),
		},
		{
			name: "pod with no required during scheduling ignored during execution",
			pod:  NewPod().WithContainersImages(fake.MultiArchImage).WithNodeAffinity().Build(),
			want: NewPod().WithContainersImages(fake.MultiArchImage).WithNodeSelectorTermsMatchExpressions(
				[]v1.NodeSelectorRequirement{
					{
						Key:      utils.ArchLabel,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{utils.ArchitectureAmd64, utils.ArchitectureArm64},
					},
				}).Build(),
		},
		{
			name: "pod with predefined node selector terms in the required during scheduling ignored during execution",
			pod: NewPod().WithContainersImages(fake.MultiArchImage).WithNodeSelectorTermsMatchExpressions([]v1.NodeSelectorRequirement{
				{
					Key:      "foo",
					Operator: v1.NodeSelectorOpIn,
					Values:   []string{"bar"},
				},
			}).Build(),
			want: NewPod().WithContainersImages(fake.MultiArchImage).WithNodeSelectorTermsMatchExpressions(
				[]v1.NodeSelectorRequirement{
					{
						Key:      "foo",
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{"bar"},
					},
					{
						Key:      utils.ArchLabel,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{utils.ArchitectureAmd64, utils.ArchitectureArm64},
					},
				}).Build(),
		},
		{
			name: "other affinity types should not be modified",
			pod: NewPod().WithContainersImages(fake.MultiArchImage).WithAffinity(&v1.Affinity{
				PodAffinity: &v1.PodAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
						{
							TopologyKey: "foo",
						},
					},
				},
				NodeAffinity: &v1.NodeAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []v1.PreferredSchedulingTerm{
						{
							Weight: 1,
							Preference: v1.NodeSelectorTerm{
								MatchExpressions: []v1.NodeSelectorRequirement{
									{
										Key:      "foo",
										Operator: v1.NodeSelectorOpIn,
										Values:   []string{"bar"},
									},
								},
							},
						},
					},
				},
			}).Build(),
			want: NewPod().WithContainersImages(fake.MultiArchImage).WithAffinity(&v1.Affinity{
				PodAffinity: &v1.PodAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
						{
							TopologyKey: "foo",
						},
					},
				},
				NodeAffinity: &v1.NodeAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []v1.PreferredSchedulingTerm{
						{
							Weight: 1,
							Preference: v1.NodeSelectorTerm{
								MatchExpressions: []v1.NodeSelectorRequirement{
									{
										Key:      "foo",
										Operator: v1.NodeSelectorOpIn,
										Values:   []string{"bar"},
									},
								},
							},
						},
					},
				},
			}).WithNodeSelectorTermsMatchExpressions(
				[]v1.NodeSelectorRequirement{
					{
						Key:      utils.ArchLabel,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{utils.ArchitectureAmd64, utils.ArchitectureArm64},
					},
				}).Build(),
		},
		{
			name:      "should not modify the pod if unable to inspect the images",
			pod:       NewPod().WithContainersImages(fake.MultiArchImage, "non-readable-image").Build(),
			want:      NewPod().WithContainersImages(fake.MultiArchImage, "non-readable-image").Build(),
			expectErr: true,
		},
		{
			name: "should prevent the pod from being scheduled when no common architecture is found",
			pod:  NewPod().WithContainersImages(fake.SingleArchAmd64Image, fake.SingleArchArm64Image).Build(),
			want: NewPod().WithContainersImages(fake.SingleArchAmd64Image, fake.SingleArchArm64Image).WithNodeSelectorTermsMatchExpressions(
				[]v1.NodeSelectorRequirement{
					{
						Key:      utils.NoSupportedArchLabel,
						Operator: v1.NodeSelectorOpExists,
					},
				}).Build(),
		},
	}
	metrics.InitPodPlacementControllerMetrics()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imageInspectionCache = fake.FacadeSingleton()
			pod := newPod(tt.pod, ctx, nil)
			_, err := pod.SetNodeAffinityArchRequirement(tt.pullSecretDataList)
			g := NewGomegaWithT(t)
			if tt.expectErr {
				g.Expect(err).Should(HaveOccurred())
			} else {
				g.Expect(err).ShouldNot(HaveOccurred())
			}
			g.Expect(pod.Spec.Affinity).Should(Equal(tt.want.Spec.Affinity))
			imageInspectionCache = mmoimage.FacadeSingleton()
		})
	}
}

// TestEnsureArchitectureLabels checks the ensureArchitectureLabels method to ensure it sets the correct labels based on NodeSelectorRequirement.
func TestEnsureArchitectureLabels(t *testing.T) {
	tests := []struct {
		name           string
		requirement    v1.NodeSelectorRequirement
		expectedLabels map[string]string
	}{
		{
			name: "No Values",
			requirement: v1.NodeSelectorRequirement{
				Values: nil,
			},
			expectedLabels: map[string]string{},
		},
		{
			name: "Zero Values",
			requirement: v1.NodeSelectorRequirement{
				Values: []string{},
			},
			expectedLabels: map[string]string{
				utils.NoSupportedArchLabel: "",
			},
		},
		{
			name: "Single Value",
			requirement: v1.NodeSelectorRequirement{
				Values: []string{utils.ArchitectureAmd64},
			},
			expectedLabels: map[string]string{
				utils.SingleArchLabel:                         "",
				utils.ArchLabelValue(utils.ArchitectureAmd64): "",
			},
		},
		{
			name: "Multiple Values",
			requirement: v1.NodeSelectorRequirement{
				Values: []string{utils.ArchitectureAmd64, utils.ArchitectureArm64},
			},
			expectedLabels: map[string]string{
				utils.MultiArchLabel:                          "",
				utils.ArchLabelValue(utils.ArchitectureAmd64): "",
				utils.ArchLabelValue(utils.ArchitectureArm64): "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := newPod(NewPod().Build(), ctx, nil)

			pod.ensureArchitectureLabels(tt.requirement)

			if len(pod.Labels) != len(tt.expectedLabels) {
				t.Errorf("expected %d labels, got %d", len(tt.expectedLabels), len(pod.Labels))
			}

			for k, v := range tt.expectedLabels {
				if pod.Labels[k] != v {
					t.Errorf("expected label %s to have value %s, got %s", k, v, pod.Labels[k])
				}
			}
		})
	}
}

func TestPod_EnsureSchedulingGate(t *testing.T) {
	tests := []struct {
		name            string
		schedulingGates []v1.PodSchedulingGate
		expectedGates   []v1.PodSchedulingGate
	}{
		{
			name:            "No SchedulingGates",
			schedulingGates: nil,
			expectedGates: []v1.PodSchedulingGate{
				{Name: utils.SchedulingGateName},
			},
		},
		{
			name:            "Empty SchedulingGates",
			schedulingGates: []v1.PodSchedulingGate{},
			expectedGates: []v1.PodSchedulingGate{
				{Name: utils.SchedulingGateName},
			},
		},
		{
			name: "SchedulingGate Already Present",
			schedulingGates: []v1.PodSchedulingGate{
				{Name: utils.SchedulingGateName},
			},
			expectedGates: []v1.PodSchedulingGate{
				{Name: utils.SchedulingGateName},
			},
		},
		{
			name: "Other SchedulingGates Present",
			schedulingGates: []v1.PodSchedulingGate{
				{Name: "other-gate"},
			},
			expectedGates: []v1.PodSchedulingGate{
				{Name: "other-gate"},
				{Name: utils.SchedulingGateName},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var schedulingGates []string
			for _, gate := range test.schedulingGates {
				schedulingGates = append(schedulingGates, gate.Name)
			}

			pod := newPod(NewPod().WithSchedulingGates(schedulingGates...).Build(), ctx, nil)

			pod.ensureSchedulingGate()
			if !reflect.DeepEqual(pod.Spec.SchedulingGates, test.expectedGates) {
				t.Errorf("expected %v, got %v", test.expectedGates, pod.Spec.SchedulingGates)
			}
		})
	}
}

func TestPod_shouldIgnorePod(t *testing.T) {
	type fields struct {
		Pod      *v1.Pod
		ctx      context.Context
		recorder record.EventRecorder
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{
			name: "pod with no node selector terms",
			fields: fields{
				Pod: NewPod().Build(),
			},
			want: false,
		},
		{
			name: "pod in the same namespace as the multiarch-tuning-operator",
			fields: fields{
				Pod: NewPod().WithNamespace(utils.Namespace()).Build(),
			},
			want: true,
		},
		{
			name: "pod in the kube- namespace",
			fields: fields{
				Pod: NewPod().WithNamespace("kube-system").Build(),
			},
			want: true,
		},
		{
			name: "pod with nodeName set",
			fields: fields{
				Pod: NewPod().WithNodeName("node-name").Build(),
			},
			want: true,
		},
		{
			name: "pod with control plane node selector",
			fields: fields{
				Pod: NewPod().WithNodeSelectors(utils.ControlPlaneNodeSelectorLabel, "").Build(),
			},
			want: true,
		},
		{
			name: "pod with DaemonSet ownerReference and Controller is true",
			fields: fields{
				Pod: NewPod().WithOwnerReferences(
					NewOwnerReferenceBuilder().WithKind("DaemonSet").
						WithController(utils.NewPtr(true)).Build()).
					Build(),
			},
			want: true,
		},
		{
			name: "pod with DaemonSet ownerReference but Controller is false",
			fields: fields{
				Pod: NewPod().WithOwnerReferences(
					NewOwnerReferenceBuilder().WithKind("DaemonSet").
						WithController(utils.NewPtr(false)).Build()).
					Build(),
			},
			want: false,
		},
		{
			name: "pod with DaemonSet ownerReference but Controller is nil",
			fields: fields{
				Pod: NewPod().WithOwnerReferences(
					NewOwnerReferenceBuilder().WithKind("DaemonSet").Build()).
					Build(),
			},
			want: false,
		},
		{
			name: "pod with nodeSelector/nodeAffinity and the preferredAffinity is not set for the kubernetes.io/arch label",
			fields: fields{
				Pod: NewPod().WithContainersImages(fake.MultiArchImage).WithNodeSelectorTermsMatchExpressions(
					[]v1.NodeSelectorRequirement{
						{
							Key:      utils.ArchLabel,
							Operator: v1.NodeSelectorOpExists,
							Values:   []string{utils.ArchitectureAmd64},
						},
					},
				).Build(),
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := newPod(tt.fields.Pod, tt.fields.ctx, tt.fields.recorder)
			if got := pod.shouldIgnorePod(&v1beta1.ClusterPodPlacementConfig{}); got != tt.want {
				t.Errorf("shouldIgnorePod() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPod_shouldIgnorePodWithPluginsEnabledInCPPC(t *testing.T) {
	type fields struct {
		Pod      *v1.Pod
		ctx      context.Context
		recorder record.EventRecorder
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{
			name: "pod with nodeSelector/nodeAffinity and the preferredAffinity is set for the kubernetes.io/arch label",
			fields: fields{
				Pod: NewPod().WithContainersImages(fake.SingleArchAmd64Image).WithPreferredDuringSchedulingIgnoredDuringExecution(
					NewPreferredSchedulingTerm().WithArchitecture(utils.ArchitectureAmd64).WithWeight(1).Build()).
					WithNodeSelectorTermsMatchExpressions(
						[]v1.NodeSelectorRequirement{
							{
								Key:      utils.ArchLabel,
								Operator: v1.NodeSelectorOpExists,
								Values:   []string{utils.ArchitectureAmd64},
							},
						},
					).Build(),
			},
			want: true,
		},
		{
			name: "pod with set nodeAffinity, the preferredAffinity is not set for the kubernetes.io/arch label",
			fields: fields{
				Pod: NewPod().WithContainersImages(fake.SingleArchAmd64Image).
					WithNodeSelectorTermsMatchExpressions(
						[]v1.NodeSelectorRequirement{
							{
								Key:      utils.ArchLabel,
								Operator: v1.NodeSelectorOpExists,
								Values:   []string{utils.ArchitectureAmd64},
							},
						},
					).Build(),
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := newPod(tt.fields.Pod, tt.fields.ctx, tt.fields.recorder)
			if got := pod.shouldIgnorePod(NewClusterPodPlacementConfig().
				WithName(common.SingletonResourceObjectName).
				WithNodeAffinityScoring(true).
				WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 1).Build(),
			); got != tt.want {
				t.Errorf("shouldIgnorePod() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPod_shouldIgnorePodWithPluginsDisabledInCPPC(t *testing.T) {
	type fields struct {
		Pod      *v1.Pod
		ctx      context.Context
		recorder record.EventRecorder
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{
			name: "pod with nodeSelector/nodeAffinity is set for the kubernetes.io/arch label and the NodeAffinityScoring plugin is disabled ",
			fields: fields{
				Pod: NewPod().WithContainersImages(fake.MultiArchImage).WithNodeSelectorTermsMatchExpressions(
					[]v1.NodeSelectorRequirement{
						{
							Key:      utils.ArchLabel,
							Operator: v1.NodeSelectorOpExists,
							Values:   []string{utils.ArchitectureAmd64},
						},
					},
				).Build(),
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := newPod(tt.fields.Pod, tt.fields.ctx, tt.fields.recorder)
			if got := pod.shouldIgnorePod(NewClusterPodPlacementConfig().
				WithName(common.SingletonResourceObjectName).
				WithNodeAffinityScoring(false).
				WithNodeAffinityScoringTerm(utils.ArchitectureAmd64, 1).Build()); got != tt.want {
				t.Errorf("shouldIgnorePod() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsPreferredAffinityConfiguredForArchitecture(t *testing.T) {
	tests := []struct {
		name     string
		affinity *v1.Affinity
		expected bool
	}{
		{
			name:     "Affinity is nil",
			affinity: nil,
			expected: false,
		},
		{
			name:     "NodeAffinity is nil",
			affinity: &v1.Affinity{NodeAffinity: nil},
			expected: false,
		},
		{
			name:     "NodeAffinity is nil",
			affinity: &v1.Affinity{NodeAffinity: &v1.NodeAffinity{PreferredDuringSchedulingIgnoredDuringExecution: nil}},
			expected: false,
		},
		{
			name: "PreferredSchedulingTerm contains matchExpression with key=kubernetes.io/arch",
			affinity: &v1.Affinity{
				NodeAffinity: &v1.NodeAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []v1.PreferredSchedulingTerm{
						{
							Weight: int32(1),
							Preference: v1.NodeSelectorTerm{
								MatchExpressions: []v1.NodeSelectorRequirement{
									{
										Key:      utils.ArchLabel,
										Operator: v1.NodeSelectorOpIn,
										Values:   []string{utils.ArchitectureArm64},
									},
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "PreferredSchedulingTerm does not contain a matchExpression with key=kubernetes.io/arch",
			affinity: &v1.Affinity{
				NodeAffinity: &v1.NodeAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []v1.PreferredSchedulingTerm{
						{
							Weight: int32(1),
							Preference: v1.NodeSelectorTerm{
								MatchExpressions: []v1.NodeSelectorRequirement{
									{
										Key:      "foo",
										Operator: v1.NodeSelectorOpIn,
										Values:   []string{"bar"},
									},
								},
							},
						},
					},
				},
			},
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pod := newPod(NewPod().WithAffinity(test.affinity).Build(), ctx, nil)
			result := pod.isPreferredAffinityConfiguredForArchitecture()
			if result != test.expected {
				t.Errorf("expected %v, got %v", test.expected, result)
			}
		})
	}
}

func TestIsNodeSelectorConfiguredForArchitecture(t *testing.T) {
	tests := []struct {
		name         string
		nodeSelector map[string]string
		affinity     *v1.Affinity
		expected     bool
	}{
		{
			name:         "Has NodeSelector for Architecture Label",
			nodeSelector: map[string]string{utils.ArchLabel: utils.ArchitectureAmd64},
			affinity:     nil,
			expected:     true,
		},
		{
			name:         "No NodeSelector and No Affinity",
			nodeSelector: nil,
			affinity:     nil,
			expected:     false,
		},
		{
			name:         "No NodeSelector, Affinity without NodeAffinity",
			nodeSelector: nil,
			affinity:     &v1.Affinity{},
			expected:     false,
		},
		{
			name:         "No NodeSelector, Affinity with empty NodeAffinity",
			nodeSelector: nil,
			affinity:     &v1.Affinity{NodeAffinity: &v1.NodeAffinity{}},
			expected:     false,
		},
		{
			name:         "No NodeSelector, has NodeAffinity with Arch Label",
			nodeSelector: nil,
			affinity: &v1.Affinity{
				NodeAffinity: &v1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
						NodeSelectorTerms: []v1.NodeSelectorTerm{
							{
								MatchExpressions: []v1.NodeSelectorRequirement{
									{Key: utils.ArchLabel, Operator: v1.NodeSelectorOpIn, Values: []string{utils.ArchitectureAmd64}},
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name:         "No NodeSelector, NodeAffinity without Arch Label",
			nodeSelector: nil,
			affinity: &v1.Affinity{
				NodeAffinity: &v1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
						NodeSelectorTerms: []v1.NodeSelectorTerm{
							{
								MatchExpressions: []v1.NodeSelectorRequirement{
									{Key: "some-other-label", Operator: v1.NodeSelectorOpIn, Values: []string{"value"}},
								},
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name:         "No NodeSelector, One NodeSelectorTerm has Arch Label, Others Do Not",
			nodeSelector: nil,
			affinity: &v1.Affinity{
				NodeAffinity: &v1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
						NodeSelectorTerms: []v1.NodeSelectorTerm{
							{
								MatchExpressions: []v1.NodeSelectorRequirement{
									{Key: "some-other-label", Operator: v1.NodeSelectorOpIn, Values: []string{"value"}},
								},
							},
							{
								MatchExpressions: []v1.NodeSelectorRequirement{
									{Key: utils.ArchLabel, Operator: v1.NodeSelectorOpIn, Values: []string{utils.ArchitectureAmd64}},
								},
							},
						},
					},
				},
			},
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var nodeSelectors []string
			for k, v := range test.nodeSelector {
				nodeSelectors = append(nodeSelectors, k, v)
			}
			pod := newPod(NewPod().WithNodeSelectors(nodeSelectors...).WithAffinity(test.affinity).Build(), ctx, nil)

			result := pod.isNodeSelectorConfiguredForArchitecture()
			if result != test.expected {
				t.Errorf("expected %v, got %v", test.expected, result)
			}
		})
	}
}
