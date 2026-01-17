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

package v1beta1

import (
	"context"
	"errors"

	runtime "k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-multiarch-openshift-io-v1beta1-clusterpodplacementconfig,mutating=false,failurePolicy=fail,sideEffects=None,groups=multiarch.openshift.io,resources=clusterpodplacementconfigs,verbs=create;update,versions=v1beta1,name=validate-clusterpodplacementconfig.multiarch.openshift.io,admissionReviewVersions=v1

func (c *ClusterPodPlacementConfig) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(c).
		WithValidator(&ClusterPodPlacementConfigValidator{}).
		Complete()
}

type ClusterPodPlacementConfigValidator struct {
}

func (v *ClusterPodPlacementConfigValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (warnings admission.Warnings, err error) {
	return v.validate(obj)
}

func (v *ClusterPodPlacementConfigValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (warnings admission.Warnings, err error) {
	return v.validate(newObj)
}

func (v *ClusterPodPlacementConfigValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (warnings admission.Warnings, err error) {
	return nil, nil
}

func (v *ClusterPodPlacementConfigValidator) validate(obj runtime.Object) (warnings admission.Warnings, err error) {
	cppc, ok := obj.(*ClusterPodPlacementConfig)
	if !ok {
		return nil, errors.New("not a ClusterPodPlacementConfig")
	}
	if cppc.Spec.Plugins == nil || cppc.Spec.Plugins.NodeAffinityScoring == nil {
		return nil, nil
	}
	// Verify unique Architecture terms
	platforms := make(map[string]struct{})
	for _, term := range cppc.Spec.Plugins.NodeAffinityScoring.Platforms {
		if _, ok := platforms[term.Architecture]; ok {
			return nil, errors.New("duplicate architecture in the .spec.plugins.nodeAffinityScoring.platforms list")
		}
		platforms[term.Architecture] = struct{}{}
	}
	return nil, nil
}
