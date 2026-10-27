package updater

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestCaptureSnapshotInventoriesSelectedModulesAndClassification(t *testing.T) {
	root := t.TempDir()
	proxy := filepath.Join(t.TempDir(), "proxy")
	for _, modulePath := range []string{"example.test/direct", "example.test/indirect", "k8s.io/api"} {
		writeProxyModule(t, proxy, modulePath, map[string]string{
			"example.test/direct":   "v1.0.0",
			"example.test/indirect": "v1.0.0",
			"k8s.io/api":            "v0.36.5",
		}[modulePath], "1.20", nil)
	}
	writeModule(t, root, `module fixture.test/root

go 1.26.7

require (
	example.test/direct v1.0.0
	example.test/indirect v1.0.0 // indirect
	k8s.io/api v0.36.5
)
	`)
	setLocalProxy(t, proxy)

	snapshot, err := CaptureSnapshot(context.Background(), root, "go")
	if err != nil {
		t.Fatalf("CaptureSnapshot() error = %v", err)
	}
	if snapshot.GoDirective != "1.26.7" {
		t.Errorf("GoDirective = %q, want 1.26.7", snapshot.GoDirective)
	}
	if snapshot.Toolchain != "" {
		t.Errorf("Toolchain = %q, want absent", snapshot.Toolchain)
	}
	want := map[string]Module{
		"fixture.test/root":     {Path: "fixture.test/root", Main: true},
		"example.test/direct":   {Path: "example.test/direct", Version: "v1.0.0", Direct: true, Required: true},
		"example.test/indirect": {Path: "example.test/indirect", Version: "v1.0.0", Required: true},
		"k8s.io/api":            {Path: "k8s.io/api", Version: "v0.36.5", Direct: true, Required: true},
	}
	if len(snapshot.Modules) != len(want) {
		t.Fatalf("module count = %d, want %d: %#v", len(snapshot.Modules), len(want), snapshot.Modules)
	}
	for path, expected := range want {
		if got := snapshot.Modules[path]; got != expected {
			t.Errorf("Modules[%q] = %#v, want %#v", path, got, expected)
		}
	}
}

func TestCaptureSnapshotRejectsLocalAndRemoteReplacements(t *testing.T) {
	tests := []struct {
		name    string
		replace string
	}{
		{name: "local", replace: "replace example.test/direct => ../direct"},
		{name: "remote", replace: "replace example.test/direct => example.test/replacement v1.2.3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeModule(t, root, fmt.Sprintf("module fixture.test/root\n\ngo 1.26.7\n\n%s\n", tt.replace))
			_, err := CaptureSnapshot(context.Background(), root, "go")
			if err == nil || !strings.Contains(err.Error(), "replace") {
				t.Fatalf("CaptureSnapshot() error = %v, want clear replacement error", err)
			}
		})
	}
}

func TestCaptureSnapshotRejectsWorkspaceMode(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, "module fixture.test/root\n\ngo 1.26.7\n")
	if err := os.WriteFile(filepath.Join(root, "go.work"), []byte("go 1.26.7\n\nuse .\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := CaptureSnapshot(context.Background(), root, "go")
	if err == nil || !strings.Contains(err.Error(), "workspace") {
		t.Fatalf("CaptureSnapshot() error = %v, want clear workspace error", err)
	}
	if got := ExitCode(err); got != 1 {
		t.Fatalf("ExitCode(workspace error) = %d, want 1", got)
	}
}

func writeModule(t *testing.T, root, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func setLocalProxy(t *testing.T, proxy string) {
	t.Helper()
	proxyURL := (&url.URL{Scheme: "file", Path: proxy}).String()
	moduleCache := t.TempDir()
	t.Cleanup(func() {
		filepath.WalkDir(moduleCache, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if entry.IsDir() {
				return os.Chmod(path, 0o700)
			}
			return os.Chmod(path, 0o600)
		})
	})
	t.Setenv("GOPROXY", proxyURL)
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOMODCACHE", moduleCache)
}

func writeProxyModule(t *testing.T, proxy, modulePath, version, goVersion string, requirements map[string]string) {
	writeProxyModuleWithSource(t, proxy, modulePath, version, goVersion, requirements, "package fixture\n")
}

func writeProxyModuleWithSource(t *testing.T, proxy, modulePath, version, goVersion string, requirements map[string]string, source string) {
	t.Helper()
	versionDir := filepath.Join(proxy, filepath.FromSlash(modulePath), "@v")
	if err := os.MkdirAll(versionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, "list"), []byte(version+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := json.Marshal(map[string]string{"Version": version, "Time": "2025-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, version+".info"), info, 0o600); err != nil {
		t.Fatal(err)
	}
	var mod strings.Builder
	fmt.Fprintf(&mod, "module %s\n\ngo %s\n", modulePath, goVersion)
	if len(requirements) > 0 {
		mod.WriteString("\nrequire (\n")
		paths := make([]string, 0, len(requirements))
		for path := range requirements {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			requiredVersion := requirements[path]
			fmt.Fprintf(&mod, "\t%s %s\n", path, requiredVersion)
		}
		mod.WriteString(")\n")
	}
	if err := os.WriteFile(filepath.Join(versionDir, version+".mod"), []byte(mod.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	zipFile, err := os.Create(filepath.Join(versionDir, version+".zip"))
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(zipFile)
	entry := fmt.Sprintf("%s@%s/go.mod", modulePath, version)
	writer, err := archive.Create(entry)
	if err != nil {
		zipFile.Close()
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(mod.String())); err != nil {
		zipFile.Close()
		t.Fatal(err)
	}
	sourceWriter, err := archive.Create(fmt.Sprintf("%s@%s/placeholder.go", modulePath, version))
	if err != nil {
		zipFile.Close()
		t.Fatal(err)
	}
	if _, err := sourceWriter.Write([]byte(source)); err != nil {
		zipFile.Close()
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		zipFile.Close()
		t.Fatal(err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatal(err)
	}
}
