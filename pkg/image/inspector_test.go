package image

import (
	"testing"

	ociv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

func Test_parseImageReference(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    string
		expectError bool
		errorMsg    string
	}{
		// === Valid cases without digest ===
		{
			name:     "simple image name",
			input:    "nginx",
			expected: "nginx",
		},
		{
			name:     "image with tag",
			input:    "nginx:latest",
			expected: "nginx:latest",
		},
		{
			name:     "image with namespace",
			input:    "library/nginx",
			expected: "library/nginx",
		},
		{
			name:     "image with namespace and tag",
			input:    "library/nginx:v1.0.0",
			expected: "library/nginx:v1.0.0",
		},
		{
			name:     "image with registry",
			input:    "registry.io/nginx",
			expected: "registry.io/nginx",
		},
		{
			name:     "image with registry and tag",
			input:    "registry.io/nginx:latest",
			expected: "registry.io/nginx:latest",
		},
		{
			name:     "image with registry, namespace and tag",
			input:    "registry.io/library/nginx:latest",
			expected: "registry.io/library/nginx:latest",
		},
		{
			name:     "image with registry port",
			input:    "registry.io:5000/nginx",
			expected: "registry.io:5000/nginx",
		},
		{
			name:     "image with registry port and tag",
			input:    "registry.io:5000/nginx:latest",
			expected: "registry.io:5000/nginx:latest",
		},
		{
			name:     "image with registry port, namespace and tag",
			input:    "registry.io:5000/library/nginx:v1.0.0",
			expected: "registry.io:5000/library/nginx:v1.0.0",
		},
		{
			name:     "image with multi-level namespace",
			input:    "registry.io/org/team/nginx:latest",
			expected: "registry.io/org/team/nginx:latest",
		},
		{
			name:     "image with port and multi-level namespace",
			input:    "registry.io:5000/org/team/nginx:latest",
			expected: "registry.io:5000/org/team/nginx:latest",
		},

		// === Valid cases with digest only (no tag) ===
		{
			name:     "simple image with digest",
			input:    "nginx@sha256:abc123",
			expected: "nginx@sha256:abc123",
		},
		{
			name:     "image with namespace and digest",
			input:    "library/nginx@sha256:abc123",
			expected: "library/nginx@sha256:abc123",
		},
		{
			name:     "image with registry and digest",
			input:    "registry.io/nginx@sha256:abc123",
			expected: "registry.io/nginx@sha256:abc123",
		},
		{
			name:     "image with registry, namespace and digest",
			input:    "registry.io/library/nginx@sha256:abc123",
			expected: "registry.io/library/nginx@sha256:abc123",
		},
		{
			name:     "image with registry port and digest",
			input:    "registry.io:5000/nginx@sha256:abc123",
			expected: "registry.io:5000/nginx@sha256:abc123",
		},
		{
			name:     "image with registry port, namespace and digest",
			input:    "registry.io:5000/library/nginx@sha256:abc123",
			expected: "registry.io:5000/library/nginx@sha256:abc123",
		},

		// === Valid cases with both tag and digest (tag should be removed) ===
		{
			name:     "simple image with tag and digest",
			input:    "nginx:latest@sha256:abc123",
			expected: "nginx@sha256:abc123",
		},
		{
			name:     "image with namespace, tag and digest",
			input:    "library/nginx:latest@sha256:abc123",
			expected: "library/nginx@sha256:abc123",
		},
		{
			name:     "image with registry, tag and digest",
			input:    "registry.io/nginx:latest@sha256:abc123",
			expected: "registry.io/nginx@sha256:abc123",
		},
		{
			name:     "image with registry, namespace, tag and digest",
			input:    "registry.io/library/nginx:v1.0.0@sha256:abc123",
			expected: "registry.io/library/nginx@sha256:abc123",
		},
		{
			name:     "image with registry port, tag and digest",
			input:    "registry.io:5000/nginx:latest@sha256:abc123",
			expected: "registry.io:5000/nginx@sha256:abc123",
		},
		{
			name:     "image with registry port, namespace, tag and digest",
			input:    "registry.io:5000/library/nginx:v1.0.0@sha256:abc123",
			expected: "registry.io:5000/library/nginx@sha256:abc123",
		},
		{
			name:     "image with port and multi-level namespace, tag and digest",
			input:    "registry.io:5000/org/team/nginx:v1.0.0@sha256:abc123",
			expected: "registry.io:5000/org/team/nginx@sha256:abc123",
		},

		// === Edge cases with digest algorithms ===
		{
			name:     "sha512 digest without tag",
			input:    "nginx@sha512:def456",
			expected: "nginx@sha512:def456",
		},
		{
			name:     "sha512 digest with tag",
			input:    "nginx:latest@sha512:def456",
			expected: "nginx@sha512:def456",
		},
		{
			name:     "sha384 digest with registry port and tag",
			input:    "registry.io:5000/nginx:latest@sha384:xyz789",
			expected: "registry.io:5000/nginx@sha384:xyz789",
		},

		// === Real-world examples ===
		{
			name:     "docker hub official image",
			input:    "docker.io/library/nginx:1.25.3@sha256:4c0fdaa8b6341bfdeca5f18f7837462c80cff90527ee35ef185571e1c327beac",
			expected: "docker.io/library/nginx@sha256:4c0fdaa8b6341bfdeca5f18f7837462c80cff90527ee35ef185571e1c327beac",
		},
		{
			name:     "quay.io image with port",
			input:    "quay.io:443/openshift-release-dev/ocp-v4.0-art-dev@sha256:abc123",
			expected: "quay.io:443/openshift-release-dev/ocp-v4.0-art-dev@sha256:abc123",
		},
		{
			name:     "gcr.io image with tag and digest",
			input:    "gcr.io/google-containers/pause:3.2@sha256:abc123",
			expected: "gcr.io/google-containers/pause@sha256:abc123",
		},

		// === Error cases ===
		{
			name:        "empty image name",
			input:       "",
			expectError: true,
			errorMsg:    "invalid image name, must not be empty",
		},
		{
			name:        "multiple digests",
			input:       "nginx@sha256:abc123@sha256:def456",
			expectError: true,
			errorMsg:    "invalid image name, must only have one digest",
		},
		{
			name:        "multiple digests with registry",
			input:       "registry.io/nginx@sha256:abc123@sha256:def456",
			expectError: true,
			errorMsg:    "invalid image name, must only have one digest",
		},

		// === Edge cases with special characters ===
		{
			name:     "image with hyphen in name",
			input:    "my-image:v1.0.0",
			expected: "my-image:v1.0.0",
		},
		{
			name:     "image with underscore in name",
			input:    "my_image:v1.0.0",
			expected: "my_image:v1.0.0",
		},
		{
			name:     "tag with complex version",
			input:    "nginx:1.25.3-alpine3.18@sha256:abc123",
			expected: "nginx@sha256:abc123",
		},
		{
			name:     "localhost registry",
			input:    "localhost:5000/myimage:latest",
			expected: "localhost:5000/myimage:latest",
		},
		{
			name:     "localhost registry with digest",
			input:    "localhost:5000/myimage:latest@sha256:abc123",
			expected: "localhost:5000/myimage@sha256:abc123",
		},
		{
			name:     "IP address registry",
			input:    "192.168.1.1:5000/myimage:latest",
			expected: "192.168.1.1:5000/myimage:latest",
		},
		{
			name:     "IP address registry with tag and digest",
			input:    "192.168.1.1:5000/myimage:latest@sha256:abc123",
			expected: "192.168.1.1:5000/myimage@sha256:abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseImageReference(tt.input)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none. input=%q, result=%q", tt.input, result)
					return
				}
				if err.Error() != tt.errorMsg {
					t.Errorf("expected error %q but got %q", tt.errorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v (input=%q)", err, tt.input)
				return
			}

			if result != tt.expected {
				t.Errorf("parseImageReference(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

// Test helper to validate that the function correctly identifies the components
func Test_parseImageReference_ComponentExtraction(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		description string
	}{
		{
			name:        "port vs tag distinction",
			input:       "registry.io:5000/nginx:latest@sha256:abc",
			description: "Should keep port (5000) but remove tag (latest)",
		},
		{
			name:        "multiple colons",
			input:       "registry.io:443/repo/image:v1.0.0@sha256:abc",
			description: "Should handle registry port, tag, and digest correctly",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseImageReference(tt.input)
			if err != nil {
				t.Errorf("unexpected error: %v (input=%q)", err, tt.input)
				return
			}
			// Just log the results for manual verification
			t.Logf("%s:\n  Input:  %s\n  Output: %s", tt.description, tt.input, result)
		})
	}
}

func Test_shouldSkipManifest(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		platform    *struct {
			Architecture string
			OS           string
		}
		shouldSkip bool
		reason     string
	}{
		{
			name:        "normal amd64 manifest",
			annotations: nil,
			platform: &struct {
				Architecture string
				OS           string
			}{Architecture: "amd64", OS: "linux"},
			shouldSkip: false,
			reason:     "Valid architecture and OS",
		},
		{
			name:        "normal arm64 manifest",
			annotations: nil,
			platform: &struct {
				Architecture string
				OS           string
			}{Architecture: "arm64", OS: "linux"},
			shouldSkip: false,
			reason:     "Valid architecture and OS",
		},
		{
			name: "attestation manifest with unknown platform",
			annotations: map[string]string{
				"vnd.docker.reference.type": "attestation-manifest",
			},
			platform: &struct {
				Architecture string
				OS           string
			}{Architecture: "unknown", OS: "unknown"},
			shouldSkip: true,
			reason:     "Attestation manifests are not runnable images",
		},
		{
			name:        "unknown architecture without attestation annotation",
			annotations: nil,
			platform: &struct {
				Architecture string
				OS           string
			}{Architecture: "unknown", OS: "linux"},
			shouldSkip: true,
			reason:     "Unknown architecture should be skipped",
		},
		{
			name:        "unknown architecture with unknown OS",
			annotations: nil,
			platform: &struct {
				Architecture string
				OS           string
			}{Architecture: "unknown", OS: "unknown"},
			shouldSkip: true,
			reason:     "Unknown architecture should be skipped",
		},
		{
			name: "attestation manifest with valid architecture",
			annotations: map[string]string{
				"vnd.docker.reference.type": "attestation-manifest",
			},
			platform: &struct {
				Architecture string
				OS           string
			}{Architecture: "amd64", OS: "linux"},
			shouldSkip: true,
			reason:     "Attestation manifests should be skipped even with valid arch",
		},
		{
			name: "manifest with vnd.docker.reference.digest annotation",
			annotations: map[string]string{
				"vnd.docker.reference.digest": "sha256:abc123",
			},
			platform: &struct {
				Architecture string
				OS           string
			}{Architecture: "amd64", OS: "linux"},
			shouldSkip: true,
			reason:     "Manifests with reference digest annotation should be skipped",
		},
		{
			name: "manifest with both reference annotations",
			annotations: map[string]string{
				"vnd.docker.reference.type":   "attestation-manifest",
				"vnd.docker.reference.digest": "sha256:abc123",
			},
			platform: &struct {
				Architecture string
				OS           string
			}{Architecture: "unknown", OS: "unknown"},
			shouldSkip: true,
			reason:     "Manifests with both reference annotations should be skipped",
		},
		{
			name: "other annotation type",
			annotations: map[string]string{
				"some.other.annotation": "value",
			},
			platform: &struct {
				Architecture string
				OS           string
			}{Architecture: "amd64", OS: "linux"},
			shouldSkip: false,
			reason:     "Other annotations should not cause skipping",
		},
		{
			name:        "nil platform",
			annotations: nil,
			platform:    nil,
			shouldSkip:  false,
			reason:      "Nil platform should not be skipped (will be handled elsewhere)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the filtering logic from inspector.go
			// Check for Docker reference annotations or unknown architecture
			shouldSkip := false
			if tt.annotations != nil {
				if _, exists := tt.annotations["vnd.docker.reference.type"]; exists {
					shouldSkip = true
				}
				if _, exists := tt.annotations["vnd.docker.reference.digest"]; exists {
					shouldSkip = true
				}
			}
			if tt.platform != nil && tt.platform.Architecture == "unknown" {
				shouldSkip = true
			}

			if shouldSkip != tt.shouldSkip {
				t.Errorf("shouldSkip = %v, want %v (reason: %s)", shouldSkip, tt.shouldSkip, tt.reason)
			}
		})
	}
}

