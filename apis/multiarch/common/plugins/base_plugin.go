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

// +k8s:deepcopy-gen=package

// LocalPlugins represents the plugins configuration for podplacementconfigs resource.
// +kubebuilder:object:generate=true
type LocalPlugins struct {
	NodeAffinityScoring *NodeAffinityScoring `json:"nodeAffinityScoring,omitempty"`
}

// Plugins represents the plugins configuration for cluster pod placement config.
// +kubebuilder:object:generate=true
type Plugins struct {
	NodeAffinityScoring *NodeAffinityScoring `json:"nodeAffinityScoring,omitempty"`

	ExecFormatErrorMonitor *ExecFormatErrorMonitor `json:"execFormatErrorMonitor,omitempty"`
}

// IsExecFormatErrorMonitorEnabled is a helper function that safely checks if the
// ExecFormatErrorMonitor plugin is configured and enabled.
func (p *Plugins) IsExecFormatErrorMonitorEnabled() bool {
	// This single function contains all the necessary nil checks.
	return p != nil && p.ExecFormatErrorMonitor != nil && p.ExecFormatErrorMonitor.IsEnabled()
}

// IsNodeAffinityScoringEnabled is a helper function that safely checks if the
// NodeAffinityScoring plugin is configured and enabled.
func (p *Plugins) IsNodeAffinityScoringEnabled() bool {
	// This single function contains all the necessary nil checks.
	return p != nil && p.NodeAffinityScoring != nil && p.NodeAffinityScoring.IsEnabled()
}

// IBasePlugin defines a basic interface for plugins.
// +k8s:deepcopy-gen=false
type IBasePlugin interface {
	// Enabled is a required boolean field.
	IsEnabled() bool
	// PluginName returns the name of the plugin.
	Name() string
}

// BasePlugin defines basic structure of a plugin
type BasePlugin struct {
	// Enabled indicates whether the plugin is enabled.
	// +kubebuilder:"validation:Required"
	Enabled bool `json:"enabled" protobuf:"varint,1,opt,name=enabled" kubebuilder:"validation:Required"`
}

// Name returns the name of the BasePlugin.
func (b *BasePlugin) Name() string {
	return "BasePlugin"
}

// IsEnabled returns the value of the Enabled field.
func (b *BasePlugin) IsEnabled() bool {
	return b.Enabled
}
