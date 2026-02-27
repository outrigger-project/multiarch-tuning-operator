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
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/util/sets"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/containers/image/v5/docker"
	"github.com/containers/image/v5/image"
	"github.com/containers/image/v5/manifest"
	"github.com/containers/image/v5/pkg/shortnames"
	"github.com/containers/image/v5/pkg/sysregistriesv2"
	"github.com/containers/image/v5/signature"
	"github.com/containers/image/v5/types"
	"github.com/opencontainers/go-digest"
	ociv1 "github.com/opencontainers/image-spec/specs-go/v1"

	"golang.org/x/sys/unix"

	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

const (
	osdkMetricsAnnotation              = "operators.operatorframework.io.metrics.builder"
	osdkMediaTypeAnnotation            = "operators.operatorframework.io.bundle.mediatype.v1"
	osdkManifestsAnnotation            = "operators.operatorframework.io.bundle.manifests.v1"
	osdkBundleMetadataAnnotation       = "operators.operatorframework.io.bundle.metadata.v1"
	osdkBundlePackageAnnotation        = "operators.operatorframework.io.bundle.package.v1"
	osdkBundleChannelsAnnotation       = "operators.operatorframework.io.bundle.channels.v1"
	osdkBundleDefaultChannelAnnotation = "operators.operatorframework.io.bundle.channel.default.v1"
)

var (
	// https://github.com/operator-framework/operator-registry/blob/c4b5f1196/docs/design/operator-bundle.md
	operatorSDKBuilderBundleAnnotationSet = sets.New[string](
		osdkMetricsAnnotation, osdkMediaTypeAnnotation, osdkManifestsAnnotation, osdkBundleMetadataAnnotation,
		osdkBundlePackageAnnotation, osdkBundleChannelsAnnotation, osdkBundleDefaultChannelAnnotation)
)

type registryInspector struct {
	globalPullSecret []byte
	// mutex is used to protect the globalPullSecret field of the singletonImageFacade from concurrent write access
	mutex sync.RWMutex
}

