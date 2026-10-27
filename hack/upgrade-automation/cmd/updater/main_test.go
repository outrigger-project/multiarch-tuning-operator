package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/openshift/multiarch-tuning-operator/hack/upgrade-automation/updater"
)

func TestParseArgsAcceptsFourModes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		mode updater.Mode
	}{
		{name: "pin both", args: []string{"--mode", "pin-both"}, mode: updater.ModePinBoth},
		{name: "Go only", args: []string{"--mode", "go-only", "--go-target", "1.26.8"}, mode: updater.ModeGoOnly},
		{name: "Kubernetes only", args: []string{"--mode", "kubernetes-only", "--k8s-target", "k8s.io/api=v0.36.6"}, mode: updater.ModeKubernetesOnly},
		{name: "both", args: []string{"--mode", "both", "--go-target", "1.26.8", "--k8s-target", "k8s.io/api=v0.36.6", "--k8s-target", "k8s.io/apimachinery=v0.36.6"}, mode: updater.ModeBoth},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options, err := parseArgs(tt.args)
			if err != nil {
				t.Fatalf("parseArgs() error = %v", err)
			}
			if options.policy.Mode != tt.mode {
				t.Fatalf("mode = %q, want %q", options.policy.Mode, tt.mode)
			}
		})
	}
}

func TestParseArgsDefaultsReportAndGoBinary(t *testing.T) {
	options, err := parseArgs([]string{"--mode", "pin-both"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	if options.reportPath != defaultReportPath || options.goBinary != "go" {
		t.Fatalf("defaults = report %q, Go %q; want %q and go", options.reportPath, options.goBinary, defaultReportPath)
	}
}

func TestParseArgsRejectsMissingAndUnexpectedTargets(t *testing.T) {
	for _, args := range [][]string{
		{"--mode", "go-only"},
		{"--mode", "kubernetes-only"},
		{"--mode", "both", "--go-target", "1.26.8"},
		{"--mode", "pin-both", "--go-target", "1.26.8"},
	} {
		if _, err := parseArgs(args); err == nil {
			t.Fatalf("parseArgs(%v) error = nil, want invalid target combination", args)
		}
	}
}

func TestExecuteWritesFailureReportAndReturnsUpdaterExitCode(t *testing.T) {
	root := cliFixture(t)
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	setFakeGitStatus(t, "")
	var stdout, stderr bytes.Buffer
	code := execute([]string{"--mode", "go-only", "--go-target", "1.26.8", "--go-bin", goBinary}, root, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("execute() exit = %d, want compiler mismatch exit 2; stderr=%s", code, stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(defaultReportPath)))
	if err != nil {
		t.Fatalf("read default failure report: %v", err)
	}
	var report updater.Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report.Status != "failed" || !report.RolledBack || report.Failure == "" || report.Baseline.GoDirective != "1.26.7" {
		t.Fatalf("failure report = %#v", report)
	}
	if len(report.Policy.KubernetesTargets) != 0 {
		t.Fatalf("pin-both Kubernetes target map unexpectedly nonempty: %#v", report.Policy.KubernetesTargets)
	}
}

func TestExecuteDirtyTreeReturnsPreconditionExitAndWritesReport(t *testing.T) {
	root := cliFixture(t)
	setFakeGitStatus(t, "?? user-file\n")
	var stdout, stderr bytes.Buffer
	code := execute([]string{"--mode", "pin-both"}, root, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("execute() exit = %d, want dirty tree exit 1", code)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(defaultReportPath)))
	if err != nil {
		t.Fatalf("read precondition report: %v", err)
	}
	var report updater.Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "failed" || report.RolledBack || !bytes.Contains(data, []byte("clean")) {
		t.Fatalf("dirty-tree report = %s", data)
	}
}

func TestExecuteInvalidArgumentsReturnOneWithoutStartingUpdater(t *testing.T) {
	root := cliFixture(t)
	var stdout, stderr bytes.Buffer
	code := execute([]string{"--mode", "go-only"}, root, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("execute() exit = %d, want invalid argument exit 1", code)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(defaultReportPath))); !os.IsNotExist(err) {
		t.Fatalf("invalid invocation wrote report: stat error = %v", err)
	}
}

func cliFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/openshift/multiarch-tuning-operator\n\ngo 1.26.7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte("all:\n\t@true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func setFakeGitStatus(t *testing.T, status string) {
	t.Helper()
	bin := t.TempDir()
	git := "#!/bin/sh\nprintf '%s' \"$MTO_GIT_STATUS\"\n"
	path := filepath.Join(bin, "git")
	if err := os.WriteFile(path, []byte(git), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MTO_GIT_STATUS", status)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}
