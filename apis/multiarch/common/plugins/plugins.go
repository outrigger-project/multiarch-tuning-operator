package plugins

import "github.com/openshift/multiarch-tuning-operator/apis/multiarch/common/plugins/nodeaffinityscoring"

// OperatorPlugins represents the base_plugins configuration.
// +k8s:deepcopy-gen=true
type OperatorPlugins struct {
	NodeAffinityScoring *nodeaffinityscoring.NodeAffinityScoring `json:"nodeAffinityScoring,omitempty"`
	// Future plugins can be added here.
}
