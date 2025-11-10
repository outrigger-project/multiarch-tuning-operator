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

import "github.com/openshift/multiarch-tuning-operator/api/common"

// +k8s:deepcopy-gen=package

// Plugins represents the plugins configuration.
// +kubebuilder:object:generate=true
type Plugins struct {
	NodeAffinityScoring *NodeAffinityScoring `json:"nodeAffinityScoring,omitempty"`

	ExecFormatErrorMonitor *ExecFormatErrorMonitor `json:"execFormatErrorMonitor,omitempty"`
}

// pluginChecks is a map that associates a plugin name with a function that can
// safely check if that specific plugin is enabled on a Plugins struct.
var pluginChecks = map[common.Plugin]func(p *Plugins) bool{
	common.NodeAffinityScoringPluginName: func(p *Plugins) bool {
		return p.NodeAffinityScoring != nil && p.NodeAffinityScoring.IsEnabled()
	},
	common.ExecFormatErrorMonitorPluginName: func(p *Plugins) bool {
		return p.ExecFormatErrorMonitor != nil && p.ExecFormatErrorMonitor.IsEnabled()
	},
}

// PluginEnabled provides a generic and safe way to check if a specific plugin is enabled.
// It handles the case where the Plugins struct itself is nil.
func (p *Plugins) PluginEnabled(plugin common.Plugin) bool {
	if checkFunc, found := pluginChecks[plugin]; found {
		return checkFunc(p)
	}
	return false
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
