package updater

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

var testGoVersion = strings.TrimPrefix(strings.Fields(runtime.Version())[0], "go")

const futureGoVersion = "999.0.0"

func TestEngineRejectsLatestGoIncompatibleThenAcceptsOlderVersion(t *testing.T) {
	root, proxy := engineFixture(t)
	writeProxyModule(t, proxy, "example.test/lib", "v1.0.0", "1.20", nil)
	writeProxyModule(t, proxy, "example.test/lib", "v1.2.0", futureGoVersion, nil)
	writeProxyModule(t, proxy, "example.test/lib", "v1.1.0", "1.20", nil)
	writeProxyVersionList(t, proxy, "example.test/lib", []string{"v1.0.0", "v1.1.0", "v1.2.0"})
	writeModule(t, root, "module fixture.test/root\n\ngo "+testGoVersion+"\n\nrequire example.test/lib v1.0.0\n")
	writeFile(t, filepath.Join(root, "fixture.go"), "package fixture\n\nimport _ \"example.test/lib\"\n")
	setLocalProxy(t, proxy)

	report, err := Run(context.Background(), Config{Root: root, Policy: mustPolicy(t, "pin-both", "", nil), GoBinary: "go"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	result := moduleResult(t, report, "example.test/lib")
	if result.InitialVersion != "v1.0.0" || result.FinalVersion != "v1.1.0" || result.Outcome != "updated" {
		t.Fatalf("lib result = %#v, want v1.0.0 -> v1.1.0 updated", result)
	}
	if len(result.Attempts) != 2 || result.Attempts[0].Version != "v1.2.0" || result.Attempts[0].Outcome != "blocked-by-constraints" || result.Attempts[1].Version != "v1.1.0" || result.Attempts[1].Outcome != "accepted" {
		t.Fatalf("lib attempts = %#v, want latest Go-incompatible then older accepted", result.Attempts)
	}
	if report.Status != "completed" {
		t.Fatalf("report status = %q, want completed", report.Status)
	}
}

func TestEngineReportsNoCompatibleUpgrade(t *testing.T) {
	root, proxy := engineFixture(t)
	writeProxyModule(t, proxy, "example.test/lib", "v1.0.0", "1.20", nil)
	writeProxyModule(t, proxy, "example.test/lib", "v1.1.0", futureGoVersion, nil)
	writeProxyVersionList(t, proxy, "example.test/lib", []string{"v1.0.0", "v1.1.0"})
	writeModule(t, root, "module fixture.test/root\n\ngo "+testGoVersion+"\n\nrequire example.test/lib v1.0.0\n")
	writeFile(t, filepath.Join(root, "fixture.go"), "package fixture\n\nimport _ \"example.test/lib\"\n")
	setLocalProxy(t, proxy)

	report, err := Run(context.Background(), Config{Root: root, Policy: mustPolicy(t, "pin-both", "", nil), GoBinary: "go"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	result := moduleResult(t, report, "example.test/lib")
	if result.FinalVersion != "v1.0.0" || result.Outcome != "blocked-by-constraints" {
		t.Fatalf("lib result = %#v, want unchanged and blocked", result)
	}
}

func TestEngineRejectsTransitiveKubernetesChangeWhenPinned(t *testing.T) {
	root, proxy := engineFixture(t)
	writeProxyModule(t, proxy, "example.test/lib", "v1.0.0", "1.20", nil)
	writeProxyModule(t, proxy, "example.test/lib", "v1.1.0", "1.20", map[string]string{"k8s.io/api": "v0.36.6"})
	writeProxyVersionList(t, proxy, "example.test/lib", []string{"v1.0.0", "v1.1.0"})
	writeProxyModule(t, proxy, "k8s.io/api", "v0.36.5", "1.20", nil)
	writeProxyModule(t, proxy, "k8s.io/api", "v0.36.6", "1.20", nil)
	writeProxyVersionList(t, proxy, "k8s.io/api", []string{"v0.36.5", "v0.36.6"})
	writeModule(t, root, "module fixture.test/root\n\ngo "+testGoVersion+"\n\nrequire (\n\texample.test/lib v1.0.0\n\tk8s.io/api v0.36.5\n)\n")
	writeFile(t, filepath.Join(root, "fixture.go"), "package fixture\n\nimport (\n\t_ \"example.test/lib\"\n\t_ \"k8s.io/api\"\n)\n")
	setLocalProxy(t, proxy)

	report, err := Run(context.Background(), Config{Root: root, Policy: mustPolicy(t, "pin-both", "", nil), GoBinary: "go"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := moduleResult(t, report, "example.test/lib").Outcome; got != "blocked-by-constraints" {
		t.Fatalf("lib outcome = %q, want blocked-by-constraints", got)
	}
	if got := report.Final.Modules["k8s.io/api"].Version; got != "v0.36.5" {
		t.Fatalf("final Kubernetes version = %q, want pinned v0.36.5", got)
	}
}

func TestEngineUpdatesIndirectModuleAndAccountsForGraphAdditionsAndRemovals(t *testing.T) {
	root, proxy := engineFixture(t)
	writeProxyModuleWithSource(t, proxy, "example.test/lib", "v1.0.0", "1.20", map[string]string{
		"example.test/old":      "v1.0.0",
		"example.test/indirect": "v1.0.0",
	}, "package fixture\nimport (\n\t_ \"example.test/old\"\n\t_ \"example.test/indirect\"\n)\n")
	writeProxyModuleWithSource(t, proxy, "example.test/lib", "v1.1.0", "1.20", map[string]string{
		"example.test/new":      "v1.0.0",
		"example.test/indirect": "v1.0.0",
	}, "package fixture\nimport (\n\t_ \"example.test/new\"\n\t_ \"example.test/indirect\"\n)\n")
	writeProxyVersionList(t, proxy, "example.test/lib", []string{"v1.0.0", "v1.1.0"})
	writeProxyModule(t, proxy, "example.test/old", "v1.0.0", "1.20", nil)
	writeProxyVersionList(t, proxy, "example.test/old", []string{"v1.0.0"})
	writeProxyModule(t, proxy, "example.test/new", "v1.0.0", "1.20", nil)
	writeProxyVersionList(t, proxy, "example.test/new", []string{"v1.0.0"})
	writeProxyModule(t, proxy, "example.test/indirect", "v1.0.0", "1.20", nil)
	writeProxyModule(t, proxy, "example.test/indirect", "v1.1.0", "1.20", nil)
	writeProxyVersionList(t, proxy, "example.test/indirect", []string{"v1.0.0", "v1.1.0"})
	writeModule(t, root, "module fixture.test/root\n\ngo "+testGoVersion+"\n\nrequire example.test/lib v1.0.0\n")
	writeFile(t, filepath.Join(root, "fixture.go"), "package fixture\n\nimport _ \"example.test/lib\"\n")
	setLocalProxy(t, proxy)

	report, err := Run(context.Background(), Config{Root: root, Policy: mustPolicy(t, "pin-both", "", nil), GoBinary: "go"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	indirect := moduleResult(t, report, "example.test/indirect")
	if indirect.InitialDirect || indirect.FinalDirect || indirect.InitialVersion != "v1.0.0" || indirect.FinalVersion != "v1.1.0" {
		t.Fatalf("indirect result = %#v, want an indirect v1.0.0 -> v1.1.0 update", indirect)
	}
	if removed := moduleResult(t, report, "example.test/old"); removed.Outcome != "removed" || removed.FinalVersion != "" {
		t.Fatalf("removed module result = %#v", removed)
	}
	if added := moduleResult(t, report, "example.test/new"); added.Outcome != "updated" || added.InitialVersion != "" || added.FinalVersion != "v1.0.0" {
		t.Fatalf("added module result = %#v", added)
	}
}

func TestEngineRequiresExplicitDecisionForNewKubernetesModule(t *testing.T) {
	root, proxy := newKubernetesIntroductionFixture(t)
	setLocalProxy(t, proxy)
	policy := mustPolicy(t, "pin-both", "", nil)
	report, err := Run(context.Background(), Config{Root: root, Policy: policy, GoBinary: "go"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	introduced := moduleResult(t, report, "k8s.io/new")
	if introduced.Outcome != "blocked-by-constraints" || introduced.FinalVersion != "" {
		t.Fatalf("new Kubernetes module result = %#v, want blocked and absent", introduced)
	}
}

func TestEngineSkipsGraphOnlyModulesAndUpdatesExplicitIndirectRequirements(t *testing.T) {
	root, proxy := engineFixture(t)
	writeProxyModuleWithSource(t, proxy, "example.test/direct", "v1.0.0", "1.20", map[string]string{
		"example.test/graph-only": "v1.0.0",
		"example.test/indirect":   "v1.0.0",
	}, "package fixture\nimport _ \"example.test/indirect\"\n")
	writeProxyModule(t, proxy, "example.test/graph-only", "v1.0.0", "1.20", nil)
	// A version lookup would fail, proving graph-only modules are never candidates.
	if err := os.Remove(filepath.Join(proxy, "example.test", "graph-only", "@v", "list")); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"v1.0.0", "v1.1.0"} {
		writeProxyModule(t, proxy, "example.test/indirect", version, "1.20", nil)
	}
	writeProxyVersionList(t, proxy, "example.test/indirect", []string{"v1.0.0", "v1.1.0"})
	writeModule(t, root, "module fixture.test/root\n\ngo "+testGoVersion+"\n\nrequire example.test/direct v1.0.0\nrequire example.test/indirect v1.0.0 // indirect\n")
	writeFile(t, filepath.Join(root, "fixture.go"), "package fixture\nimport _ \"example.test/direct\"\n")
	setLocalProxy(t, proxy)
	var progress bytes.Buffer
	report, err := Run(context.Background(), Config{Root: root, Policy: mustPolicy(t, "pin-both", "", nil), GoBinary: "go", Progress: &progress})
	if err != nil {
		t.Fatalf("Run(): %v", err)
	}
	graphOnly := moduleResult(t, report, "example.test/graph-only")
	if len(graphOnly.Attempts) != 0 || graphOnly.Outcome != moduleNotInGoMod || graphOnly.FinalVersion != "v1.0.0" {
		t.Fatalf("graph-only result = %#v, want skipped without candidate attempts", graphOnly)
	}
	indirect := moduleResult(t, report, "example.test/indirect")
	if indirect.Outcome != moduleUpdated || indirect.FinalVersion != "v1.1.0" || indirect.FinalDirect {
		t.Fatalf("explicit indirect result = %#v, want indirect upgrade", indirect)
	}
	if !strings.Contains(progress.String(), "Skipping example.test/graph-only at v1.0.0 (not-in-go-mod)") {
		t.Fatalf("missing skip reason in progress: %s", progress.String())
	}
}

func TestKubernetesMapAllowsExplicitTargetForNewTransitiveModule(t *testing.T) {
	root, proxy := newKubernetesIntroductionFixture(t)
	setLocalProxy(t, proxy)
	policy := mustPolicy(t, "kubernetes-only", "", []string{"k8s.io/new=v0.2.0"})
	report, err := Run(context.Background(), Config{Root: root, Policy: policy, GoBinary: "go"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := report.Final.Modules["k8s.io/new"].Version; got != "v0.2.0" {
		t.Fatalf("new Kubernetes module selected %q, want exact v0.2.0", got)
	}
}

func TestEngineRespectsExcludedCandidateAndContinues(t *testing.T) {
	root, proxy := engineFixture(t)
	for _, version := range []string{"v1.0.0", "v1.1.0", "v1.2.0"} {
		writeProxyModule(t, proxy, "example.test/lib", version, "1.20", nil)
	}
	writeProxyVersionList(t, proxy, "example.test/lib", []string{"v1.0.0", "v1.1.0", "v1.2.0"})
	writeModule(t, root, "module fixture.test/root\n\ngo "+testGoVersion+"\n\nrequire example.test/lib v1.0.0\n\nexclude example.test/lib v1.2.0\n")
	writeFile(t, filepath.Join(root, "fixture.go"), "package fixture\n\nimport _ \"example.test/lib\"\n")
	setLocalProxy(t, proxy)

	report, err := Run(context.Background(), Config{Root: root, Policy: mustPolicy(t, "pin-both", "", nil), GoBinary: "go"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	result := moduleResult(t, report, "example.test/lib")
	if result.FinalVersion != "v1.1.0" || len(result.Attempts) != 1 || result.Attempts[0].Version != "v1.1.0" {
		t.Fatalf("excluded candidate result = %#v", result)
	}
}

func TestEngineIsIdempotentWithFrozenLocalProxy(t *testing.T) {
	root, proxy := engineFixture(t)
	writeProxyModule(t, proxy, "example.test/lib", "v1.0.0", "1.20", nil)
	writeProxyModule(t, proxy, "example.test/lib", "v1.1.0", "1.20", nil)
	writeProxyVersionList(t, proxy, "example.test/lib", []string{"v1.0.0", "v1.1.0"})
	writeModule(t, root, "module fixture.test/root\n\ngo "+testGoVersion+"\n\nrequire example.test/lib v1.0.0\n")
	writeFile(t, filepath.Join(root, "fixture.go"), "package fixture\n\nimport _ \"example.test/lib\"\n")
	setLocalProxy(t, proxy)
	policy := mustPolicy(t, "pin-both", "", nil)
	first, err := Run(context.Background(), Config{Root: root, Policy: policy, GoBinary: "go"})
	if err != nil {
		t.Fatalf("first Run(): %v", err)
	}
	firstState := snapshotFiles(t, root)
	second, err := Run(context.Background(), Config{Root: root, Policy: policy, GoBinary: "go"})
	if err != nil {
		t.Fatalf("second Run(): %v", err)
	}
	secondState := snapshotFiles(t, root)
	if !reflect.DeepEqual(firstState, secondState) {
		t.Fatalf("second run changed dependency artifacts:\nfirst=%v\nsecond=%v", sortedFileNames(firstState), sortedFileNames(secondState))
	}
	secondLib := moduleResult(t, second, "example.test/lib")
	if first.Final.Modules["example.test/lib"].Version != "v1.1.0" || secondLib.FinalVersion != "v1.1.0" || secondLib.Outcome != moduleAlreadyCurrent {
		t.Fatalf("run reports do not show the accepted stable dependency: first=%#v second=%#v", first, second)
	}
}

func TestEngineProgressShowsVersionMovementAndNoUpdateReason(t *testing.T) {
	root, proxy := engineFixture(t)
	for _, version := range []string{"v1.0.0", "v1.1.0"} {
		writeProxyModule(t, proxy, "example.test/lib", version, "1.20", nil)
	}
	writeProxyVersionList(t, proxy, "example.test/lib", []string{"v1.0.0", "v1.1.0"})
	writeProxyModule(t, proxy, "example.test/current", "v1.0.0", "1.20", nil)
	writeProxyVersionList(t, proxy, "example.test/current", []string{"v1.0.0"})
	writeProxyModule(t, proxy, "k8s.io/api", "v0.36.5", "1.20", nil)
	writeProxyVersionList(t, proxy, "k8s.io/api", []string{"v0.36.5"})
	writeModule(t, root, "module fixture.test/root\n\ngo "+testGoVersion+"\n\nrequire (\n\texample.test/lib v1.0.0\n\texample.test/current v1.0.0\n\tk8s.io/api v0.36.5\n)\n")
	writeFile(t, filepath.Join(root, "fixture.go"), "package fixture\n\nimport (\n\t_ \"example.test/lib\"\n\t_ \"example.test/current\"\n\t_ \"k8s.io/api\"\n)\n")
	setLocalProxy(t, proxy)
	var progress bytes.Buffer
	_, err := Run(context.Background(), Config{Root: root, Policy: mustPolicy(t, "pin-both", "", nil), GoBinary: "go", Progress: &progress})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	output := progress.String()
	for _, expected := range []string{
		"Trying example.test/lib: v1.0.0 -> v1.1.0",
		"Updated example.test/lib: v1.0.0 -> v1.1.0",
		"No update example.test/current: remains at v1.0.0 (already-current)",
		"No update k8s.io/api: remains at v0.36.5 (policy-pinned)",
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("progress output missing %q:\n%s", expected, output)
		}
	}
}

func TestPassBoundReturnsIncompleteAndRollsBack(t *testing.T) {
	root, proxy := engineFixture(t)
	writeProxyModule(t, proxy, "example.test/lib", "v1.0.0", "1.20", nil)
	writeProxyModuleWithSource(t, proxy, "example.test/lib", "v1.1.0", "1.20", map[string]string{"example.test/new": "v1.0.0"}, "package fixture\nimport _ \"example.test/new\"\n")
	writeProxyVersionList(t, proxy, "example.test/lib", []string{"v1.0.0", "v1.1.0"})
	writeProxyModule(t, proxy, "example.test/new", "v1.0.0", "1.20", nil)
	writeProxyVersionList(t, proxy, "example.test/new", []string{"v1.0.0"})
	writeModule(t, root, "module fixture.test/root\n\ngo "+testGoVersion+"\n\nrequire example.test/lib v1.0.0\n")
	writeFile(t, filepath.Join(root, "fixture.go"), "package fixture\n\nimport _ \"example.test/lib\"\n")
	setLocalProxy(t, proxy)
	originalMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}

	report, err := Run(context.Background(), Config{Root: root, Policy: mustPolicy(t, "pin-both", "", nil), GoBinary: "go", MaxPasses: 1})
	if err == nil || report.Status != "incomplete" || ExitCode(err) != 3 {
		t.Fatalf("Run() status/error = %q/%v, want incomplete error", report.Status, err)
	}
	afterMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if string(afterMod) != string(originalMod) {
		t.Fatalf("incomplete run go.mod = %q, want original %q", afterMod, originalMod)
	}
}

func TestCandidateLimitCapsEachModuleAtTwentyAcrossPasses(t *testing.T) {
	root, proxy := engineFixture(t)
	writeProxyModule(t, proxy, "example.test/a", "v1.0.0", "1.20", nil)
	versions := []string{"v1.0.0"}
	for patch := 1; patch <= 22; patch++ {
		version := fmt.Sprintf("v1.0.%d", patch)
		writeProxyModule(t, proxy, "example.test/a", version, "1.27.0", nil)
		versions = append(versions, version)
	}
	writeProxyVersionList(t, proxy, "example.test/a", versions)
	for _, version := range []string{"v1.0.0", "v1.1.0"} {
		writeProxyModule(t, proxy, "example.test/b", version, "1.20", nil)
	}
	writeProxyVersionList(t, proxy, "example.test/b", []string{"v1.0.0", "v1.1.0"})
	writeModule(t, root, "module fixture.test/root\n\ngo "+testGoVersion+"\n\nrequire (\n\texample.test/a v1.0.0\n\texample.test/b v1.0.0\n)\n")
	writeFile(t, filepath.Join(root, "fixture.go"), "package fixture\nimport (\n_ \"example.test/a\"\n_ \"example.test/b\"\n)\n")
	setLocalProxy(t, proxy)
	var progress bytes.Buffer
	report, err := Run(context.Background(), Config{Root: root, Policy: mustPolicy(t, "pin-both", "", nil), GoBinary: "go", Progress: &progress})
	if err == nil || ExitCode(err) != 3 || report.Status != runIncomplete || !report.RolledBack {
		t.Fatalf("Run() status=%s rollback=%t error=%v, want incomplete rollback", report.Status, report.RolledBack, err)
	}
	a := moduleResult(t, report, "example.test/a")
	if len(a.Attempts) != 20 || !a.CandidateLimitReached || a.Attempts[0].Version != "v1.0.22" || a.Attempts[19].Version != "v1.0.3" {
		t.Fatalf("capped module = %#v, want exactly the twenty newest candidates", a)
	}
	b := moduleResult(t, report, "example.test/b")
	if len(b.Attempts) != 1 || b.Attempts[0].Outcome != attemptAccepted || b.FinalVersion != "v1.0.0" {
		t.Fatalf("other module = %#v, want processed then rolled back with incomplete run", b)
	}
	if !strings.Contains(progress.String(), "Candidate limit reached for example.test/a: 20 attempts") {
		t.Fatalf("missing candidate-limit progress: %s", progress.String())
	}
}

func TestCandidateLimitAllowsAcceptanceOnTwentiethTrial(t *testing.T) {
	root, proxy := engineFixture(t)
	writeProxyModule(t, proxy, "example.test/lib", "v1.0.0", "1.20", nil)
	versions := []string{"v1.0.0"}
	for patch := 1; patch <= 20; patch++ {
		version := fmt.Sprintf("v1.0.%d", patch)
		goVersion := "1.27.0"
		if patch == 1 {
			goVersion = "1.20"
		}
		writeProxyModule(t, proxy, "example.test/lib", version, goVersion, nil)
		versions = append(versions, version)
	}
	writeProxyVersionList(t, proxy, "example.test/lib", versions)
	writeModule(t, root, "module fixture.test/root\n\ngo "+testGoVersion+"\nrequire example.test/lib v1.0.0\n")
	writeFile(t, filepath.Join(root, "fixture.go"), "package fixture\nimport _ \"example.test/lib\"\n")
	setLocalProxy(t, proxy)
	report, err := Run(context.Background(), Config{Root: root, Policy: mustPolicy(t, "pin-both", "", nil), GoBinary: "go"})
	if err != nil {
		t.Fatalf("Run(): %v", err)
	}
	result := moduleResult(t, report, "example.test/lib")
	if len(result.Attempts) != 20 || result.CandidateLimitReached || result.FinalVersion != "v1.0.1" {
		t.Fatalf("twentieth-trial result = %#v", result)
	}
}

func TestPassCycleTrackerDetectsRepeatedGraph(t *testing.T) {
	first := Snapshot{GoDirective: "1.26.7", Modules: map[string]Module{
		"example.test/a": {Path: "example.test/a", Version: "v1.0.0"},
		"example.test/b": {Path: "example.test/b", Version: "v1.0.0"},
	}}
	second := Snapshot{GoDirective: "1.26.7", Modules: map[string]Module{
		"example.test/b": {Path: "example.test/b", Version: "v1.0.0"},
		"example.test/a": {Path: "example.test/a", Version: "v1.0.0"},
	}}
	tracker := graphTracker{}
	if tracker.Seen(first) {
		t.Fatal("first graph was already seen")
	}
	if !tracker.Seen(second) {
		t.Fatal("same graph with different map insertion order was not detected")
	}
}

func engineFixture(t *testing.T) (string, string) {
	t.Helper()
	return t.TempDir(), filepath.Join(t.TempDir(), "proxy")
}

func newKubernetesIntroductionFixture(t *testing.T) (string, string) {
	t.Helper()
	root, proxy := engineFixture(t)
	writeProxyModule(t, proxy, "example.test/lib", "v1.0.0", "1.20", nil)
	writeProxyModuleWithSource(t, proxy, "example.test/lib", "v1.1.0", "1.20", map[string]string{"k8s.io/new": "v0.1.0"}, "package fixture\nimport _ \"k8s.io/new\"\n")
	writeProxyVersionList(t, proxy, "example.test/lib", []string{"v1.0.0", "v1.1.0"})
	for _, version := range []string{"v0.1.0", "v0.2.0"} {
		writeProxyModule(t, proxy, "k8s.io/new", version, "1.20", nil)
	}
	writeProxyVersionList(t, proxy, "k8s.io/new", []string{"v0.1.0", "v0.2.0"})
	writeModule(t, root, "module fixture.test/root\n\ngo "+testGoVersion+"\n\nrequire example.test/lib v1.0.0\n")
	writeFile(t, filepath.Join(root, "fixture.go"), "package fixture\n\nimport _ \"example.test/lib\"\n")
	return root, proxy
}

func mustPolicy(t *testing.T, mode, goTarget string, k8sTargets []string) Policy {
	t.Helper()
	policy, err := ParsePolicy(mode, goTarget, k8sTargets)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func snapshotFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := make(map[string][]byte)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[relative] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func sortedFileNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func moduleResult(t *testing.T, report Report, path string) ModuleResult {
	t.Helper()
	for _, result := range report.Modules {
		if result.Path == path {
			return result
		}
	}
	t.Fatalf("report has no result for %s: %#v", path, report.Modules)
	return ModuleResult{}
}
