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

package operator

import (
	"context"
	"errors"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	errorutils "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/openshift/library-go/pkg/operator/events"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"go.uber.org/zap/zapcore"

	"github.com/openshift/multiarch-tuning-operator/api/common"
	"github.com/openshift/multiarch-tuning-operator/api/common/plugins"
	multiarchv1beta1 "github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/framework"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

// ClusterPodPlacementConfigReconciler reconciles a ClusterPodPlacementConfig object
type ClusterPodPlacementConfigReconciler struct {
	client.Client
	DynamicClient *dynamic.DynamicClient
	Scheme        *runtime.Scheme
	ClientSet     *kubernetes.Clientset
	Recorder      events.Recorder
}

const (
	operandName       = "pod-placement-controller"
	priorityClassName = "system-cluster-critical"
)

const (
	waitingForUngatingPodsError         = "waiting for pods with the scheduling gate to be ungated"
	waitingForWebhookSInterruptionError = "re-queueing to ensure the webhook objects deletion interrupt pods gating before checking the pods gating status"
	clusterPodPlacementConfigNotReady   = "cluster pod placement config is not ready yet. re-queueing"
	RemainingPodPlacementConfig         = "cannot delete: namespaced PodPlacementConfig resources still exist"
)

//+kubebuilder:rbac:groups=multiarch.openshift.io,resources=clusterpodplacementconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=multiarch.openshift.io,resources=clusterpodplacementconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=multiarch.openshift.io,resources=clusterpodplacementconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,verbs=get;update;patch;create;delete;list;watch
//+kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations/status,verbs=get

//+kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch;create;delete
//+kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get
//+kubebuilder:rbac:groups=apps,resources=deployments/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;update;patch;create;delete
//+kubebuilder:rbac:groups=core,resources=services/status,verbs=get
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete

//+kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;update;patch;create;delete
//+kubebuilder:rbac:groups=core,resources=serviceaccounts/status,verbs=get
//+kubebuilder:rbac:groups=core,resources=serviceaccounts/finalizers,verbs=update
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;list;watch;update;patch;create;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles/status,verbs=get
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles/finalizers,verbs=update
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;update;patch;create;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings/status,verbs=get
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings/finalizers,verbs=update
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;update;patch;create;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles/status,verbs=get
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles/finalizers,verbs=update
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;update;patch;create;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings/status,verbs=get
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings/finalizers,verbs=update

//+kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;update;patch;create;delete
//+kubebuilder:rbac:groups=monitoring.coreos.com,resources=prometheusrules,verbs=get;list;watch;update;patch;create;delete

// Reconcile reconciles the ClusterPodPlacementConfig object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
func (r *ClusterPodPlacementConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.V(1).Info("+++++++++++++++++++ Reconciling ClusterPodPlacementConfig +++++++++++++++++++")
	// Lookup the ClusterPodPlacementConfig instance for this reconcile request
	clusterPodPlacementConfig := &multiarchv1beta1.ClusterPodPlacementConfig{}
	var err error

	if err = r.Get(ctx, client.ObjectKey{
		Name: req.Name,
	}, clusterPodPlacementConfig); err != nil {
		log.Error(err, "Unable to fetch ClusterPodPlacementConfig")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	log.V(1).Info("ClusterPodPlacementConfig fetched...", "name", clusterPodPlacementConfig.Name)
	switch {
	case !clusterPodPlacementConfig.DeletionTimestamp.IsZero() && !controllerutil.ContainsFinalizer(clusterPodPlacementConfig, utils.PodPlacementFinalizerName):
		log.V(2).Info("the ClusterPodPlacementConfig object is being deleted, and the finalizer has already been removed successfully.")
		return ctrl.Result{}, nil
	case !clusterPodPlacementConfig.DeletionTimestamp.IsZero():
		// Only execute deletion if the object is being deleted and the finalizer is present
		return ctrl.Result{}, r.handleDelete(ctx, clusterPodPlacementConfig)
	}
	// Move the finalizer block before applying the corresponding resources
	// to ensure that finalizers are properly added and can be cleaned up
	// even if the clusterPodPlacementConfig CR is deleted shortly after creation.
	if !controllerutil.ContainsFinalizer(clusterPodPlacementConfig, utils.PodPlacementFinalizerName) {
		// Add the finalizer to the object
		log.V(1).Info("Adding finalizer to the ClusterPodPlacementConfig")
		controllerutil.AddFinalizer(clusterPodPlacementConfig, utils.PodPlacementFinalizerName)
		if err := r.Update(ctx, clusterPodPlacementConfig); err != nil {
			log.Error(err, "Unable to update finalizers in the ClusterPodPlacementConfig")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Handle the no-pod-placement-config finalizer
	ppcList := &multiarchv1beta1.PodPlacementConfigList{}
	if err := r.List(ctx, ppcList); err != nil {
		log.Error(err, "Unable to list PodPlacementConfigs")
		return ctrl.Result{}, err
	}
	shouldHavePPCFinalizer := len(ppcList.Items) > 0
	hasPPCFinalizer := controllerutil.ContainsFinalizer(clusterPodPlacementConfig, utils.CPPCNoPPCObjectFinalizer)
	if shouldHavePPCFinalizer != hasPPCFinalizer {
		if shouldHavePPCFinalizer {
			log.V(1).Info("Adding no-pod-placement-config finalizer to the ClusterPodPlacementConfig")
			controllerutil.AddFinalizer(clusterPodPlacementConfig, utils.CPPCNoPPCObjectFinalizer)
		} else {
			log.V(1).Info("Removing no-pod-placement-config finalizer from the ClusterPodPlacementConfig")
			controllerutil.RemoveFinalizer(clusterPodPlacementConfig, utils.CPPCNoPPCObjectFinalizer)
		}
		if err := r.Update(ctx, clusterPodPlacementConfig); err != nil {
			log.Error(err, "Unable to update finalizers on the ClusterPodPlacementConfig")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	if clusterPodPlacementConfig.PluginsEnabled(common.ExecFormatErrorMonitorPluginName) {
		if !controllerutil.ContainsFinalizer(clusterPodPlacementConfig, utils.ExecFormatErrorFinalizerName) {
			// Add the finalizer to the object
			log.V(1).Info("Adding ENoExecEvent finalizer to the ClusterPodPlacementConfig")
			controllerutil.AddFinalizer(clusterPodPlacementConfig, utils.ExecFormatErrorFinalizerName)
			if err := r.Update(ctx, clusterPodPlacementConfig); err != nil {
				log.Error(err, "Unable to update finalizers in the ClusterPodPlacementConfig")
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}

		// Attempt to fetch the ENoExecEvent Deployment.
		eNoExecEventDeployment := &appsv1.Deployment{}
		err := r.Get(ctx, client.ObjectKey{
			Name:      utils.EnoexecControllerName,
			Namespace: utils.Namespace(),
		}, eNoExecEventDeployment)
		if err != nil {
			// If the deployment is not found, it may be created later.
			// If any other error occurs, log it and requeue.
			log.Error(err, "Unable to fetch EnoexecController Deployment")
			if client.IgnoreNotFound(err) != nil {
				return ctrl.Result{}, err
			}
		} else {
			if !controllerutil.ContainsFinalizer(eNoExecEventDeployment, utils.ExecFormatErrorFinalizerName) {
				log.V(1).Info("Adding finalizers to the ENoExecEvent Deployment for the ENoExecEvents")
				controllerutil.AddFinalizer(eNoExecEventDeployment, utils.ExecFormatErrorFinalizerName)
				if err := r.Update(ctx, eNoExecEventDeployment); err != nil {
					log.Error(err, "Unable to update finalizers on the EnoexecController Deployment")
					// Requeue on conflict, as another process might have updated the object.
					return ctrl.Result{}, err
				}
				// After a successful update, requeue to ensure the next state is processed.
				return ctrl.Result{Requeue: true}, nil
			}
		}
	}

	err = r.dependentsStatusToClusterPodPlacementConfig(ctx, clusterPodPlacementConfig)
	if err != nil {
		log.Error(err, "Unable to retrieve the status of the PodPlacementConfig dependencies")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, r.reconcile(ctx, clusterPodPlacementConfig)
}

func (r *ClusterPodPlacementConfigReconciler) ensureNamespaceLabels(ctx context.Context) error {
	// https://kubernetes.io/docs/concepts/security/pod-security-standards/
	// https://kubernetes.io/docs/concepts/security/pod-security-admission/
	// https://kubernetes.io/docs/tasks/configure-pod-container/enforce-standards-namespace-labels/
	log := ctrllog.FromContext(ctx)
	log.V(1).Info("Ensuring namespace labels", "namespace", utils.Namespace())

	ns, err := r.ClientSet.CoreV1().Namespaces().Get(ctx, utils.Namespace(), metav1.GetOptions{})
	if err != nil {
		log.Error(err, "Unable to get the namespace")
		return err
	}
	if ns.Labels == nil {
		ns.Labels = make(map[string]string)
	}
	ns.Labels["pod-security.kubernetes.io/audit"] = "privileged"
	ns.Labels["pod-security.kubernetes.io/audit-version"] = "v1.29"
	ns.Labels["pod-security.kubernetes.io/enforce"] = "privileged"
	ns.Labels["pod-security.kubernetes.io/enforce-version"] = "v1.29"
	ns.Labels["pod-security.kubernetes.io/warn"] = "privileged"
	ns.Labels["pod-security.kubernetes.io/warn-version"] = "v1.29"
	// See https://github.com/openshift/enhancements/blob/c5b9aea25e/enhancements/workload-partitioning/management-workload-partitioning.md
	ns.Labels["workload.openshift.io/allowed"] = "management"
	log.V(2).Info("Updating the namespace labels", "labels", ns.Labels)
	_, err = r.ClientSet.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{})
	return err
}

// getCorrectHostmountAnyUIDSCC computes the SCC to use for the operator's wokrloads requiring hostPath mounts.
// OpenShift 4.19 introduced a new SCC `hostmount-anyuid-v2` with elevated privileges
// compared to `hostmount-anyuid`, allowing pods to mount specific hostPath volumes
// that may otherwise be restricted. For more details, see following bugs:
// https://issues.redhat.com/browse/OCPBUGS-55013
// https://issues.redhat.com/browse/MULTIARCH-5405
func (r *ClusterPodPlacementConfigReconciler) getCorrectHostmountAnyUIDSCC(ctx context.Context) (string, *corev1.SELinuxOptions, error) {
	log := ctrllog.FromContext(ctx)
	log.V(1).Info("Ensuring using the correct hostmount scc for podplacementconfig")

	minor, err := framework.GetClusterMinorVersion(r.ClientSet)
	if err != nil {
		return "", nil, err
	}
	// OpenShift version to Kubernetes version mapping assumption:
	// - OpenShift 4.18 maps to Kubernetes 1.31.x
	// - OpenShift 4.19 maps to Kubernetes 1.32.x and so on
	// - Assume this mapping will remain stable after GA (Generally Available) release
	// We use this check to decide which SCC (SecurityContextConstraint) to use
	if minor < 30 {
		// logic for Kubernetes < 1.30 (OpenShift < 4.17)
		return "hostmount-anyuid", nil, nil
	}

	// Because this MCO PR https://github.com/openshift/machine-config-operator/pull/4933
	// was backported to OCP 4.17 and 4.18, it is causing a regression for MTO in those versions.
	// Adding a temporary workaround here to give the pod temporary privileged SCC access.
	if minor == 30 || minor == 31 {
		return "privileged", &corev1.SELinuxOptions{
			Type: "spc_t",
		}, nil
	}

	// logic for default set for Kubernetes >= 1.32 (OpenShift >= 4.19)
	return "hostmount-anyuid-v2", &corev1.SELinuxOptions{
		Type: "spc_t",
	}, nil
}

// dependentsStatusToClusterPodPlacementConfig gathers the status of the dependents of the ClusterPodPlacementConfig object.
// The status is propagated to the ClusterPodPlacementConfig object.
func (r *ClusterPodPlacementConfigReconciler) dependentsStatusToClusterPodPlacementConfig(ctx context.Context, config *multiarchv1beta1.ClusterPodPlacementConfig) error {
	log := ctrllog.FromContext(ctx).WithValues("ClusterPodPlacementConfig", config.Name,
		"function", "updateStatus")
	podPlacementController, err := r.ClientSet.AppsV1().Deployments(utils.Namespace()).Get(ctx, utils.PodPlacementControllerName, metav1.GetOptions{})
	if client.IgnoreNotFound(err) != nil {
		log.Error(err, "Unable to get the PodPlacement controller deployment")
		return err
	}

	if config.DeletionTimestamp.IsZero() && !podPlacementController.DeletionTimestamp.IsZero() {
		// remove the finalizer in case the pod placement controller is deleted and we should reconcile it
		log.Info("Removing the finalizer from the pod-placement-controller to allow reconciliation")
		if controllerutil.RemoveFinalizer(podPlacementController, utils.PodPlacementFinalizerName) {
			if err = r.Update(ctx, podPlacementController); err != nil {
				log.Error(err, "Unable to remove the finalizer from the pod-placement-controller")
				return err
			}
		}
	}

	podPlacementWebhook, err := r.ClientSet.AppsV1().Deployments(utils.Namespace()).Get(ctx, utils.PodPlacementWebhookName, metav1.GetOptions{})
	if client.IgnoreNotFound(err) != nil {
		log.Error(err, "Unable to get the PodPlacement webhook deployment")
		return err
	}
	_, err = r.ClientSet.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, utils.PodMutatingWebhookConfigurationName, metav1.GetOptions{})
	if client.IgnoreNotFound(err) != nil {
		log.Error(err, "Unable to get the mutating webhook configuration")
		return err
	}
	config.Status.Build(
		isDeploymentAvailable(podPlacementController), isDeploymentAvailable(podPlacementWebhook),
		isDeploymentUpToDate(podPlacementController), isDeploymentUpToDate(podPlacementWebhook),
		// err == nil means the MutatingWebhookConfiguration is available
		err == nil, !config.DeletionTimestamp.IsZero())
	return nil
}

// handleDelete handles the deletion of the PodPlacement operand's resources.
func (r *ClusterPodPlacementConfigReconciler) handleDelete(ctx context.Context,
	clusterPodPlacementConfig *multiarchv1beta1.ClusterPodPlacementConfig) error {
	// The ClusterPodPlacementConfig is being deleted, cleanup the resources
	log := ctrllog.FromContext(ctx).WithValues("operation", "handleDelete")

	err := r.handleEnoexecDelete(ctx, clusterPodPlacementConfig)
	if err != nil {
		return err
	}

	// The error by the updateStatus function, if any, is ignored, as the deletion should always proceed.
	// We execute the update here because this function returns multiple times before the whole deletion process is completed.
	// Executing it here ensures that the conditions are updated throughout the deletion process.
	_ = r.updateStatus(ctx, clusterPodPlacementConfig)
	objsToDelete := []utils.ToDeleteRef{
		{
			NamespacedTypedClient: r.ClientSet.AdmissionregistrationV1().MutatingWebhookConfigurations(),
			ObjName:               utils.PodMutatingWebhookConfigurationName,
		},
		{
			NamespacedTypedClient: r.ClientSet.CoreV1().Services(utils.Namespace()),
			ObjName:               utils.PodPlacementWebhookName,
		},
		{
			NamespacedTypedClient: r.ClientSet.AppsV1().Deployments(utils.Namespace()),
			ObjName:               utils.PodPlacementWebhookName,
		},
		{
			NamespacedTypedClient: r.ClientSet.RbacV1().ClusterRoles(),
			ObjName:               utils.PodPlacementWebhookName,
		},
		{
			NamespacedTypedClient: r.ClientSet.RbacV1().ClusterRoleBindings(),
			ObjName:               utils.PodPlacementWebhookName,
		},
		{
			NamespacedTypedClient: r.ClientSet.CoreV1().ServiceAccounts(utils.Namespace()),
			ObjName:               utils.PodPlacementWebhookName,
		},
	}
	log.Info("Deleting the pod placement operand's resources")
	// NOTE: err aggregates non-nil errors, excluding NotFound errors
	if err := utils.DeleteResources(ctx, objsToDelete); err != nil {
		log.Error(err, "Unable to delete resources")
		return err
	}
	_, err = r.ClientSet.CoreV1().Services(utils.Namespace()).Get(ctx, utils.PodPlacementWebhookName, metav1.GetOptions{})
	// We look for the webhook service to ensure that the webhook has stopped communicating with the API server.
	// If the error is nil, the service was found. If the error is not nil and the error is not NotFound, some other error occurred.
	// In both the cases we return an error, to requeue the request and ensure no race conditions between the verification of the
	// pods gating status and the webhook stopping to communicate with the API server.
	if err == nil || client.IgnoreNotFound(err) != nil {
		return errors.New(waitingForWebhookSInterruptionError)
	}

	// Prevent deletion while PodPlacementConfig resources still exist
	if controllerutil.ContainsFinalizer(clusterPodPlacementConfig, utils.CPPCNoPPCObjectFinalizer) {
		log.V(1).Info("Checking for existing PodPlacementConfig resources before deletion")
		ppcList := &multiarchv1beta1.PodPlacementConfigList{}
		if err := r.List(ctx, ppcList); err != nil {
			log.Error(err, "Unable to list PodPlacementConfigs during deletion")
			return err
		}
		if len(ppcList.Items) > 0 {
			err := errors.New(RemainingPodPlacementConfig)
			log.Error(err, "Deletion blocked due to existing PodPlacementConfig resources")
			return err
		}
		// All PPCs are gone, remove the finalizer
		log.V(1).Info("No PodPlacementConfig resources found, removing no-pod-placement-config finalizer")
		controllerutil.RemoveFinalizer(clusterPodPlacementConfig, utils.CPPCNoPPCObjectFinalizer)
		if err := r.Update(ctx, clusterPodPlacementConfig); err != nil {
			log.Error(err, "Unable to remove no-pod-placement-config finalizer from ClusterPodPlacementConfig")
			return err
		}
		return nil
	}

	log.Info("Looking for pods with the scheduling gate")
	// get pending pods as we cannot query for the scheduling gate
	pods, err := r.ClientSet.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: "status.phase=Pending",
	})
	if err != nil {
		log.Error(err, "Unable to list pods")
		return err
	}
	if len(pods.Items) != 0 {
		// Check if any pods really have our scheduling gate
		found := false
		for _, pod := range pods.Items {
			for _, sg := range pod.Spec.SchedulingGates {
				log.V(2).Info("Pod has scheduling gate", "pod", pod.Name, "gate", sg.Name)
				if sg.Name == utils.SchedulingGateName {
					log.Info("Found pod with the pod placement scheduling gate", "pod", pod.Name)
					found = true
				}
			}
		}
		if found {
			return errors.New(waitingForUngatingPodsError)
		}
	}

	// The pods have been ungated and no other errors occurred, so we can remove the finalizer
	log.Info("Pods have been ungated")
	log = log.WithValues("finalizer", utils.PodPlacementFinalizerName)
	ppcDeployment := &appsv1.Deployment{}
	err = r.Get(ctx, client.ObjectKey{
		Name:      utils.PodPlacementControllerName,
		Namespace: utils.Namespace(),
	}, ppcDeployment)
	if client.IgnoreNotFound(err) != nil {
		log.Error(err, "Unable to fetch the deployment")
		return err
	}
	if err == nil && controllerutil.RemoveFinalizer(ppcDeployment, utils.PodPlacementFinalizerName) {
		log.V(2).Info("Updating the deployment")
		if err = r.Update(ctx, ppcDeployment); err != nil {
			log.Error(err, "Unable to remove the finalizer")
			return err
		}
	}
	// we can remove the finalizer in the ClusterPodPlacementConfig object now
	log.Info("Removing the finalizer from the ClusterPodPlacementConfig")
	if controllerutil.RemoveFinalizer(clusterPodPlacementConfig, utils.PodPlacementFinalizerName) {
		if err = r.Update(ctx, clusterPodPlacementConfig); err != nil {
			log.Error(err, "Unable to remove finalizers.",
				clusterPodPlacementConfig.Kind, clusterPodPlacementConfig.Name)
			return err
		}
	}
	objsToDelete = []utils.ToDeleteRef{
		{
			NamespacedTypedClient: r.ClientSet.CoreV1().Services(utils.Namespace()),
			ObjName:               utils.PodPlacementControllerName,
		},
		{
			NamespacedTypedClient: r.ClientSet.AppsV1().Deployments(utils.Namespace()),
			ObjName:               utils.PodPlacementControllerName,
		},
		{
			NamespacedTypedClient: r.ClientSet.RbacV1().ClusterRoles(),
			ObjName:               utils.PodPlacementControllerName,
		},
		{
			NamespacedTypedClient: r.ClientSet.RbacV1().ClusterRoleBindings(),
			ObjName:               utils.PodPlacementControllerName,
		},
		{
			NamespacedTypedClient: r.ClientSet.RbacV1().Roles(utils.Namespace()),
			ObjName:               utils.PodPlacementControllerName,
		},
		{
			NamespacedTypedClient: r.ClientSet.RbacV1().RoleBindings(utils.Namespace()),
			ObjName:               utils.PodPlacementControllerName,
		},
		{
			NamespacedTypedClient: r.ClientSet.CoreV1().ServiceAccounts(utils.Namespace()),
			ObjName:               utils.PodPlacementControllerName,
		},
	}

	if utils.IsResourceAvailable(ctx, r.DynamicClient, monitoringv1.SchemeGroupVersion.WithResource("servicemonitors")) {
		objsToDelete = append(objsToDelete, utils.ToDeleteRef{
			NamespacedTypedClient: utils.NewDynamicDeleter(r.DynamicClient.Resource(schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "servicemonitors"}).Namespace(utils.Namespace())),
			ObjName:               utils.PodPlacementWebhookName,
		}, utils.ToDeleteRef{
			NamespacedTypedClient: utils.NewDynamicDeleter(r.DynamicClient.Resource(schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "servicemonitors"}).Namespace(utils.Namespace())),
			ObjName:               utils.PodPlacementControllerName,
		}, utils.ToDeleteRef{
			NamespacedTypedClient: utils.NewDynamicDeleter(r.DynamicClient.Resource(schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "prometheusrules"}).Namespace(utils.Namespace())),
			ObjName:               utils.OperatorName,
		})
	}

	log.Info("Deleting the remaining resources after cleanup")
	// NOTE: err aggregates non-nil errors, excluding NotFound errors
	if err := utils.DeleteResources(ctx, objsToDelete); err != nil {
		log.Error(err, "Unable to delete resources")
		return err
	}
	return err
}

func (r *ClusterPodPlacementConfigReconciler) handleEnoexecDelete(ctx context.Context, clusterPodPlacementConfig *multiarchv1beta1.ClusterPodPlacementConfig) error {
	var err error
	log := ctrllog.FromContext(ctx, "operation", "handleEnoexecDelete")
	daemonSetRelatedObjsToDelete := []utils.ToDeleteRef{
		{
			NamespacedTypedClient: r.ClientSet.RbacV1().ClusterRoles(),
			ObjName:               utils.EnoexecDaemonSet,
		},
		{
			NamespacedTypedClient: r.ClientSet.RbacV1().ClusterRoleBindings(),
			ObjName:               utils.EnoexecDaemonSet,
		},
		{
			NamespacedTypedClient: r.ClientSet.RbacV1().Roles(utils.Namespace()),
			ObjName:               utils.EnoexecDaemonSet,
		},
		{
			NamespacedTypedClient: r.ClientSet.RbacV1().RoleBindings(utils.Namespace()),
			ObjName:               utils.EnoexecDaemonSet,
		},
		{
			NamespacedTypedClient: r.ClientSet.CoreV1().ServiceAccounts(utils.Namespace()),
			ObjName:               utils.EnoexecDaemonSet,
		},
		{
			NamespacedTypedClient: r.ClientSet.AppsV1().DaemonSets(utils.Namespace()),
			ObjName:               utils.EnoexecDaemonSet,
		},
		{
			NamespacedTypedClient: r.ClientSet.AppsV1().Deployments(utils.Namespace()),
			ObjName:               utils.EnoexecControllerName,
		},
	}
	log.Info("Deleting the DaemonSet ENoExecEvent resources and Deployment")
	if err := utils.DeleteResources(ctx, daemonSetRelatedObjsToDelete); err != nil {
		log.Error(err, "Unable to delete DaemonSet resources")
		return err
	}
	ds := &appsv1.DaemonSet{}
	err = r.Get(ctx, client.ObjectKey{
		Name:      utils.EnoexecDaemonSet,
		Namespace: utils.Namespace(),
	}, ds)
	if err == nil {
		log.Info("Waiting for the eNoExecEvent DaemonSet to be fully deleted...")
		// Return an error to force requeue. This is a common pattern for waiting.
		return errors.New("enoexec DaemonSet is still terminating")
	}
	if client.IgnoreNotFound(err) != nil {
		log.Error(err, "Failed to check for the eNoExecEvent DaemonSet status")
		return err
	}
	log.Info("eNoExecEvent DaemonSet has been deleted. Proceeding with eNoExecEvent controller cleanup.")
	enoexecEventList := &multiarchv1beta1.ENoExecEventList{}
	err = r.List(ctx, enoexecEventList, client.InNamespace(utils.Namespace()))
	if err != nil {
		log.Error(err, "Failed to list ENoExecEvent resources")
		return err
	}

	// Check if any ENoExecEvent resources were found.
	if len(enoexecEventList.Items) > 0 {
		log.Info("Found existing ENoExecEvent resources", "count", len(enoexecEventList.Items))
		return errors.New("found existing ENoExecEvent resources")
	} else {
		log.Info("No ENoExecEvent resources found in the cluster. Removing the ExecEvent finalizer from the ENoExec Deployment")
		deployment := &appsv1.Deployment{}
		err = r.Get(ctx, client.ObjectKey{
			Name:      utils.EnoexecControllerName,
			Namespace: utils.Namespace(),
		}, deployment)
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "Could not fetch ENoExecEvent Deployment")
			return err
		}
		if err == nil && controllerutil.RemoveFinalizer(deployment, utils.ExecFormatErrorFinalizerName) {
			if err = r.Update(ctx, deployment); err != nil {
				log.Error(err, "Unable to remove finalizers.",
					deployment.Kind, deployment.Name)
				return err
			}
		}
	}

	deploymentRelatedObjsToDelete := []utils.ToDeleteRef{
		{
			NamespacedTypedClient: r.ClientSet.RbacV1().Roles(utils.Namespace()),
			ObjName:               utils.EnoexecControllerName,
		},
		{
			NamespacedTypedClient: r.ClientSet.RbacV1().RoleBindings(utils.Namespace()),
			ObjName:               utils.EnoexecControllerName,
		},
		{
			NamespacedTypedClient: r.ClientSet.RbacV1().ClusterRoles(),
			ObjName:               utils.EnoexecControllerName,
		},
		{
			NamespacedTypedClient: r.ClientSet.RbacV1().ClusterRoleBindings(),
			ObjName:               utils.EnoexecControllerName,
		},
		{
			NamespacedTypedClient: r.ClientSet.CoreV1().ServiceAccounts(utils.Namespace()),
			ObjName:               utils.EnoexecControllerName,
		},
		{
			NamespacedTypedClient: r.ClientSet.CoreV1().ServiceAccounts(utils.Namespace()),
			ObjName:               utils.EnoexecDaemonSet,
		},
		{
			NamespacedTypedClient: r.ClientSet.CoreV1().Services(utils.Namespace()),
			ObjName:               utils.EnoexecControllerName,
		},
	}

	if utils.IsResourceAvailable(ctx, r.DynamicClient, monitoringv1.SchemeGroupVersion.WithResource("servicemonitors")) {
		deploymentRelatedObjsToDelete = append(deploymentRelatedObjsToDelete, utils.ToDeleteRef{
			NamespacedTypedClient: utils.NewDynamicDeleter(r.DynamicClient.Resource(schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "servicemonitors"}).Namespace(utils.Namespace())),
			ObjName:               utils.EnoexecControllerName,
		}, utils.ToDeleteRef{
			NamespacedTypedClient: utils.NewDynamicDeleter(r.DynamicClient.Resource(schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "prometheusrules"}).Namespace(utils.Namespace())),
			ObjName:               plugins.ExecFormatErrorMonitorPluginName,
		}, utils.ToDeleteRef{
			NamespacedTypedClient: utils.NewDynamicDeleter(r.DynamicClient.Resource(schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "prometheusrules"}).Namespace(utils.Namespace())),
			ObjName:               utils.ExecFormatErrorsDetected,
		})
	}

	log.Info("Deleting the ENoExecEvent Deployment resources")
	if err = utils.DeleteResources(ctx, deploymentRelatedObjsToDelete); err != nil {
		log.Error(err, "Unable to delete deployment resources")
		return err
	}
	log.Info("Removing the ENoExecEvent finalizer from the ClusterPodPlacementConfig")
	if controllerutil.RemoveFinalizer(clusterPodPlacementConfig, utils.ExecFormatErrorFinalizerName) {
		if err = r.Update(ctx, clusterPodPlacementConfig); err != nil {
			log.Error(err, "Unable to remove finalizers.",
				clusterPodPlacementConfig.Kind, clusterPodPlacementConfig.Name)
			return err
		}
	}
	return nil
}

