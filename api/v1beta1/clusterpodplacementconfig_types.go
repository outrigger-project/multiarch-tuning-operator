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
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/multiarch-tuning-operator/api/common"
	"github.com/openshift/multiarch-tuning-operator/api/common/plugins"
)

// ClusterPodPlacementConfigSpec defines the desired state of ClusterPodPlacementConfig
type ClusterPodPlacementConfigSpec struct {
	// LogVerbosity is the log level for the pod placement components.
	// Valid values are: "Normal", "Debug", "Trace", "TraceAll".
	// Defaults to "Normal".
	// +optional
	// +kubebuilder:default=Normal
	LogVerbosity common.LogVerbosityLevel `json:"logVerbosity,omitempty"`

	// NamespaceSelector selects the namespaces where the pod placement operand can process the nodeAffinity
	// of the pods. If left empty, all the namespaces are considered.
	// The default sample allows to exclude all the namespaces where the
	// label "multiarch.openshift.io/exclude-pod-placement" exists.
	// +optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`

	// Plugins defines the configurable plugins for this component.
	// This field is optional and will be omitted from the output if not set.
	// +optional
	Plugins *plugins.Plugins `json:"plugins,omitempty"`
}

// ClusterPodPlacementConfigStatus defines the observed state of ClusterPodPlacementConfig
type ClusterPodPlacementConfigStatus struct {
	// Conditions represents the latest available observations of a ClusterPodPlacementConfig's current state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// The following fields are used to derive the conditions. They are not exposed to the user.
	available                                bool `json:"-"`
	progressing                              bool `json:"-"`
	degraded                                 bool `json:"-"`
	deprovisioning                           bool `json:"-"`
	podPlacementControllerNotReady           bool `json:"-"`
	podPlacementWebhookNotReady              bool `json:"-"`
	mutatingWebhookConfigurationNotAvailable bool `json:"-"`
	canDeployMutatingWebhook                 bool `json:"-"`
}

func (s *ClusterPodPlacementConfigStatus) IsReady() bool {
	return s.available
}

func (s *ClusterPodPlacementConfigStatus) IsProgressing() bool {
	return s.progressing
}

func (s *ClusterPodPlacementConfigStatus) IsDegraded() bool {
	return s.degraded
}

func (s *ClusterPodPlacementConfigStatus) IsDeprovisioning() bool {
	return s.deprovisioning
}

func (s *ClusterPodPlacementConfigStatus) IsPodPlacementControllerNotReady() bool {
	return s.podPlacementControllerNotReady
}

func (s *ClusterPodPlacementConfigStatus) IsPodPlacementWebhookNotReady() bool {
	return s.podPlacementWebhookNotReady
}

func (s *ClusterPodPlacementConfigStatus) IsMutatingWebhookConfigurationNotAvailable() bool {
	return s.mutatingWebhookConfigurationNotAvailable
}

func (s *ClusterPodPlacementConfigStatus) CanDeployMutatingWebhook() bool {
	return s.canDeployMutatingWebhook
}

// Build sets the conditions in the ClusterPodPlacementConfig object.
// The build Conditions are:
//   - Degraded: if some components are not available (no replicas) and the object is not deprovisioning
//   - Deprovisioning: if the object is being deleted
//   - MutatingWebhookConfigurationNotAvailable: if the mutating webhook configuration does not exist
//   - PodPlacementControllerNotReady: if the pod placement controller is not available or up-to-date
//   - PodPlacementWebhookNotReady: if the pod placement webhook is not available or up-to-date
//   - Progressing: if the object is not deprovisioning and some of the components are not up-to-date.
//   - Available: if all the components are available to serve the requests and reconcile node affinities (at least one replica).
func (s *ClusterPodPlacementConfigStatus) Build(
	podPlacementControllerAvailable, podPlacementWebhookAvailable,
	podPlacementControllerUpToDate, podPlacementWebhookUpToDate,
	mutatingWebhookConfigurationAvailable,
	deprovisioning bool) {
	s.deprovisioning = deprovisioning
	// tracks existence of the mutating webhook configuration
	s.mutatingWebhookConfigurationNotAvailable = !mutatingWebhookConfigurationAvailable
	// tracks the availability of the pod placement controller and webhook and if they are up to date
	s.podPlacementControllerNotReady = !podPlacementControllerAvailable || !podPlacementControllerUpToDate
	s.podPlacementWebhookNotReady = !podPlacementWebhookAvailable || !podPlacementWebhookUpToDate
	// if all the components exist and have at least one replica ready
	s.available = mutatingWebhookConfigurationAvailable && podPlacementWebhookAvailable && podPlacementControllerAvailable
	// if some components are not available (no replicas)
	s.degraded = !s.available && !s.deprovisioning // degraded will not track deprovisioning
	// allow the deployment of the mutating webhook configuration if the pod placement controller and webhook are available
	// (at least one replica)
	s.canDeployMutatingWebhook = podPlacementWebhookAvailable && podPlacementControllerAvailable && !s.deprovisioning
	s.progressing = (!podPlacementControllerUpToDate || !podPlacementWebhookUpToDate || !mutatingWebhookConfigurationAvailable) && !s.deprovisioning
	s.buildConditions()
}

