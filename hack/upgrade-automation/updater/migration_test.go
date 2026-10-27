package updater

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenamedModulePathOnlyAcceptsMatchingPathMismatch(t *testing.T) {
	tests := []struct{ message, want string }{
		{`go: github.com/google/cel-go@v0.32.0: parsing go.mod:
module declares its path as: cel.dev/cel-go
        but was required as: github.com/google/cel-go`, "cel.dev/cel-go"},
		{`module declares its path as: cel.dev/cel-go
        but was required as: unrelated.test/module`, ""},
		{"proxy retrieval failed: 401 Unauthorized", ""},
		{`module declares its path as: ../local
        but was required as: github.com/google/cel-go`, ""},
	}
	for _, tt := range tests {
		if got := renamedModulePath(fmt.Errorf("%s", tt.message), "github.com/google/cel-go"); got != tt.want {
			t.Errorf("renamedModulePath(%q) = %q, want %q", tt.message, got, tt.want)
		}
	}
}

func TestImportMigrationOnlyChangesImportsOwnedByRenamedModule(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, "module fixture.test/root\ngo 1.26.7\n")
	source := `package fixture
// old.test/lib/pkg must not change in comments.
import (
    named "old.test/lib/pkg"
    _ "old.test/library/pkg"
    _ "old.test/lib/nested/pkg"
)
var text = "old.test/lib/pkg"
`
	writeFile(t, filepath.Join(root, "fixture.go"), source)
	writeFile(t, filepath.Join(root, "vendor", "keep.go"), "package vendor\nimport _ \"old.test/lib/pkg\"\n")
	writeFile(t, filepath.Join(root, "nested", "go.mod"), "module fixture.test/nested\ngo 1.26.7\n")
	writeFile(t, filepath.Join(root, "nested", "keep.go"), "package nested\nimport _ \"old.test/lib/pkg\"\n")
	before := snapshotFiles(t, root)
	transaction, err := BeginTransaction(root)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	checkpoint, err := transaction.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	graph := Snapshot{Modules: map[string]Module{
		"old.test/lib":        {Path: "old.test/lib", Version: "v1.0.0"},
		"old.test/lib/nested": {Path: "old.test/lib/nested", Version: "v1.0.0"},
	}}
	files, err := rewriteModuleImports(root, "old.test/lib", "new.test/lib", graph, transaction, checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "fixture.go" {
		t.Fatalf("rewritten files = %v", files)
	}
	data, err := os.ReadFile(filepath.Join(root, "fixture.go"))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(source, `named "old.test/lib/pkg"`, `named "new.test/lib/pkg"`, 1)
	if string(data) != want {
		t.Fatalf("rewritten source = %s, want %s", data, want)
	}
	if err := transaction.RestoreCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	after := snapshotFiles(t, root)
	for path, original := range before {
		if !bytes.Equal(original, after[path]) {
			t.Errorf("checkpoint rollback changed %s", path)
		}
	}
}