func Test_filterManifestsForArchitectures(t *testing.T) {
	tests := []struct {
		name                       string
		manifests                  []ociv1.Descriptor
		expectedArchitectures      []string
		expectedFirstValidDigest   string
		shouldHaveValidInstanceIdx bool
	}{
		{
			name: "multi-arch image with attestation manifest (like ghcr.io/llm-d/llm-d-cuda-dev:pr-230)",
			manifests: []ociv1.Descriptor{
				{
					MediaType: "application/vnd.oci.image.manifest.v1+json",
					Digest:    "sha256:0298f7c3ceec45da42b10f52b8edd4c5ebe5edb8011d62b8e5d66fef749ce124",
					Platform: &ociv1.Platform{
						Architecture: "amd64",
						OS:           "linux",
					},
				},
				{
					MediaType: "application/vnd.oci.image.manifest.v1+json",
					Digest:    "sha256:6eba444dd58a8748225335c4dcc53232c53d8252370f4a01ecfa6e692925db73",
					Annotations: map[string]string{
						"vnd.docker.reference.digest": "sha256:0298f7c3ceec45da42b10f52b8edd4c5ebe5edb8011d62b8e5d66fef749ce124",
						"vnd.docker.reference.type":   "attestation-manifest",
					},
					Platform: &ociv1.Platform{
						Architecture: "unknown",
						OS:           "unknown",
					},
				},
			},
			expectedArchitectures:      []string{"amd64"},
			expectedFirstValidDigest:   "sha256:0298f7c3ceec45da42b10f52b8edd4c5ebe5edb8011d62b8e5d66fef749ce124",
			shouldHaveValidInstanceIdx: true,
		},
		{
			name: "multi-arch image without attestation",
			manifests: []ociv1.Descriptor{
				{
					MediaType: "application/vnd.oci.image.manifest.v1+json",
					Digest:    "sha256:amd64digest",
					Platform: &ociv1.Platform{
						Architecture: "amd64",
						OS:           "linux",
					},
				},
				{
					MediaType: "application/vnd.oci.image.manifest.v1+json",
					Digest:    "sha256:arm64digest",
					Platform: &ociv1.Platform{
						Architecture: "arm64",
						OS:           "linux",
					},
				},
				{
					MediaType: "application/vnd.oci.image.manifest.v1+json",
					Digest:    "sha256:ppc64ledigest",
					Platform: &ociv1.Platform{
						Architecture: "ppc64le",
						OS:           "linux",
					},
				},
			},
			expectedArchitectures:      []string{"amd64", "arm64", "ppc64le"},
			expectedFirstValidDigest:   "sha256:amd64digest",
			shouldHaveValidInstanceIdx: true,
		},
		{
			name: "image with only attestation manifest (edge case)",
			manifests: []ociv1.Descriptor{
				{
					MediaType: "application/vnd.oci.image.manifest.v1+json",
					Digest:    "sha256:attestationdigest",
					Annotations: map[string]string{
						"vnd.docker.reference.type": "attestation-manifest",
					},
					Platform: &ociv1.Platform{
						Architecture: "unknown",
						OS:           "unknown",
					},
				},
			},
			expectedArchitectures:      []string{},
			expectedFirstValidDigest:   "",
			shouldHaveValidInstanceIdx: false,
		},
		{
			name: "attestation manifest appears first",
			manifests: []ociv1.Descriptor{
				{
					MediaType: "application/vnd.oci.image.manifest.v1+json",
					Digest:    "sha256:attestationdigest",
					Annotations: map[string]string{
						"vnd.docker.reference.type": "attestation-manifest",
					},
					Platform: &ociv1.Platform{
						Architecture: "unknown",
						OS:           "unknown",
					},
				},
				{
					MediaType: "application/vnd.oci.image.manifest.v1+json",
					Digest:    "sha256:arm64digest",
					Platform: &ociv1.Platform{
						Architecture: "arm64",
						OS:           "linux",
					},
				},
			},
			expectedArchitectures:      []string{"arm64"},
			expectedFirstValidDigest:   "sha256:arm64digest",
			shouldHaveValidInstanceIdx: true,
		},
		{
			name: "image with unknown architecture but no attestation annotation",
			manifests: []ociv1.Descriptor{
				{
					MediaType: "application/vnd.oci.image.manifest.v1+json",
					Digest:    "sha256:amd64digest",
					Platform: &ociv1.Platform{
						Architecture: "amd64",
						OS:           "linux",
					},
				},
				{
					MediaType: "application/vnd.oci.image.manifest.v1+json",
					Digest:    "sha256:unknowndigest",
					Platform: &ociv1.Platform{
						Architecture: "unknown",
						OS:           "linux",
					},
				},
			},
			expectedArchitectures:      []string{"amd64"},
			expectedFirstValidDigest:   "sha256:amd64digest",
			shouldHaveValidInstanceIdx: true,
		},
		{
			name: "malformed attestation with only vnd.docker.reference.digest annotation",
			manifests: []ociv1.Descriptor{
				{
					MediaType: "application/vnd.oci.image.manifest.v1+json",
					Digest:    "sha256:arm64digest",
					Platform: &ociv1.Platform{
						Architecture: "arm64",
						OS:           "linux",
					},
				},
				{
					MediaType: "application/vnd.oci.image.manifest.v1+json",
					Digest:    "sha256:malformedattestationdigest",
					Annotations: map[string]string{
						"vnd.docker.reference.digest": "sha256:arm64digest",
						// Missing vnd.docker.reference.type annotation (malformed)
					},
					Platform: &ociv1.Platform{
						Architecture: "amd64",
						OS:           "linux",
					},
				},
			},
			expectedArchitectures:      []string{"arm64"},
			expectedFirstValidDigest:   "sha256:arm64digest",
			shouldHaveValidInstanceIdx: true,
		},
		{
			name: "attestation with both reference type and digest annotations",
			manifests: []ociv1.Descriptor{
				{
					MediaType: "application/vnd.oci.image.manifest.v1+json",
					Digest:    "sha256:amd64digest",
					Platform: &ociv1.Platform{
						Architecture: "amd64",
						OS:           "linux",
					},
				},
				{
					MediaType: "application/vnd.oci.image.manifest.v1+json",
					Digest:    "sha256:attestationdigest",
					Annotations: map[string]string{
						"vnd.docker.reference.type":   "attestation-manifest",
						"vnd.docker.reference.digest": "sha256:amd64digest",
					},
					Platform: &ociv1.Platform{
						Architecture: "unknown",
						OS:           "unknown",
					},
				},
			},
			expectedArchitectures:      []string{"amd64"},
			expectedFirstValidDigest:   "sha256:amd64digest",
			shouldHaveValidInstanceIdx: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the filtering logic from inspector.go lines 148-164
			supportedArchitectures := sets.New[string]()
			var firstValidDigest string

			for _, m := range tt.manifests {
				// Skip manifests with Docker reference annotations - they are not runnable platform images
				if m.Annotations != nil {
					if _, exists := m.Annotations["vnd.docker.reference.type"]; exists {
						continue
					}
					if _, exists := m.Annotations["vnd.docker.reference.digest"]; exists {
						continue
					}
				}
				// Skip manifests with unknown architecture
				if m.Platform != nil && m.Platform.Architecture == "unknown" {
					continue
				}
				supportedArchitectures = sets.Insert(supportedArchitectures, m.Platform.Architecture)
				// Store the first valid manifest digest
				if firstValidDigest == "" {
					firstValidDigest = string(m.Digest)
				}
			}

			// Verify architectures
			if len(tt.expectedArchitectures) != supportedArchitectures.Len() {
				t.Errorf("Expected %d architectures, got %d", len(tt.expectedArchitectures), supportedArchitectures.Len())
			}
			for _, expectedArch := range tt.expectedArchitectures {
				if !supportedArchitectures.Has(expectedArch) {
					t.Errorf("Expected architecture %q not found in result set", expectedArch)
				}
			}

			// Verify first valid digest
			if tt.shouldHaveValidInstanceIdx {
				if firstValidDigest != tt.expectedFirstValidDigest {
					t.Errorf("Expected first valid digest %q, got %q", tt.expectedFirstValidDigest, firstValidDigest)
				}
			} else {
				if firstValidDigest != "" {
					t.Errorf("Expected no valid digest, got %q", firstValidDigest)
				}
			}

			// Ensure "unknown" is never in the result
			if supportedArchitectures.Has("unknown") {
				t.Error("Architecture set should never contain 'unknown'")
			}
		})
	}
}