// reconcile reconciles the ClusterPodPlacementConfig operand's resources.
func (r *ClusterPodPlacementConfigReconciler) reconcile(ctx context.Context, clusterPodPlacementConfig *multiarchv1beta1.ClusterPodPlacementConfig) error {
	log := ctrllog.FromContext(ctx)
	desiredLogLevel := int8(-clusterPodPlacementConfig.Spec.LogVerbosity.ToZapLevelInt()) // #nosec G115 -- LogVerbosity values are constrained to 0-3 by enum
	if int8(utils.AtomicLevel.Level()) != desiredLogLevel {
		log.Info("Setting log level", "level", desiredLogLevel)
		utils.AtomicLevel.SetLevel(zapcore.Level(desiredLogLevel))
	}
	if err := r.ensureNamespaceLabels(ctx); err != nil {
		log.Error(err, "Unable to ensure namespace labels")
		return errorutils.NewAggregate([]error{err, r.updateStatus(ctx, clusterPodPlacementConfig)})
	}
	clusterPodPlacementConfigObjects, err := r.buildPodPlacementConfigObjects(clusterPodPlacementConfig, ctx)
	if err != nil {
		return err
	}

	execFormatErrorObjects := []client.Object{}
	if clusterPodPlacementConfig.PluginsEnabled(common.ExecFormatErrorMonitorPluginName) {
		execFormatErrorObjects, err = r.buildENoExecEventObjects(ctx, clusterPodPlacementConfig)
		if err != nil {
			return err
		}
	} else {
		err := r.handleEnoexecDelete(ctx, clusterPodPlacementConfig)
		if err != nil {
			return err
		}
	}

	objects := append(clusterPodPlacementConfigObjects, execFormatErrorObjects...)

	// We ensure the MutatingWebHookConfiguration is created and present only if the operand is ready to serve the admission request and add/remove the scheduling gate.
	shouldEnsureMWC := clusterPodPlacementConfig.Status.CanDeployMutatingWebhook()
	shouldDeleteMWC := !shouldEnsureMWC && !clusterPodPlacementConfig.Status.IsMutatingWebhookConfigurationNotAvailable()
	if shouldEnsureMWC {
		objects = append(objects, buildMutatingWebhookConfiguration(clusterPodPlacementConfig))
	}
	if shouldDeleteMWC {
		log.Info("Deleting the mutating webhook configuration as the operand is not ready to serve the admission request or remove the scheduling gate")
		_ = r.ClientSet.AdmissionregistrationV1().MutatingWebhookConfigurations().Delete(ctx, utils.PodMutatingWebhookConfigurationName, metav1.DeleteOptions{})
	}

	// If the servicemonitors.monitoring.coreos.com CRD is available, we create the ServiceMonitor objects
	if utils.IsResourceAvailable(ctx, r.DynamicClient, monitoringv1.SchemeGroupVersion.WithResource("servicemonitors")) {
		log.V(1).Info("Creating ServiceMonitors")
		objects = append(objects,
			buildServiceMonitor(utils.PodPlacementControllerName),
			buildServiceMonitor(utils.PodPlacementWebhookName),
			buildCPPCAvailabilityAlertRule(),
		)
	} else {
		log.V(1).Info("servicemonitoring.monitoring.coreos.com is not available. Skipping the creation of the ServiceMonitors")
	}
	errs := make([]error, 0)
	for _, o := range objects {
		if err := ctrl.SetControllerReference(clusterPodPlacementConfig, o, r.Scheme); err != nil {
			log.Error(err, "Unable to set controller reference", "name", o.GetName())
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errorutils.NewAggregate(append(errs, r.updateStatus(ctx, clusterPodPlacementConfig)))
	}

	if err := utils.ApplyResources(ctx, r.ClientSet, r.DynamicClient, r.Recorder, objects); err != nil {
		log.Error(err, "Unable to apply resources")
		return errorutils.NewAggregate([]error{err, r.updateStatus(ctx, clusterPodPlacementConfig)})
	}

	return r.updateStatus(ctx, clusterPodPlacementConfig)
}

// updateStatus updates the status of the ClusterPodPlacementConfig object.
// It returns an error if the object is progressing or the status update fails. Otherwise, it returns nil.
// When it returns an error, the caller should requeue the request, unless the Reconciler is handling the deletion of the object.
func (r *ClusterPodPlacementConfigReconciler) updateStatus(ctx context.Context, config *multiarchv1beta1.ClusterPodPlacementConfig) error {
	log := ctrllog.FromContext(ctx).WithValues("ClusterPodPlacementConfig", config.Name,
		"function", "updateStatus")
	log.V(2).Info("----------------- ClusterPodPlacementConfig status report ------------------",
		"ready", config.Status.IsReady(),
		"progressing", config.Status.IsProgressing(),
		"degraded", config.Status.IsDegraded(),
		"deprovisioning", config.Status.IsDeprovisioning(),
		"podPlacementControllerNotReady", config.Status.IsPodPlacementControllerNotReady(),
		"podPlacementWebhookNotReady", config.Status.IsPodPlacementWebhookNotReady(),
		"mutatingWebhookConfigurationNotAvailable", config.Status.IsMutatingWebhookConfigurationNotAvailable())

	progressing := config.Status.IsProgressing()
	if err := r.Status().Update(ctx, config); err != nil {
		log.Error(err, "Unable to update conditions in the ClusterPodPlacementConfig")
		return err
	}
	var err error = nil
	if progressing {
		err = errors.New(clusterPodPlacementConfigNotReady)
	}
	return err
}

func (r *ClusterPodPlacementConfigReconciler) buildPodPlacementConfigObjects(clusterPodPlacementConfig *multiarchv1beta1.ClusterPodPlacementConfig, ctx context.Context) ([]client.Object, error) {
	log := ctrllog.FromContext(ctx)

	requiredSCCHostmountAnyUID, seLinuxOptionsType, err := r.getCorrectHostmountAnyUIDSCC(ctx)
	if err != nil {
		log.Error(err, "Unable to set correct hostmount SCC", "requiredSCCHostmoundAnyUID", requiredSCCHostmountAnyUID)
		return []client.Object{}, errorutils.NewAggregate([]error{err, r.updateStatus(ctx, clusterPodPlacementConfig)})
	}
	objects := []client.Object{
		// The finalizer will not affect the reconciliation of ReplicaSets and Pods
		// when updates to the ClusterPodPlacementConfig are made.
		buildService(utils.PodPlacementControllerName),
		buildService(utils.PodPlacementWebhookName),
		buildClusterRoleController(), buildClusterRoleWebhook(), buildRoleController(),
		buildServiceAccount(utils.PodPlacementWebhookName), buildServiceAccount(utils.PodPlacementControllerName),
		buildClusterRoleBinding(utils.PodPlacementControllerName, rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     clusterRoleKind,
			Name:     utils.PodPlacementControllerName,
		}, []rbacv1.Subject{
			{
				Kind:      serviceAccountKind,
				Name:      utils.PodPlacementControllerName,
				Namespace: utils.Namespace(),
			},
		}),
		buildRoleBinding(utils.PodPlacementControllerName, rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     roleKind,
			Name:     utils.PodPlacementControllerName,
		}, []rbacv1.Subject{
			{
				Kind: serviceAccountKind,
				Name: utils.PodPlacementControllerName,
			},
		}),
		buildClusterRoleBinding(utils.PodPlacementWebhookName, rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     clusterRoleKind,
			Name:     utils.PodPlacementWebhookName,
		}, []rbacv1.Subject{
			{
				Kind:      serviceAccountKind,
				Name:      utils.PodPlacementWebhookName,
				Namespace: utils.Namespace(),
			},
		}),
		buildControllerDeployment(clusterPodPlacementConfig, requiredSCCHostmountAnyUID, seLinuxOptionsType),
		buildWebhookDeployment(clusterPodPlacementConfig),
	}
	return objects, nil
}

