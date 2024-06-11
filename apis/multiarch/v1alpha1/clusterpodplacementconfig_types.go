/*
Copyright 2023 Red Hat, Inc.

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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterPodPlacementConfigSpec defines the desired state of ClusterPodPlacementConfig
type ClusterPodPlacementConfigSpec struct {
	// LogVerbosity is the log level for the pod placement controller.
	// Valid values are: "Normal", "Debug", "Trace", "TraceAll".
	// Defaults to "Normal".
	// +optional
	// +kubebuilder:default=Normal
	LogVerbosity LogVerbosityLevel `json:"logVerbosity,omitempty"`

	// NamespaceSelector filters the namespaces that the architecture aware pod placement can operate.
	//
	// For example, users can configure an opt-out filter to disallow the operand from operating on namespaces with a given
	// label:
	//
	// {"namespaceSelector":{"matchExpressions":[{"key":"multiarch.openshift.io/exclude-pod-placement","operator":"DoesNotExist"}]}}
	//
	// The operand will set the node affinity requirement in all the pods created in namespaces that do not have
	// the `multiarch.openshift.io/exclude-pod-placement` label.
	//
	// Alternatively, users can configure an opt-in filter to operate only on namespaces with specific labels:
	//
	// {"namespaceSelector":{"matchExpressions":[{"key":"multiarch.openshift.io/include-pod-placement","operator":"Exists"}]}}
	//
	// The operand will set the node affinity requirement in all the pods created in namespace labeled with the key
	// `multiarch.ioenshift.io/include-pod-placement`.
	//
	// See
	// https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/
	// for more examples of label selectors.
	//
	// Default to the empty LabelSelector, which matches everything. Selectors are ANDed.
	// +optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
}

// ClusterPodPlacementConfigStatus defines the observed state of ClusterPodPlacementConfig
type ClusterPodPlacementConfigStatus struct {
	// Conditions represents the latest available observations of a ClusterPodPlacementConfig's current state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ClusterPodPlacementConfig defines the configuration for the PodPlacement operand.
// It is a singleton resource that can consist of an object named cluster.
// Creating this object will trigger the deployment of the architecture aware pod placement operand.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:resource:path=clusterpodplacementconfigs,scope=Cluster
type ClusterPodPlacementConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterPodPlacementConfigSpec   `json:"spec,omitempty"`
	Status ClusterPodPlacementConfigStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ClusterPodPlacementConfigList contains a list of ClusterPodPlacementConfig
type ClusterPodPlacementConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterPodPlacementConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterPodPlacementConfig{}, &ClusterPodPlacementConfigList{})
}
