package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/containers/image/v5/manifest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"

	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

const (
	GlobalPullSecretUser     = "global-pull-secret-user"
	GlobalPullSecretPassword = "global-pull-secret-password"
	GlobalRepo               = "global-repo"
	LocalPullSecretUser1     = "local-secret-user-1"
	LocalPullSecretPassword1 = "local-secret-password-1"
	User1Repo                = "user-1-repo"
	LocalPullSecretUser2     = "local-secret-user-2"
	LocalPullSecretPassword2 = "local-secret-password-2"
	User2Repo                = "user-2-repo"
	CommonRepo               = "common-repo"
	PublicRepo               = "public-repo"
)

var log = ctrl.Log.WithName("fake-registry")
var mockImages []MockImage

func SeedMockRegistry(ctx context.Context) (err error) {
	authFilePath := makeAuthFile()
	for _, image := range GetMockImages() {
		_, _, err = image.pushImage(ctx, authFilePath)
		if err != nil {
			return
		}
	}
	return nil
}

// GetMockImages returns a list of mock images. In particular, it generates a list of images for each
// MediaType and available repository. For List types, it generates both a list of images with multiple
// architectures and a list of images with a single architecture.
func GetMockImages() []MockImage {
	if len(mockImages) > 0 {
		return mockImages
	}
	for _, repo := range []string{GlobalRepo, User1Repo, User2Repo, CommonRepo, PublicRepo} {
		for _, mediaType := range imageMediaTypes() {
			if manifest.MIMETypeIsMultiImage(mediaType) {
				mockImages = append(mockImages, MockImage{
					Architectures: sets.New[string](utils.ArchitectureArm64, utils.ArchitectureAmd64),
					MediaType:     mediaType,
					Repository:    repo,
					Name:          ComputeNameByMediaType(mediaType),
					Tag:           "latest",
				})
			} else {
				mockImages = append(mockImages, MockImage{
					Architectures: sets.New[string](utils.ArchitecturePpc64le),
					MediaType:     mediaType,
					Repository:    repo,
					Name:          ComputeNameByMediaType(mediaType),
					Tag:           "latest",
				})
			}
		}
	}
	mockImages = append(mockImages, MockImage{
		Architectures: sets.New[string](utils.ArchitecturePpc64le, utils.ArchitectureArm64),
		MediaType:     imgspecv1.MediaTypeImageIndex,
		Repository:    PublicRepo,
		Name:          ComputeNameByMediaType(imgspecv1.MediaTypeImageIndex, "bundle"),
		Tag:           "latest",
		Labels: map[string]string{
			"operators.operatorframework.io.metrics.builder": "",
		},
	}, MockImage{
		Architectures: sets.New[string](utils.ArchitecturePpc64le),
		MediaType:     imgspecv1.MediaTypeImageManifest,
		Repository:    PublicRepo,
		Name:          ComputeNameByMediaType(imgspecv1.MediaTypeImageManifest, "bundle"),
		Tag:           "latest",
		Labels: map[string]string{
			"operators.operatorframework.io.bundle.package.v1": "",
		},
	}, MockImage{
		Architectures: sets.New[string](utils.ArchitecturePpc64le, utils.ArchitectureS390x),
		MediaType:     imgspecv1.MediaTypeImageIndex,
		Repository:    PublicRepo,
		Name:          ComputeNameByMediaType(imgspecv1.MediaTypeImageIndex, "ppc64le-s390x"),
		Tag:           "latest",
	})
	return mockImages
}

func PushMockImage(ctx context.Context, newImage *MockImage) error {
	authFilePath := makeAuthFile()
	_, _, err := newImage.pushImage(ctx, authFilePath)
	if err != nil {
		return err
	}
	for i, image := range GetMockImages() {
		if image.Equals(newImage) {
			mockImages = append(mockImages[:i], mockImages[i+1:]...)
			break
		}
	}
	mockImages = append(mockImages, *newImage)
	return nil
}

// ComputeNameByMediaType returns the name of the image given the media type.
// The name of the image is given by the media type, replacing the "." with "-" and
// removing the "+...." suffix and "application/" prefix.
// The tag is given by the indices of the for loops.
func ComputeNameByMediaType(mediaType string, suffixes ...string) string {
	name := strings.Split(mediaType, "+")[0]
	name = strings.Split(name, "/")[1]
	name = strings.ReplaceAll(name, ".", "-")
	for _, suffix := range suffixes {
		name = fmt.Sprintf("%s-%s", name, suffix)
	}
	return name
}

// imageMediaTypes returns a list of image media types supported by the mock registry for which we want to
// generate mock images.
func imageMediaTypes() []string {
	return []string{
		imgspecv1.MediaTypeImageManifest,  // <- application/vnd.oci.image.manifest.v1+json docker://registry.ci.openshift.org/openshift/centos@sha256:dad7fffc7460a52e341a2899bd786bb307f0ba27c0301da815115f94143174e4
		manifest.DockerV2Schema2MediaType, // <- application/vnd.docker.distribution.manifest.v2+json docker://quay.io/centos/centos:7
		// manifest.DockerV2Schema1SignedMediaType, // <- application/vnd.docker.distribution.manifest.v1+prettyjws // TODO
		// manifest.DockerV2Schema1MediaType,       // <- application/vnd.docker.distribution.manifest.v1+json // not supported
		manifest.DockerV2ListMediaType, // <- application/vnd.docker.distribution.manifest.list.v2+json  docker://quay.io/openshifttest/hello-openshift:1.2.0
		imgspecv1.MediaTypeImageIndex,  // <- application/vnd.oci.image.index.v1+json docker://quay.io/centos/centos:stream9
	}
}

// getMockCredentials returns a map of username:password for each user.
func getMockCredentials() map[string]string {
	return map[string]string{
		GlobalPullSecretUser: GlobalPullSecretPassword,
		LocalPullSecretUser1: LocalPullSecretPassword1,
		LocalPullSecretUser2: LocalPullSecretPassword2,
	}
}

// getMockAllowedUsersByRepos returns a map of repository:users that are allowed to pull from that repository.
// it is used to generate the auth configuration for the registry
func getMockAllowedUsersByRepos() map[string]sets.Set[string] {
	return map[string]sets.Set[string]{
		GlobalRepo: sets.New[string](GlobalPullSecretUser),
		User1Repo:  sets.New[string](LocalPullSecretUser1),
		User2Repo:  sets.New[string](LocalPullSecretUser2),
		CommonRepo: sets.New[string](GlobalPullSecretUser, LocalPullSecretUser2),
	}
}

// makeAuthFile creates a temporary file containing the auth configuration for the mock registry.
func makeAuthFile() string {
	auth := map[string]map[string]map[string]string{
		"auths": {},
	}
	auths := auth["auths"]
	for repo, users := range getMockAllowedUsersByRepos() {
		// Since the purpose is testing, we can assume that the set is not empty
		user, _ := users.PopAny()
		pass := getMockCredentials()[user]
		b64encoded := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", user, pass)))
		auths[fmt.Sprintf("%s/%s", url, repo)] = map[string]string{
			"auth": b64encoded,
		}
	}
	j, err := json.Marshal(auth)
	if err != nil {
		panic(err)
	}
	// Write to file
	f, err := os.CreateTemp(os.TempDir(), "integration-testing-auth-file-*.json")

	if err != nil {
		panic(err)
	}
	//nolint:errcheck
	defer f.Close()
	_, err = f.Write(j)
	if err != nil {
		panic(err)
	}
	return f.Name()
}
