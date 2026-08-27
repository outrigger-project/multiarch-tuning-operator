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

package operator

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/multiarch-tuning-operator/api/common"
	multiarchv1beta1 "github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	. "github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

// getStub implements client.Reader.Get only. The helper under test never calls other methods.
type getStub struct {
	client.Reader
	obj    client.Object
	err    error
	called *bool
	gotKey *client.ObjectKey
}

func (g getStub) Get(_ context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
	if g.called != nil {
		*g.called = true
	}
	if g.gotKey != nil {
		*g.gotKey = key
	}
	if g.err != nil {
		return g.err
	}
	if g.obj == nil {
		return apierrors.NewNotFound(appsv1.Resource("deployments"), obj.GetName())
	}
	switch dst := obj.(type) {
	case *appsv1.Deployment:
		src, ok := g.obj.(*appsv1.Deployment)
		if !ok {
			return apierrors.NewBadRequest("expected Deployment")
		}
		*dst = *src
	case *multiarchv1beta1.ClusterPodPlacementConfig:
		src, ok := g.obj.(*multiarchv1beta1.ClusterPodPlacementConfig)
		if !ok {
			return apierrors.NewBadRequest("expected ClusterPodPlacementConfig")
		}
		*dst = *src
	default:
		return apierrors.NewBadRequest("unsupported object type")
	}
	return nil
}

func testDeployment(from string, uid types.UID) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.PodPlacementControllerName,
			Namespace: utils.Namespace(),
			UID:       uid,
			Labels:    map[string]string{"from": from},
		},
	}
}

func testCPPC(from string, uid types.UID) *multiarchv1beta1.ClusterPodPlacementConfig {
	return &multiarchv1beta1.ClusterPodPlacementConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:   common.SingletonResourceObjectName,
			UID:    uid,
			Labels: map[string]string{"from": from},
		},
	}
}

