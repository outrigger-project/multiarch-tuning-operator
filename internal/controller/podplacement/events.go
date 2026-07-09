package podplacement

import "github.com/openshift/multiarch-tuning-operator/pkg/utils"

const (
	ArchitecturePredicatesConflict                = "ArchAwarePredicatesConflict"
	ImageArchitectureInspectionError              = "ArchAwareInspectionError"
	ArchitectureAwareNodeAffinitySet              = "ArchAwarePredicateSet"
	ArchitectureAwareGatedPodIgnored              = "ArchAwareGatedPodIgnored"
	ArchitectureAwareSchedulingGateAdded          = "ArchAwareSchedGateAdded"
	ArchitectureAwareSchedulingGateRemovalFailure = "ArchAwareSchedGateRemovalFailed"
	ArchitectureAwareSchedulingGateRemovalSuccess = "ArchAwareSchedGateRemovalSuccess"
	NoSupportedArchitecturesFound                 = "NoSupportedArchitecturesFound"
	ArchitecturePreferredAffinityDuplicates       = "ArchAwarePreferredAffinityDuplicates"
	ArchitectureAwareFallbackNodeAffinitySet      = "ArchAwareFallbackPredicateSet"

	SchedulingGateAddedMsg            = "Successfully gated with the " + utils.SchedulingGateName + " scheduling gate"
	SchedulingGateRemovalSuccessMsg   = "Successfully removed the " + utils.SchedulingGateName + " scheduling gate"
	SchedulingGateRemovalFailureMsg   = "Failed to remove the scheduling gate \"" + utils.SchedulingGateName + "\""
	ArchitecturePredicatesConflictMsg = "All the scheduling predicates already include architecture-specific constraints"
	ArchitecturePredicateSetupMsg     = "Set the supported architectures to "

	ArchitecturePreferredPredicateSetupMsg         = "Applied all architecture preferences from configuration"
	ArchitecturePreferredAffinityWithDuplicatesMsg = "Applied some architecture preferences from configuration; others were already set"
	ArchitecturePreferredAffinityAllDuplicatesMsg  = "Skipped all architecture preferences from configuration; all were already set"
	ArchitecturePreferredPredicateSkippedMsg       = "Skipped configuration; no architecture preferences were provided"

	ImageArchitectureInspectionErrorMsg = "The operator encountered an error while inspecting the container image to determine its supported architectures. " +
		"This is typically caused by the image registry being unreachable, returning an error, or a misconfiguration. " +
		"Registry error: "
	NoSupportedArchitecturesFoundMsg    = "Pod cannot be scheduled due to incompatible image architectures; container images have no supported architectures in common"
	ArchitectureAwareGatedPodIgnoredMsg = "The gated pod has been modified and is no longer eligible for architecture-aware scheduling"
	ImageInspectionErrorMaxRetriesMsg   = "The operator was unable to determine the supported architectures after multiple retries. " +
		"This is typically caused by the image registry being unreachable, returning an error, or a misconfiguration in the cluster's pull secrets or network. " +
		"Registry error"
	ArchitectureFallbackSetupMsg = "Image inspection failed; setting the nodeAffinity to the fallback architecture: "
)
