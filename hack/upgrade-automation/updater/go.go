package updater

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type GoRunner struct {
	Root   string
	Binary string
}

func (r GoRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	root := r.Root
	if root == "" {
		root = "."
	}
	goBin := r.Binary
	if goBin == "" {
		goBin = "go"
	}
	cmd := exec.CommandContext(ctx, goBin, args...) // #nosec G204 -- the maintainer selects the compiler; arguments are passed without a shell.
	cmd.Dir = root
	cmd.Env = environmentWith(map[string]string{"GOTOOLCHAIN": "local", "GOFLAGS": "-mod=mod"})
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go %s: %w: %s", strings.Join(args, " "), err, sanitizeEvidence(strings.TrimSpace(string(output))))
	}
	return output, nil
}

func (r GoRunner) CheckCompiler(ctx context.Context, expected string) error {
	output, err := r.Run(ctx, "version")
	if err != nil {
		return fmt.Errorf("check Go compiler: %w", err)
	}
	fields := strings.Fields(string(output))
	if len(fields) < 3 || fields[0] != "go" || fields[1] != "version" {
		return fmt.Errorf("unexpected output from Go compiler %q: %q", r.Binary, strings.TrimSpace(string(output)))
	}
	actual := strings.TrimPrefix(fields[2], "go")
	expected = strings.TrimPrefix(expected, "go")
	want := strings.Split(expected, ".")
	got := strings.Split(actual, ".")
	if len(want) < 2 || len(want) > 3 || len(got) < len(want) {
		return fmt.Errorf("invalid expected Go compiler version %q", expected)
	}
	for i, component := range want {
		if got[i] != component {
			return fmt.Errorf("go compiler is %s, policy requires %s", actual, expected)
		}
	}
	if len(want) == 3 && len(got) != 3 {
		return fmt.Errorf("go compiler is %s, policy requires exact %s", actual, expected)
	}
	return nil
}

func SetToolchainDirective(ctx context.Context, runner GoRunner, expected string) error {
	if expected != "" {
		if _, err := runner.Run(ctx, "mod", "edit", "-toolchain="+expected); err != nil {
			return fmt.Errorf("set exact toolchain directive %q: %w", expected, err)
		}
	} else {
		metadata, err := runner.Run(ctx, "mod", "edit", "-json")
		if err != nil {
			return fmt.Errorf("read toolchain directive: %w", err)
		}
		var mod goModJSON
		if err := json.Unmarshal(metadata, &mod); err != nil {
			return fmt.Errorf("parse toolchain directive: %w", err)
		}
		if mod.Toolchain != "" {
			if err := removeToolchainDirective(runner.Root); err != nil {
				return fmt.Errorf("remove toolchain directive: %w", err)
			}
		}
	}
	metadata, err := runner.Run(ctx, "mod", "edit", "-json")
	if err != nil {
		return fmt.Errorf("verify toolchain directive: %w", err)
	}
	var mod goModJSON
	if err := json.Unmarshal(metadata, &mod); err != nil {
		return fmt.Errorf("parse verified toolchain directive: %w", err)
	}
	if mod.Toolchain != expected {
		return fmt.Errorf("toolchain directive is %q, want %q", mod.Toolchain, expected)
	}
	return nil
}

func removeToolchainDirective(root string) error {
	if root == "" {
		root = "."
	}
	path := filepath.Join(root, "go.mod")
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path) // #nosec G304 -- fixed go.mod path under the maintainer-selected repository root.
	if err != nil {
		return err
	}
	var output bytes.Buffer
	removed := false
	for _, line := range bytes.SplitAfter(data, []byte("\n")) {
		fields := strings.Fields(strings.TrimSpace(string(line)))
		if len(fields) != 0 && fields[0] == "toolchain" {
			removed = true
			continue
		}
		if _, err := output.Write(line); err != nil {
			return err
		}
	}
	if !removed {
		return nil
	}
	return writeFileAtomically(path, output.Bytes(), info.Mode().Perm())
}

func writeFileAtomically(path string, content []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".dependency-upgrade-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func environmentWith(overrides map[string]string) []string {
	env := os.Environ()
	filtered := make([]string, 0, len(env)+len(overrides))
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		if _, replaced := overrides[key]; !replaced {
			filtered = append(filtered, item)
		}
	}
	for key, value := range overrides {
		filtered = append(filtered, key+"="+value)
	}
	return filtered
}
