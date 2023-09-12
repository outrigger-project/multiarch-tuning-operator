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
	"multiarch-operator/pkg/system_config"

	"github.com/go-logr/logr"
	v1 "github.com/openshift/api/config/v1"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// ITMSs report the set of registry sources that the cluster needs to reach via mirrors.
// Each registry source can have multiple mirrors.
// The ICSPSyncer watches ICSPs and updates the registry mirroring config accordingly by using
// the SystemConfigSyncer.
// The configuration written by the SystemConfigSyncer due to the ICSPSyncer is stored in-memory in the
// SystemConfigSyncer.registriesConfContent (type registriesConf) and written to disk in the /$conf_dir/container/registries.conf file.

// In particular, an example of the configuration written by the SystemConfigSyncer due to the ITMSSyncer in /$conf_dir/containers/registries.conf is:
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

type ITMSSyncer struct {
	mgr manager.Manager
	log logr.Logger
}

func NewITMSSyncer(mgr manager.Manager) *ITMSSyncer {
	return &ITMSSyncer{
		mgr: mgr,
	}
}

func (s *ITMSSyncer) Start(ctx context.Context) (err error) {
	s.log = log.FromContext(ctx, "handler", "ITMSSyncer", "kind", "ImageTagMirrorSet [config.openshift.io/v1]")
	s.log.Info("Starting System Config Syncer")
	mgr := s.mgr
	ic := system_config.SystemConfigSyncerSingleton()
	icspInformer, err := mgr.GetCache().GetInformerForKind(ctx, v1.GroupVersion.WithKind("ImageTagMirrorSet"))
	if err != nil {
		s.log.Error(err, "Error getting informer for ImageTagMirrorSet")
		return err
	}
	_, err = icspInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.onAdd(ic),
		UpdateFunc: s.onUpdate(ic),
		DeleteFunc: s.onDelete(ic),
	})
	if err != nil {
		s.log.Error(err, "Error registering handler for ImageTagMirrorSet")
		return err
	}
	return nil
}

func (s *ITMSSyncer) onAdd(ic system_config.IConfigSyncer) func(obj interface{}) {
	return func(obj interface{}) {
		icsp, ok := obj.(*v1.ImageTagMirrorSet)
		if !ok {
			s.log.Error(errors.New("unexpected type, expected ImageTagMirrorSet"), "unexpected type",
				"type", fmt.Sprintf("%T", obj))
			return
		}
		for _, source := range icsp.Spec.ImageTagMirrors {
			err := ic.UpdateRegistryMirroringConfig(source.Source, mirrorsToStrings(source.Mirrors), system_config.PullTypeTagOnly)
			if err != nil {
				s.log.Error(err, "Error updating registry mirroring config",
					"name", icsp.Name, "source", source.Source)
				continue
			}
		}
	}
}

func (s *ITMSSyncer) onDelete(ic system_config.IConfigSyncer) func(obj interface{}) {
	return func(obj interface{}) {
		itms, ok := obj.(*v1.ImageTagMirrorSet)
		if !ok {
			s.log.Error(errors.New("unexpected type, expected ImageTagMirrorSet"), "unexpected type",
				"type", fmt.Sprintf("%T", obj))
			return
		}
		for _, source := range itms.Spec.ImageTagMirrors {
			err := ic.DeleteRegistryMirroringConfig(source.Source)
			if err != nil {
				s.log.Error(err, "Error removing registry mirroring config",
					"name", itms.Name, "source", source.Source)
				continue
			}
		}
	}
}

func (s *ITMSSyncer) onUpdate(ic system_config.IConfigSyncer) func(oldobj, newobj interface{}) {
	return func(oldobj, newobj interface{}) {
		s.onDelete(ic)(oldobj)
		s.onAdd(ic)(newobj)
	}
}
