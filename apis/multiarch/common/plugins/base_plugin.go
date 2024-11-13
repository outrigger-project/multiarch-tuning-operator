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

// Plugins represents the plugins configuration.
type Plugins struct {
	NodeAffinityScoring *NodeAffinityScoring `json:"nodeAffinityScoring,omitempty"`
	// Future plugins can be added here.
}

// IBasePlugin defines a basic interface for plugins.
// +k8s:deepcopy-gen=false
type IBasePlugin interface {
	// Enabled is a required boolean field.
	IsEnabled() bool
	// PluginName returns the name of the plugin.
	Name() string
}

// BasePlugin
type BasePlugin struct {
	// Enabled indicates whether the plugin is enabled.
	// +kubebuilder:"validation:Required"
	Enabled bool `json:"enabled" protobuf:"varint,1,opt,name=enabled" kubebuilder:"validation:Required"`
}

// Name returns the name of the BasePlugin.
func (b *BasePlugin) Name() string {
	return "BasePlugin"
}

// Enabled returns the value of the Enabled field.
func (b *BasePlugin) IsEnabled() bool {
	return b.Enabled
}
