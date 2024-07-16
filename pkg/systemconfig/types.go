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

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/containers/image/v5/pkg/sysregistriesv2"
	"github.com/containers/image/v5/signature"
	"k8s.io/apimachinery/pkg/util/json"
)

type PullType string

const (
	PullTypeDigestOnly PullType = sysregistriesv2.MirrorByDigestOnly
	PullTypeTagOnly    PullType = sysregistriesv2.MirrorByTagOnly

	dockerDaemonTransport = "docker-daemon"
	dockerTransport       = "docker"
	atomicTransport       = "atomic"
)

type registryCertTuple struct {
	registry string
	cert     string
}

var (
	dockerCertsDir,
	registriesCertsDir,
	registriesConfPath,
	policyConfPath string
)

func DockerCertsDir() string {
	if dockerCertsDir == "" {
		dockerCertsDir = lookupEnvOr("DOCKER_CERTS_DIR", "/tmp/docker/certs.d")
	}
	return dockerCertsDir
}

func RegistryCertsDir() string {
	if registriesCertsDir == "" {
		registriesCertsDir = lookupEnvOr("REGISTRIES_CERTS_DIR", "/tmp/containers/registries.d")
	}
	return registriesCertsDir
}

func RegistriesConfPath() string {
	if registriesConfPath == "" {
		registriesConfPath = lookupEnvOr("REGISTRIES_CONF_PATH", "/tmp/containers/registries.conf")
	}
	return registriesConfPath
}

func PolicyConfPath() string {
	if policyConfPath == "" {
		policyConfPath = lookupEnvOr("POLICY_CONF_PATH", "/tmp/containers/policy.json")
	}
	return policyConfPath
}

func lookupEnvOr(key, defaultValue string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return defaultValue
}

func (t registryCertTuple) writeToFile() error {
	// create folder if it doesn't exist
	absoluteFolderPath := fmt.Sprintf("%s/%s", DockerCertsDir(), t.getFolderName())
	if _, err := os.Stat(absoluteFolderPath); os.IsNotExist(err) {
		err = os.MkdirAll(absoluteFolderPath, 0700)
		if err != nil {
			return err
		}
	}
	// write cert to file
	absoluteFilePath := fmt.Sprintf("%s/%s/ca.crt", DockerCertsDir(), t.getFolderName())
	f, err := os.Create(filepath.Clean(absoluteFilePath))
	if err != nil {
		return err
	}
	defer close(f)
	_, err = f.WriteString(t.cert)
	if err != nil {
		return err
	}
	return nil
}

func (t registryCertTuple) getFolderName() string {
	// the registry name could report the port number after two dots, e.g. registry.example.com..5000.
	// we need to replace the two dots with a colon to get the correct folder name.
	return strings.Replace(t.registry, "..", ":", 1)
}

type registriesConf struct {
	UnqualifiedSearchRegistries []string                 `toml:"unqualified-search-registries"`
	ShortNameMode               string                   `toml:"short-name-mode"`
	Registries                  []*registryConf          `toml:"registry"`
	registriesMap               map[string]*registryConf `toml:"-"`
}

func (rsc *registriesConf) getRegistryConfOrCreate(registry string) *registryConf {
	rc := rsc.registriesMap[registry]
	if rc == nil {
		rc = &registryConf{
			Location: registry,
		}
		rsc.registriesMap[registry] = rc
		rsc.Registries = append(rsc.Registries, rc)
	}
	return rc
}

func (rsc *registriesConf) writeToFile() error {
	return writeTomlFile(RegistriesConfPath(), rsc)
}

func (rsc *registriesConf) getRegistryConf(registry string) (*registryConf, bool) {
	rc, ok := rsc.registriesMap[registry]
	return rc, ok
}

func (rsc *registriesConf) cleanupRegistryConfIfEmpty(registry string) {
	if rc, ok := rsc.getRegistryConf(registry); ok {
		if rc.Insecure == nil && rc.Blocked == nil && len(rc.Mirrors) == 0 {
			delete(rsc.registriesMap, registry)
			for i, r := range rsc.Registries {
				if r == rc {
					rsc.Registries = append(rsc.Registries[:i], rsc.Registries[i+1:]...)
					break
				}
			}
		}
	}
}

func (rsc *registriesConf) cleanupAllRegistryConfIfEmpty() {
	for _, registry := range rsc.Registries {
		rsc.cleanupRegistryConfIfEmpty(registry.Location)
	}
}

type registryConf struct {
	Location string    `toml:"location"`
	Prefix   string    `toml:"prefix"`
	Mirrors  []*Mirror `toml:"mirror"`
	// Setting the blocked, allowed and insecure fields to nil will cause them to be omitted from the output
	Blocked  *bool `toml:"blocked"`
	Insecure *bool `toml:"insecure"`
}

type Mirror struct {
	Location       string   `toml:"location"`
	PullFromMirror PullType `toml:"pull-from-mirror"`
	Insecure       *bool    `toml:"insecure"`
}

func mirrorFor(location string, pullType PullType, insecure *bool) *Mirror {
	return &Mirror{
		Location:       location,
		PullFromMirror: pullType,
		Insecure:       insecure,
	}
}

// defaultRegistriesConf returns a default registriesConf object
func defaultRegistriesConf() registriesConf {
	return registriesConf{
		UnqualifiedSearchRegistries: []string{"registry.access.redhat.com", "docker.io"},
		ShortNameMode:               "",
		Registries:                  []*registryConf{},
		registriesMap:               map[string]*registryConf{},
	}
}

func defaultPolicy() signature.Policy {
	return signature.Policy{
		Default: signature.PolicyRequirements{signature.NewPRInsecureAcceptAnything()},
		Transports: map[string]signature.PolicyTransportScopes{
			dockerTransport: {},
			atomicTransport: {},
			dockerDaemonTransport: {
				"": {signature.NewPRInsecureAcceptAnything()},
			},
		},
	}
}

func writeTomlFile(path string, data interface{}) error {
	createBaseDir(path)
	f, err := os.Create(filepath.Clean(path))
	if err != nil {
		return err
	}
	defer close(f)
	return toml.NewEncoder(f).Encode(data)
}

func createBaseDir(path string) {
	// create base dir if it doesn't exist
	baseDir := filepath.Dir(filepath.Clean(path))
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		err := os.MkdirAll(baseDir, os.ModePerm)
		if err != nil {
			log.Error(err, "Unable to create the base dir", "path", path)
		}
	}
}

func writeJSONFile(path string, data interface{}) error {
	createBaseDir(path)
	f, err := os.Create(filepath.Clean(path))
	if err != nil {
		return err
	}
	defer close(f)
	return json.NewEncoder(f).Encode(data)
}

func close(f *os.File) {
	err := f.Close()
	if err != nil {
		log.Error(err, "When cosing fd")
	}
}

/* example policy.json
{
  "default": [
    {
      "type": "insecureAcceptAnything"
    }
  ],
  "transports": {
    "atomic": {
      "docker.io": [
        {
          "type": "reject"
        }
      ]
    },
    "docker": {
      "docker.io": [
        {
          "type": "reject"
        }
      ]
    },
    "docker-daemon": {
      "": [
        {
          "type": "insecureAcceptAnything"
        }
      ]
    }
  }
}

*/
