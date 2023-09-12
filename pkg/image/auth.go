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

import "k8s.io/apimachinery/pkg/util/json"

type authData struct {
	Auth string `json:"auth"`
}

// authCfg struct for storing registry credentials
type authCfg struct {
	Auths map[string]authData `json:"auths"`
}

// addAuth takes a registry and an authData and stores it in the authCfg's Auths field
// in case of duplicated registries, the last authData will be kept
func (ac authCfg) addAuth(registry string, auth authData) {
	ac.Auths[registry] = auth
}

// addAuthString takes a registry and an auth string and stores it in the authCfg's Auths field.
// In case of duplicated registries, the last generated authData will be kept.
// This function is currently not used.
//
//nolint:unused
func (ac authCfg) addAuthString(registry, auth string) {
	ac.Auths[registry] = authData{Auth: auth}
}

// addAuths takes a map of registry:authData and stores it in the authCfg's Auths field
// in case of duplicated registries, the last authData will be kept
func (ac authCfg) addAuths(registryAuths map[string]authData) {
	for registry, auth := range registryAuths {
		ac.addAuth(registry, auth)
	}
}

// unmarshallAuthsDataAndStore takes a byte array and unmarshalls it into a map of registry:authData
// then, it stores the authData in the authCfg's Auths field
// authBytes is expected to be the representation of the docker config.json's auths field.
// The pod_reconciler will extract the imagePullSecrets field from the pod spec, get the secrets' data and store it as a [][]byte
// each []byte is expected to be consumed as authBytes here
// example of authsBytes:
//
//	{
//	  "https://index.docker.io/v1/": {
//	    "auth": "dXNlcm5hbWU6cGFzc3dvcmQ="
//	  },
//	  "https://gcr.io": {
//	    "auth": "dXNlcm5hbWU6cGFzc3dvcmQ="
//	  }
//	}
func (ac authCfg) unmarshallAuthsDataAndStore(authsBytes []byte) error {
	var auths map[string]authData
	if err := json.Unmarshal(authsBytes, &auths); err != nil {
		return err
	}
	ac.addAuths(auths)
	return nil
}

// marshallAuths takes the authCfg's Auths field and marshalls it into a byte array
// the byte array is expected to be the representation of the docker config.json file
// example of authsBytes:
//
//	{
//	  "auths": {
//	    "https://index.docker.io/v1/": {
//	      "auth": "dXNlcm5hbWU6cGFzc3dvcmQ="
//	    },
//	    "https://gcr.io": {
//	      "auth": "dXNlcm5hbWU6cGFzc3dvcmQ="
//	    }
//	  }
//	}
func (ac authCfg) marshallAuths() ([]byte, error) {
	return json.Marshal(ac)
}
