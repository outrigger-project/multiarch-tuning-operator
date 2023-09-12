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

package image

import (
	"context"

	"k8s.io/apimachinery/pkg/util/sets"
)

type ICache interface {
	// GetCompatibleArchitecturesSet takes an image reference. a list of secrets and the client to the cluster and
	// returns a set of architectures that are compatible with the image reference.
	GetCompatibleArchitecturesSet(ctx context.Context, imageReference string, secrets [][]byte) (sets.Set[string], error)
}

type IRegistryInspector interface {
	ICache
	// StoreGlobalPullSecret takes a pull secret and stores it in the ImageFacade. It will be used by the controller
	// in charge of watching the global pull secret and to store it in the ImageFacade's relevant private field.
	// Then, the ImageFacade will be responsible for consuming it during the inspection.
	StoreGlobalPullSecret(pullSecret []byte)
}
