package builder

import (
	"crypto/sha256"
	"encoding/hex"

	v1 "k8s.io/api/core/v1"
)

// PodSpecBuilder is a builder for v1.PodSpec objects to be used only in unit tests.
type PodSpecBuilder struct {
	podspec v1.PodSpec
}

// NewPodSpec returns a new PodSpecBuilder to build v1.PodSpec objects. It is meant to be used only in unit tests.
func NewPodSpec() *PodSpecBuilder {
	return &PodSpecBuilder{
		podspec: v1.PodSpec{},
	}
}

func (ps *PodSpecBuilder) WithImagePullSecrets(imagePullSecrets ...string) *PodSpecBuilder {
	ps.podspec.ImagePullSecrets = make([]v1.LocalObjectReference, len(imagePullSecrets))
	for i, secret := range imagePullSecrets {
		ps.podspec.ImagePullSecrets[i] = v1.LocalObjectReference{
			Name: secret,
		}
	}
	return ps
}

func (ps *PodSpecBuilder) WithSchedulingGates(schedulingGates ...string) *PodSpecBuilder {
	ps.podspec.SchedulingGates = make([]v1.PodSchedulingGate, len(schedulingGates))
	for i, gate := range schedulingGates {
		ps.podspec.SchedulingGates[i] = v1.PodSchedulingGate{
			Name: gate,
		}
	}
	return ps
}

func (ps *PodSpecBuilder) WithContainers(containers ...*v1.Container) *PodSpecBuilder {
	for i := range containers {
		if containers[i] == nil {
			panic("nil value passed to WithContainers")
		}
		ps.podspec.Containers = append(ps.podspec.Containers, *containers[i])
	}
	return ps
}

func (ps *PodSpecBuilder) WithCommand(command ...string) *PodSpecBuilder {
	if len(ps.podspec.Containers) == 0 {
		panic("cannot set command on a podspec with no containers; call WithContainersImages first")
	}
	ps.podspec.Containers[0].Command = command
	return ps
}

func (ps *PodSpecBuilder) WithArgs(args ...string) *PodSpecBuilder {
	if len(ps.podspec.Containers) == 0 {
		panic("cannot set args on a podspec with no containers; call WithContainersImages first")
	}
	ps.podspec.Containers[0].Args = args
	return ps
}

func (ps *PodSpecBuilder) WithRestartPolicy(restartPolicy v1.RestartPolicy) *PodSpecBuilder {
	ps.podspec.RestartPolicy = restartPolicy
	return ps
}

func (ps *PodSpecBuilder) WithContainersImages(images ...string) *PodSpecBuilder {
	ps.podspec.Containers = make([]v1.Container, len(images))

	for i, image := range images {
		// compute hash of the image name using SHA-256
		hasher := sha256.New()
		hasher.Write([]byte(image))

		ps.podspec.Containers[i] = v1.Container{
			Image: image,
			Name:  hex.EncodeToString(hasher.Sum(nil))[:63], // hash of the image name (63 is max)
		}
	}
	return ps
}

func (ps *PodSpecBuilder) WithInitContainersImages(images ...string) *PodSpecBuilder {
	ps.podspec.InitContainers = make([]v1.Container, len(images))
	for i, image := range images {
		ps.podspec.InitContainers[i] = v1.Container{
			Image: image,
		}
	}
	return ps
}

func (ps *PodSpecBuilder) WithNodeName(nodeName string) *PodSpecBuilder {
	ps.podspec.NodeName = nodeName
	return ps
}

func (ps *PodSpecBuilder) WithVolumes(volumes ...*v1.Volume) *PodSpecBuilder {
	for i := range volumes {
		if volumes[i] == nil {
			panic("nil value passed to WithVolumes")
		}
		ps.podspec.Volumes = append(ps.podspec.Volumes, *volumes[i])
	}
	return ps
}

func (ps *PodSpecBuilder) WithServiceAccountName(serviceAccountName string) *PodSpecBuilder {
	ps.podspec.ServiceAccountName = serviceAccountName
	return ps
}

// WithAffinity adds the affinity to the pod. If initialAffinity is not nil, it is used as the initial value
// of the pod's affinity. Otherwise, the pod's affinity is initialized to an empty affinity if it is nil.
func (ps *PodSpecBuilder) WithAffinity(initialAffinity *v1.Affinity) *PodSpecBuilder {
	if ps.podspec.Affinity == nil {
		ps.podspec.Affinity = &v1.Affinity{}
	}
	if initialAffinity != nil {
		ps.podspec.Affinity = initialAffinity
	}
	return ps
}

func (ps *PodSpecBuilder) WithNodeAffinity() *PodSpecBuilder {
	ps.WithAffinity(nil)
	if ps.podspec.Affinity.NodeAffinity == nil {
		ps.podspec.Affinity.NodeAffinity = &v1.NodeAffinity{}
	}
	return ps
}

func (ps *PodSpecBuilder) WithRequiredDuringSchedulingIgnoredDuringExecution() *PodSpecBuilder {
	ps.WithNodeAffinity()
	if ps.podspec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		ps.podspec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = &v1.NodeSelector{}
	}
	return ps
}

func (ps *PodSpecBuilder) WithNodeSelectorTerms(nodeSelectorTerms ...v1.NodeSelectorTerm) *PodSpecBuilder {
	ps.WithRequiredDuringSchedulingIgnoredDuringExecution()
	ps.podspec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms = nodeSelectorTerms
	return ps
}

func (ps *PodSpecBuilder) WithNodeSelectors(entries map[string]string) *PodSpecBuilder {
	if ps.podspec.NodeSelector == nil && len(entries) > 0 {
		ps.podspec.NodeSelector = make(map[string]string, len(entries))
	}
	for k, v := range entries {
		ps.podspec.NodeSelector[k] = v
	}
	return ps
}

func (ps *PodSpecBuilder) WithPreferredNodeAffinities(preferredNodeAffinities ...v1.PreferredSchedulingTerm) *PodSpecBuilder {
	ps.WithNodeAffinity()
	ps.podspec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution = preferredNodeAffinities
	return ps
}

func (ps *PodSpecBuilder) Build() v1.PodSpec {
	return ps.podspec
}
