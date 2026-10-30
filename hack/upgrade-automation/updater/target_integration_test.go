package updater

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTargetSetupEnforcesFourIndependentModes(t *testing.T) {
	tests := []struct {
		name       string
		mode       string
		baselineGo string
		toolchain  string
		goTarget   string
		k8sTargets []string
		wantGo     string
		wantTool   string
		wantK8s    map[string]string
	}{
		{
			name: "pin both",
			mode: "pin-both", baselineGo: testGoVersion,
			wantGo:  testGoVersion,
			wantK8s: map[string]string{"k8s.io/api": "v0.36.5", "k8s.io/apimachinery": "v0.36.4"},
		},
		{
			name: "update Go only and keep absent toolchain absent",
			mode: "go-only", baselineGo: "1.26.6", goTarget: testGoVersion,
			wantGo:  testGoVersion,
			wantK8s: map[string]string{"k8s.io/api": "v0.36.5", "k8s.io/apimachinery": "v0.36.4"},
		},
		{
			name: "update Kubernetes only with independent targets",
			mode: "kubernetes-only", baselineGo: testGoVersion,
			k8sTargets: []string{"k8s.io/api=v0.36.6", "k8s.io/apimachinery=v0.36.6"},
			wantGo:     testGoVersion,
			wantK8s:    map[string]string{"k8s.io/api": "v0.36.6", "k8s.io/apimachinery": "v0.36.6"},
		},
		{
			name: "update both and preserve existing toolchain presence",
			mode: "both", baselineGo: "1.26.6", toolchain: "go1.26.6", goTarget: testGoVersion,
			k8sTargets: []string{"k8s.io/api=v0.36.6", "k8s.io/apimachinery=v0.36.6"},
			wantGo:     testGoVersion, wantTool: "go" + testGoVersion,
			wantK8s: map[string]string{"k8s.io/api": "v0.36.6", "k8s.io/apimachinery": "v0.36.6"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, proxy := targetFixture(t, tt.baselineGo, tt.toolchain)
			setLocalProxy(t, proxy)
			baseline, err := CaptureSnapshot(context.Background(), root, "go")
			if err != nil {
				t.Fatalf("CaptureSnapshot(baseline): %v", err)
			}
			policy, err := ParsePolicy(tt.mode, tt.goTarget, tt.k8sTargets)
			if err != nil {
				t.Fatalf("ParsePolicy(): %v", err)
			}
			transaction, err := BeginTransaction(root)
			if err != nil {
				t.Fatalf("BeginTransaction(): %v", err)
			}
			after, err := ApplyTargets(context.Background(), GoRunner{Root: root, Binary: "go"}, baseline, policy, transaction)
			if err != nil {
				t.Fatalf("ApplyTargets(): %v", err)
			}
			if err := transaction.Commit(); err != nil {
				t.Fatalf("Commit(): %v", err)
			}
			if after.GoDirective != tt.wantGo || after.Toolchain != tt.wantTool {
				t.Errorf("Go/toolchain = %q/%q, want %q/%q", after.GoDirective, after.Toolchain, tt.wantGo, tt.wantTool)
			}
			for modulePath, version := range tt.wantK8s {
				if got := after.Modules[modulePath].Version; got != version {
					t.Errorf("%s selected %q, want exact %q", modulePath, got, version)
				}
			}
		})
	}
}

