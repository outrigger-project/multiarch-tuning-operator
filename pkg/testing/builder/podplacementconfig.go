package builder

import (
	"github.com/openshift/multiarch-tuning-operator/api/common/plugins"
	"github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type PodPlacementConfigBuilder struct {
	*v1beta1.PodPlacementConfig
}

func NewPodPlacementConfig() *PodPlacementConfigBuilder {
	return &PodPlacementConfigBuilder{
		PodPlacementConfig: &v1beta1.PodPlacementConfig{},
	}
}

func (p *PodPlacementConfigBuilder) WithName(name string) *PodPlacementConfigBuilder {
	p.Name = name
	return p
}

func (p *PodPlacementConfigBuilder) WithNamespace(namespace string) *PodPlacementConfigBuilder {
	p.Namespace = namespace
	return p
}

func (p *PodPlacementConfigBuilder) WithNamespaceSelector(labelSelector *v1.LabelSelector) *PodPlacementConfigBuilder {
	p.Spec.LabelSelector = labelSelector
	return p
}

func (p *PodPlacementConfigBuilder) WithLabelSelector(labelSelector *v1.LabelSelector) *PodPlacementConfigBuilder {
	p.Spec.LabelSelector = labelSelector
	return p
}

func (p *PodPlacementConfigBuilder) Build() *v1beta1.PodPlacementConfig {
	return p.PodPlacementConfig
}

func (p *PodPlacementConfigBuilder) WithPlugins() *PodPlacementConfigBuilder {
	if p.Spec.Plugins == nil {
		p.Spec.Plugins = &plugins.LocalPlugins{}
	}
	return p
}

func (p *PodPlacementConfigBuilder) WithNodeAffinityScoring(enabled bool) *PodPlacementConfigBuilder {
	if p.Spec.Plugins == nil {
		p.Spec.Plugins = &plugins.LocalPlugins{}
	}
	if p.Spec.Plugins.NodeAffinityScoring == nil {
		p.Spec.Plugins.NodeAffinityScoring = &plugins.NodeAffinityScoring{}
	}
	p.Spec.Plugins.NodeAffinityScoring.Enabled = enabled
	return p
}

func (p *PodPlacementConfigBuilder) WithNodeAffinityScoringTerm(architecture string, weight int32) *PodPlacementConfigBuilder {
	if p.Spec.Plugins.NodeAffinityScoring == nil {
		p.Spec.Plugins.NodeAffinityScoring = &plugins.NodeAffinityScoring{}
	}
	p.Spec.Plugins.NodeAffinityScoring.Platforms = append(p.Spec.Plugins.NodeAffinityScoring.Platforms, plugins.NodeAffinityScoringPlatformTerm{
		Architecture: architecture,
		Weight:       weight,
	})
	return p
}

func (p *PodPlacementConfigBuilder) WithPriority(priority uint8) *PodPlacementConfigBuilder {
	p.Spec.Priority = priority
	return p
}
