/*
Copyright 2026 Red Hat, Inc.

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

package podplacement

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/openshift/multiarch-tuning-operator/api/common"
	multiarchv1beta1 "github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

// applyCELArchitecturePlacement evaluates and applies celArchitecturePlacement plugin rules
// Returns true if the plugin was applied, false otherwise
func (r *PodReconciler) applyCELArchitecturePlacement(ctx context.Context, ppc multiarchv1beta1.PodPlacementConfig, pod *Pod) bool {
	log := ctrllog.FromContext(ctx).WithName("celArchitecturePlacement")

	// Check if plugin is enabled
	if !ppc.PluginsEnabled(common.CelArchitecturePlacementPluginName) {
		return false
	}

	// Access plugin directly, following existing pattern for NodeAffinityScoring
	celPlugin := ppc.Spec.Plugins.CelArchitecturePlacement
	if celPlugin == nil {
		// Should not occur: webhook validation ensures the plugin is non-nil when enabled.
		log.V(1).Info("celArchitecturePlacement plugin enabled but configuration is nil; skipping",
			"PodPlacementConfig", ppc.Name, "pod", pod.Name)
		return false
	}

	// Evaluate CEL rules
	result, err := evaluateCELArchitecturePlacement(celPlugin.Rules, celPlugin.FallbackArchitectures, pod.PodObject())
	if err != nil {
		log.Error(err, "Failed to evaluate CEL rules", "PodPlacementConfig", ppc.Name, "pod", pod.Name)
		pod.PublishEvent(corev1.EventTypeWarning, "CELEvaluationError", utils.EventActionCELEvaluation, fmt.Sprintf("Failed to evaluate CEL rules: %v", err))
		return false
	}

	if result.allRulesErrored {
		// Every CEL expression in this PPC is malformed.
		// Do not apply the fallback — return false so that lower-priority PPCs
		// can still claim the pod (mirrors the webhook's applyCELInWebhook behaviour).
		log.V(1).Info("All CEL rules in PPC failed to evaluate (malformed); skipping PPC",
			"PodPlacementConfig", ppc.Name, "pod", pod.Name)
		return false
	}

	if result.matched {
		log.V(2).Info("CEL rule matched, applying architecture constraints",
			"PodPlacementConfig", ppc.Name,
			"pod", pod.Name,
			"ruleName", result.ruleName,
			"architectures", result.architectures)
	} else {
		log.V(2).Info("No CEL rules matched, using fallback architectures",
			"PodPlacementConfig", ppc.Name,
			"pod", pod.Name,
			"architectures", result.architectures)
	}

	// Apply architecture constraints in-place (no NodeSelectorTerms deletions).
	// If no architectures were produced (empty/nil result), do not claim the plugin
	// applied — fall through to image-based detection so the pod is not ungated
	// without any architecture constraint.
	//
	// We use applyArchitectureNodeAffinity (not applyArchitectureConstraints) so that
	// the reconciler never deletes NodeSelectorTerms.  KEP-3838 forbids adding or
	// removing terms via an Update on a Pod that already has a non-empty
	// NodeSelectorTerms list; the webhook has already persisted the correct term
	// count, so here we only update matchExpressions within each existing term.
	if len(result.architectures) == 0 {
		log.V(1).Info("CEL plugin produced no architectures; skipping",
			"PodPlacementConfig", ppc.Name, "pod", pod.Name,
			"fallbackArchitectures", result.architectures)
		return false
	}
	removeArchitectureFromNodeSelector(pod.PodObject())
	applyArchitectureNodeAffinity(pod.PodObject(), result.architectures)

	// CEL successfully applied architecture constraints. Mark the pod so downstream
	// components (e.g. reconciler, e2e tests) can distinguish CEL-placed pods from
	// image-inspection-placed pods. The value "overriden" is intentional per review.
	pod.EnsureLabel(utils.NodeAffinityLabel, utils.NodeAffinityLabelValueOverriden)

	// Publish event
	configSource := fmt.Sprintf("%s-%s", multiarchv1beta1.PodPlacementConfigKind, ppc.Name)
	if result.matched {
		pod.PublishEvent(corev1.EventTypeNormal, "CELArchitecturePlacementApplied", utils.EventActionNodeAffinityUpdate,
			fmt.Sprintf("Applied CEL rule '%s' from %s, architectures: %v", result.ruleName, configSource, result.architectures))
	} else {
		pod.PublishEvent(corev1.EventTypeNormal, "CELArchitecturePlacementFallback", utils.EventActionNodeAffinityUpdate,
			fmt.Sprintf("No CEL rules matched, using fallback architectures from %s: %v", configSource, result.architectures))
	}

	return true
}
