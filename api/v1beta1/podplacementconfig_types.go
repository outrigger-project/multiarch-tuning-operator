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

package v1beta1

import (
	"fmt"

	"github.com/openshift/multiarch-tuning-operator/api/common"
	"github.com/openshift/multiarch-tuning-operator/api/common/plugins"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// PodPlacementConfigSpec defines the desired state of PodPlacementConfig
type PodPlacementConfigSpec struct {
	// labelSelector selects the pods that the pod placement operand should process according to the other specs provided in the PodPlacementConfig object.
	// of the pods. If left empty, all the pods are considered.
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

	// Plugins defines the configurable plugins for this component.
	// This field is optional and will be omitted from the output if not set.
	// +optional
	Plugins *plugins.LocalPlugins `json:"plugins,omitempty"`

	// Priority defines the priority of the PodPlacementConfig and only accepts values in the range 0-255.
	// This field is optional and will default to 0 if not set.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=255
	Priority uint8 `json:"priority,omitempty"`
}

// PodPlacementConfig defines the configuration for the architecture aware pod placement operand in a given namespace for a subset of its pods based on the provided labelSelector.
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=podplacementconfigs,scope=Namespaced
type PodPlacementConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PodPlacementConfigSpec   `json:"spec,omitempty"`
	Status PodPlacementConfigStatus `json:"status,omitempty"`
}

// PluginsEnabled checks if a specific plugin is enabled.
func (p *PodPlacementConfig) PluginsEnabled(plugin common.Plugin) bool {
	if p.Spec.Plugins != nil {
		return p.Spec.Plugins.PluginEnabled(plugin)
	}
	return false
}

// ValidatePriorityUpdate checks whether the updated Priority value is valid
func (p *PodPlacementConfig) ValidatePriorityUpdate(old *PodPlacementConfig, list runtime.Object) (bool, error) {
	// Assert list type to *PodPlacementConfigList
	ppcList, ok := list.(*PodPlacementConfigList)
	if !ok {
		return false, fmt.Errorf("invalid list type: expected *PodPlacementConfigList")
	}

	// Skip if priority hasn't changed
	if p.Spec.Priority == old.Spec.Priority {
		return true, nil
	}

	// Check for duplicate priority in the list
	for _, item := range ppcList.Items {
		if item.Name != p.Name && item.Spec.Priority == p.Spec.Priority {
			return false, fmt.Errorf("priority %q already used by %q", p.Spec.Priority, item.Name)
		}
	}

	return true, nil
}

// ValidatePriorityNew checks whether the Priority value for a new object is valid
func (p *PodPlacementConfig) ValidatePriorityNew(list runtime.Object) (bool, error) {
	// Assert list type to *PodPlacementConfigList
	ppcList, ok := list.(*PodPlacementConfigList)
	if !ok {
		return false, fmt.Errorf("invalid list type: expected *PodPlacementConfigList")
	}

	// Check for duplicate priority in the list
	for _, item := range ppcList.Items {
		if item.Spec.Priority == p.Spec.Priority {
			return false, fmt.Errorf("priority %q already used by %q", p.Spec.Priority, item.Name)
		}
	}

	return true, nil
}

//+kubebuilder:object:root=true

// PodPlacementConfigList contains a list of PodPlacementConfig
type PodPlacementConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PodPlacementConfig `json:"items"`
}

// PodPlacementConfigStatus defines the observed state of PodPlacementConfig
type PodPlacementConfigStatus struct {
	//TODO: Define the status fields for PodPlacementConfig
}

func init() {
	SchemeBuilder.Register(&PodPlacementConfig{}, &PodPlacementConfigList{})
}
