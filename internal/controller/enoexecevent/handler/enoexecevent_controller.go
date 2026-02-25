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

package handler

import (
	"context"
	runtime2 "runtime"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrl2 "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	multiarchv1beta1 "github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/internal/controller/enoexecevent/handler/metrics"
	"github.com/openshift/multiarch-tuning-operator/pkg/models"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

// Reconciler reconciles a ENoExecEvent object
type Reconciler struct {
	client.Client
	clientSet *kubernetes.Clientset
	Scheme    *runtime.Scheme
	recorder  record.EventRecorder
}

func NewReconciler(client client.Client, clientSet *kubernetes.Clientset, scheme *runtime.Scheme, recorder record.EventRecorder) *Reconciler {
	return &Reconciler{
		Client:    client,
		clientSet: clientSet,
		Scheme:    scheme,
		recorder:  recorder,
	}
}

//+kubebuilder:rbac:groups=multiarch.openshift.io,resources=enoexecevents,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=multiarch.openshift.io,resources=enoexecevents/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=multiarch.openshift.io,resources=enoexecevents/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;update
//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list

// Reconcile will reconcile the ENoExecEvent resource.
// It will fetch the ENoExecEvent instance, retrieve the pod and node information,
// label the pod with the ENoExecEvent label, update the metrics, and publish an event.
// Finally, it will delete the ENoExecEvent resource if the reconciliation was successful or if the pod was not found.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	metrics.InitMetrics()
	// Fetch the ENoExecEvent instance
	enoExecEventObj := &multiarchv1beta1.ENoExecEvent{}
	if err := r.Get(ctx, req.NamespacedName, enoExecEventObj); err != nil {
		// If the resource is not found, we simply return. This is not an error.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	eNoExecEvent := NewENoExecEvent(enoExecEventObj, ctx, r.recorder)

	if eNoExecEvent.Namespace != utils.Namespace() {
		logger.Info("ENoExecEvent is not in the operator namespace, skipping reconciliation", "name", eNoExecEvent.Name, "namespace", eNoExecEvent.Namespace)
		r.markAsError(ctx, eNoExecEvent, ErrorReasonWrongNamespace)
		// Return nil to avoid requeuing - this event will be cleaned up by deleteErroredENoExecEvents
		return ctrl.Result{}, nil
	}

	if eNoExecEvent.Status.PodName == "" {
		// If the ENoExecEvent does not have a pod name, we ignore it
		logger.V(5).Info("ENoExecEvent does not have a pod name, skipping reconciliation", "name", eNoExecEvent.Name, "namespace", eNoExecEvent.Namespace)
		r.markAsError(ctx, eNoExecEvent, ErrorReasonPodNotFound)
		// Return nil to avoid requeuing - this event will be cleaned up by deleteErroredENoExecEvents
		return ctrl.Result{}, nil
	}

	// Log the ENoExecEvent instance
	logger.Info("Reconciling ENoExecEvent", "name", eNoExecEvent.Name, "namespace", eNoExecEvent.Namespace)
	ret, err := r.reconcile(ctx, eNoExecEvent)
	// If the reconciliation was successful, or one of the objects was not found, we delete the ENoExecEvent resource.
	if client.IgnoreNotFound(err) == nil {
		if err := r.Delete(ctx, &eNoExecEvent.ENoExecEvent); err != nil {
			logger.Error(err, "Failed to delete ENoExecEvent resource after reconciliation", "name", eNoExecEvent.Name)
			// Mark as error but return the original error so client.IgnoreNotFound works
			r.markAsError(ctx, eNoExecEvent, ErrorReasonReconciliation)
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		metrics.EnoexecCounter.Inc()
		logger.Info("Deleted ENoExecEvent resource after successful reconciliation", "name", eNoExecEvent.Name)
		return ret, nil
	}
	metrics.EnoexecCounterInvalid.Inc()
	logger.Error(err, "Failed to reconcile ENoExecEvent", "name", eNoExecEvent.Name)
	// Mark as error - this is a real reconciliation error that will be retried
	r.markAsError(ctx, eNoExecEvent, ErrorReasonReconciliation)
	return ctrl.Result{}, err
}

func (r *Reconciler) reconcile(ctx context.Context, eNoExecEvent *ENoExecEvent) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	// Retrieve the pod
	podObj, err := r.clientSet.CoreV1().Pods(eNoExecEvent.Status.PodNamespace).Get(ctx, eNoExecEvent.Status.PodName, metav1.GetOptions{})
	if err != nil {
		// If the pod is not found, the error will be ignored and the ENoExecEvent will be deleted by the caller.
		// TODO: increment counter for exec format error failures and missed ENoExecEvent reconciliations
		logger.Error(err, "Failed to get pod for ENoExecEvent", "podName",
			eNoExecEvent.Status.PodName, "namespace", eNoExecEvent.Status.PodNamespace)
		// Mark as error but return the original error so client.IgnoreNotFound works
		r.markAsError(ctx, eNoExecEvent, ErrorReasonPodNotFound)
		return ctrl.Result{}, err
	}

	pod := models.NewPod(podObj, ctx, r.recorder)
	if pod.PodObject().Spec.NodeName != eNoExecEvent.Status.NodeName {
		// If the pod is not scheduled on the node where the ENoExecEvent was generated, we skip the reconciliation.
		logger.Info("Pod is not scheduled on the node where the ENoExecEvent was generated", "podName",
			pod.Name, "namespace", pod.Namespace, "nodeName", eNoExecEvent.Status.NodeName)
		// This is not an error case - mark it and return nil so it gets deleted
		r.markAsError(ctx, eNoExecEvent, ErrorReasonNodeNotFound)
		return ctrl.Result{}, nil
	}

	// Retrieve the node
	node, err := r.clientSet.CoreV1().Nodes().Get(ctx, eNoExecEvent.Status.NodeName, metav1.GetOptions{})
	if err != nil {
		// When the error is "not found", the ENoExecEvent may refer a new pod with the same name scheduled on a different node.
		// This ENoExecEvent is then not relevant anymore and will be deleted by the caller.
		// Other errors will be returned to the caller, which will retry the reconciliation.
		// TODO: increment counter for exec format error failures and missed ENoExecEvent reconciliations
		logger.Error(err, "Failed to get node for ENoExecEvent", "nodeName", podObj.Spec.NodeName,
			"podName", eNoExecEvent.Status.PodName, "namespace", eNoExecEvent.Status.PodNamespace)
		// Mark as error but return the original error so client.IgnoreNotFound works
		r.markAsError(ctx, eNoExecEvent, ErrorReasonNodeNotFound)
		return ctrl.Result{}, err
	}

	containerName, err := pod.ContainerNameFor(eNoExecEvent.Status.ContainerID)
	if err != nil {
		logger.Error(err, "Container ID not found in pod status", "containerID", eNoExecEvent.Status.ContainerID)
		containerName = utils.UnknownContainer
	}

	logger.Info("Publishing event for ENoExecEvent", "podName", pod.Name, "namespace", pod.Namespace)
	pod.PublishEvent(v1.EventTypeWarning, utils.ExecFormatErrorEventReason,
		utils.ExecFormatErrorEventMessage(containerName, node.Labels[utils.ArchLabel]))

	// Label the pod with the ENoExecEvent label.
	pod.EnsureLabel(utils.ExecFormatErrorLabelKey, utils.True)
	// TODO: increment counter for exec format error failures
	// Update the pod with the new label
	if _, err = r.clientSet.CoreV1().Pods(pod.Namespace).Update(ctx, pod.PodObject(), metav1.UpdateOptions{}); err != nil {
		logger.Error(err, "Failed to label the pod", "podName", pod.Name, "namespace", pod.Namespace)
		// if the error is "not found", it means the pod has been deleted, the caller will handle this case.
		// Mark as error but return the original error so client.IgnoreNotFound works
		r.markAsError(ctx, eNoExecEvent, ErrorReasonPodNotFound)
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	// This reconciler is mostly I/O bound due to the pod and node retrievals, so we can increase the number of concurrent
	// reconciles to the number of CPUs * 4.
	maxConcurrentReconciles := runtime2.NumCPU() * 4
	log.FromContext(context.Background()).Info("Setting up the ENoExecEventReconciler with the manager with max"+
		" concurrent reconciles", "maxConcurrentReconciles", maxConcurrentReconciles)

	return ctrl.NewControllerManagedBy(mgr).
		For(&multiarchv1beta1.ENoExecEvent{}).
		WithOptions(ctrl2.Options{
			MaxConcurrentReconciles: maxConcurrentReconciles,
		}).
		Complete(r)
}

// markAsError marks the ENoExecEvent with an error label and persists it.
// The errored events are kept in the cluster and cleaned up later by the operator controller's
// deleteErroredENoExecEvents function during CPPC deletion or plugin disable.
func (r *Reconciler) markAsError(ctx context.Context, eNoExecEvent *ENoExecEvent, errorReason string) {
	logger := log.FromContext(ctx)

	// Set the error label
	eNoExecEvent.EnsureLabel(ENoExecEventErrorLabel, errorReason)

	// Update the ENoExecEvent to persist the label.
	// Ignore NotFound errors - the object may have already been deleted.
	if err := r.Update(ctx, &eNoExecEvent.ENoExecEvent); client.IgnoreNotFound(err) != nil {
		logger.Error(err, "Failed to update ENoExecEvent with error label", "name", eNoExecEvent.Name, "reason", errorReason)
	}
}