// buildENoExecEventObjects if the ExecFormatErrorMonitor plugin is enabled create the deployment to start the controller
// if it does not already exist
func (r *ClusterPodPlacementConfigReconciler) buildENoExecEventObjects(ctx context.Context, clusterPodPlacementConfig *multiarchv1beta1.ClusterPodPlacementConfig) ([]client.Object, error) {
	log := ctrllog.FromContext(ctx)
	logVerbosityLevel := clusterPodPlacementConfig.Spec.LogVerbosity.ToZapLevelInt()

	log.Info("Starting ENoExecEvent Controller")
	objects := []client.Object{
		buildService(utils.EnoexecControllerName),
		buildServiceAccount(utils.EnoexecControllerName),
		buildServiceAccount(utils.EnoexecDaemonSet),

		// Then Roles and ClusterRoles
		buildClusterRoleENoExecEventsController(),
		buildRoleENoExecEventController(),
		buildClusterRoleENoExecEventsDaemonSet(),
		buildRoleENoExecEventDaemonSet(),

		buildClusterRoleBinding(
			utils.EnoexecControllerName,
			rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     clusterRoleKind,
				Name:     utils.EnoexecControllerName,
			},
			[]rbacv1.Subject{
				{
					Kind:      serviceAccountKind,
					Name:      utils.EnoexecControllerName,
					Namespace: utils.Namespace(),
				},
			},
		),
		buildRoleBinding(
			utils.EnoexecControllerName,
			rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     roleKind,
				Name:     utils.EnoexecControllerName,
			},
			[]rbacv1.Subject{
				{
					Kind:      serviceAccountKind,
					Name:      utils.EnoexecControllerName,
					Namespace: utils.Namespace(),
				},
			},
		),
		buildClusterRoleBinding(
			utils.EnoexecDaemonSet,
			rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     clusterRoleKind,
				Name:     utils.EnoexecDaemonSet,
			},
			[]rbacv1.Subject{
				{
					Kind:      serviceAccountKind,
					Name:      utils.EnoexecDaemonSet,
					Namespace: utils.Namespace(),
				},
			},
		),
		buildRoleBinding(
			utils.EnoexecDaemonSet,
			rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     roleKind,
				Name:     utils.EnoexecDaemonSet,
			},
			[]rbacv1.Subject{
				{
					Kind:      serviceAccountKind,
					Name:      utils.EnoexecDaemonSet,
					Namespace: utils.Namespace(),
				},
			},
		),
		buildDeploymentENoExecEventHandler(logVerbosityLevel),
		buildDaemonSetENoExecEvent(utils.EnoexecDaemonSet, utils.EnoexecDaemonSet, logVerbosityLevel),
	}
	// If the servicemonitors.monitoring.coreos.com CRD is available, we create the ServiceMonitor objects
	if utils.IsResourceAvailable(ctx, r.DynamicClient, monitoringv1.SchemeGroupVersion.WithResource("servicemonitors")) {
		log.V(1).Info("Creating ServiceMonitors")
		objects = append(objects,
			buildServiceMonitor(utils.EnoexecControllerName),
			buildExecFormatErrorAvailabilityAlertRule(),
			buildExecFormatErrorsDetectedAlertRule(),
		)
	} else {
		log.V(1).Info("servicemonitoring.monitoring.coreos.com is not available. Skipping the creation of the ServiceMonitors")
	}
	return objects, nil
}

