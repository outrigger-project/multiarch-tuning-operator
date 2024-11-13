/*
Copyright 2025 Red Hat, Inc.

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

const (
	// PluginName for NodeAffinityScoring.
	NodeAffinityScoringPluginName = "NodeAffinityScoring"
)

// NodeAffinityScoring is the plugin that implements the ScorePlugin interface.
type NodeAffinityScoring struct {
	BasePlugin `json:",inline"`

	// Platforms is a required field and must contain at least one entry.
	// +kubebuilder:"validation:Required"
	Platforms []NodeAffinityScoringPlatformTerm `json:"platforms" protobuf:"bytes,2,opt,name=platforms" kubebuilder:"validation:Required"`
}

// PlatformConfig holds configuration for specific platforms, with required fields validated.
type NodeAffinityScoringPlatformTerm struct {
	// Architecture must be a list of non-empty string of arch names.
	// +kubebuilder:"validation:Required"
	// +kubebuilder:validation:Enum=arm64;amd64;ppc64le;s390x
	Architecture string `json:"architecture" protobuf:"bytes,1,rep,name=architecture" kubebuilder:"validation:Required"`

	// Operating system is an optional string field.
	// +optional"
	// Os string `json:"os,omitempty" protobuf:"bytes,2,rep,name=os"`

	// weight associated with matching the corresponding NodeAffinityScoringPlatformTerm,
	// in the range 0-100.
	// +kubebuilder:"validation:Required
	// +kubebuilder:validation:Minimum:=0
	Weight int32 `json:"weight" protobuf:"bytes,3,rep,name=weight" kubebuilder:"validation:Required,validation:Minimum:=0"`
}

// Name returns the name of the NodeAffinityScoringPluginName.
func (b *NodeAffinityScoring) Name() string {
	return NodeAffinityScoringPluginName
}
