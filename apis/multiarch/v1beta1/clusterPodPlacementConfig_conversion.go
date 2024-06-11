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
	multiarchv1alpha1 "github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// +kubebuilder:docs-gen:collapse=Imports

// ConvertTo converts this ClusterPodPlacementConfig to the Hub version v1alpha1.
func (src *ClusterPodPlacementConfig) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*multiarchv1alpha1.ClusterPodPlacementConfig)

	// ObjectMeta
	dst.ObjectMeta = src.ObjectMeta

	// Spec
	dst.Spec.LogVerbosity = multiarchv1alpha1.LogVerbosityLevel(src.Spec.LogVerbosity)
	dst.Spec.NamespaceSelector = src.Spec.NamespaceSelector

	// Status
	dst.Status.Conditions = src.Status.Conditions

	// +kubebuilder:docs-gen:collapse=rote conversion
	return nil
}

/*
ConvertFrom is expected to modify its receiver to contain the converted object.
Most of the conversion is straightforward copying, except for converting our changed field.
*/

// ConvertFrom converts from the Hub version (1valpha1) to this 1vbeta1.
func (dst *ClusterPodPlacementConfig) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*multiarchv1alpha1.ClusterPodPlacementConfig)

	// ObjectMeta
	dst.ObjectMeta = src.ObjectMeta

	// Spec
	dst.Spec.LogVerbosity = LogVerbosityLevel(src.Spec.LogVerbosity)
	dst.Spec.NamespaceSelector = src.Spec.NamespaceSelector

	// Status
	dst.Status.Conditions = src.Status.Conditions

	if dst.ObjectMeta.Annotations == nil {
		dst.ObjectMeta.Annotations = make(map[string]string)
	}

	return nil
}
