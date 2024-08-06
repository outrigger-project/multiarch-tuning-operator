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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

// PodReconciler reconciles a Pod object
type PodReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	ClientSet *kubernetes.Clientset
}

// RBACs for the operands' controllers are added manually because kubebuilder can't handle multiple service accounts
// and roles.
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch
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
// Reconcile has to watch the pod object if it has the scheduling gate with name schedulingGateName,
// inspect the images in the pod spec, update the nodeAffinity accordingly and remove the scheduling gate.
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)

	pod := &Pod{
		ctx: ctx,
	}

	if err := r.Get(ctx, req.NamespacedName, &pod.Pod); err != nil {
		log.V(4).Info("Unable to fetch pod", "error", err)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// verify whether the pod has the scheduling gate
	if !pod.HasSchedulingGate() {
		log.V(4).Info("Pod does not have the scheduling gate. Ignoring...")
		// if not, return
		return ctrl.Result{}, nil
	}

	// The scheduling gate is found.
	log.V(3).Info("Processing pod")

	// Prepare the requirement for the node affinity.
	psdl, err := r.pullSecretDataList(ctx, pod)
	if err != nil {
		log.Error(err, "Unable to retrieve the image pull secret data for the pod. "+
			"The nodeAffinity for this pod will not be set.")
		// we still need to remove the scheduling gate. Therefore, we do not return here.
	} else {
		pod.SetNodeAffinityArchRequirement(psdl)
	}

	// Remove the scheduling gate
	log.V(3).Info("Removing the scheduling gate from pod.")
	pod.RemoveSchedulingGate()

	err = r.Client.Update(ctx, &pod.Pod)
	if err != nil {
		log.Error(err, "Unable to update the pod")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// pullSecretDataList returns the list of secrets data for the given pod given its imagePullSecrets field
func (r *PodReconciler) pullSecretDataList(ctx context.Context, pod *Pod) ([][]byte, error) {
	log := ctrllog.FromContext(ctx)
	secretAuths := make([][]byte, 0)
	secretList := pod.GetPodImagePullSecrets()
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
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Complete(r)
}
