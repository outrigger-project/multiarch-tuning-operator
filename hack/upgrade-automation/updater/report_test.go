package updater

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReportJSONIsDeterministicAndSorted(t *testing.T) {
	report := Report{
		Status: runCompleted,
		Policy: Policy{Mode: ModePinBoth},
		Baseline: Snapshot{GoDirective: "1.26.7", Modules: map[string]Module{
			"example.test/z": {Path: "example.test/z", Version: "v1.0.0"},
		}},
		Final: Snapshot{GoDirective: "1.26.7", Modules: map[string]Module{
			"example.test/z": {Path: "example.test/z", Version: "v1.1.0"},
		}},
		Modules: []ModuleResult{
			{Path: "example.test/z", InitialVersion: "v1.0.0", FinalVersion: "v1.1.0", Outcome: moduleUpdated, Attempts: []Attempt{
				{Version: "v1.2.0", Outcome: moduleBlocked},
				{Version: "v1.1.0", Outcome: attemptAccepted, Changes: []Change{
					{Path: "example.test/z", Before: "v1.0.0", After: "v1.1.0"},
					{Path: "example.test/a", After: "v1.2.0"},
				}},
			}},
			{Path: "example.test/a", InitialVersion: "v1.0.0", FinalVersion: "v1.2.0", Outcome: moduleUpdated},
		},
	}
	firstPath := filepath.Join(t.TempDir(), "first", "report.json")
	secondPath := filepath.Join(t.TempDir(), "second", "report.json")
	if err := WriteReport(firstPath, report); err != nil {
		t.Fatalf("WriteReport(first): %v", err)
	}
	if err := WriteReport(secondPath, report); err != nil {
		t.Fatalf("WriteReport(second): %v", err)
	}
	first, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("same report produced different JSON:\n%s\n%s", first, second)
	}
	var decoded Report
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	paths := []string{decoded.Modules[0].Path, decoded.Modules[1].Path}
	if !reflect.DeepEqual(paths, []string{"example.test/a", "example.test/z"}) {
		t.Fatalf("module order = %v, want sorted paths", paths)
	}
	attempts := decoded.Modules[1].Attempts
	if len(attempts) != 2 || attempts[0].Version != "v1.2.0" || attempts[1].Version != "v1.1.0" {
		t.Fatalf("candidate attempts lost chronological order: %#v", attempts)
	}
	changePaths := []string{attempts[1].Changes[0].Path, attempts[1].Changes[1].Path}
	if !reflect.DeepEqual(changePaths, []string{"example.test/a", "example.test/z"}) {
		t.Fatalf("accepted changes order = %v, want sorted paths", changePaths)
	}
	if decoded.Baseline.GoDirective != "1.26.7" || decoded.Final.Modules["example.test/z"].Version != "v1.1.0" {
		t.Fatalf("baseline/final graph missing from report: %#v", decoded)
	}
}

