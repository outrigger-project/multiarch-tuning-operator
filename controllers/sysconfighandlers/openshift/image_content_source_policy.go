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

package openshift

import (
	"context"
	"errors"
	"fmt"
	"multiarch-operator/pkg/systemconfig"

	"github.com/go-logr/logr"
	ocpv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// ICSPs report the set of registry sources that the cluster needs to reach via mirrors.
// Each registry source can have multiple mirrors.
// The ICSPSyncer watches ICSPs and updates the registry mirroring config accordingly by using
// the SystemConfigSyncer.
// The configuration written by the SystemConfigSyncer due to the ICSPSyncer is stored in-memory in the
// SystemConfigSyncer.registriesConfContent (type registriesConf) and written to disk in the /$conf_dir/container/registries.conf file.

// In particular, an example of the configuration written by the SystemConfigSyncer due to the ICSPSyncer in /$conf_dir/containers/registries.conf is:
// [[registries]]
//
//	location = "registry.redhat.io"
//	prefix = ""
//	mirror = ["myregistry.example.com"]
//
// [[registries]]
//
//	location = "docker.io"
//	prefix = ""
//	mirror = ["myregistry.example.com"]
//

type ICSPSyncer struct {
	mgr manager.Manager
	log logr.Logger
}

func NewICSPSyncer(mgr manager.Manager) *ICSPSyncer {
	return &ICSPSyncer{
		mgr: mgr,
	}
}

func (s *ICSPSyncer) Start(ctx context.Context) (err error) {
	s.log = log.FromContext(ctx, "handler", "ICSPSyncer", "kind",
		"ImageContentSourcePolicy [operator.openshift.io/v1alpha1]")
	s.log.Info("Starting System Config Syncer")
	mgr := s.mgr
	ic := systemconfig.SystemConfigSyncerSingleton()
	// Watch ICSPs and Sync SystemConfig
	icspInformer, err := mgr.GetCache().GetInformerForKind(ctx, ocpv1alpha1.GroupVersion.WithKind("ImageContentSourcePolicy"))
	if err != nil {
		s.log.Error(err, "Error getting informer for ImageContentSourcePolicy")
		return err
	}
	_, err = icspInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.onAdd(ic),
		UpdateFunc: s.onUpdate(ic),
		DeleteFunc: s.onDelete(ic),
	})
	if err != nil {
		s.log.Error(err, "Error registering handler for ImageContentSourcePolicy")
		return err
	}
	return nil
}

func (s *ICSPSyncer) onAdd(ic systemconfig.IConfigSyncer) func(obj interface{}) {
	return func(obj interface{}) {
		icsp, ok := obj.(*ocpv1alpha1.ImageContentSourcePolicy)
		if !ok {
			s.log.Error(errors.New("unexpected type, expected ImageContentSourcePolicy"), "unexpected type",
				"type", fmt.Sprintf("%T", obj))
			return
		}
		for _, source := range icsp.Spec.RepositoryDigestMirrors {
			err := ic.UpdateRegistryMirroringConfig(source.Source, source.Mirrors, systemconfig.PullTypeDigestOnly)
			if err != nil {
				s.log.Error(err, "Error updating registry mirroring config",
					"name", icsp.Name, "source", source.Source)
			}
		}
	}
}

func (s *ICSPSyncer) onDelete(ic systemconfig.IConfigSyncer) func(obj interface{}) {
	return func(obj interface{}) {
		icsp, ok := obj.(*ocpv1alpha1.ImageContentSourcePolicy)
		if !ok {
			s.log.Error(errors.New("unexpected type, expected ImageContentSourcePolicy"), "unexpected type",
				"type", fmt.Sprintf("%T", obj))
			return
		}
		for _, source := range icsp.Spec.RepositoryDigestMirrors {
			err := ic.DeleteRegistryMirroringConfig(source.Source)
			if err != nil {
				s.log.Error(err, "Error removing registry mirroring config",
					"name", icsp.Name, "source", source.Source)
			}
		}
	}
}

func (s *ICSPSyncer) onUpdate(ic systemconfig.IConfigSyncer) func(oldobj, newobj interface{}) {
	return func(oldobj, newobj interface{}) {
		s.onDelete(ic)(oldobj)
		s.onAdd(ic)(newobj)
	}
}
