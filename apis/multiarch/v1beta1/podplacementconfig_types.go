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
	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/common/plugins"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PodPlacementConfigSpec defines the desired state of PodPlacementConfig
type PodPlacementConfigSpec struct {
	// labelSelector selects the namespaces where the pod placement operand can process the nodeAffinity
	// of the pods. If left empty, all the namespaces are considered.
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

	// Plugins defines the configurable plugins for this component.
	// This field is optional and will be omitted from the output if not set.
	// +optional
	Plugins *plugins.LocalPlugins `json:"plugins,omitempty"`

	// Priority defines the priority of the pod placement operand and only accepts range 0-255.
	// This field is optional and will be omitted from the output if not set.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=255
	Priority uint8 `json:"priority,omitempty"`
}

// PodPlacementConfig defines the configuration for the architecture aware pod placement operand.
// Users can only deploy a single object named "Namespaced".
// Creating the object enables the operand.
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:resource:path=podplacementconfigs,scope=Namespaced
type PodPlacementConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec PodPlacementConfigSpec `json:"spec,omitempty"`
}

//+kubebuilder:object:root=true

// PodPlacementConfigList contains a list of PodPlacementConfig
type PodPlacementConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PodPlacementConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PodPlacementConfig{}, &PodPlacementConfigList{})
}
