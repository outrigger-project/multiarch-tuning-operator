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

package v1alpha1

import (
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	multiarchv1beta1 "github.com/openshift/multiarch-tuning-operator/api/v1beta1"
)

const FallbackArchAnnotation = "multiarch.openshift.io/fallback-architecture"

// ConvertTo converts this ClusterPodPlacementConfig to the Hub version v1beta1.
func (src *ClusterPodPlacementConfig) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*multiarchv1beta1.ClusterPodPlacementConfig)

	// ObjectMeta
	dst.ObjectMeta = src.ObjectMeta

	// Spec
	dst.Spec.LogVerbosity = src.Spec.LogVerbosity
	dst.Spec.NamespaceSelector = src.Spec.NamespaceSelector
	// Restore FallbackArchitecture from the annotation, if present.
	// This ensures the value is preserved across API version conversions.
	if arch, ok := src.Annotations[FallbackArchAnnotation]; ok {
		dst.Spec.FallbackArchitecture = arch
	}

	// Status
	dst.Status.Conditions = src.Status.Conditions

	// +kubebuilder:docs-gen:collapse=rote conversion
	return nil
}

/*
ConvertFrom is expected to modify its receiver to contain the converted object.
Most of the conversion is straightforward copying, except for converting our changed field.
*/

// ConvertFrom converts from the Hub version (v1beta1) to this.
func (dst *ClusterPodPlacementConfig) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*multiarchv1beta1.ClusterPodPlacementConfig)

	// ObjectMeta
	dst.ObjectMeta = src.ObjectMeta
	// v1alpha1 does not have the FallbackArchitecture field.
	// Preserve the value in an annotation to avoid data loss during
	// v1beta1 -> v1alpha1 -> v1beta1 round-trip conversions.
	if dst.Annotations == nil {
		dst.Annotations = make(map[string]string)
	}
	if src.Spec.FallbackArchitecture != "" {
		dst.Annotations[FallbackArchAnnotation] = src.Spec.FallbackArchitecture
	} else {
		delete(dst.Annotations, FallbackArchAnnotation)
	}

	// Spec
	dst.Spec.LogVerbosity = src.Spec.LogVerbosity
	dst.Spec.NamespaceSelector = src.Spec.NamespaceSelector

	// Status
	dst.Status.Conditions = src.Status.Conditions

	if dst.Annotations == nil {
		dst.Annotations = make(map[string]string)
	}

	return nil
}