func TestReportPreservesOutcomesAndRollbackEvidence(t *testing.T) {
	outcomes := []string{moduleUpdated, moduleAlreadyCurrent, modulePolicyPinned, moduleBlocked, moduleRemoved, moduleExecutionFailure}
	report := Report{
		Status:     runFailed,
		RolledBack: true,
		Failure:    "proxy retrieval failed",
		Policy:     Policy{Mode: ModePinBoth},
		Baseline:   Snapshot{GoDirective: "1.26.7", Modules: map[string]Module{}},
		Final:      Snapshot{GoDirective: "1.26.7", Modules: map[string]Module{}},
	}
	for index, outcome := range outcomes {
		path := "example.test/module"
		report.Modules = append(report.Modules, ModuleResult{
			Path:           path + string(rune('a'+index)),
			InitialVersion: "v1.0.0",
			FinalVersion:   "v1.1.0",
			Outcome:        outcome,
			Attempts:       []Attempt{{Version: "v1.1.0", Outcome: attemptAccepted, Changes: []Change{{Path: path, Before: "v1.0.0", After: "v1.1.0"}}}},
		})
	}
	path := filepath.Join(t.TempDir(), "report.json")
	if err := WriteReport(path, report); err != nil {
		t.Fatalf("WriteReport(): %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.RolledBack || decoded.Status != runFailed || decoded.Failure != "proxy retrieval failed" {
		t.Fatalf("rollback evidence = %#v", decoded)
	}
	got := make([]string, 0, len(decoded.Modules))
	for _, module := range decoded.Modules {
		got = append(got, module.Outcome)
	}
	if !reflect.DeepEqual(got, outcomes) {
		t.Fatalf("module outcomes = %v, want %v", got, outcomes)
	}
}

func TestReportRedactsProxyCredentials(t *testing.T) {
	input := "proxy https://user:secret@proxy.example/path?token=abc123&retry=1"
	got := sanitizeEvidence(input)
	for _, secret := range []string{"user:secret", "abc123"} {
		if strings.Contains(got, secret) {
			t.Fatalf("sanitized evidence leaks %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("sanitized evidence has no redaction marker: %s", got)
	}
}

func TestRetrievalFailureIsExecutionFailureAndRollsBackAcceptedTrial(t *testing.T) {
	for _, failureMode := range []string{"unauthorized", "missing zip"} {
		t.Run(failureMode, func(t *testing.T) {
			root, proxy := engineFixture(t)
			for _, modulePath := range []string{"example.test/a", "example.test/b"} {
				writeProxyModule(t, proxy, modulePath, "v1.0.0", "1.20", nil)
				writeProxyModule(t, proxy, modulePath, "v1.1.0", "1.20", nil)
				writeProxyVersionList(t, proxy, modulePath, []string{"v1.0.0", "v1.1.0"})
			}
			writeModule(t, root, "module fixture.test/root\n\ngo "+testGoVersion+"\n\nrequire (\n\texample.test/a v1.0.0\n\texample.test/b v1.0.0\n)\n")
			writeFile(t, filepath.Join(root, "fixture.go"), "package fixture\n\nimport (\n\t_ \"example.test/a\"\n\t_ \"example.test/b\"\n)\n")
			writeFile(t, filepath.Join(root, "vendor", "sentinel.txt"), "baseline vendor\n")
			setLocalProxy(t, proxy)
			originalMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
			if err != nil {
				t.Fatal(err)
			}
			if failureMode == "unauthorized" {
				files := http.FileServer(http.Dir(proxy))
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/example.test/b/@v/v1.1.0.zip" {
						http.Error(w, "unauthorized", http.StatusUnauthorized)
						return
					}
					w.Header().Set("Content-Type", "application/octet-stream")
					files.ServeHTTP(w, r)
				}))
				defer server.Close()
				t.Setenv("GOPROXY", server.URL)
			} else if err := os.Remove(filepath.Join(proxy, "example.test", "b", "@v", "v1.1.0.zip")); err != nil {
				t.Fatal(err)
			}

			report, runErr := Run(context.Background(), Config{Root: root, Policy: mustPolicy(t, "pin-both", "", nil), GoBinary: "go"})
			if runErr == nil || report.Status != runFailed || !report.RolledBack || ExitCode(runErr) != 2 {
				t.Fatalf("Run() status/error/rollback = %q/%v/%t, want failed retrieval with rollback", report.Status, runErr, report.RolledBack)
			}
			wantEvidence := map[string]string{"unauthorized": "401", "missing zip": "no such file"}[failureMode]
			if !strings.Contains(strings.ToLower(report.Failure), wantEvidence) {
				t.Fatalf("failure evidence = %q, want retrieval evidence for %s", report.Failure, failureMode)
			}
			a := moduleResult(t, report, "example.test/a")
			if a.FinalVersion != "v1.0.0" || a.Outcome != moduleExecutionFailure || len(a.Attempts) == 0 || a.Attempts[0].Outcome != attemptAccepted {
				t.Fatalf("accepted module after run rollback = %#v", a)
			}
			b := moduleResult(t, report, "example.test/b")
			if b.Outcome != moduleExecutionFailure {
				t.Fatalf("retrieval-failed module outcome = %q, want execution-failure", b.Outcome)
			}
			afterMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(afterMod, originalMod) {
				t.Fatalf("go.mod after rollback differs from baseline:\n%s\n%s", afterMod, originalMod)
			}
			if got, err := os.ReadFile(filepath.Join(root, "vendor", "sentinel.txt")); err != nil || string(got) != "baseline vendor\n" {
				t.Fatalf("vendor after rollback = %q, error=%v", got, err)
			}
			if _, err := os.Stat(filepath.Join(root, "go.sum")); !os.IsNotExist(err) {
				t.Fatalf("new go.sum remains after rollback: %v", err)
			}
		})
	}
}

func TestReportContainsIncompatibleTargetEvidence(t *testing.T) {
	root, proxy := targetFixture(t, testGoVersion, "")
	setLocalProxy(t, proxy)
	policy := mustPolicy(t, "kubernetes-only", "", []string{"k8s.io/api=v0.36.6"})
	report, err := Run(context.Background(), Config{Root: root, Policy: policy, GoBinary: "go"})
	if err == nil || ExitCode(err) != 2 || report.Status != runFailed || !report.RolledBack {
		t.Fatalf("Run() report/error = %#v/%v, want failed target transaction with exit 2", report, err)
	}
	if !strings.Contains(report.Failure, "k8s.io/apimachinery") || !strings.Contains(report.Failure, "v0.36.4") {
		t.Fatalf("report failure evidence = %q, want exact independent Kubernetes pin conflict", report.Failure)
	}
	if got := report.Final.Modules["k8s.io/api"].Version; got != "v0.36.5" {
		t.Fatalf("final Kubernetes version after rollback = %q, want v0.36.5", got)
	}
}

func TestReportFatalExecutionFailureOutranksRejectedCandidates(t *testing.T) {
	baseline := Snapshot{Modules: map[string]Module{
		"example.test/lib": {Path: "example.test/lib", Version: "v1.0.0"},
	}}
	results := map[string]*ModuleResult{
		"example.test/lib": {
			Path:           "example.test/lib",
			InitialVersion: "v1.0.0",
			Attempts: []Attempt{
				{Version: "v1.2.0", Outcome: moduleBlocked},
				{Version: "v1.1.0", Outcome: moduleExecutionFailure},
			},
		},
	}
	finalizeResults(results, baseline, Policy{Mode: ModePinBoth}, true)
	if got := results["example.test/lib"].Outcome; got != moduleExecutionFailure {
		t.Fatalf("module outcome = %q, want execution-failure after fatal retrieval error", got)
	}
}
