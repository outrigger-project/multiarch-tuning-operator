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
	"strings"

	"k8s.io/apimachinery/pkg/util/json"
)

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

// expandGlobs takes an image reference and expands the registry globs in the authCfg's Auths field
func (ac authCfg) expandGlobs(imageReference string) *authCfg {
	// From Kubernetes documentation:
	// *.kubernetes.io will not match kubernetes.io, but abc.kubernetes.io
	// *.*.kubernetes.io will not match abc.kubernetes.io, but abc.def.kubernetes.io
	// prefix.*.io will match prefix.kubernetes.io
	// *-good.kubernetes.io will match prefix-good.kubernetes.io
	// also see https://github.com/kubernetes/kubelet/blob/8419bc34b9162d136ddcb9e9846f299962a3ef3f/config/v1alpha1/types.go#L48-L71
	// https://github.com/kubernetes/kubernetes/blob/7cb2bd78b22c4ac8d9a401920fbcf7e2b240522d/pkg/credentialprovider/plugin/plugin_test.go#L194-L507
	for registry, auth := range ac.Auths {
		if regURL, ok := matchAndExpandGlob(registry, imageReference); ok && len(regURL) > 0 {
			// check if the registry is already in the authCfg's Auths field
			if _, ok := ac.Auths[regURL]; !ok {
				ac.addAuth(regURL, auth)
			}
		}
	}
	return &ac
}

// matchAndExpandGlob takes a registry glob and an image reference and checks if they match according to the Kubernetes
// globbing rules. If they match, it returns the expanded registry URL with the image reference's host part replaced.
func matchAndExpandGlob(registryGlob, imageReference string) (string, bool) {
	if !strings.ContainsAny(registryGlob, "*?]") {
		return "", false
	}
	imageReference = strings.TrimPrefix(imageReference, "//")
	m, e := URLsMatchStr(registryGlob, imageReference)
	if e != nil || !m {
		return "", false
	}

	// Swap the registry glob in the host part with the image reference's host part
	irURL, e := ParseSchemelessURL(imageReference)
	if e != nil {
		return "", false
	}
	regURL, e := ParseSchemelessURL(registryGlob)
	if e != nil {
		return "", false
	}
	regURL.Host = irURL.Host
	return strings.TrimPrefix(regURL.String(), "//"), true
}