func isDeploymentAvailable(deployment *appsv1.Deployment) bool {
	if deployment == nil {
		return false
	}
	return deployment.Status.AvailableReplicas > 0
}

func isDeploymentUpToDate(deployment *appsv1.Deployment) bool {
	if deployment == nil {
		return false
	}
	expectedReplicas := int32(-1)
	if deployment.Spec.Replicas != nil {
		expectedReplicas = *deployment.Spec.Replicas
	}
	return deployment.Status.UpdatedReplicas == expectedReplicas &&
		deployment.Status.Replicas == expectedReplicas &&
		deployment.Status.AvailableReplicas == expectedReplicas &&
		deployment.Status.ReadyReplicas == expectedReplicas &&
		deployment.Status.UnavailableReplicas == 0 &&
		deployment.Status.ObservedGeneration == deployment.Generation
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterPodPlacementConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c := ctrl.NewControllerManagedBy(mgr).
		For(&multiarchv1beta1.ClusterPodPlacementConfig{}).
		// Watch PodPlacementConfig to reconcile ClusterPodPlacementConfig only on create/delete events.
		// Updates to PodPlacementConfig are intentionally ignored.
		Watches(
			&multiarchv1beta1.PodPlacementConfig{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{
							Name: common.SingletonResourceObjectName,
						},
					},
				}
			}),
			builder.WithPredicates(predicate.Funcs{
				CreateFunc: func(e event.CreateEvent) bool {
					return true
				},
				DeleteFunc: func(e event.DeleteEvent) bool {
					return true
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					return false
				},
			}),
		).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.DaemonSet{}).
		Owns(&corev1.Service{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&admissionv1.MutatingWebhookConfiguration{})
	if utils.IsResourceAvailable(context.Background(), r.DynamicClient,
		monitoringv1.SchemeGroupVersion.WithResource("servicemonitors")) {
		c = c.Owns(&monitoringv1.ServiceMonitor{}).Owns(&monitoringv1.PrometheusRule{})
	}
	return c.Complete(r)
}
