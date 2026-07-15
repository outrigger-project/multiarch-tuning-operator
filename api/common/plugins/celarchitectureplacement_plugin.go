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

import (
	"fmt"

	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

// +kubebuilder:object:generate=true

const (
	// celArchitecturePlacementPluginName stores the name for the celArchitecturePlacement plugin.
	celArchitecturePlacementPluginName = "celArchitecturePlacement"
)

// CelArchitecturePlacement is a plugin that provides CEL-based architecture selection rules.
// This plugin is only available in namespace-scoped PodPlacementConfig resources.
// When a rule matches, the plugin removes any existing architecture constraints from the pod's
// nodeSelector and nodeAffinity, then sets new architecture constraints based on the rule.
type CelArchitecturePlacement struct {
	BasePlugin `json:",inline"`

	// fallbackArchitectures is a required list of architectures to use when no rules match.
	// This limits the explosion of possible rules by providing a sensible default.
	// When applied, existing architecture constraints are removed and replaced with these architectures.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=4
	FallbackArchitectures []string `json:"fallbackArchitectures" protobuf:"bytes,2,rep,name=fallbackArchitectures"`

	// Rules is a list of architecture selection rules evaluated in order.
	// The first matching rule determines the target architecture.
	// When a rule matches, existing architecture constraints are removed and replaced.
	// Maximum of 1000 rules per configuration to prevent excessive evaluation time during
	// pod admission. This limit balances flexibility with performance, ensuring CEL
	// evaluation completes within acceptable latency bounds (microseconds per expression).
	// The limit may be adjusted based on production usage patterns and performance data.
	// +optional
	// +kubebuilder:validation:MaxItems=1000
	Rules []ArchitectureRule `json:"rules,omitempty" protobuf:"bytes,3,rep,name=rules"`
}

// ArchitectureRule defines a single CEL-based rule for architecture selection
type ArchitectureRule struct {
	// Name is a descriptive name for this rule
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name" protobuf:"bytes,1,opt,name=name"`

	// Expression is a CEL expression that evaluates against a Pod resource.
	// The expression must return a boolean value.
	// The expression has access to the pod via the 'self' variable.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Expression string `json:"expression" protobuf:"bytes,2,opt,name=expression"`

	// Architectures is the list of target architectures to use when this rule matches.
	// When applied, any existing architecture constraints in the pod's nodeSelector
	// and nodeAffinity are removed and replaced with these architectures.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=4
	Architectures []string `json:"architectures" protobuf:"bytes,3,rep,name=architectures"`
}

// Name returns the name of the celArchitecturePlacementPluginName.
func (c *CelArchitecturePlacement) Name() string {
	return celArchitecturePlacementPluginName
}

// ValidateArchitectures checks whether the architectures are valid
func (c *CelArchitecturePlacement) ValidateArchitectures() error {
	validArchs := map[string]bool{
		utils.ArchitectureAmd64:   true,
		utils.ArchitectureArm64:   true,
		utils.ArchitecturePpc64le: true,
		utils.ArchitectureS390x:   true,
	}

	// Validate fallback architectures
	for _, arch := range c.FallbackArchitectures {
		if !validArchs[arch] {
			return fmt.Errorf("invalid default architecture: %s", arch)
		}
	}

	// Validate rule architectures
	for _, rule := range c.Rules {
		for _, arch := range rule.Architectures {
			if !validArchs[arch] {
				return fmt.Errorf("invalid architecture in rule %s: %s", rule.Name, arch)
			}
		}
	}

	return nil
}