func TestMigrationReplacesRequirementAndRewritesImports(t *testing.T) {
	root, proxy := engineFixture(t)
	oldPath, newPath := "old.test/lib", "new.test/lib"
	writeProxyModule(t, proxy, oldPath, "v1.0.0", "1.20", nil)
	// Serve the renamed module's go.mod under the old path, as the real release does.
	writeProxyModule(t, proxy, oldPath, "v1.1.0", "1.20", nil)
	writeFile(t, filepath.Join(proxy, oldPath, "@v", "v1.1.0.mod"), "module "+newPath+"\n\ngo 1.20\n")
	writeProxyVersionList(t, proxy, oldPath, []string{"v1.0.0", "v1.1.0"})
	writeProxyModule(t, proxy, newPath, "v1.1.0", "1.20", nil)
	writeModule(t, root, "module fixture.test/root\n\ngo 1.26.7\nrequire "+oldPath+" v1.0.0\n")
	writeFile(t, filepath.Join(root, "fixture.go"), "package fixture\nimport _ \""+oldPath+"\"\n")
	setLocalProxy(t, proxy)
	report, err := Run(context.Background(), Config{Root: root, Policy: mustPolicy(t, "pin-both", "", nil), GoBinary: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Final.Modules[newPath].Version != "v1.1.0" {
		t.Fatalf("new module missing: %#v", report.Final.Modules)
	}
	if _, exists := report.Final.Modules[oldPath]; exists {
		t.Fatal("old module still selected without transitive users")
	}
	result := moduleResult(t, report, oldPath)
	if result.MigratedTo != newPath || result.Outcome != moduleUpdated {
		t.Fatalf("migration report = %#v", result)
	}
	if len(result.Attempts) != 1 || result.Attempts[0].TargetPath != newPath || len(result.Attempts[0].SourceFiles) != 1 {
		t.Fatalf("migration evidence = %#v", result.Attempts)
	}
	data, err := os.ReadFile(filepath.Join(root, "fixture.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"new.test/lib"`) {
		t.Fatalf("imports not migrated: %s", data)
	}
}

func TestMigrationRollbackRestoresImportsAfterLaterFailure(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, "module fixture.test/root\ngo 1.26.7\n")
	path := filepath.Join(root, "fixture.go")
	writeFile(t, path, "package fixture\nimport _ \"old.test/lib\"\n")
	transaction, err := BeginTransaction(root)
	if err != nil {
		t.Fatal(err)
	}
	first, err := transaction.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	graph := Snapshot{Modules: map[string]Module{}}
	if _, err := rewriteModuleImports(root, "old.test/lib", "new.test/lib", graph, transaction, first); err != nil {
		t.Fatal(err)
	}
	second, err := transaction.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rewriteModuleImports(root, "new.test/lib", "next.test/lib", graph, transaction, second); err != nil {
		t.Fatal(err)
	}
	if err := transaction.RestoreCheckpoint(second); err != nil {
		t.Fatal(err)
	}
	assertFile(t, path, "package fixture\nimport _ \"new.test/lib\"\n")
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertFile(t, path, "package fixture\nimport _ \"old.test/lib\"\n")
}

func TestMigrationRejectedByPinsRestoresSourceAndTriesOlderCandidate(t *testing.T) {
	root, proxy := engineFixture(t)
	for _, version := range []string{"v1.0.0", "v1.0.1", "v1.1.0"} {
		writeProxyModule(t, proxy, "old.test/lib", version, "1.20", nil)
	}
	writeFile(t, filepath.Join(proxy, "old.test/lib/@v/v1.1.0.mod"), "module new.test/lib\n\ngo 1.20\n")
	writeProxyVersionList(t, proxy, "old.test/lib", []string{"v1.0.0", "v1.0.1", "v1.1.0"})
	writeProxyModule(t, proxy, "new.test/lib", "v1.1.0", "1.20", map[string]string{"k8s.io/api": "v0.36.6"})
	for _, version := range []string{"v0.36.5", "v0.36.6"} {
		writeProxyModule(t, proxy, "k8s.io/api", version, "1.20", nil)
	}
	writeProxyVersionList(t, proxy, "k8s.io/api", []string{"v0.36.5", "v0.36.6"})
	writeModule(t, root, "module fixture.test/root\n\ngo 1.26.7\nrequire (\nold.test/lib v1.0.0\nk8s.io/api v0.36.5\n)\n")
	source := "package fixture\nimport (\n_ \"old.test/lib\"\n_ \"k8s.io/api\"\n)\n"
	writeFile(t, filepath.Join(root, "fixture.go"), source)
	setLocalProxy(t, proxy)
	report, err := Run(context.Background(), Config{Root: root, Policy: mustPolicy(t, "pin-both", "", nil), GoBinary: "go"})
	if err != nil {
		t.Fatal(err)
	}
	result := moduleResult(t, report, "old.test/lib")
	if result.FinalVersion != "v1.0.1" || result.MigratedTo != "" || len(result.Attempts) != 2 {
		t.Fatalf("fallback result = %#v", result)
	}
	if result.Attempts[0].TargetPath != "new.test/lib" || result.Attempts[0].Outcome != moduleBlocked {
		t.Fatalf("rejected migration missing evidence: %#v", result.Attempts)
	}
	assertFile(t, filepath.Join(root, "fixture.go"), source)
	if report.Final.Modules["k8s.io/api"].Version != "v0.36.5" {
		t.Fatal("Kubernetes pin changed")
	}
}

func TestMigrationCompileFailureRestoresSourceAndTriesOlderCandidate(t *testing.T) {
	root, proxy := engineFixture(t)
	for _, version := range []string{"v1.0.0", "v1.0.1", "v1.1.0"} {
		writeProxyModuleWithSource(t, proxy, "old.test/lib", version, "1.20", nil, "package fixture\nconst Value = 1\n")
	}
	writeFile(t, filepath.Join(proxy, "old.test/lib/@v/v1.1.0.mod"), "module new.test/lib\n\ngo 1.20\n")
	writeProxyVersionList(t, proxy, "old.test/lib", []string{"v1.0.0", "v1.0.1", "v1.1.0"})
	writeProxyModule(t, proxy, "new.test/lib", "v1.1.0", "1.20", nil)
	writeModule(t, root, "module fixture.test/root\n\ngo 1.26.7\nrequire old.test/lib v1.0.0\n")
	source := "package fixture\nimport lib \"old.test/lib\"\nvar _ = lib.Value\n"
	writeFile(t, filepath.Join(root, "fixture.go"), source)
	setLocalProxy(t, proxy)
	report, err := Run(context.Background(), Config{Root: root, Policy: mustPolicy(t, "pin-both", "", nil), GoBinary: "go"})
	if err != nil {
		t.Fatal(err)
	}
	result := moduleResult(t, report, "old.test/lib")
	if result.FinalVersion != "v1.0.1" || len(result.Attempts) != 2 || result.Attempts[0].Outcome != moduleBlocked {
		t.Fatalf("compile-rejected fallback result = %#v", result)
	}
	assertFile(t, filepath.Join(root, "fixture.go"), source)
}
