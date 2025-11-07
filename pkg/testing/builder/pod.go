package builder

import (
	"encoding/hex"
	"hash/fnv"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PodBuilder is a builder for v1.Pod objects to be used only in unit tests.
type PodBuilder struct {
	pod *v1.Pod
}

// NewPod returns a new PodBuilder to build v1.Pod objects. It is meant to be used only in unit tests.
func NewPod() *PodBuilder {
	return &PodBuilder{
		pod: &v1.Pod{},
	}
}

func (p *PodBuilder) WithName(name string) *PodBuilder {
	p.pod.Name = name
	return p
}

func (p *PodBuilder) WithImagePullSecrets(imagePullSecrets ...string) *PodBuilder {
	p.pod.Spec.ImagePullSecrets = make([]v1.LocalObjectReference, len(imagePullSecrets))
	for i, secret := range imagePullSecrets {
		p.pod.Spec.ImagePullSecrets[i] = v1.LocalObjectReference{
			Name: secret,
		}
	}
	return p
}

func (p *PodBuilder) WithSchedulingGates(schedulingGates ...string) *PodBuilder {
	p.pod.Spec.SchedulingGates = make([]v1.PodSchedulingGate, len(schedulingGates))
	for i, gate := range schedulingGates {
		p.pod.Spec.SchedulingGates[i] = v1.PodSchedulingGate{
			Name: gate,
		}
	}
	return p
}

func (p *PodBuilder) WithContainersImages(images ...string) *PodBuilder {
	for _, image := range images {
		p.WithContainer(image, v1.PullIfNotPresent)
	}
	return p
}

func (p *PodBuilder) WithContainerImagePullAlways(image string) *PodBuilder {
	return p.WithContainer(image, v1.PullAlways)
}

func (p *PodBuilder) WithContainer(image string, imagePullPolicy v1.PullPolicy) *PodBuilder {
	// compute hash of the image name
	hasher := fnv.New128()
	hasher.Write([]byte(image))
	name := hex.EncodeToString(hasher.Sum(nil))
	if len(name) > 63 {
		name = name[:63]
	}
	p.pod.Spec.Containers = append(p.pod.Spec.Containers, v1.Container{
		Image:           image,
		Name:            name,
		ImagePullPolicy: imagePullPolicy,
	})
	return p
}

func (p *PodBuilder) WithInitContainersImages(images ...string) *PodBuilder {
	p.pod.Spec.InitContainers = make([]v1.Container, len(images))
	for i, image := range images {
		p.pod.Spec.InitContainers[i] = v1.Container{
			Image: image,
		}
	}
	return p
}

// WithAffinity adds the affinity to the pod. If initialAffinity is not nil, it is used as the initial value
// of the pod's affinity. Otherwise, the pod's affinity is initialized to an empty affinity if it is nil.
func (p *PodBuilder) WithAffinity(initialAffinity *v1.Affinity) *PodBuilder {
	if p.pod.Spec.Affinity == nil {
		p.pod.Spec.Affinity = &v1.Affinity{}
	}
	if initialAffinity != nil {
		p.pod.Spec.Affinity = initialAffinity
	}
	return p
}

func (p *PodBuilder) WithNodeAffinity() *PodBuilder {
	p.WithAffinity(nil)
	if p.pod.Spec.Affinity.NodeAffinity == nil {
		p.pod.Spec.Affinity.NodeAffinity = &v1.NodeAffinity{}
	}
	return p
}

func (p *PodBuilder) WithRequiredDuringSchedulingIgnoredDuringExecution() *PodBuilder {
	p.WithNodeAffinity()
	if p.pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		p.pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = &v1.NodeSelector{}
	}
	return p
}

func (p *PodBuilder) WithPreferredDuringSchedulingIgnoredDuringExecution(values ...*v1.PreferredSchedulingTerm) *PodBuilder {
	p.WithNodeAffinity()
	if p.pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution == nil {
		p.pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution = []v1.PreferredSchedulingTerm{}
	}
	for i := range values {
		if values[i] == nil {
			panic("nil value passed to WithPreferredDuringSchedulingIgnoredDuringExecution")
		}
		p.pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution = append(
			p.pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution, *values[i])
	}
	return p
}

func (p *PodBuilder) WithNodeSelectorTermsMatchExpressions(
	nodeSelectorTermsMatchExpressions ...[]v1.NodeSelectorRequirement) *PodBuilder {
	p.WithRequiredDuringSchedulingIgnoredDuringExecution()
	p.pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms = make(
		[]v1.NodeSelectorTerm, len(nodeSelectorTermsMatchExpressions))
	for i, matchExpressions := range nodeSelectorTermsMatchExpressions {
		p.pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[i] =
			v1.NodeSelectorTerm{
				MatchExpressions: matchExpressions,
			}
	}
	return p
}

func (p *PodBuilder) WithNodeSelectors(kv ...string) *PodBuilder {
	if p.pod.Spec.NodeSelector == nil {
		p.pod.Spec.NodeSelector = make(map[string]string)
	}
	if len(kv)%2 != 0 {
		panic("the number of arguments must be even")
	}
	for i := 0; i < len(kv); i += 2 {
		p.pod.Spec.NodeSelector[kv[i]] = kv[i+1]
	}
	return p
}

func (p *PodBuilder) WithOwnerReferences(values ...*metav1.OwnerReference) *PodBuilder {
	for i := range values {
		if values[i] == nil {
			panic("nil value passed to WithOwnerReferences")
		}
		p.pod.OwnerReferences = append(p.pod.OwnerReferences, *values[i])
	}
	return p
}

func (p *PodBuilder) WithGenerateName(name string) *PodBuilder {
	p.pod.GenerateName = name
	return p
}

func (p *PodBuilder) WithNamespace(namespace string) *PodBuilder {
	p.pod.Namespace = namespace
	return p
}

func (p *PodBuilder) WithNodeName(nodeName string) *PodBuilder {
	p.pod.Spec.NodeName = nodeName
	return p
}

func (p *PodBuilder) WithAnnotations(annotations map[string]string) *PodBuilder {
	if p.pod.Annotations == nil {
		p.pod.Annotations = make(map[string]string)
	}
	for key, value := range annotations {
		p.pod.Annotations[key] = value
	}
	return p
}

func (p *PodBuilder) WithLabels(labelsKeyValuesPair ...string) *PodBuilder {
	if p.pod.Labels == nil {
		p.pod.Labels = make(map[string]string)
	}
	if len(labelsKeyValuesPair)%2 != 0 {
		// It's ok to panic as this is only used in unit tests.
		panic("the number of arguments must be even")
	}
	for i := 0; i < len(labelsKeyValuesPair); i += 2 {
		p.pod.Labels[labelsKeyValuesPair[i]] = labelsKeyValuesPair[i+1]
	}
	return p
}

func (p *PodBuilder) WithOwnerReference(or metav1.OwnerReference) *PodBuilder {
	p.pod.OwnerReferences = append(p.pod.OwnerReferences, or)
	return p
}

func (p *PodBuilder) WithContainerStatuses(statuses ...v1.ContainerStatus) *PodBuilder {
	p.pod.Status.ContainerStatuses = statuses
	return p
}

func (p *PodBuilder) Build() *v1.Pod {
	return p.pod
}
