/*
Copyright 2024 Red Hat, Inc.

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
	"strings"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
)

type NamespaceCache struct {
	mgr          manager.Manager
	namespaceMap sets.Set[string]
	log          logr.Logger
}

func NewNamespaceCache(mgr manager.Manager) *NamespaceCache {
	return &NamespaceCache{
		mgr:          mgr,
		namespaceMap: sets.New[string](),
	}
}

func (s *NamespaceCache) Start(ctx context.Context) (err error) {
	s.log = log.FromContext(ctx, "handler", "NamespaceCache", "kind", "Namespace")
	s.log.Info("Starting Namespace informer")
	// Setup an event handler for the informer
	s.namespaceMap.Insert("openshift-route-controller-manager", "openshift-ingress-canary")
	namespaceInformer, err := s.mgr.GetCache().GetInformer(ctx, &v1.Namespace{})
	if err != nil {
		s.log.Error(err, "Error getting the informer")
		return err
	}
	_, err = namespaceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.onAdd(),
		DeleteFunc: s.onDelete(),
	})

	if err != nil {
		s.log.Error(err, "Error registering handler")
		return err
	}

	return nil
}

func (s *NamespaceCache) onAdd() func(interface{}) {
	return func(obj interface{}) {
		ns := obj.(*v1.Namespace)
		if len(ns.OwnerReferences) != 0 && (ns.Name != "openshift-marketplace" && ns.Name != "openshift-operators") {
			// Exclude namespaces managed by CVO and networking operators
			if ns.OwnerReferences[0].Kind == "ClusterVersion" || ns.OwnerReferences[0].Kind == "Network" {
				s.namespaceMap.Insert(ns.Name)
				s.log.V(5).Info("Added namespace ", ns.Name, " to map")
			}
		}
	}
}

func (s *NamespaceCache) onDelete() func(interface{}) {
	return func(obj interface{}) {
		ns := obj.(*v1.Namespace)
		if s.namespaceMap.Has(ns.Name) {
			s.namespaceMap.Delete(ns.Name)
			s.log.V(5).Info("Deleted namespace ", ns.Name, " from map")
		}
	}
}

func (s *NamespaceCache) ShouldExcludeNamespace(namespaceName string) bool {
	if s.namespaceMap.Has(namespaceName) {
		return true
	}
	if (!strings.HasSuffix(namespaceName, "-operator")) && (namespaceName != "openshift-marketplace" && namespaceName != "openshift-operators") {
		if s.namespaceMap.Has(namespaceName + "-operator") {
			return true
		}
	}
	return false
}
