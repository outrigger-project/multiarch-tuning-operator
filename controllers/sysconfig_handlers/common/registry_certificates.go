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

package common

import (
	"context"
	"errors"
	"fmt"
	"multiarch-operator/pkg/system_config"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// RegistryCertificatesSyncer watches a configmap (openshift-image-registry/image-registry-certificates) and updates
// the registry certificates accordingly by using the SystemConfigSyncer.
// The configuration written by the SystemConfigSyncer due to the RegistryCertificatesSyncer is stored in-memory in the
// SystemConfigSyncer.registryCertTuples (type []system_config.registryCertTuple) and written to disk in the $conf_dir/docker/certs.d directory.
// In particular, an example of the configuration written by the SystemConfigSyncer due to the RegistryCertificatesSyncer in $conf_dir/docker/certs.d is:
// $conf_dir/docker/certs.d/registry.redhat.io/ca.crt
// $conf_dir/docker/certs.d/registry.redhat.io:5000/ca.crt
type RegistryCertificatesSyncer struct {
	// clientSet is the kubernetes clientset
	clientSet *kubernetes.Clientset
	// namespace is the namespace where the configmap that contains the registry certificates is stored
	namespace string
	// name is the name of the configmap that contains the registry certificates
	name string
	// log is the logger
	log logr.Logger
}

func NewRegistryCertificatesSyncer(clientSet *kubernetes.Clientset, namespace, name string) *RegistryCertificatesSyncer {
	return &RegistryCertificatesSyncer{
		clientSet: clientSet,
		namespace: namespace,
		name:      name,
	}
}

func (s *RegistryCertificatesSyncer) Start(ctx context.Context) (err error) {
	s.log = log.FromContext(ctx, "handler", "RegistryCertificatesSyncer", "kind", "ConfigMap [core/v1]",
		"namespace", s.namespace, "name", s.name)
	s.log.Info("Starting System Config Syncer")
	ic := system_config.SystemConfigSyncerSingleton()
	clientSet := s.clientSet
	// Watch the ConfigMap that contains the registry certificates and Sync SystemConfig
	registryCertificatesInformer := v1.NewConfigMapInformer(clientSet, s.namespace, 0, cache.Indexers{})

	_, err = registryCertificatesInformer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    s.onAdd(ic),
			UpdateFunc: s.onUpdate(ic),
		},
	)

	if err != nil {
		s.log.Error(err, "Error registering handler for the image-registry-certificates configmap")
		return err
	}

	registryCertificatesInformer.Run(ctx.Done())

	s.log.Info("Stopping System Config Syncer")
	return nil
}

func (s *RegistryCertificatesSyncer) onAddOrUpdate(ic system_config.IConfigSyncer, obj interface{}) {
	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		s.log.Error(errors.New("unexpected type, expected ConfigMap"), "unexpected type", "type", fmt.Sprintf("%T", obj))
		return
	}
	if cm.Name != s.name {
		// Ignore other configmaps
		return
	}
	s.log.Info("The configmap has been updated")
	err := ic.StoreRegistryCerts(system_config.ParseRegistryCerts(cm.Data))
	if err != nil {
		s.log.Error(err, "Error updating registry certs")
		return
	}
}

func (s *RegistryCertificatesSyncer) onUpdate(ic system_config.IConfigSyncer) func(oldobj, newobj interface{}) {
	return func(oldobj, newobj interface{}) {
		s.onAddOrUpdate(ic, newobj)
	}
}

func (s *RegistryCertificatesSyncer) onAdd(ic system_config.IConfigSyncer) func(obj interface{}) {
	return func(obj interface{}) {
		s.onAddOrUpdate(ic, obj)
	}
}