// GetCompatibleArchitecturesSet returns the set of compatibles architectures given an imageReference and a list of secrets.
// It uses the containers/image library to get the manifest of the image and extract the architecture from it.
// If the image is a manifest list, it will return the set of architectures supported by the manifest list.
// If the image is a manifest, it will return the architecture set in the manifest's config.
// If the image is an operator bundle image, it will return an empty set. This is because operator bundle images
// are not tied to a specific architecture, and we should not set any constraints based on the architecture they report.
func (i *registryInspector) GetCompatibleArchitecturesSet(ctx context.Context, imageReference string, _ bool, secrets [][]byte) (supportedArchitectures sets.Set[string], err error) {
	// Create the auth file
	log := ctrllog.FromContext(ctx, "imageReference", imageReference)
	i.mutex.RLock()
	globalPullSecret := i.globalPullSecret
	i.mutex.RUnlock()
	authFile, err := i.createAuthFile(imageReference, append([][]byte{globalPullSecret}, secrets...)...)
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
	// Invalidate registry cache before calling image APIs to catch updates to registry configurations.
	// TODO: watch ICSP/IDMS/ITMS for changes or alternatively invalidate only on MCP updates rather
	// than do this everytime
	sysregistriesv2.InvalidateCache()

	// check if image reference has both tag and digest
	imageReference, err = parseImageReference(imageReference)
	if err != nil {
		log.Error(err, "Couldn't parse image reference")
		return nil, err
	}

	sys := &types.SystemContext{
		AuthFilePath:                authFile.Name(),
		RegistriesDirPath:           RegistryCertsDir(),
		SystemRegistriesConfPath:    RegistriesConfPath(),
		SystemRegistriesConfDirPath: RegistriesConfDir(),
		SignaturePolicyPath:         PolicyConfPath(),
		DockerPerHostCertDirPath:    DockerCertsDir(),
	}

	// Check if the image is a manifest list
	src, err := resolveAndOpenImageSource(ctx, sys, imageReference)
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
	policy, err := signature.DefaultPolicy(sys)
	if err != nil {
		log.Error(err, "Error loading the systemContext's policy")
		return nil, err
	}
	policyCtx, err := signature.NewPolicyContext(policy)
	if err != nil {
		log.Error(err, "Error creating the PolicyContext")
		return nil, err
	}

	supportedArchitectures = sets.New[string]()
	var instanceDigest *digest.Digest = nil
	if manifest.MIMETypeIsMultiImage(manifest.GuessMIMEType(rawManifest)) {
		index, err := manifest.OCI1IndexFromManifest(rawManifest)
		if err != nil {
			log.Error(err, "Error parsing the OCI index from the raw manifest of the image")
			return nil, err
		}
		for _, m := range index.Manifests {
			// Skip manifests with Docker reference annotations - they are not runnable platform images
			// Per Docker spec, these annotations indicate special manifest types (e.g., attestation-manifest)
			// vnd.docker.reference.type: indicates the manifest type (e.g., "attestation-manifest")
			// vnd.docker.reference.digest: points to the subject manifest being attested
			// If either annotation is present, the manifest should be ignored as it's not a platform image
			if m.Annotations != nil {
				if refType, exists := m.Annotations["vnd.docker.reference.type"]; exists {
					log.V(3).Info("Skipping manifest with reference type annotation", "type", refType, "digest", m.Digest)
					continue
				}
				if refDigest, exists := m.Annotations["vnd.docker.reference.digest"]; exists {
					log.V(3).Info("Skipping manifest with reference digest annotation", "refDigest", refDigest, "digest", m.Digest)
					continue
				}
			}
			// Skip manifests with unknown architecture
			if m.Platform == nil || (m.Platform.Architecture == "unknown") {
				log.V(3).Info("Skipping manifest with unknown platform", "architecture", m.Platform.Architecture, "os", m.Platform.OS, "digest", m.Digest)
				continue
			}
			supportedArchitectures = sets.Insert(supportedArchitectures, m.Platform.Architecture)
			// Store the first valid manifest digest for bundle image detection
			if instanceDigest == nil {
				instanceDigest = &m.Digest
			}
		}
		// In the case of non-manifest-list images, we will not execute this code path and the instanceDigest will be nil.
		// The architecture will be only one, i.e., the one from the config object of the single manifest.
		// In the case of manifest-list images, we will get the first valid (non-attestation, non-unknown) manifest and check
		// the config object for the operator-sdk label. The set of architectures will be the union of the architectures of all
		// the valid manifests in the index and computed later. In this way, we can avoid the library from looking for the manifest
		// that matches the architecture of the node where this code is running. That would lead to a failure if the node
		// architecture is not present in the list of architectures of the image.
	}

	unparsedImage := image.UnparsedInstance(src, instanceDigest)
	if allowed, err := policyCtx.IsRunningImageAllowed(ctx, unparsedImage); !allowed {
		// IsRunningImageAllowed returns true iff the policy allows running the image.
		// If it returns false, err must be non-nil, and should be an PolicyRequirementError if evaluation
		// succeeded but the result was rejection.
		var e *signature.PolicyRequirementError
		if errors.As(err, &e) {
			// false and valid error
			log.V(3).Info("The signature policy JSON file configuration does not allow inspecting this image",
				"validationError", e)
			return nil, e
		}
		log.Error(err, "Unable to perform the signature validation")
		return nil, err
	}

	parsedImage, err := image.FromUnparsedImage(ctx, sys, unparsedImage)
	if err != nil {
		log.Error(err, "Error parsing the manifest of the image")
		return nil, err
	}

	config, err := parsedImage.OCIConfig(ctx)

	if err != nil {
		log.Error(err, "Error parsing the OCI config of the image")
		return nil, err
	}
	if isBundleImage(config.Config) {
		log.V(3).Info("The image is an operator bundle image")
		// Operator bundle images are not tied to a specific architecture, so we should not set any constraints
		// based on the architecture they report.
		// We return the full set of supported architectures so that the intersection with the node architecture set
		// does not change later.
		// See https://issues.redhat.com/browse/OCPBUGS-38823 for more information.
		return utils.AllSupportedArchitecturesSet(), nil
	}

	if !manifest.MIMETypeIsMultiImage(manifest.GuessMIMEType(rawManifest)) {
		log.V(3).Info("The image is not a manifest list... getting the supported architecture")
		return sets.New[string](config.Architecture), nil
	}
	return supportedArchitectures, nil
}

// parseImageReference normalizes an imageName into a reference suitable for use
// with the inspection library. It returns one of the following:
//  1. A tag-only reference if no digest is present
//  2. A digest-only reference if a digest is present, dropping the tag when both
//     a tag and digest are specified in the pod's container image field
func parseImageReference(imageName string) (string, error) {
	// Check for empty image name first
	if imageName == "" {
		return "", errors.New("invalid image name, must not be empty")
	}

	// Find digest separator (supports sha256, sha512, sha384, etc.)
	digestIdx := strings.Index(imageName, "@sha")

	// Check if digest is present
	switch digestIdx {
	case -1:
		// No digest is present
		return imageName, nil
	case 0:
		// Image name can't start with digest
		return "", errors.New("invalid image name, image must not be empty")
	default:
		// Now check that if there is a tag present
	}

	// Validate there's only one digest
	if strings.Count(imageName[digestIdx+1:], "@sha") > 0 {
		return "", errors.New("invalid image name, must only have one digest")
	}

	namePart := imageName[:digestIdx]
	digestPart := imageName[digestIdx:] // includes the @

	// Find last "/" and last ":" to determine if we have a tag to remove
	// Format: [registry[:port]/][namespace/]image[:tag]@digest
	lastSlash := strings.LastIndex(namePart, "/")
	lastColon := strings.LastIndex(namePart, ":")

	//determine what to do with colons
	switch {
	case lastColon == -1:
		// No colon at all - no port, no tag
		return imageName, nil
	case lastColon > lastSlash:
		// Colon is after last slash (or no slash) - it's a tag, remove it
		namePart = namePart[:lastColon]
		return namePart + digestPart, nil
	default:
		// Colon is before last slash - it's a port, keep it
		return imageName, nil
	}
}

