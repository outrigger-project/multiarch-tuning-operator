/*
Copyright 2024 Red Hat, Inc.

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

package common

const (
	// PluginName for NodeAffinityScoring.
	PluginName = "NodeAffinityScoring"
)

// PlatformConfig holds configuration for specific platforms (like architecture, weight)
type PlatformConfig struct {
	Architecture string `json:"architecture,omitempty" protobuf:"bytes,1,rep,name=architecture"`
	Weight       int    `json:"weight,omitempty" protobuf:"bytes,2,rep,name=weight"`
}

// NodeAffinityScoringArgs holds the configuration for the scoring plugin
type NodeAffinityScoringArgs struct {
	Enabled   bool             `json:"enabled,omitempty" protobuf:"bytes,1,rep,name=enabled"`
	Platforms []PlatformConfig `json:"platforms,omitempty" protobuf:"bytes,2,rep,name=platforms"`
}

// NodeAffinityScoring is the plugin that implements the ScorePlugin interface
type NodeAffinityScoring struct {
	args *NodeAffinityScoringArgs
}

// Name returns the name of the plugin used by the scheduling framework
func (n *NodeAffinityScoring) Name() string {
	return PluginName
}
