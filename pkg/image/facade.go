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
	"sync"

	"k8s.io/apimachinery/pkg/util/sets"
)

var (
	singletonImageFacade *Facade
	// once is used for lazy initialization of the singletonImageFacade
	once sync.Once
)

type Facade struct {
	inspectionCache       ICache
	storeGlobalPullSecret func(pullSecret []byte)
	clearCache            func()
}

func (i *Facade) GetCompatibleArchitecturesSet(ctx context.Context, imageReference string, skipCache bool, secrets [][]byte) (architectures sets.Set[string], err error) {
	return i.inspectionCache.GetCompatibleArchitecturesSet(ctx, imageReference, skipCache, secrets)
}

func (i *Facade) StoreGlobalPullSecret(pullSecret []byte) {
	i.storeGlobalPullSecret(pullSecret)
	i.clearCache()
}

func newImageFacade() *Facade {
	inspectionCache := newCacheProxy()
	return &Facade{
		inspectionCache:       inspectionCache,
		storeGlobalPullSecret: inspectionCache.registryInspector.storeGlobalPullSecret,
		clearCache:            inspectionCache.clearCache,
	}
}

func FacadeSingleton() *Facade {
	once.Do(func() {
		singletonImageFacade = newImageFacade()
	})
	return singletonImageFacade
}
