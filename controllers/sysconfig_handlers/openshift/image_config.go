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
	ocpv1 "github.com/openshift/api/config/v1"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// ImageRegistryConfigSyncer watches the image.config.openshift.io/cluster object and updates the registry configuration accordingly by using
// the SystemConfigSyncer.
// The configuration written by the SystemConfigSyncer due to the ImageRegistryConfigSyncer is stored in-memory in the
// SystemConfigSyncer.registryConfContent (type registryConf) and written to disk in the $conf_dir/containers/registries.conf,
// and $conf_dir/containers/polices.json files.
// In particular, an example of the configuration written by the SystemConfigSyncer due to the ImageRegistryConfigSyncer in $conf_dir/containers/registries.conf is:
// [[registries]]
//
//	location = "registry.redhat.io"
//	allowed = true
//
// An example of the configuration written by the SystemConfigSyncer due to the ImageRegistryConfigSyncer in $conf_dir/containers/policies.json is:
// {
//   "default": [
//     {
//       "type": "insecureAcceptAnything"
//     }
//   ],
//   "transports": {
//     "atomic": {
//       "docker.io": [
//         {
//           "type": "reject"
//         }
//       ]
//     },
//     "docker": {
//       "docker.io": [
//         {
//           "type": "reject"
//         }
//       ]
//     },
//     "docker-daemon": {
//       "": [
//         {
//           "type": "insecureAcceptAnything"
//         }
//       ]
//     }
//   }
// }
//

type ImageRegistryConfigSyncer struct {
	mgr manager.Manager
	log logr.Logger
}

func NewImageRegistryConfigSyncer(mgr manager.Manager) *ImageRegistryConfigSyncer {
	return &ImageRegistryConfigSyncer{
		mgr: mgr,
	}
}

func (s *ImageRegistryConfigSyncer) Start(ctx context.Context) (err error) {
	s.log = log.FromContext(ctx, "handler", "ImageRegistryConfigSyncer", "kind", "Image [config.openshift.io/v1]")
	s.log.Info("Starting System Config Syncer")
	mgr := s.mgr
	ic := system_config.SystemConfigSyncerSingleton()
	imageInformer, err := mgr.GetCache().GetInformerForKind(ctx, ocpv1.GroupVersion.WithKind("Image"))
	if err != nil {
		s.log.Error(err, "Error getting the informer")
		return err
	}
	_, err = imageInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.onAdd(ic),
		UpdateFunc: s.onUpdate(ic),
	})
	if err != nil {
		s.log.Error(err, "Error registering handler")
		return err
	}

	return nil
}

func (s *ImageRegistryConfigSyncer) onAddOrUpdate(ic system_config.IConfigSyncer, obj interface{}) {
	image, ok := obj.(*ocpv1.Image)
	if !ok {
		s.log.Error(errors.New("unexpected type, expected Image"), "unexpected type", "type", fmt.Sprintf("%T", obj))
		return
	}
	if image.Name != "cluster" {
		s.log.V(4).Info("Ignoring unexpected object", "name", image.Name)
		return
	}
	s.log.Info("The object has been updated")
	err := ic.StoreImageRegistryConf(image.Spec.RegistrySources.AllowedRegistries,
		image.Spec.RegistrySources.BlockedRegistries, image.Spec.RegistrySources.InsecureRegistries)
	if err != nil {
		s.log.Error(err, "Error updating registry conf")
		return
	}
}

func (s *ImageRegistryConfigSyncer) onAdd(ic system_config.IConfigSyncer) func(interface{}) {
	return func(obj interface{}) {
		s.onAddOrUpdate(ic, obj)
	}
}

func (s *ImageRegistryConfigSyncer) onUpdate(ic system_config.IConfigSyncer) func(interface{}, interface{}) {
	return func(oldobj, newobj interface{}) {
		s.onAddOrUpdate(ic, newobj)
	}
}
