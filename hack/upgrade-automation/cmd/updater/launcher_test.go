package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLauncherBootstrapsWithoutVendor(t *testing.T) {
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join("../../../..", name))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	launcher := filepath.Join(root, "hack/upgrade-automation/scripts/upgrade.sh")
	if err = os.MkdirAll(filepath.Dir(launcher), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../scripts/upgrade.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(launcher, data, 0o700); err != nil {
		t.Fatal(err)
	}
	cmdDir := filepath.Join(root, "hack/upgrade-automation/cmd/updater")
	if err = os.MkdirAll(cmdDir, 0o700); err != nil {
		t.Fatal(err)
	}
	source := `package main
import ("fmt"; "golang.org/x/mod/modfile")
func main(){fmt.Println(modfile.ModulePath([]byte("module fixture.test/root"))) }
`
	if err = os.WriteFile(filepath.Join(cmdDir, "main.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(root, "vendor"), 0o700); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err = os.WriteFile(filepath.Join(bin, "go"), []byte("#!/bin/sh\nexit 42\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(bin, "selected compiler")
	if err = os.Symlink(realGo, selected); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, flags := range []string{"", "-mod=vendor"} {
		t.Run("GOFLAGS="+flags, func(t *testing.T) {
			t.Setenv("GOFLAGS", flags)
			output, err := exec.Command("bash", launcher, "--k8s-release", "1.36.6", "--go-bin", selected, "--help").
				CombinedOutput()
			if err != nil || !bytes.Contains(output, []byte("fixture.test/root")) {
				t.Fatalf("launcher: %v, %s", err, output)
			}
		})
	}
	for _, name := range []string{"go.mod", "go.sum"} {
		before, _ := os.ReadFile(filepath.Join("../../../..", name))
		after, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("%s changed: %v", name, err)
		}
	}
}
