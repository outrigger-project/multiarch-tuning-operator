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

package common

// +k8s:deepcopy-gen=package

// Priority represents the priority configuration.
// priority field indicates the priority of in range 1-100.
// +kubebuilder:"validation:Required
// +kubebuilder:validation:Minimum:=1
// +kubebuilder:validation:Maximum:=100
type Priority int32
