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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrl2 "sigs.k8s.io/controller-runtime/pkg/controller"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/openshift/multiarch-tuning-operator/api/common"
	multiarchv1beta1 "github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/internal/controller/podplacement/metrics"
	"github.com/openshift/multiarch-tuning-operator/pkg/informers/clusterpodplacementconfig"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

// PodReconciler reconciles a Pod object
type PodReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	ClientSet *kubernetes.Clientset
	Recorder  record.EventRecorder
}

// RBACs for the operands' controllers are added manually because kubebuilder can't handle multiple service accounts
// and roles.
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=use
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch

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
	// Pods without the scheduling gate should be ignored.
	if !pod.HasSchedulingGate() {
		log.V(2).Info("Pod does not have the scheduling gate. Ignoring...")
		return ctrl.Result{}, nil
	}
	metrics.ProcessedPodsCtrl.Inc()
	defer utils.HistogramObserve(now, metrics.TimeToProcessGatedPod)
	r.processPod(ctx, pod)
	err := r.Update(ctx, pod.PodObject())
	if err != nil {
		log.Error(err, "Unable to update the pod")
		pod.PublishEvent(corev1.EventTypeWarning, ArchitectureAwareSchedulingGateRemovalFailure, SchedulingGateRemovalFailureMsg)
		return ctrl.Result{}, err
	}
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
	if pod.shouldIgnorePod(cppc) {
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

	r.applyPodPlacementConfigs(ctx, pod)

	if cppc != nil && cppc.PluginsEnabled(common.NodeAffinityScoringPluginName) {
		pod.SetPreferredArchNodeAffinity(cppc.Spec.Plugins.NodeAffinityScoring)
	}

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
	}
	// If the pod has been processed successfully or the max retries have been reached, remove the scheduling gate.
	if err == nil || pod.maxRetries() {
		if pod.Labels[utils.PreferredNodeAffinityLabel] == utils.LabelValueNotSet {
			pod.PublishEvent(corev1.EventTypeNormal, ArchitectureAwareNodeAffinitySet,
				ArchitecturePreferredPredicateSkippedMsg)
		}

		log.V(1).Info("Removing the scheduling gate from pod.")
		pod.RemoveSchedulingGate()
	}
}

func (r *PodReconciler) applyPodPlacementConfigs(ctx context.Context, pod *Pod) {
	log := ctrllog.FromContext(ctx).WithName("PodPlacementConfig")
	// List existing PodPlacementConfigs in the same namespace
	ppcList := &multiarchv1beta1.PodPlacementConfigList{}
	if err := r.List(ctx, ppcList, client.InNamespace(pod.Namespace)); err != nil {
		pod.handleError(err, "failed to list existing PodPlacementConfigs in namespace")
		return
	}

	// Sort the configurations by descending priority
	sort.Slice(ppcList.Items, func(i, j int) bool {
		return ppcList.Items[i].Spec.Priority > ppcList.Items[j].Spec.Priority
	})

	// For each namespace-scoped configuration, check selector and apply
	for _, ppc := range ppcList.Items {
		log.V(1).Info("Processing PodPlacementConfig", "namespace", ppc.Namespace, "name", ppc.Name)

		// check if plugin is enabled
		if !ppc.PluginsEnabled(common.NodeAffinityScoringPluginName) {
			log.V(1).Info("Skipping PodPlacementConfig NodeAffinityScoring disabled", "namespace", ppc.Namespace, "name", ppc.Name)
			continue
		}

		selector, err := metav1.LabelSelectorAsSelector(ppc.Spec.LabelSelector)
		if err != nil {
			pod.handleError(err, "Invalid label selector in PodPlacementConfig")
			continue
		}

		// Check if the pod matches the label selector
		if selector == labels.Nothing() || selector.Matches(labels.Set(pod.Labels)) {
			log.Info("Applying namespace-scoped config", "PodPlacementConfig", ppc.Name)
			// Apply the configuration, checking for overlaps
			pod.SetPreferredArchNodeAffinity(ppc.Spec.Plugins.NodeAffinityScoring)
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
		For(&corev1.Pod{}).WithOptions(ctrl2.Options{
		MaxConcurrentReconciles: maxConcurrentReconciles,
	}).
		Complete(r)
}
