package utils

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	ControllerNameKey = "controller"
	OperandLabelKey   = "multiarch.openshift.io/operand"
	OperatorName      = "multiarch-tuning-operator"
)

const (
	ArchitectureAmd64   = "amd64"
	ArchitectureArm64   = "arm64"
	ArchitecturePpc64le = "ppc64le"
	ArchitectureS390x   = "s390x"
)

const (
	ArchLabel                  = "kubernetes.io/arch"
	NodeAffinityLabel          = "multiarch.openshift.io/node-affinity"
	PreferredNodeAffinityLabel = "multiarch.openshift.io/preferred-node-affinity"
	// PreferredNodeAffinitySourcesAnnotation tracks the complete audit trail of which
	// configurations (ClusterPodPlacementConfig or PodPlacementConfig) attempted to set
	// preferred node affinity for which architectures on a pod.
	//
	// Format: Comma-separated list of entries, where each entry follows:
	//   architecture:weight:source[:skipped]
	//
	// Fields:
	//   - architecture: CPU architecture (amd64, arm64, ppc64le, s390x)
	//   - weight: Integer weight from NodeAffinityScoring plugin configuration (1-100)
	//   - source: Configuration source that attempted to set this preference
	//             Either "ClusterPodPlacementConfig" or "PodPlacementConfig-<name>"
	//   - skipped: Optional suffix indicating this architecture was skipped because
	//              it was already set by a higher-priority configuration
	//
	// Example:
	//   "arm64:30:PodPlacementConfig-high-priority,amd64:50:ClusterPodPlacementConfig:skipped"
	//
	// This annotation provides transparency for debugging preferred affinity application
	// and helps operators understand which configurations affected a pod's scheduling.
	//
	// Note: The annotation may grow with multiple PodPlacementConfigs. Kubernetes has a
	// 256KB total annotation limit per object. No per-annotation size validation is
	// currently enforced by the operator.
	PreferredNodeAffinitySourcesAnnotation = "multiarch.openshift.io/preferred-affinity-sources"
	NodeAffinityLabelValueSet              = "set"
	LabelValueNotSet                       = "not-set"
	HostnameLabel                          = "kubernetes.io/hostname"
	SchedulingGateLabel                    = "multiarch.openshift.io/scheduling-gate"
	SchedulingGateLabelValueGated          = "gated"
	SchedulingGateLabelValueRemoved        = "removed"
	PodPlacementFinalizerName              = "finalizers.multiarch.openshift.io/pod-placement"
	CPPCNoPPCObjectFinalizer               = "finalizers.multiarch.openshift.io/no-pod-placement-config"
	SingleArchLabel                        = "multiarch.openshift.io/single-arch"
	MultiArchLabel                         = "multiarch.openshift.io/multi-arch"
	NoSupportedArchLabel                   = "multiarch.openshift.io/no-supported-arch"
	ImageInspectionErrorLabel              = "multiarch.openshift.io/image-inspect-error"
	ImageInspectionErrorCountLabel         = "multiarch.openshift.io/image-inspect-error-count"
	LabelGroup                             = "multiarch.openshift.io"
)

const (
	// SchedulingGateName is the name of the Scheduling Gate
	SchedulingGateName            = "multiarch.openshift.io/scheduling-gate"
	MasterNodeSelectorLabel       = "node-role.kubernetes.io/master"
	ControlPlaneNodeSelectorLabel = "node-role.kubernetes.io/control-plane"
)

const (
	PodMutatingWebhookConfigurationName = "pod-placement-mutating-webhook-configuration"
	PodMutatingWebhookName              = "pod-placement-scheduling-gate.multiarch.openshift.io"
	PodPlacementControllerName          = "pod-placement-controller"
	PodPlacementWebhookName             = "pod-placement-web-hook"
)

const (
	ExecFormatErrorFinalizerName = "finalizers.multiarch.openshift.io/enoexec-events"
	ExecFormatErrorLabelKey      = "multiarch.openshift.io/exec-format-error"
	True                         = "true"
	False                        = "false"
	ExecFormatErrorsDetected     = "ExecFormatErrorsDetected"
	ExecFormatErrorEventReason   = "ExecFormatError"
	UnknownContainer             = "unknown-container" // Used when the container name is not known or not provided
	EnoexecControllerName        = "enoexec-event-handler-controller"
	EnoexecDaemonSet             = "enoexec-event-daemon"
)

func AllSupportedArchitecturesSet() sets.Set[string] {
	return sets.New(ArchitectureAmd64, ArchitectureArm64, ArchitecturePpc64le, ArchitectureS390x)
}

func ExecFormatErrorEventMessage(containerName, nodeArch string) string {
	var b strings.Builder

	if containerName == UnknownContainer {
		b.WriteString("A container ")
	} else {
		b.WriteString(fmt.Sprintf("Container %q ", containerName))
	}

	b.WriteString("is running a binary")
	b.WriteString(" that is not compatible with the node architecture")
	if nodeArch != "" {
		b.WriteString(fmt.Sprintf(" (%s)", nodeArch))
	}
	b.WriteString(`. This is likely due to an error in the image build process or a misconfiguration 
in the container's startup scripts. Please ensure that the container image is built 
for the correct architecture and that all scripts and binaries are compatible with 
the target node's architecture.`)

	return b.String()
}
