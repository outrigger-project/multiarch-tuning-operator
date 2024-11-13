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

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
)

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

// Score calculates the score of a node based on node affinity and architecture
func (n *NodeAffinityScoring) ArchWeight(ctx context.Context, pod *v1.Pod, node *v1.Node) (int64, error) {
	// Skip scoring if the plugin is disabled
	if !n.args.Enabled {
		return 0, nil
	}

	// Apply weight based on architecture
	for _, platform := range n.args.Platforms {
		// Check if Architecture is non-empty.
		if len(platform.Architecture) == 0 {
			return 0, fmt.Errorf("Architecture cannot be empty")
		}
		// Check if Weight is within range.
		if platform.Weight < 0 || platform.Weight > 100 {
			return 0, fmt.Errorf("Weight must be between 0 and 100")
		}
		// Validate architecture value (simulate Enum validation).
		validArchitectures := map[string]bool{
			"arm64":   true,
			"amd64":   true,
			"ppc64le": true,
			"s390x":   true,
		}
		if !validArchitectures[platform.Architecture] {
			return 0, fmt.Errorf("Invalid architecture: %s", platform.Architecture)
		}
	}
	// If no architecture matches
	return 0, nil
}
