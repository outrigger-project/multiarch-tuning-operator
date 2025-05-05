package builder

import (
	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/common"
	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/common/plugins"
	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1beta1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ClusterPodPlacementConfigBuilder struct {
	*v1beta1.ClusterPodPlacementConfig
}

func NewClusterPodPlacementConfig() *ClusterPodPlacementConfigBuilder {
	return &ClusterPodPlacementConfigBuilder{
		ClusterPodPlacementConfig: &v1beta1.ClusterPodPlacementConfig{},
	}
}

func (p *ClusterPodPlacementConfigBuilder) WithName(name string) *ClusterPodPlacementConfigBuilder {
	p.Name = name
	return p
}

func (p *ClusterPodPlacementConfigBuilder) WithNamespaceSelector(labelSelector *v1.LabelSelector) *ClusterPodPlacementConfigBuilder {
	p.Spec.NamespaceSelector = labelSelector
	return p
}

func (p *ClusterPodPlacementConfigBuilder) WithLogVerbosity(logVerbosity common.LogVerbosityLevel) *ClusterPodPlacementConfigBuilder {
	p.Spec.LogVerbosity = logVerbosity
	return p
}

func (p *ClusterPodPlacementConfigBuilder) Build() *v1beta1.ClusterPodPlacementConfig {
	return p.ClusterPodPlacementConfig
}

func (p *ClusterPodPlacementConfigBuilder) WithPlugins() *ClusterPodPlacementConfigBuilder {
	if p.Spec.Plugins == nil {
		p.Spec.Plugins = &plugins.Plugins{}
	}
	return p
}

func (p *ClusterPodPlacementConfigBuilder) WithExecFormatErrorMonitor(enabled bool) *ClusterPodPlacementConfigBuilder {
	if p.Spec.Plugins == nil {
		p.Spec.Plugins = &plugins.Plugins{}
	}
	if p.Spec.Plugins.ExecFormatErrorMonitor == nil {
		p.Spec.Plugins.ExecFormatErrorMonitor = &plugins.ExecFormatErrorMonitor{}
	}
	p.Spec.Plugins.ExecFormatErrorMonitor.Enabled = enabled
	return p
}

func (p *ClusterPodPlacementConfigBuilder) WithNodeAffinityScoring(enabled bool) *ClusterPodPlacementConfigBuilder {
	if p.Spec.Plugins == nil {
		p.Spec.Plugins = &plugins.Plugins{}
	}
	if p.Spec.Plugins.NodeAffinityScoring == nil {
		p.Spec.Plugins.NodeAffinityScoring = &plugins.NodeAffinityScoring{}
	}
	p.Spec.Plugins.NodeAffinityScoring.Enabled = enabled
	return p
}

func (p *ClusterPodPlacementConfigBuilder) WithNodeAffinityScoringTerm(architecture string, weight int32) *ClusterPodPlacementConfigBuilder {
	if p.Spec.Plugins.NodeAffinityScoring == nil {
		p.Spec.Plugins.NodeAffinityScoring = &plugins.NodeAffinityScoring{}
	}
	p.Spec.Plugins.NodeAffinityScoring.Platforms = append(p.Spec.Plugins.NodeAffinityScoring.Platforms, plugins.NodeAffinityScoringPlatformTerm{
		Architecture: architecture,
		Weight:       weight,
	})
	return p
}
