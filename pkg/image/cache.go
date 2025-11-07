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
	"encoding/hex"
	"hash/fnv"
	"time"

	"github.com/openshift/multiarch-tuning-operator/pkg/image/metrics"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"k8s.io/apimachinery/pkg/util/sets"

	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

type cacheProxy struct {
	registryInspector IRegistryInspector
	imageRefsCache    *expirable.LRU[string, sets.Set[string]] // LRU cache with expirable keys
}

func (c *cacheProxy) GetCompatibleArchitecturesSet(ctx context.Context, imageReference string,
	skipCache bool, secrets [][]byte) (sets.Set[string], error) {
	metrics.InitCommonMetrics()
	metrics.InspectionGauge.Set(float64(c.imageRefsCache.Len()))
	now := time.Now()
	authJSON, err := marshaledImagePullSecrets(imageReference, secrets)
	if err != nil {
		return nil, err
	}

	log := ctrllog.FromContext(ctx).WithValues("imageReference", imageReference)
	hash := computeFNV128Hash(imageReference, authJSON)
	if architectures, ok := c.imageRefsCache.Get(hash); ok && !skipCache {
		log.V(3).Info("Cache hit", "architectures", architectures, "hash", hash)
		defer utils.HistogramObserve(now, metrics.TimeToInspectImageGivenHit)
		return architectures, nil
	}
	architectures, err := c.registryInspector.GetCompatibleArchitecturesSet(ctx, imageReference, true, secrets)
	if err != nil {
		return nil, err
	}

	log.V(3).Info("Cache miss...adding to cache", "architectures", architectures, "hash", hash)
	if !skipCache {
		c.imageRefsCache.Add(hash, architectures)
	}
	defer utils.HistogramObserve(now, metrics.TimeToInspectImageGivenMiss)
	return architectures, nil
}

func (c *cacheProxy) GetRegistryInspector() IRegistryInspector {
	return c.registryInspector
}

// clearCache purges the image metadata cache
func (c *cacheProxy) clearCache() {
	c.imageRefsCache.Purge()
}

func newCacheProxy() *cacheProxy {
	return &cacheProxy{
		registryInspector: newRegistryInspector(),
		imageRefsCache:    expirable.NewLRU[string, sets.Set[string]](256, nil, time.Hour*6),
	}
}

func computeFNV128Hash(imageReference string, secrets []byte) string {
	hash := fnv.New128()
	hash.Write([]byte(imageReference)) // Add the image reference
	hash.Write(secrets)                // Add the secrets

	return hex.EncodeToString(hash.Sum(nil))
}
