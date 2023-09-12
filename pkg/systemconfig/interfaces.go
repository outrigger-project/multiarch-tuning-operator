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

package systemconfig

type IConfigSyncer interface {

	// StoreImageRegistryConf stores the allowedRegistries and blockedRegistries in the structs representing the
	// registries.conf and policy.json files. It fails if both allowedRegistries and blockedRegistries are set.
	StoreImageRegistryConf(allowedRegistries []string, blockedRegistries []string, insecureRegistries []string) error

	StoreRegistryCerts(registryCertTuples []registryCertTuple) error

	UpdateRegistryMirroringConfig(registry string, mirrors []string, pullType PullType) error
	DeleteRegistryMirroringConfig(registry string) error
	CleanupRegistryMirroringConfig() error

	sync() error
	getch() chan bool
}
