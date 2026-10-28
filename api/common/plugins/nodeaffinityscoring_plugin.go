/*
Copyright 2026 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package plugins

import "fmt"

const (
	// PluginName for NodeAffinityScoring.
	NodeAffinityScoringPluginName = "NodeAffinityScoring"
)

// NodeAffinityScoring is the plugin that implements the ScorePlugin interface.
type NodeAffinityScoring struct {
	// BasePlugin controls whether node affinity scoring is enabled.
	BasePlugin `json:",inline"`

	// Platforms is a required field and must contain at least one entry.
	// +kubebuilder:validation:MinItems=1
	Platforms []NodeAffinityScoringPlatformTerm `json:"platforms" protobuf:"bytes,2,opt,name=platforms"`
}

// ValidateArchitecturesSet checks whether duplicate architectures are set in NodeAffinityScoring
func (n *NodeAffinityScoring) ValidateArchitecturesSet() (bool, error) {
	seen := make(map[string]struct{})
	for _, term := range n.Platforms {
		if _, exists := seen[term.Architecture]; exists {
			return false, fmt.Errorf("duplicate architecture %q found in nodeAffinityScoring.platforms", term.Architecture)
		}

		seen[term.Architecture] = struct{}{}
	}
	return true, nil
}

// NodeAffinityScoringPlatformTerm holds configuration for specific platforms, with required fields validated.
type NodeAffinityScoringPlatformTerm struct {
	// Architecture identifies the node architecture to which the weight applies.
	// When set to arm64, the weight applies to ARM64 nodes.
	// When set to amd64, the weight applies to AMD64 nodes.
	// When set to ppc64le, the weight applies to little-endian PowerPC 64-bit nodes.
	// When set to s390x, the weight applies to IBM Z nodes.
	// +kubebuilder:validation:Enum=arm64;amd64;ppc64le;s390x
	Architecture string `json:"architecture" protobuf:"bytes,1,rep,name=architecture"`

	// weight associated with matching the corresponding NodeAffinityScoringPlatformTerm,
	// in the range 1-100.
	// +kubebuilder:validation:Minimum:=1
	// +kubebuilder:validation:Maximum:=100
	Weight int32 `json:"weight" protobuf:"bytes,3,rep,name=weight"`
}

// Name returns the name of the NodeAffinityScoringPluginName.
func (b *NodeAffinityScoring) Name() string {
	return NodeAffinityScoringPluginName
}
