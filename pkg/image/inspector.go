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
	"context"
	"fmt"
	"multiarch-operator/pkg/systemconfig"
	"os"
	"sync"

	"github.com/containers/image/v5/docker"
	"github.com/containers/image/v5/image"
	"github.com/containers/image/v5/manifest"
	"github.com/containers/image/v5/types"
	"github.com/go-logr/logr"
	"golang.org/x/sys/unix"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

type registryInspector struct {
	globalPullSecret []byte
	// mutex is used to protect the globalPullSecret field of the singletonImageFacade from concurrent write access
	mutex sync.Mutex
}

func (i *registryInspector) GetCompatibleArchitecturesSet(ctx context.Context, imageReference string, secrets [][]byte) (supportedArchitectures sets.Set[string], err error) {
	// Create the auth file
	log := ctrllog.FromContext(ctx, "imageReference", imageReference)
	authFile, err := i.createAuthFile(log, append([][]byte{i.globalPullSecret}, secrets...)...)
	if err != nil {
		log.Error(err, "Couldn't write auth file")
		return nil, err
	} else {
		defer func(f *os.File) {
			if err := f.Close(); err != nil {
				log.Error(err, "Failed to close auth file", "filename", f.Name())
			}
		}(authFile)
	}
	// Check if the image is a manifest list
	ref, err := docker.ParseReference(imageReference)
	if err != nil {
		log.Error(err, "Error parsing the image reference for the image")
		return nil, err
	}
	sys := &types.SystemContext{
		AuthFilePath:                authFile.Name(),
		SystemRegistriesConfPath:    systemconfig.RegistriesConfPath,
		SystemRegistriesConfDirPath: systemconfig.RegistryCertsDir,
		SignaturePolicyPath:         systemconfig.PolicyConfPath,
		DockerPerHostCertDirPath:    systemconfig.DockerCertsDir,
	}
	src, err := ref.NewImageSource(ctx, sys)
	if err != nil {
		log.Error(err, "Error creating the image source")
		return nil, err
	}
	defer func(src types.ImageSource) {
		err := src.Close()
		if err != nil {
			log.Error(err, "Error closing the image source for the image")
		}
	}(src)
	rawManifest, _, err := src.GetManifest(ctx, nil)
	if err != nil {
		log.Error(err, "Error getting the image manifest: %v")
		return nil, err
	}
	supportedArchitectures = sets.New[string]()
	if manifest.MIMETypeIsMultiImage(manifest.GuessMIMEType(rawManifest)) {
		log.V(5).Info("Image is a manifest list... getting the list of supported architectures")
		// The image is a manifest list
		index, err := manifest.OCI1IndexFromManifest(rawManifest)
		if err != nil {
			log.Error(err, "Error parsing the OCI index from the raw manifest of the image")
		}
		for _, m := range index.Manifests {
			supportedArchitectures = sets.Insert(supportedArchitectures, m.Platform.Architecture)
		}
		return supportedArchitectures, nil
	} else {
		log.V(5).Info("The image is not a manifest list... getting the supported architecture")
		parsedImage, err := image.FromUnparsedImage(ctx, sys, image.UnparsedInstance(src, nil))
		if err != nil {
			log.Error(err, "Error parsing the manifest of the image")
			return nil, err
		}
		config, err := parsedImage.OCIConfig(ctx)
		if err != nil {
			// Ignore errors due to invalid images at this stage
			log.Error(err, "Error parsing the OCI config of the image")
			return nil, err
		}
		supportedArchitectures = sets.Insert(supportedArchitectures, config.Architecture)
	}
	return supportedArchitectures, nil
}

func (i *registryInspector) createAuthFile(log logr.Logger, secrets ...[]byte) (*os.File, error) {
	// Create the auth file
	authCfgContent := &authCfg{
		Auths: make(map[string]authData),
	}

	for _, secret := range secrets {
		if err := authCfgContent.unmarshallAuthsDataAndStore(secret); err != nil {
			log.Error(err, "Error unmarshalling pull secrets")
			continue
		}
	}
	authJson, err := authCfgContent.marshallAuths()
	if err != nil {
		log.Error(err, "Error marshalling pull secrets")
		return nil, err
	}
	// TODO: constant-name-for-now is a placeholder. Do we need this parameter at all?
	fd, err := writeMemFile("constant-name-for-now", authJson)
	if err != nil {
		return nil, err
	}
	// filepath to our newly created in-memory file descriptor
	fp := fmt.Sprintf("/proc/self/fd/%d", fd)
	return os.NewFile(uintptr(fd), fp), nil
}

// writeMemFile creates an in memory file based on memfd_create
// returns a file descriptor. Once all references to the file are
// dropped it is automatically released. It is up to the caller
// to close the returned descriptor.
func writeMemFile(name string, b []byte) (int, error) {
	fd, err := unix.MemfdCreate(name, 0)
	if err != nil {
		return 0, fmt.Errorf("MemfdCreate: %v", err)
	}
	err = unix.Ftruncate(fd, int64(len(b)))
	if err != nil {
		return 0, fmt.Errorf("Ftruncate: %v", err)
	}
	data, err := unix.Mmap(fd, 0, len(b), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return 0, fmt.Errorf("Mmap: %v", err)
	}
	copy(data, b)
	err = unix.Munmap(data)
	if err != nil {
		return 0, fmt.Errorf("Munmap: %v", err)
	}
	return fd, nil
}

func (i *registryInspector) StoreGlobalPullSecret(pullSecret []byte) {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	i.globalPullSecret = pullSecret
}

func newRegistryInspector() IRegistryInspector {
	ri := &registryInspector{}
	return ri
}
