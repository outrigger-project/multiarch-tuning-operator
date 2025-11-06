package builder

import (
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
	v1 "k8s.io/api/core/v1"
)

// PreferredSchedulingTermBuilder is a builder for v1.NodeSelectorTerm objects to be used only in unit tests.
type PreferredSchedulingTermBuilder struct {
	preferredSchedulingTerm *v1.PreferredSchedulingTerm
}

// NewPreferredSchedulingTerm returns a new PreferredSchedulingTermBuilder to build v1.NodeSelectorTerm objects. It is meant to be used only in unit tests.
func NewPreferredSchedulingTerm() *PreferredSchedulingTermBuilder {
	return &PreferredSchedulingTermBuilder{
		preferredSchedulingTerm: &v1.PreferredSchedulingTerm{},
	}
}

func (p *PreferredSchedulingTermBuilder) WithKeyAndValues(weight int, nodeSelectorTerm v1.NodeSelectorTerm) *PreferredSchedulingTermBuilder {
	// Cast to int32 for kubernetes Weight field (valid range is typically 1-100)
	p.preferredSchedulingTerm.Weight = int32(weight) // #nosec G115 -- weight values are controlled in test code
	p.preferredSchedulingTerm.Preference = nodeSelectorTerm
	return p
}

func (p *PreferredSchedulingTermBuilder) WithArchitecture(architecture string) *PreferredSchedulingTermBuilder {
	p.preferredSchedulingTerm.Preference.MatchExpressions = []v1.NodeSelectorRequirement{
		{
			Key:      utils.ArchLabel,
			Operator: v1.NodeSelectorOpIn,
			Values:   []string{architecture},
		},
	}
	return p
}

func (p *PreferredSchedulingTermBuilder) WithCustomKeyValue(key string, value string) *PreferredSchedulingTermBuilder {
	p.preferredSchedulingTerm.Preference.MatchExpressions = []v1.NodeSelectorRequirement{
		{
			Key:      key,
			Operator: v1.NodeSelectorOpIn,
			Values:   []string{value},
		},
	}
	return p
}

func (p *PreferredSchedulingTermBuilder) WithWeight(weight int32) *PreferredSchedulingTermBuilder {
	p.preferredSchedulingTerm.Weight = weight
	return p
}

func (p *PreferredSchedulingTermBuilder) Build() *v1.PreferredSchedulingTerm {
	return p.preferredSchedulingTerm
}
