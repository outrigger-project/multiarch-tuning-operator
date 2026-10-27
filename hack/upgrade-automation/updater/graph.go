package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type goModJSON struct {
	Go        string `json:"Go"`
	Toolchain string `json:"Toolchain"`
	Require   []struct {
		Path     string `json:"Path"`
		Version  string `json:"Version"`
		Indirect bool   `json:"Indirect"`
	} `json:"Require"`
	Replace []struct {
		Old struct {
			Path    string `json:"Path"`
			Version string `json:"Version"`
		} `json:"Old"`
		New struct {
			Path    string `json:"Path"`
			Version string `json:"Version"`
		} `json:"New"`
	} `json:"Replace"`
}

type listedModule struct {
	Path    string `json:"Path"`
	Version string `json:"Version"`
	Main    bool   `json:"Main"`
	Replace *struct {
		Path    string `json:"Path"`
		Version string `json:"Version"`
	} `json:"Replace"`
}

func CaptureSnapshot(ctx context.Context, root, goBin string) (Snapshot, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Snapshot{}, &PreconditionError{Err: fmt.Errorf("resolve repository root: %w", err)}
	}
	if goBin == "" {
		goBin = "go"
	}
	runner := GoRunner{Root: root, Binary: goBin}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return Snapshot{}, &PreconditionError{Err: fmt.Errorf("read go.mod: %w", err)}
	}
	if err := rejectWorkspace(ctx, runner); err != nil {
		return Snapshot{}, err
	}

	modOutput, err := runner.Run(ctx, "mod", "edit", "-json")
	if err != nil {
		return Snapshot{}, fmt.Errorf("read go.mod metadata: %w", err)
	}
	var mod goModJSON
	if err := json.Unmarshal(modOutput, &mod); err != nil {
		return Snapshot{}, &PreconditionError{Err: fmt.Errorf("parse go.mod metadata: %w", err)}
	}
	if len(mod.Replace) != 0 {
		replacement := mod.Replace[0]
		return Snapshot{}, &PreconditionError{Err: fmt.Errorf("replace directives are unsupported: %s %s => %s %s", replacement.Old.Path, replacement.Old.Version, replacement.New.Path, replacement.New.Version)}
	}

	direct := make(map[string]bool, len(mod.Require))
	for _, requirement := range mod.Require {
		direct[requirement.Path] = !requirement.Indirect
	}
	modulesOutput, err := runner.Run(ctx, "list", "-m", "-json", "all")
	if err != nil {
		return Snapshot{}, fmt.Errorf("inventory selected module graph: %w", err)
	}
	modules := make(map[string]Module)
	decoder := json.NewDecoder(strings.NewReader(string(modulesOutput)))
	for {
		var listed listedModule
		if err := decoder.Decode(&listed); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return Snapshot{}, fmt.Errorf("decode selected module graph: %w", err)
		}
		if listed.Path == "" {
			return Snapshot{}, &PreconditionError{Err: errors.New("selected module graph contains an empty module path")}
		}
		if listed.Replace != nil {
			return Snapshot{}, &PreconditionError{Err: fmt.Errorf("replace directives are unsupported: selected module %q is replaced by %q", listed.Path, listed.Replace.Path)}
		}
		modules[listed.Path] = Module{Path: listed.Path, Version: listed.Version, Main: listed.Main, Direct: direct[listed.Path]}
	}
	if len(modules) == 0 {
		return Snapshot{}, &PreconditionError{Err: errors.New("selected module graph is empty")}
	}
	return Snapshot{GoDirective: mod.Go, Toolchain: mod.Toolchain, Modules: modules}, nil
}

func rejectWorkspace(ctx context.Context, runner GoRunner) error {
	output, err := runner.Run(ctx, "env", "GOWORK")
	if err != nil {
		return fmt.Errorf("detect Go workspace: %w", err)
	}
	workspace := strings.TrimSpace(string(output))
	if workspace != "" && workspace != "off" {
		return &PreconditionError{Err: fmt.Errorf("active Go workspace %q is unsupported; run from a module with GOWORK=off", workspace)}
	}
	return nil
}
