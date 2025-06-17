package builder

import (
	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/common"
	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/common/plugins"
	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1beta1"
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

func (p *PodPlacementConfigBuilder) WithNamespaceSelector(labelSelector *v1.LabelSelector) *PodPlacementConfigBuilder {
	p.Spec.NamespaceSelector = labelSelector
	return p
}

func (p *PodPlacementConfigBuilder) WithLogVerbosity(logVerbosity common.LogVerbosityLevel) *PodPlacementConfigBuilder {
	p.Spec.LogVerbosity = logVerbosity
	return p
}

func (p *PodPlacementConfigBuilder) Build() *v1beta1.PodPlacementConfig {
	return p.PodPlacementConfig
}

func (p *PodPlacementConfigBuilder) WithPlugins() *PodPlacementConfigBuilder {
	if p.Spec.Plugins == nil {
		p.Spec.Plugins = &plugins.Plugins{}
	}
	return p
}

func (p *PodPlacementConfigBuilder) WithNodeAffinityScoring(enabled bool) *PodPlacementConfigBuilder {
	if p.Spec.Plugins == nil {
		p.Spec.Plugins = &plugins.Plugins{}
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

func (p *PodPlacementConfigBuilder) WithPriority(priority common.Priority) *PodPlacementConfigBuilder {
	p.Spec.Priority = priority
	return p
}
