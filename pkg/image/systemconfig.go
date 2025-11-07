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
	"os"
	"sync"
)

var (
	dockerCertsDir,
	registriesCertsDir,
	registriesConfPath,
	registriesConfDir,
	policyConfPath string
	rwMutex sync.RWMutex
)

func DockerCertsDir() string {
	rwMutex.RLock()
	if dockerCertsDir != "" {
		defer rwMutex.RUnlock()
		return dockerCertsDir
	}
	rwMutex.RUnlock()
	rwMutex.Lock()
	defer rwMutex.Unlock()
	if dockerCertsDir == "" {
		// avoid race condition in-between rwMutex.RUnlock and rwMutex.Lock
		dockerCertsDir = lookupEnvOr("DOCKER_CERTS_DIR", "/etc/docker/certs.d")
	}
	return dockerCertsDir
}

func RegistryCertsDir() string {
	rwMutex.RLock()
	if registriesCertsDir != "" {
		defer rwMutex.RUnlock()
		return registriesCertsDir
	}
	rwMutex.RUnlock()
	rwMutex.Lock()
	defer rwMutex.Unlock()
	if registriesCertsDir == "" {
		// avoid race condition in-between rwMutex.RUnlock and rwMutex.Lock
		registriesCertsDir = lookupEnvOr("REGISTRIES_CERTS_DIR", "/etc/containers/registries.d")
	}
	return registriesCertsDir
}

func RegistriesConfPath() string {
	rwMutex.RLock()
	if registriesConfPath != "" {
		defer rwMutex.RUnlock()
		return registriesConfPath
	}
	rwMutex.RUnlock()
	rwMutex.Lock()
	defer rwMutex.Unlock()
	if registriesConfPath == "" {
		// avoid race condition in-between rwMutex.RUnlock and rwMutex.Lock
		registriesConfPath = lookupEnvOr("REGISTRIES_CONF_PATH", "/etc/containers/registries.conf")
	}
	return registriesConfPath
}

func RegistriesConfDir() string {
	rwMutex.RLock()
	if registriesConfDir != "" {
		defer rwMutex.RUnlock()
		return registriesConfDir
	}
	rwMutex.RUnlock()
	rwMutex.Lock()
	defer rwMutex.Unlock()
	if registriesConfDir == "" {
		// avoid race condition in-between rwMutex.RUnlock and rwMutex.Lock
		registriesConfDir = lookupEnvOr("REGISTRIES_CONF_DIR", "/etc/containers/registries.conf.d")
	}
	return registriesConfDir
}

func PolicyConfPath() string {
	rwMutex.RLock()
	if policyConfPath != "" {
		defer rwMutex.RUnlock()
		return policyConfPath
	}
	rwMutex.RUnlock()
	rwMutex.Lock()
	defer rwMutex.Unlock()
	if policyConfPath == "" {
		// avoid race condition in-between rwMutex.RUnlock and rwMutex.Lock
		policyConfPath = lookupEnvOr("POLICY_CONF_PATH", "/etc/containers/policy.json")
	}
	return policyConfPath
}

func lookupEnvOr(key, defaultValue string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return defaultValue
}
