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

// +kubebuilder:object:generate=true
package plugins

const (
	// PluginName for execFormatErrorMonitor.
	execFormatErrorMonitorPluginName = "execFormatErrorMonitor"
)

// ExecFormatErrorMonitor is a plugin that provides Exec Format Errors events reporting and monitoring
type ExecFormatErrorMonitor struct {
	BasePlugin `json:",inline"`
}

// Name returns the name of the execFormatErrorMonitorPluginName.
func (b *ExecFormatErrorMonitor) Name() string {
	return execFormatErrorMonitorPluginName
}