func (s *ClusterPodPlacementConfigStatus) buildConditions() {
	if s.Conditions == nil {
		s.Conditions = []metav1.Condition{}
	}
	reason := ""
	if s.podPlacementControllerNotReady {
		reason += PodPlacementControllerNotRolledOutType
	}
	if s.podPlacementWebhookNotReady {
		reason += PodPlacementWebhookNotRolledOutType
	}
	if s.mutatingWebhookConfigurationNotAvailable {
		reason += MutatingWebhookConfigurationNotAvailable
	}
	if reason == "" {
		reason = AllComponentsReady
	}
	v1helpers.SetCondition(&s.Conditions, metav1.Condition{
		Type:    AvailableType,
		Status:  conditionFromBool(s.available),
		Reason:  reason,
		Message: fmt.Sprintf(ReadyMsg, notFromBool(s.available), strings.TrimSpace(notFromBool(s.available))),
	})
	v1helpers.SetCondition(&s.Conditions, metav1.Condition{
		Type:    ProgressingType,
		Status:  conditionFromBool(s.progressing),
		Reason:  reason,
		Message: fmt.Sprintf(ProgressingMsg, notFromBool(s.progressing)),
	})
	v1helpers.SetCondition(&s.Conditions, metav1.Condition{
		Type:    DegradedType,
		Status:  conditionFromBool(s.degraded),
		Reason:  fmt.Sprintf("%s%s", trimAndCapitalize(notFromBool(s.degraded)), DegradedType),
		Message: fmt.Sprintf(DegradedMsg, notFromBool(s.degraded)),
	})
	deprovisinoingMessagePostfix := ""
	if s.deprovisioning {
		deprovisinoingMessagePostfix = PendingDeprovisioningMsg
	}
	v1helpers.SetCondition(&s.Conditions, metav1.Condition{
		Type:    DeprovisioningType,
		Status:  conditionFromBool(s.deprovisioning),
		Reason:  fmt.Sprintf("%s%s", trimAndCapitalize(notFromBool(s.deprovisioning)), DeprovisioningType),
		Message: fmt.Sprintf(DeprovisioningMsg, notFromBool(s.deprovisioning), deprovisinoingMessagePostfix),
	})
	v1helpers.SetCondition(&s.Conditions, metav1.Condition{
		Type:    PodPlacementControllerNotRolledOutType,
		Status:  conditionFromBool(s.podPlacementControllerNotReady),
		Reason:  fmt.Sprintf("PodPlacementController%sReady", trimAndCapitalize(notFromBool(!s.podPlacementControllerNotReady))),
		Message: fmt.Sprintf(PodPlacementControllerRolledOutMsg, notFromBool(!s.podPlacementControllerNotReady)),
	})
	v1helpers.SetCondition(&s.Conditions, metav1.Condition{
		Type:    PodPlacementWebhookNotRolledOutType,
		Status:  conditionFromBool(s.podPlacementWebhookNotReady),
		Reason:  fmt.Sprintf("PodPlacementWebhook%sReady", trimAndCapitalize(notFromBool(!s.podPlacementWebhookNotReady))),
		Message: fmt.Sprintf(PodPlacementWebhookRolledOutMsg, notFromBool(!s.podPlacementWebhookNotReady)),
	})
	v1helpers.SetCondition(&s.Conditions, metav1.Condition{
		Type:    MutatingWebhookConfigurationNotAvailable,
		Status:  conditionFromBool(s.mutatingWebhookConfigurationNotAvailable),
		Reason:  reason,
		Message: fmt.Sprintf(MutatingWebhookConfigurationReadyMsg, notFromBool(!s.mutatingWebhookConfigurationNotAvailable)),
	})
}

// ClusterPodPlacementConfig defines the configuration for the architecture aware pod placement operand.
// Users can only deploy a single object named "cluster".
// Creating the object enables the operand.
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=clusterpodplacementconfigs,scope=Cluster
// +kubebuilder:printcolumn:name=Available,JSONPath=.status.conditions[?(@.type=="Available")].status,type=string
// +kubebuilder:printcolumn:name=Progressing,JSONPath=.status.conditions[?(@.type=="Progressing")].status,type=string
// +kubebuilder:printcolumn:name=Degraded,JSONPath=.status.conditions[?(@.type=="Degraded")].status,type=string
// +kubebuilder:printcolumn:name=Since,JSONPath=.status.conditions[?(@.type=="Progressing")].lastTransitionTime,type=date
// +kubebuilder:printcolumn:name=Status,JSONPath=.status.conditions[?(@.type=="Available")].reason,type=string
type ClusterPodPlacementConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterPodPlacementConfigSpec   `json:"spec,omitempty"`
	Status ClusterPodPlacementConfigStatus `json:"status,omitempty"`
}

func (c *ClusterPodPlacementConfig) PluginsEnabled(plugin common.Plugin) bool {
	if c.Spec.Plugins != nil {
		return c.Spec.Plugins.PluginEnabled(plugin)
	}
	return false
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

func conditionFromBool(b bool) metav1.ConditionStatus {
	if b {
		return metav1.ConditionTrue
	}
	return metav1.ConditionFalse
}

func notFromBool(b bool) string {
	if b {
		return ""
	}
	return "not "
}

func trimAndCapitalize(s string) string {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) == 0 {
		return trimmed
	}
	return strings.ToUpper(string(trimmed[0])) + trimmed[1:]
}
