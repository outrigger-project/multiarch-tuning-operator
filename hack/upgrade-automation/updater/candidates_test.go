package updater

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestListCandidatesReturnsStableNewerVersionsNewestFirst(t *testing.T) {
	root := t.TempDir()
	proxy := filepath.Join(t.TempDir(), "proxy")
	versions := []string{"v1.2.0", "v1.1.9", "v1.2.1", "v1.3.0-rc.1", "v1.3.0"}
	for _, version := range versions {
		writeProxyModule(t, proxy, "example.test/lib", version, "1.20", nil)
	}
	writeProxyVersionList(t, proxy, "example.test/lib", versions)
	writeModule(t, root, "module fixture.test/root\n\ngo 1.26.7\n\nrequire example.test/lib v1.2.0\n")
	setLocalProxy(t, proxy)

	got, err := ListCandidates(context.Background(), GoRunner{Root: root, Binary: "go"}, "example.test/lib", "v1.2.0")
	if err != nil {
		t.Fatalf("ListCandidates() error = %v", err)
	}
	want := []string{"v1.3.0", "v1.2.1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListCandidates() = %v, want %v", got, want)
	}
}

func TestListCandidatesTreatsStableTagNewerThanPseudoVersion(t *testing.T) {
	root := t.TempDir()
	proxy := filepath.Join(t.TempDir(), "proxy")
	pseudo := "v1.2.1-0.20250101000000-abcdefabcdef"
	versions := []string{"v1.2.0", pseudo, "v1.2.1"}
	for _, version := range versions {
		writeProxyModule(t, proxy, "example.test/lib", version, "1.20", nil)
	}
	writeProxyVersionList(t, proxy, "example.test/lib", versions)
	writeModule(t, root, "module fixture.test/root\n\ngo 1.26.7\n\nrequire example.test/lib "+pseudo+"\n")
	setLocalProxy(t, proxy)

	got, err := ListCandidates(context.Background(), GoRunner{Root: root, Binary: "go"}, "example.test/lib", pseudo)
	if err != nil {
		t.Fatalf("ListCandidates() error = %v", err)
	}
	if !reflect.DeepEqual(got, []string{"v1.2.1"}) {
		t.Fatalf("ListCandidates() = %v, want [v1.2.1]", got)
	}
}

func writeProxyVersionList(t *testing.T, proxy, modulePath string, versions []string) {
	t.Helper()
	path := filepath.Join(proxy, filepath.FromSlash(modulePath), "@v", "list")
	content := strings.Join(versions, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