func TestGetCacheWithAPIFallback(t *testing.T) {
	ctx := context.Background()
	key := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.PodPlacementControllerName,
			Namespace: utils.Namespace(),
		},
	}

	t.Run("returns from cache when Get finds an object with a UID", func(t *testing.T) {
		apiCalled := false
		obj := key.DeepCopy()
		err := getCacheWithAPIFallback(ctx,
			getStub{obj: testDeployment("cache", "uid-cache")},
			getStub{obj: testDeployment("api", "uid-api"), called: &apiCalled},
			obj)
		if err != nil {
			t.Fatalf("expected cache hit, got error: %v", err)
		}
		if apiCalled {
			t.Fatal("APIReader should not be called on cache hit")
		}
		if obj.Labels["from"] != "cache" {
			t.Fatalf("expected cached object, got labels %v", obj.Labels)
		}
	})

	t.Run("does not fall back when cache has a UID even if APIReader has a different object", func(t *testing.T) {
		// A populated cache hit is trusted. Stale-but-present objects (for example a
		// missing deletionTimestamp) are not detected by this helper.
		apiCalled := false
		obj := key.DeepCopy()
		err := getCacheWithAPIFallback(ctx,
			getStub{obj: testDeployment("cache", "uid-stale")},
			getStub{obj: testDeployment("api", "uid-fresh"), called: &apiCalled},
			obj)
		if err != nil {
			t.Fatalf("expected cache hit, got error: %v", err)
		}
		if apiCalled {
			t.Fatal("APIReader should not be called on a cache hit with a UID")
		}
		if obj.UID != "uid-stale" {
			t.Fatalf("expected cached UID, got %q", obj.UID)
		}
	})

	t.Run("falls back to APIReader when cache Get is NotFound", func(t *testing.T) {
		cacheCalled := false
		apiCalled := false
		obj := key.DeepCopy()
		err := getCacheWithAPIFallback(ctx,
			getStub{called: &cacheCalled},
			getStub{obj: testDeployment("api", "uid-api"), called: &apiCalled},
			obj)
		if err != nil {
			t.Fatalf("expected APIReader fallback, got error: %v", err)
		}
		if !cacheCalled {
			t.Fatal("cache Get should be called first")
		}
		if !apiCalled {
			t.Fatal("APIReader should be called on cache NotFound")
		}
		if obj.Labels["from"] != "api" {
			t.Fatalf("expected APIReader object, got labels %v", obj.Labels)
		}
	})

	t.Run("falls back to APIReader when cache Get is empty (no UID)", func(t *testing.T) {
		obj := key.DeepCopy()
		err := getCacheWithAPIFallback(ctx,
			getStub{obj: testDeployment("cache", "")},
			getStub{obj: testDeployment("api", "uid-api")},
			obj)
		if err != nil {
			t.Fatalf("expected APIReader fallback for empty cache object, got error: %v", err)
		}
		if obj.Labels["from"] != "api" {
			t.Fatalf("expected APIReader object, got labels %v", obj.Labels)
		}
	})

	t.Run("looks up the object using Name and Namespace set on obj", func(t *testing.T) {
		var cacheKey, apiKey client.ObjectKey
		obj := key.DeepCopy()
		err := getCacheWithAPIFallback(ctx,
			getStub{gotKey: &cacheKey},
			getStub{obj: testDeployment("api", "uid-api"), gotKey: &apiKey},
			obj)
		if err != nil {
			t.Fatalf("expected APIReader fallback, got error: %v", err)
		}
		want := client.ObjectKey{Name: utils.PodPlacementControllerName, Namespace: utils.Namespace()}
		if cacheKey != want {
			t.Fatalf("cache Get key = %#v, want %#v", cacheKey, want)
		}
		if apiKey != want {
			t.Fatalf("APIReader Get key = %#v, want %#v", apiKey, want)
		}
	})

	t.Run("returns NotFound when missing from cache and APIReader", func(t *testing.T) {
		obj := key.DeepCopy()
		err := getCacheWithAPIFallback(ctx, getStub{}, getStub{}, obj)
		if !apierrors.IsNotFound(err) {
			t.Fatalf("expected NotFound, got %v", err)
		}
	})

	t.Run("returns APIReader error when fallback Get fails", func(t *testing.T) {
		obj := key.DeepCopy()
		err := getCacheWithAPIFallback(ctx,
			getStub{},
			getStub{err: apierrors.NewServiceUnavailable("api unavailable")},
			obj)
		if !apierrors.IsServiceUnavailable(err) {
			t.Fatalf("expected ServiceUnavailable from APIReader, got %v", err)
		}
	})

	t.Run("does not fall back on unexpected cache errors", func(t *testing.T) {
		apiCalled := false
		obj := key.DeepCopy()
		err := getCacheWithAPIFallback(ctx,
			getStub{err: apierrors.NewServiceUnavailable("cache unavailable")},
			getStub{obj: testDeployment("api", "uid-api"), called: &apiCalled},
			obj)
		if !apierrors.IsServiceUnavailable(err) {
			t.Fatalf("expected ServiceUnavailable, got %v", err)
		}
		if apiCalled {
			t.Fatal("APIReader should not be called on unexpected cache error")
		}
	})

	t.Run("returns cache NotFound when APIReader is nil", func(t *testing.T) {
		obj := key.DeepCopy()
		err := getCacheWithAPIFallback(ctx, getStub{}, nil, obj)
		if !apierrors.IsNotFound(err) {
			t.Fatalf("expected NotFound, got %v", err)
		}
	})

	t.Run("returns NotFound when cache object has no UID and APIReader is nil", func(t *testing.T) {
		obj := key.DeepCopy()
		err := getCacheWithAPIFallback(ctx, getStub{obj: testDeployment("cache", "")}, nil, obj)
		if !apierrors.IsNotFound(err) {
			t.Fatalf("expected NotFound, got %v", err)
		}
	})

	t.Run("falls back for ClusterPodPlacementConfig singleton cache miss", func(t *testing.T) {
		obj := NewClusterPodPlacementConfig().WithName(common.SingletonResourceObjectName).Build()
		var cacheKey client.ObjectKey
		err := getCacheWithAPIFallback(ctx,
			getStub{gotKey: &cacheKey},
			getStub{obj: testCPPC("api", "uid-api")},
			obj)
		if err != nil {
			t.Fatalf("expected APIReader fallback, got error: %v", err)
		}
		if cacheKey != (client.ObjectKey{Name: common.SingletonResourceObjectName}) {
			t.Fatalf("cache Get key = %#v, want name %q", cacheKey, common.SingletonResourceObjectName)
		}
		if obj.Labels["from"] != "api" {
			t.Fatalf("expected APIReader object, got labels %v", obj.Labels)
		}
	})
}
