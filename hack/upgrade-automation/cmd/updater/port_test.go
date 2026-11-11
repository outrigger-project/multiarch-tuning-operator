package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPortAcceptsQuotedMTOIdentity(t *testing.T) {
	root := cliFixture(t)
	setFakeGitStatus(t, "")
	goMod := "module \"github.com/openshift/multiarch-tuning-operator\" // valid quoted identity\n\ngo 1.26.7\n"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateRepository(root); err != nil {
		t.Fatalf("valid MTO repository rejected: %v", err)
	}
}

func TestPortAcceptsKubernetesReleaseFlag(t *testing.T) {
	if _, err := parseArgs([]string{"--mode", "kubernetes-only", "--k8s-release", "1.36.6"}); err != nil {
		t.Fatalf("release flag rejected: %v", err)
	}
}

func TestPortRejectsUnrelatedRepository(t *testing.T) {
	root := cliFixture(t)
	setFakeGitStatus(t, "")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/other-operator\n\ngo 1.26.7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateRepository(root); err == nil || !strings.Contains(err.Error(), "expected repository module github.com/openshift/multiarch-tuning-operator") {
		t.Fatalf("want MTO identity rejection, got %v", err)
	}
}
