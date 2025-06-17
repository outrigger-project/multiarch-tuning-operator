/*
Copyright 2025 Red Hat, Inc.

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

package v1beta1

import (
	"context"
	"errors"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	admission "sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-multiarch-openshift-io-v1beta1-podplacementconfig,mutating=false,failurePolicy=fail,sideEffects=None,groups=multiarch.openshift.io,resources=podplacementconfigs,verbs=create;update,versions=v1beta1,name=validate-podplacementconfig.multiarch.openshift.io,admissionReviewVersions=v1

func (c *PodPlacementConfig) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(c).
		WithValidator(&PodPlacementConfigValidator{}).
		Complete()
}

type PodPlacementConfigValidator struct {
}

func (v *PodPlacementConfigValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return v.validate(ctx, obj)
}

func (v *PodPlacementConfigValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	return v.validate(ctx, newObj)
}

func (v *PodPlacementConfigValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (v *PodPlacementConfigValidator) validate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	ppc, ok := obj.(*PodPlacementConfig)
	if !ok {
		return nil, errors.New("object is not a PodPlacementConfig")
	}

	// Check if any ClusterPodPlacementConfig exists in the cluster
	var clusterConfigs ClusterPodPlacementConfigList

	if len(clusterConfigs.Items) == 0 {
		return nil, errors.New("cannot create PodPlacementConfig: no ClusterPodPlacementConfig exists")
	}

	// duplicate architecture check like in cluster validator
	if ppc.Spec.Plugins != nil && ppc.Spec.Plugins.NodeAffinityScoring != nil {
		platforms := make(map[string]struct{})
		for _, term := range ppc.Spec.Plugins.NodeAffinityScoring.Platforms {
			if _, ok := platforms[term.Architecture]; ok {
				return nil, errors.New("duplicate architecture in the .spec.plugins.nodeAffinityScoring.platforms list")
			}
			platforms[term.Architecture] = struct{}{}
		}
	}

	return nil, nil
}
