package framework

import (
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
)

func Test_GetClusterMinorVersion(t *testing.T) {
	tests := []struct {
		name       string
		serverInfo *version.Info
		want       int
		wantErr    bool
	}{
		{
			name: "valid plain numeric version",
			serverInfo: &version.Info{
				Major: "1",
				Minor: "32",
			},
			want:    32,
			wantErr: false,
		},
		{
			name: "valid version with . in suffix",
			serverInfo: &version.Info{
				Major: "1",
				Minor: "31.alpha.3",
			},
			want:    31,
			wantErr: false,
		},
		{
			name: "valid version with rc in suffix",
			serverInfo: &version.Info{
				Major: "1",
				Minor: "30+rc1.1",
			},
			want:    30,
			wantErr: false,
		},
		{
			name: "valid version with + in suffix",
			serverInfo: &version.Info{
				Major: "1",
				Minor: "29+build1.sha123abc",
			},
			want:    29,
			wantErr: false,
		},
		{
			name: "valid version with - in suffix",
			serverInfo: &version.Info{
				Major: "1",
				Minor: "28-1beta2.3",
			},
			want:    28,
			wantErr: false,
		},
		{
			name: "invalid version string",
			serverInfo: &version.Info{
				Major: "1",
				Minor: "beta.1",
			},
			wantErr: true,
		},
		{
			name: "completely malformed version",
			serverInfo: &version.Info{
				Major: "1",
				Minor: "xyz",
			},
			wantErr: true,
		},
		{
			name: "empty version",
			serverInfo: &version.Info{
				Major: "1",
				Minor: "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGomegaWithT(t)
			client := fake.NewSimpleClientset()
			client.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = tt.serverInfo

			version, err := GetClusterMinorVersion(client)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred(), "failed to get server minor version", err)
				g.Expect(version).To(Equal(tt.want))
			}
		})
	}
}
