package updater

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoEnvironmentOverridesToolchainAndVendorFlags(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, "module fixture.test/root\n\ngo 1.26.7\n")
	t.Setenv("GOFLAGS", "-mod=vendor")
	t.Setenv("GOTOOLCHAIN", "auto")

	runner := GoRunner{Root: root, Binary: "go"}
	output, err := runner.Run(context.Background(), "env", "GOTOOLCHAIN", "GOFLAGS", "GOMOD")
	if err != nil {
		t.Fatalf("GoRunner.Run() error = %v", err)
	}
	want := "local\n-mod=mod\n" + filepath.Join(root, "go.mod") + "\n"
	if string(output) != want {
		t.Fatalf("GoRunner.Run() output = %q, want %q", output, want)
	}
}

func TestCompilerVersionRejectsMismatch(t *testing.T) {
	runner := GoRunner{Root: t.TempDir(), Binary: "go"}
	if err := runner.CheckCompiler(context.Background(), "1.26.8"); err == nil || !strings.Contains(err.Error(), "1.26.8") {
		t.Fatalf("CheckCompiler() error = %v, want mismatch for 1.26.8", err)
	}
}

func TestCompilerVersionAcceptsExact(t *testing.T) {
	runner := GoRunner{Root: t.TempDir(), Binary: "go"}
	if err := runner.CheckCompiler(context.Background(), "1.26.7"); err != nil {
		t.Fatalf("CheckCompiler() error = %v", err)
	}
}

func TestCompilerVersionUsesSelectedBinary(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, "module fixture.test/root\n\ngo 1.26.7\n")
	runner := GoRunner{Root: root, Binary: "/bin/true"}
	if err := runner.CheckCompiler(context.Background(), "1.26.7"); err == nil {
		t.Fatal("CheckCompiler() error = nil, want selected binary mismatch")
	}
}