func TestTargetSetupRollsBackIncompatibleKubernetesTargets(t *testing.T) {
	root, proxy := targetFixture(t, testGoVersion, "")
	setLocalProxy(t, proxy)
	baseline, err := CaptureSnapshot(context.Background(), root, "go")
	if err != nil {
		t.Fatalf("CaptureSnapshot(baseline): %v", err)
	}
	policy, err := ParsePolicy("kubernetes-only", "", []string{"k8s.io/api=v0.36.6"})
	if err != nil {
		t.Fatalf("ParsePolicy(): %v", err)
	}
	beforeMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := BeginTransaction(root)
	if err != nil {
		t.Fatalf("BeginTransaction(): %v", err)
	}
	_, err = ApplyTargets(context.Background(), GoRunner{Root: root, Binary: "go"}, baseline, policy, transaction)
	if err == nil || !strings.Contains(err.Error(), "kubernetes module") {
		t.Fatalf("ApplyTargets() error = %v, want exact pin conflict", err)
	}
	if got := ExitCode(err); got != 2 {
		t.Fatalf("ExitCode(incompatible target set) = %d, want 2", got)
	}
	afterMod, readErr := os.ReadFile(filepath.Join(root, "go.mod"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(afterMod) != string(beforeMod) {
		t.Fatalf("go.mod after rejected target = %q, want exact baseline %q", afterMod, beforeMod)
	}
	after, err := CaptureSnapshot(context.Background(), root, "go")
	if err != nil {
		t.Fatalf("CaptureSnapshot(after rollback): %v", err)
	}
	if !reflect.DeepEqual(after.Modules, baseline.Modules) {
		t.Fatalf("selected modules after rollback = %#v, want baseline %#v", after.Modules, baseline.Modules)
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatalf("Rollback(): %v", err)
	}
}

func TestTargetSetupCanRemoveToolchainAddedDuringTidy(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, "module fixture.test/root\n\ngo 1.26.7\n")
	runner := GoRunner{Root: root, Binary: "go"}
	if _, err := runner.Run(context.Background(), "mod", "edit", "-toolchain=go1.26.7"); err != nil {
		t.Fatalf("add test toolchain directive: %v", err)
	}
	if err := SetToolchainDirective(context.Background(), runner, ""); err != nil {
		t.Fatalf("SetToolchainDirective(absent): %v", err)
	}
	output, err := runner.Run(context.Background(), "mod", "edit", "-json")
	if err != nil {
		t.Fatalf("read go.mod metadata: %v", err)
	}
	if strings.Contains(string(output), `"Toolchain": "go1.26.7"`) {
		t.Fatalf("toolchain directive remains after enforcing absence: %s", output)
	}
}

func TestTargetSetupSetsExactExistingToolchain(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, "module fixture.test/root\n\ngo 1.26.7\ntoolchain go1.26.5\n")
	runner := GoRunner{Root: root, Binary: "go"}
	if err := SetToolchainDirective(context.Background(), runner, "go1.26.7"); err != nil {
		t.Fatalf("SetToolchainDirective(exact): %v", err)
	}
	output, err := runner.Run(context.Background(), "mod", "edit", "-json")
	if err != nil {
		t.Fatalf("read go.mod metadata: %v", err)
	}
	if !strings.Contains(string(output), `"Toolchain": "go1.26.7"`) {
		t.Fatalf("toolchain directive = %s, want go1.26.7", output)
	}
}

func targetFixture(t *testing.T, goVersion, toolchain string) (string, string) {
	t.Helper()
	root := t.TempDir()
	proxy := filepath.Join(t.TempDir(), "proxy")
	apiVersions := []string{"v0.36.5", "v0.36.6"}
	for _, version := range apiVersions {
		requirements := map[string]string(nil)
		if version == "v0.36.6" {
			requirements = map[string]string{"k8s.io/apimachinery": "v0.36.6"}
		}
		writeProxyModule(t, proxy, "k8s.io/api", version, "1.20", requirements)
	}
	writeProxyVersionList(t, proxy, "k8s.io/api", apiVersions)
	for _, version := range []string{"v0.36.4", "v0.36.6"} {
		writeProxyModule(t, proxy, "k8s.io/apimachinery", version, "1.20", nil)
	}
	writeProxyVersionList(t, proxy, "k8s.io/apimachinery", []string{"v0.36.4", "v0.36.6"})
	toolchainDirective := ""
	if toolchain != "" {
		toolchainDirective = "toolchain " + toolchain + "\n"
	}
	writeModule(t, root, "module fixture.test/root\n\ngo "+goVersion+"\n"+toolchainDirective+"\nrequire (\n\tk8s.io/api v0.36.5\n\tk8s.io/apimachinery v0.36.4\n)\n")
	writeFile(t, filepath.Join(root, "fixture.go"), "package fixture\n\nimport (\n\t_ \"k8s.io/api\"\n\t_ \"k8s.io/apimachinery\"\n)\n")
	return root, proxy
}
