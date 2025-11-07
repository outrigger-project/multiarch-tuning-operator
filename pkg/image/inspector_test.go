package image

import (
	"testing"
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
