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

// IDMSs report the set of registry sources that the cluster needs to reach via mirrors.
// Each registry source can have multiple mirrors.
// The ICSPSyncer watches ICSPs and updates the registry mirroring config accordingly by using
// the SystemConfigSyncer.
// The configuration written by the SystemConfigSyncer due to the ICSPSyncer is stored in-memory in the
// SystemConfigSyncer.registriesConfContent (type registriesConf) and written to disk in the /$conf_dir/container/registries.conf file.

// In particular, an example of the configuration written by the SystemConfigSyncer due to the IDMSSyncer in /$conf_dir/containers/registries.conf is:
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

type IDMSSyncer struct {
	mgr manager.Manager
	log logr.Logger
}

func NewIDMSSyncer(mgr manager.Manager) *IDMSSyncer {
	return &IDMSSyncer{
		mgr: mgr,
	}
}

func (s *IDMSSyncer) Start(ctx context.Context) (err error) {
	s.log = log.FromContext(ctx, "handler", "IDMSSynver", "kind", "ImageDigestMirrorSet [config.openshift.io/v1]")
	s.log.Info("Starting System Config Syncer")
	mgr := s.mgr
	ic := system_config.SystemConfigSyncerSingleton()
	icspInformer, err := mgr.GetCache().GetInformerForKind(ctx, v1.GroupVersion.WithKind("ImageDigestMirrorSet"))
	if err != nil {
		s.log.Error(err, "Error getting informer for ImageDigestMirrorSet")
		return err
	}
	_, err = icspInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.onAdd(ic),
		UpdateFunc: s.onUpdate(ic),
		DeleteFunc: s.onDelete(ic),
	})
	if err != nil {
		s.log.Error(err, "Error registering handler for ImageDigestMirrorSet [config.openshift.io/v1]")
		return err
	}
	return nil
}

func (s *IDMSSyncer) onAdd(ic system_config.IConfigSyncer) func(obj interface{}) {
	return func(obj interface{}) {
		idms, ok := obj.(*v1.ImageDigestMirrorSet)
		if !ok {
			s.log.Error(errors.New("unexpected type, expected ImageDigestMirrorSet "), "unexpected type",
				"type", fmt.Sprintf("%T", obj))
			return
		}
		for _, source := range idms.Spec.ImageDigestMirrors {
			err := ic.UpdateRegistryMirroringConfig(source.Source, mirrorsToStrings(source.Mirrors), system_config.PullTypeDigestOnly)
			if err != nil {
				s.log.Error(err, "Error updating registry mirroring config",
					idms.Name, source.Source, err)
			}
		}
	}
}

func (s *IDMSSyncer) onDelete(ic system_config.IConfigSyncer) func(obj interface{}) {
	return func(obj interface{}) {
		idms, ok := obj.(*v1.ImageDigestMirrorSet)
		if !ok {
			s.log.Error(errors.New("unexpected type, expected ImageDigestMirrorSet"), "unexpected type",
				"type", fmt.Sprintf("%T", obj))
			return
		}
		for _, source := range idms.Spec.ImageDigestMirrors {
			err := ic.DeleteRegistryMirroringConfig(source.Source)
			if err != nil {
				s.log.Error(err, "Error removing registry mirroring config",
					"name", idms.Name, "source", source.Source)
				continue
			}
		}
	}
}

func (s *IDMSSyncer) onUpdate(ic system_config.IConfigSyncer) func(oldobj, newobj interface{}) {
	return func(oldobj, newobj interface{}) {
		s.onDelete(ic)(oldobj)
		s.onAdd(ic)(newobj)
	}
}

func mirrorsToStrings(mirrors []v1.ImageMirror) []string {
	var mirrorsStr []string
	for _, mirror := range mirrors {
		mirrorsStr = append(mirrorsStr, string(mirror))
	}
	return mirrorsStr
}
