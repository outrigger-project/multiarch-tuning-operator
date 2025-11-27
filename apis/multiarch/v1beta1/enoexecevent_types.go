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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ENoExecEventSpec defines the desired state of ENoExecEvent
type ENoExecEventSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of the object
}

// ENoExecEventStatus defines the observed state of ENoExecEvent
type ENoExecEventStatus struct {
	// For validating the fields of NodeName and PodName we mimic the functionality of IsDNS1123Subdomain (https://github.com/kubernetes/kubernetes/blob/5be5fd022920e0aa77e29792fffbb5f3690547b3/staging/src/k8s.io/apimachinery/pkg/util/validation/validation.go#L219)
	// For validating the fields of PodNamespace we  mimic the functionality of IsDNS1123Label (https://github.com/kubernetes/kubernetes/blob/5be5fd022920e0aa77e29792fffbb5f3690547b3/staging/src/k8s.io/apimachinery/pkg/util/validation/validation.go#L188)

	// NodeName must follow the RFC 1123 DNS subdomain format.
	// - Max length: 253 characters
	// - Consists of lowercase letters, digits, hyphens (`-`), and dots (`.`)
	// - Must start and end with an alphanumeric character
	// Ref: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#dns-subdomain-names
	//      https://github.com/kubernetes/kubernetes/blob/b4de8bc1b1095d8f465995521a6986e201812342/pkg/apis/core/validation/validation.go#L273
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	NodeName string `json:"nodeName,omitempty"`

	// PodName must follow the RFC 1123 DNS subdomain format:
	// - Max length: 253 characters
	// - Characters: lowercase letters, digits, hyphens (`-`), and dots (`.`)
	// - Must start and end with an alphanumeric character
	// Ref: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#dns-subdomain-names
	//      https://github.com/kubernetes/kubernetes/blob/b4de8bc1b1095d8f465995521a6986e201812342/pkg/apis/core/validation/validation.go#L257
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	PodName string `json:"podName,omitempty"`

	// PodNamespace must follow the RFC 1123 DNS label format.
	// - Max length: 63 characters
	// - Characters: lowercase letters, digits, and hyphens ('-')
	// - Must start and end with an alphanumeric character
	// Ref: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#dns-label-names
	//      https://github.com/kubernetes/kubernetes/blob/5be5fd022920e0aa77e29792fffbb5f3690547b3/staging/src/k8s.io/apimachinery/pkg/api/validation/generic.go#L63
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	PodNamespace string `json:"podNamespace,omitempty"`

	// ContainerID must be a runtime-prefixed 64-character hexadecimal string.
	// Example: containerd://<64-hex-chars>
	// Ref: https://github.com/kubernetes/kubernetes/blob/02eb7d424ad5ccf4f00863fe861f165be0d491da/pkg/apis/core/types.go#L2875
	//      https://github.com/elastic/apm/blob/c7655441bb5f15db5ddbd7f4b60cb0735758d44d/specs/agents/metadata.md?plain=1#L111
	// +kubebuilder:validation:Pattern=`^.+://[a-f0-9]{64}$`
	ContainerID string `json:"containerID,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ENoExecEvent is the Schema for the enoexecevents API
// +kubebuilder:printcolumn:name=NodeName,JSONPath=.status.nodeName,type=string
// +kubebuilder:printcolumn:name=PodName,JSONPath=.status.podName,type=string
// +kubebuilder:printcolumn:name=PodNamespace,JSONPath=.status.podNamespace,type=string
// +kubebuilder:printcolumn:name=ContainerID,JSONPath=.status.containerID,type=string
type ENoExecEvent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ENoExecEventSpec   `json:"spec,omitempty"`
	Status ENoExecEventStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ENoExecEventList contains a list of ENoExecEvent
type ENoExecEventList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ENoExecEvent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ENoExecEvent{}, &ENoExecEventList{})
}
