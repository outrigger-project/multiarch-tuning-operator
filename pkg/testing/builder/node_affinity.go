package builder

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

type NodeAffinityTerm struct {
	Arch   []string
	Weight int32
	Key    string
}

// NodeAffinityBuilder is a utility to build Kubernetes Node objects.
type NodeAffinityBuilder struct {
	nodeAffinity *corev1.NodeAffinity
}

// NewNodeAffinityBuilder creates a new instance of AffinityBuilder.
func NewNodeAffinityBuilder() *NodeAffinityBuilder {
	return &NodeAffinityBuilder{
		nodeAffinity: &corev1.NodeAffinity{},
	}
}

// WithPreferredNodeAffinity create preferred node affinity with wanted arch and weights
func (a *NodeAffinityBuilder) WithPreferredNodeAffinity(terms []NodeAffinityTerm) *NodeAffinityBuilder {
	if a.nodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution == nil {
		a.nodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution = []corev1.PreferredSchedulingTerm{}
	}
	for _, term := range terms {
		if term.Key == "" {
			term.Key = utils.ArchLabel
		}
		preferredSchedulingTerm := corev1.PreferredSchedulingTerm{
			Weight: term.Weight,
			Preference: corev1.NodeSelectorTerm{
				MatchExpressions: []corev1.NodeSelectorRequirement{
					{
						Key:      term.Key,
						Operator: corev1.NodeSelectorOpIn,
						Values:   term.Arch,
					},
				},
			},
		}
		a.nodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution = append(a.nodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution,
			preferredSchedulingTerm)
	}
	return a
}

// Build finalizes and returns the Node object.
func (a *NodeAffinityBuilder) Build() *corev1.NodeAffinity {
	return a.nodeAffinity
}
