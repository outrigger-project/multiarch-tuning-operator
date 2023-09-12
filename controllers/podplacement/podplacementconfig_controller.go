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
	"errors"
	multiarchv1alpha1 "multiarch-operator/apis/multiarch/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// PodPlacementConfigReconciler reconciles a PodPlacementConfig object
type PodPlacementConfigReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	ClientSet *kubernetes.Clientset
}

const (
	// Pod MutatingWebHook name
	podMutatingWebhookName                       = "pod-placement-scheduling-gate.multiarch.openshift.io"
	operatorMutatingWebhookConfigurationselector = "multiarch.openshift.io/webhook=mutating"
)

//+kubebuilder:rbac:groups=multiarch.openshift.io,resources=podplacementconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=multiarch.openshift.io,resources=podplacementconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=multiarch.openshift.io,resources=podplacementconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,verbs=get;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the PodPlacementConfig object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *PodPlacementConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)

	// Lookup the PodPlacementConfig instance for this reconcile request
	podPlacementConfig := &multiarchv1alpha1.PodPlacementConfig{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: req.NamespacedName.Namespace,
		Name:      req.NamespacedName.Name,
	}, podPlacementConfig); err != nil {
		log.Error(err, "Unable to fetch PodPlacementConfig")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if podPlacementConfig.Name != "cluster" {
		err := errors.New("no PodPlacementConfig with name different than cluster should be created")
		log.Error(err, "PodPlacementConfig name is not cluster")
		return ctrl.Result{}, err
	}

	if err := r.updateMutatingAdmissionWebhook(ctx, podPlacementConfig); err != nil {
		// TODO: implement backoff retry
		return ctrl.Result{}, err
	}

	// TODO: Any update to the PodPlacementConfig we should consider?
	err := r.Client.Update(ctx, podPlacementConfig)
	if err != nil {
		log.Error(err, "Unable to update the podPlacementConfig")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *PodPlacementConfigReconciler) updateMutatingAdmissionWebhook(ctx context.Context, ppc *multiarchv1alpha1.PodPlacementConfig) error {
	log := ctrllog.FromContext(ctx)

	// getting by label as the name for the mutating webhook can change based on the kustomization
	podPlacementWebhooks, err := r.ClientSet.AdmissionregistrationV1().MutatingWebhookConfigurations().List(ctx, metav1.ListOptions{
		LabelSelector: operatorMutatingWebhookConfigurationselector,
	})
	if err != nil {
		log.Error(err, "Unable to fetch mutating webhook")
		return err
	}
	if len(podPlacementWebhooks.Items) != 1 {
		err := errors.New("the length of the list of mutating webhooks is not 1")
		log.Error(err, "Unable to fetch mutating webhook", "length", len(podPlacementWebhooks.Items))
		return err
	}
	podPlacementWebhook := &podPlacementWebhooks.Items[0]
	for _, webhook := range podPlacementWebhook.Webhooks {
		if webhook.Name == podMutatingWebhookName {
			webhook.NamespaceSelector = ppc.Spec.NamespaceSelector
			break
		}
	}

	podPlacementWebhook, err = r.ClientSet.AdmissionregistrationV1().MutatingWebhookConfigurations().Update(ctx, podPlacementWebhook, metav1.UpdateOptions{})
	if err != nil {
		log.Error(err, "Unable to update MutatingWebhookConfiguration", "name", podPlacementWebhook.Name)
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodPlacementConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&multiarchv1alpha1.PodPlacementConfig{}).
		Complete(r)
}
