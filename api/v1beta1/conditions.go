package v1beta1

import "github.com/openshift/multiarch-tuning-operator/pkg/utils"

const (
	MutatingWebhookConfigurationNotAvailable = "MutatingWebhookConfigurationNotAvailable"
	PodPlacementControllerNotRolledOutType   = "PodPlacementControllerNotRolledOut"
	PodPlacementWebhookNotRolledOutType      = "PodPlacementWebhookNotRolledOut"
	AvailableType                            = "Available"
	DegradedType                             = "Degraded"
	ProgressingType                          = "Progressing"
	DeprovisioningType                       = "Deprovisioning"

	MutatingWebhookConfigurationReadyMsg = "The mutating webhook configuration is %sready."
	PodPlacementControllerRolledOutMsg   = "The pod placement controller is %sfully rolled out."
	PodPlacementWebhookRolledOutMsg      = "The pod placement webhook is %sfully rolled out."
	ReadyMsg                             = "The cluster pod placement config operand is %sready. We can%s gate and reconcile pods."
	DegradedMsg                          = "The cluster pod placement config operand is %sdegraded."
	ProgressingMsg                       = "The cluster pod placement config operand is %sprogressing."
	DeprovisioningMsg                    = "The cluster pod placement config operand is %sbeing deprovisioned. %s"
	PendingDeprovisioningMsg             = "Some pods may still have the " + utils.SchedulingGateName +
		"scheduling gate. The pod placement controller is updating them and will terminate."
	AllComponentsReady = "AllComponentsReady"
)
