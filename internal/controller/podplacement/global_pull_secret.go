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

package podplacement

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"

	corev1 "k8s.io/api/core/v1"
	clientv1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/openshift/multiarch-tuning-operator/pkg/image"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

type GlobalPullSecretSyncer struct {
	clientSet *kubernetes.Clientset
	namespace string
	name      string
	log       logr.Logger
}

func NewGlobalPullSecretSyncer(clientSet *kubernetes.Clientset, namespace, name string) *GlobalPullSecretSyncer {
	return &GlobalPullSecretSyncer{
		clientSet: clientSet,
		namespace: namespace,
		name:      name,
	}
}

func (s *GlobalPullSecretSyncer) Start(ctx context.Context) (err error) {
	s.log = log.FromContext(ctx, "handler", "GlobalPullSecretSyncer", "kind", "Secret [core/v1]",
		"namespace", s.namespace, "name", s.name)
	s.log.Info("Starting System Config Syncer")
	clientSet := s.clientSet
	// Watch the Secret that contains the global pull secret and Sync the inspector
	globalPullSecretInformer := clientv1.NewSecretInformer(clientSet, s.namespace, time.Hour, cache.Indexers{})

	_, err = globalPullSecretInformer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    s.onAddOrUpdate,
			UpdateFunc: s.onUpdate(),
		},
	)
	if err != nil {
		s.log.Error(err, "Error registering handler for the global pull-secret configmap")
		return err
	}

	globalPullSecretInformer.Run(ctx.Done())

	s.log.Info("Stopping System Config Syncer")
	return nil
}

func (s *GlobalPullSecretSyncer) onAddOrUpdate(obj interface{}) {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		s.log.Error(errors.New("undexpected type, expected v1.Secret"), "unexpected type", "type", fmt.Sprintf("%T", obj))
		return
	}
	if secret.Name != s.name {
		// Ignore other configmaps
		return
	}
	s.log.Info("The global pull secret was updated")
	if pullSecret, err := utils.ExtractAuthFromSecret(secret); err == nil {
		image.FacadeSingleton().StoreGlobalPullSecret(pullSecret)
	} else {
		s.log.Error(err, "Error extracting the auth from the secret")
	}
}

func (s *GlobalPullSecretSyncer) onUpdate() func(oldobj, newobj interface{}) {
	return func(oldobj, newobj interface{}) {
		oldSecret, ok := oldobj.(*corev1.Secret)
		if !ok {
			s.log.Error(errors.New("undexpected type, expected v1.Secret"), "unexpected type", "type", fmt.Sprintf("%T", oldobj))
			return
		}
		newSecret, ok := newobj.(*corev1.Secret)
		if !ok {
			s.log.Error(errors.New("undexpected type, expected v1.Secret"), "unexpected type", "type", fmt.Sprintf("%T", newobj))
			return
		}
		if oldSecret.ResourceVersion == newSecret.ResourceVersion {
			return
		}
		s.onAddOrUpdate(newobj)
	}
}
