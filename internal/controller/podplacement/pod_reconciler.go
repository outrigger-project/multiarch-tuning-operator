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

package podplacement

import (
	"context"
	"fmt"
	runtime2 "runtime"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrl2 "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/openshift/multiarch-tuning-operator/api/common"
	multiarchv1beta1 "github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/internal/controller/podplacement/metrics"
	"github.com/openshift/multiarch-tuning-operator/pkg/informers/clusterpodplacementconfig"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

// PodReconciler reconciles a Pod object
type PodReconciler struct {
	client.Client
	APIReader client.Reader
	Scheme    *runtime.Scheme
	ClientSet *kubernetes.Clientset
	Recorder  record.EventRecorder
}

// Note: The operand (pod-placement-controller) RBAC is defined programmatically in
// podplacement_objects.go buildClusterRoleController(). Do NOT add operand-specific
// RBAC markers here — kubebuilder merges all markers into the manager's ClusterRole,
// which would over-provision the operator SA with permissions only the operand needs.

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// the Pod object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
// Reconcile has to watch the pod object if it has the scheduling gate with name SchedulingGateName,
// inspect the images in the pod spec, update the nodeAffinity accordingly and remove the scheduling gate.
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Lazy initialization of the metrics to support concurrent reconciles
	metrics.InitPodPlacementControllerMetrics()
	now := time.Now()
	defer utils.HistogramObserve(now, metrics.TimeToProcessPod)

	log := ctrllog.FromContext(ctx)

	pod := newPod(&corev1.Pod{}, ctx, r.Recorder)

	if err := r.Get(ctx, req.NamespacedName, pod.PodObject()); err != nil {
		log.V(2).Info("Unable to fetch pod", "error", err)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.V(1).Info("Reconciling pod",
		"pod", pod.Name,
		"namespace", pod.Namespace,
		"hasSchedulingGate", pod.HasSchedulingGate())

	// Pods without the scheduling gate should be ignored.
	if !pod.HasSchedulingGate() {
		log.V(2).Info("Pod does not have the scheduling gate. Ignoring...")
		return ctrl.Result{}, nil
	}
	metrics.ProcessedPodsCtrl.Inc()
	defer utils.HistogramObserve(now, metrics.TimeToProcessGatedPod)

	r.processPod(ctx, pod)

	log.V(1).Info("Updating pod",
		"pod", pod.Name,
		"namespace", pod.Namespace)

	err := r.Update(ctx, pod.PodObject())
	if err != nil {
		log.Error(err, "Unable to update the pod",
			"pod", pod.Name,
			"namespace", pod.Namespace)
		pod.PublishEvent(corev1.EventTypeWarning, ArchitectureAwareSchedulingGateRemovalFailure, SchedulingGateRemovalFailureMsg)
		return ctrl.Result{}, err
	}

	log.V(1).Info("Pod updated successfully",
		"pod", pod.Name,
		"namespace", pod.Namespace)
	if !pod.HasSchedulingGate() {
		// Only publish the event if the scheduling gate has been removed and the pod has been updated successfully.
		pod.PublishEvent(corev1.EventTypeNormal, ArchitectureAwareSchedulingGateRemovalSuccess, SchedulingGateRemovalSuccessMsg)
		metrics.GatedPodsGauge.Dec()
	}
	return ctrl.Result{}, nil
}

func (r *PodReconciler) processPod(ctx context.Context, pod *Pod) {
	log := ctrllog.FromContext(ctx)
	log.V(1).Info("Processing pod")

	cppc := clusterpodplacementconfig.GetClusterPodPlacementConfig()

	// List existing PodPlacementConfigs in the same namespace
	ppcList := &multiarchv1beta1.PodPlacementConfigList{}
	if err := r.List(ctx, ppcList, client.InNamespace(pod.Namespace)); err != nil {
		log.Error(err, "failed to list existing PodPlacementConfigs in namespace",
			"namespace", pod.Namespace)
		pod.handleError(err, "failed to list existing PodPlacementConfigs in namespace")
		return
	}
	// The informer cache can lag behind the API server. If the cache shows no PPCs,
	// verify with a direct API read to avoid a race where a just-created PPC is missed
	// and the pod is permanently ungated without its preferred affinity.
	if len(ppcList.Items) == 0 {
		if err := r.APIReader.List(ctx, ppcList, client.InNamespace(pod.Namespace)); err != nil {
			pod.handleError(err, "failed to list PodPlacementConfigs from API server")
			return
		}
	}

	// Filter to only PPCs that match this pod's labels - do this once for efficiency
	matchingPPCs := pod.filterMatchingPPCs(ppcList)

	if pod.shouldIgnorePod(cppc, matchingPPCs) {
		log.V(3).Info("A pod with the scheduling gate should be ignored. Ignoring...")
		// We can reach this branch when:
		// - The pod has been gated but not processed before the operator changed configuration such that the pod should be ignored.
		// - The pod has got some other changes in the admission chain from another webhook that makes it not suitable for processing anymore
		//	(for example another actor set the nodeAffinity already for the kubernetes.io/arch label).
		// In both cases, we should just remove the scheduling gate.
		log.V(1).Info("Removing the scheduling gate from pod.")
		pod.RemoveSchedulingGate()
		pod.PublishEvent(corev1.EventTypeWarning, ArchitectureAwareGatedPodIgnored, ArchitectureAwareGatedPodIgnoredMsg)
		return
	}

	// Skip preferred affinity processing if the user has already configured architecture-related preferred affinity
	// or if the reconcile loop has already applied the PPCs/CPPC (e.g., due to a retry or re-reconciliation)
	celApplied := false
	if !pod.isPreferredAffinityConfiguredForArchitecture() {
		celApplied = r.applyMatchingPPCs(ctx, matchingPPCs, pod)

		if cppc != nil && cppc.PluginsEnabled(common.NodeAffinityScoringPluginName) {
			pod.SetPreferredArchNodeAffinity(cppc.Spec.Plugins.NodeAffinityScoring, multiarchv1beta1.ClusterPodPlacementConfigKind)
		}
	} else {
		log.V(2).Info("Pod already has architecture-related preferred affinity. This could be user-defined or from a previous reconcile loop. Skipping PPC/CPPC preferred affinity processing.")
		// Track that configs were skipped due to user-defined preferences
		r.trackSkippedMatchingConfigs(ctx, pod, cppc, matchingPPCs)
	}

	// celArchitecturePlacement takes precedence over image-based detection.
	// When the CEL plugin applies, return immediately without running image-based
	// architecture detection, fallback logic, or the default architecture append.
	if celApplied {
		// If no preferred node affinity was set by any config, log and publish an event
		if pod.Labels[utils.PreferredNodeAffinityLabel] == utils.LabelValueNotSet {
			pod.PublishEvent(corev1.EventTypeNormal, ArchitectureAwareNodeAffinitySet,
				ArchitecturePreferredPredicateSkippedMsg)
			log.V(2).Info("No preferred node affinity was set")
		}
		log.V(1).Info("Removing the scheduling gate from pod.")
		pod.RemoveSchedulingGate()
		return
	}

	// Image-based architecture detection (only executed when CEL plugin was NOT applied)
	var err error
	// Prepare the requirement for the node affinity.
	psdl, err := r.pullSecretDataList(ctx, pod)
	pod.handleError(err, "Unable to retrieve the image pull secret data for the pod.")
	// If no error occurred when retrieving the image pull secret data, set the node affinity.
	if err == nil {
		_, err = pod.SetNodeAffinityArchRequirement(psdl)
		pod.handleError(err, "Unable to set the node affinity for the pod.")
	}

	if pod.maxRetries() && err != nil {
		// the number of retries is incremented in the handleError function when the error is not nil.
		// If we enter this branch, the retries counter has been incremented and reached the max retries.
		// The counter starts at 1 when the first error occurs. Therefore, when the reconciler tries maxRetries times,
		// the counter is equal to the maxRetries value and the pod should not be processed again.
		// Publish this event and remove the scheduling gate.
		log.Info("Max retries Reached. The pod will not have the nodeAffinity set.")
		pod.PublishEvent(corev1.EventTypeWarning, ImageArchitectureInspectionError, fmt.Sprintf("%s: %s", ImageInspectionErrorMaxRetriesMsg, err.Error()))

		if cppc != nil && cppc.Spec.FallbackArchitecture != "" {
			log.Info("Setting the nodeAffinity to the fallback architecture", "fallbackArchitecture", cppc.Spec.FallbackArchitecture)
			pod.setRequiredNodeAffinityToFallbackArchitecture(cppc.Spec.FallbackArchitecture)
		}
	}
	// If the pod has been processed successfully or the max retries have been reached, remove the scheduling gate.
	if err == nil || pod.maxRetries() {
		// If no preferred node affinity was set by any config, log and publish an event
		if pod.Labels[utils.PreferredNodeAffinityLabel] == utils.LabelValueNotSet {
			pod.PublishEvent(corev1.EventTypeNormal, ArchitectureAwareNodeAffinitySet,
				ArchitecturePreferredPredicateSkippedMsg)
			log.V(2).Info("No preferred node affinity was set")
		}
		log.V(1).Info("Removing the scheduling gate from pod.")
		pod.RemoveSchedulingGate()
	}
}

// applyMatchingPPCs applies the pre-filtered matching PodPlacementConfigs to the pod.
// The matchingPPCs slice should already be filtered to only include PPCs whose label selector matches the pod.
// Returns true if CEL architecture placement was applied, false otherwise.
func (r *PodReconciler) applyMatchingPPCs(ctx context.Context, matchingPPCs []multiarchv1beta1.PodPlacementConfig, pod *Pod) bool {
	log := ctrllog.FromContext(ctx).WithName("PodPlacementConfig")

	// Sort the configurations by descending priority
	sort.Slice(matchingPPCs, func(i, j int) bool {
		return matchingPPCs[i].Spec.Priority > matchingPPCs[j].Spec.Priority
	})

	// Check for celArchitecturePlacement plugin first (highest priority)
	// Only the first matching PPC with celArchitecturePlacement enabled is applied
	celApplied := false
	for _, ppc := range matchingPPCs {
		if r.applyCELArchitecturePlacement(ctx, ppc, pod) {
			log.V(1).Info("celArchitecturePlacement plugin applied, will skip image-based detection", "PodPlacementConfig", ppc.Name)
			celApplied = true
			// CEL plugin was applied, it takes precedence over image-based detection
			// Continue to allow NodeAffinityScoring to run (coexistence per enhancement)
			break
		}
	}

	// For each matching namespace-scoped configuration, apply NodeAffinityScoring if plugin is enabled
	// This allows NodeAffinityScoring to coexist with celArchitecturePlacement per enhancement doc
	for _, ppc := range matchingPPCs {
		log.V(1).Info("Processing PodPlacementConfig", "namespace", ppc.Namespace, "name", ppc.Name)

		// check if plugin is enabled
		if !ppc.PluginsEnabled(common.NodeAffinityScoringPluginName) {
			log.V(1).Info("Skipping PodPlacementConfig NodeAffinityScoring disabled", "namespace", ppc.Namespace, "name", ppc.Name)
			continue
		}

		log.Info("Applying namespace-scoped config", "PodPlacementConfig", ppc.Name)
		configSource := fmt.Sprintf("%s-%s", multiarchv1beta1.PodPlacementConfigKind, ppc.Name)
		pod.SetPreferredArchNodeAffinity(ppc.Spec.Plugins.NodeAffinityScoring, configSource)
	}

	return celApplied
}

// trackSkippedMatchingConfigs tracks in the annotation which PPC/CPPC configs would have been applied
// but were skipped due to user-defined architecture-related preferred affinity.
// The matchingPPCs slice should already be filtered to only include PPCs whose label selector matches the pod.
func (r *PodReconciler) trackSkippedMatchingConfigs(ctx context.Context, pod *Pod, cppc *multiarchv1beta1.ClusterPodPlacementConfig, matchingPPCs []multiarchv1beta1.PodPlacementConfig) {
	// Sort by descending priority (same as applyMatchingPPCs)
	sort.Slice(matchingPPCs, func(i, j int) bool {
		return matchingPPCs[i].Spec.Priority > matchingPPCs[j].Spec.Priority
	})

	// Track skipped PodPlacementConfigs
	for _, ppc := range matchingPPCs {
		if !ppc.PluginsEnabled(common.NodeAffinityScoringPluginName) {
			continue
		}

		configSource := fmt.Sprintf("%s-%s", multiarchv1beta1.PodPlacementConfigKind, ppc.Name)
		// Track each platform term as skipped
		for _, platform := range ppc.Spec.Plugins.NodeAffinityScoring.Platforms {
			pod.trackAffinitySource(platform.Architecture, platform.Weight, configSource, false)
		}
	}

	// Track skipped ClusterPodPlacementConfig
	if cppc != nil && cppc.PluginsEnabled(common.NodeAffinityScoringPluginName) {
		for _, platform := range cppc.Spec.Plugins.NodeAffinityScoring.Platforms {
			pod.trackAffinitySource(platform.Architecture, platform.Weight, multiarchv1beta1.ClusterPodPlacementConfigKind, false)
		}
	}
}

// pullSecretDataList returns the list of secrets data for the given pod given its imagePullSecrets field
func (r *PodReconciler) pullSecretDataList(ctx context.Context, pod *Pod) ([][]byte, error) {
	log := ctrllog.FromContext(ctx)
	secretAuths := make([][]byte, 0)
	secretList := pod.getPodImagePullSecrets()
	for _, pullsecret := range secretList {
		secret, err := r.ClientSet.CoreV1().Secrets(pod.Namespace).Get(ctx, pullsecret, metav1.GetOptions{})
		if err != nil {
			log.Error(err, "Error getting secret", "secret", pullsecret)
			continue
		}
		if secretData, err := utils.ExtractAuthFromSecret(secret); err != nil {
			log.Error(err, "Error extracting auth from secret", "secret", pullsecret)
			continue
		} else {
			secretAuths = append(secretAuths, secretData)
		}
	}
	return secretAuths, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// This reconciler is mostly I/O bound due to the pod and node retrievals, so we can increase the number of concurrent
	// reconciles to the number of CPUs * 4.
	// The main bottleneck is the image inspection.
	maxConcurrentReconciles := runtime2.NumCPU() * 4
	ctrllog.FromContext(context.Background()).Info("Setting up the PodReconciler with the manager with max"+
		" concurrent reconciles", "maxConcurrentReconciles", maxConcurrentReconciles)

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		// Watch PodPlacementConfig to re-queue gated pods when a PPC is created, updated, or deleted.
		// Without this, there is a race between a PPC appearing in the API server and the informer
		// cache syncing it. If a pod is reconciled before the cache has the PPC, the pod is processed
		// without the PPC's preferred affinity and the scheduling gate is removed — permanently missing
		// the PPC configuration. By watching PPCs, we re-queue any still-gated pods in the namespace
		// so they are reprocessed with the PPC now in the cache.
		//
		// Limitation: this only helps pods that are still gated (gate=gated) when the PPC watch event
		// arrives. If a pod's image inspection completes instantly (e.g., cache hit) and the gate is
		// removed before the PPC watch fires, the pod won't be re-queued. In practice this is rare
		// because image inspection involves network I/O (hundreds of ms) while informer watches
		// deliver events in ~10ms. Fully closing this gap would require re-processing ungated pods,
		// which conflicts with the one-shot scheduling gate design.
		Watches(
			&multiarchv1beta1.PodPlacementConfig{},
			handler.EnqueueRequestsFromMapFunc(r.mapPPCToPods),
		).
		WithOptions(ctrl2.Options{
			MaxConcurrentReconciles: maxConcurrentReconciles,
		}).
		Complete(r)
}

// mapPPCToPods returns reconcile requests for all gated pods in the PPC's namespace.
func (r *PodReconciler) mapPPCToPods(ctx context.Context, obj client.Object) []reconcile.Request {
	log := ctrllog.FromContext(ctx)
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList,
		client.InNamespace(obj.GetNamespace()),
		client.MatchingLabels{utils.SchedulingGateLabel: utils.SchedulingGateLabelValueGated},
	); err != nil {
		log.Error(err, "Failed to list gated pods for PodPlacementConfig change", "namespace", obj.GetNamespace())
		return nil
	}
	requests := make([]reconcile.Request, 0, len(podList.Items))
	for i := range podList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      podList.Items[i].Name,
				Namespace: podList.Items[i].Namespace,
			},
		})
	}
	if len(requests) > 0 {
		log.V(1).Info("Re-queuing gated pods due to PodPlacementConfig change",
			"ppc", obj.GetName(), "namespace", obj.GetNamespace(), "podCount", len(requests))
	}
	return requests
}