func isBundleImage(image ociv1.ImageConfig) bool {
	// Check if the image is an operator bundle image by looking for the operator-sdk annotation
	for label := range image.Labels {
		if operatorSDKBuilderBundleAnnotationSet.Has(label) {
			// The image is an operator bundle image
			return true
		}
	}
	return false
}

func (i *registryInspector) createAuthFile(imageReference string, secrets ...[]byte) (*os.File, error) {
	authJSON, err := marshaledImagePullSecrets(imageReference, secrets)
	if err != nil {
		return nil, err
	}
	fd, err := writeMemFile("mto_ppc_inspector", authJSON)
	if err != nil {
		return nil, err
	}
	// filepath to our newly created in-memory file descriptor
	fp := fmt.Sprintf("/proc/self/fd/%d", fd)
	return os.NewFile(uintptr(fd), fp), nil
}

func marshaledImagePullSecrets(imageReference string, secrets [][]byte) ([]byte, error) {
	log := ctrllog.Log.WithName("registryInspector")

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
	authJSON, err := authCfgContent.expandGlobs(imageReference).marshallAuths()
	if err != nil {
		log.Error(err, "Error marshalling pull secrets")
		return nil, err
	}
	return authJSON, nil
}

func resolveAndOpenImageSource(ctx context.Context, sys *types.SystemContext, imageReference string) (types.ImageSource, error) {
	log := ctrllog.FromContext(ctx).WithValues("imageReference", imageReference)

	// Ensure the image is a fully-qualified reference.
	// If it's a short name, shortnames.Resolve will expand it into one or more fully-qualified names.
	// Since imageReference may start with "//", which shortnames.Resolve cannot handle,
	// strip the leading "//" if present.
	resolved, err := shortnames.Resolve(sys, strings.TrimPrefix(imageReference, "//"))
	if err != nil {
		log.Error(err, "Failed to resolve image shortname")
		return nil, err
	}

	if desc := resolved.Description(); desc != "" {
		log.V(2).Info("Shortname resolution details", "description", desc)
	}

	var pullErrs []error
	for i, cand := range resolved.PullCandidates {
		fqName := fmt.Sprintf("//%s", cand.Value.String())
		log.V(1).Info("Trying candidate", "index", i, "fullName", fqName)

		ref, err := docker.ParseReference(fqName)
		if err != nil {
			log.Error(err, "Failed to parse image reference")
			pullErrs = append(pullErrs, err)
			continue
		}

		src, err := ref.NewImageSource(ctx, sys)
		if err != nil {
			log.Error(err, "Failed to create image source")
			pullErrs = append(pullErrs, err)
			continue
		}
		return src, nil
	}

	err = resolved.FormatPullErrors(pullErrs)
	log.Error(err, "All image pull candidates failed")
	return nil, err
}

// writeMemFile creates an in memory file based on memfd_create
// returns a file descriptor. Once all references to the file are
// dropped it is automatically released. It is up to the caller
// to close the returned descriptor.
func writeMemFile(name string, b []byte) (int, error) {
	fd, err := unix.MemfdCreate(name, unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		_ = unix.Close(fd)
		return 0, fmt.Errorf("MemfdCreate: %w", err)
	}
	err = unix.Ftruncate(fd, int64(len(b)))
	if err != nil {
		_ = unix.Close(fd)
		return 0, fmt.Errorf("ftruncate: %w", err)
	}
	data, err := unix.Mmap(fd, 0, len(b), unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		_ = unix.Close(fd)
		return 0, fmt.Errorf("mmap: %w", err)
	}
	copy(data, b)
	err = unix.Munmap(data)
	if err != nil {
		_ = unix.Close(fd)
		return 0, fmt.Errorf("munmap: %w", err)
	}
	_, err = unix.FcntlInt(uintptr(fd), unix.F_ADD_SEALS, unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK)
	if err != nil {
		_ = unix.Close(fd)
		return 0, fmt.Errorf("fcntl (add seals): %w", err)
	}
	return fd, nil
}

func (i *registryInspector) storeGlobalPullSecret(pullSecret []byte) {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	i.globalPullSecret = pullSecret
}

func newRegistryInspector() IRegistryInspector {
	ri := &registryInspector{}
	return ri
}
