package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

var modulePathMismatch = regexp.MustCompile(`module declares its path as:\s*(\S+)\s+but was required as:\s*(\S+)`)

type migrationConstraint struct{ err error }

type migrationTrial struct {
	TargetPath  string
	SourceFiles []string
}

func (e *migrationConstraint) Error() string { return e.err.Error() }
func (e *migrationConstraint) Unwrap() error { return e.err }

func renamedModulePath(err error, requested string) string {
	match := modulePathMismatch.FindStringSubmatch(err.Error())
	if len(match) != 3 || match[2] != requested || match[1] == requested || module.CheckPath(match[1]) != nil {
		return ""
	}
	return match[1]
}

func migrateModule(ctx context.Context, runner GoRunner, oldPath, newPath, version string, graph Snapshot, policy Policy, transaction *Transaction, checkpoint ModuleCheckpoint) ([]string, error) {
	if isProtectedModule(oldPath, policy) || isProtectedModule(newPath, policy) {
		return nil, &migrationConstraint{fmt.Errorf("module-path migration %s -> %s touches a policy-protected module", oldPath, newPath)}
	}
	if err := module.Check(newPath, version); err != nil {
		return nil, &migrationConstraint{fmt.Errorf("invalid renamed module target %s@%s: %w", newPath, version, err)}
	}
	if selected, exists := graph.Modules[newPath]; exists && selected.Version != version {
		return nil, &migrationConstraint{fmt.Errorf("renamed module %s already selected at %s; refusing to override it with %s", newPath, selected.Version, version)}
	}
	// Download before changing imports; verify the new path really declares itself.
	output, err := runner.Run(ctx, "mod", "download", "-json", newPath+"@"+version)
	if err != nil {
		return nil, err
	}
	var downloaded struct {
		Path    string
		Version string
		GoMod   string
	}
	if err := json.Unmarshal(output, &downloaded); err != nil {
		return nil, fmt.Errorf("decode renamed module download: %w", err)
	}
	if downloaded.Path != newPath || downloaded.Version != version || downloaded.GoMod == "" {
		return nil, fmt.Errorf("unexpected downloaded renamed module metadata for %s@%s", newPath, version)
	}
	data, err := os.ReadFile(downloaded.GoMod)
	if err != nil {
		return nil, fmt.Errorf("read renamed module declaration: %w", err)
	}
	if declared := modfile.ModulePath(data); declared != newPath {
		return nil, &migrationConstraint{fmt.Errorf("renamed target %s declares module path %q", newPath, declared)}
	}
	files, err := rewriteModuleImports(runner.Root, oldPath, newPath, graph, transaction, checkpoint)
	if err != nil {
		return files, err
	}
	if len(files) == 0 {
		return nil, &migrationConstraint{fmt.Errorf("cannot migrate %s: no root-module imports use this path; its transitive consumers must migrate first", oldPath)}
	}
	if _, err := runner.Run(ctx, "mod", "edit", "-droprequire="+oldPath, "-require="+newPath+"@"+version); err != nil {
		return files, err
	}
	if _, err := runner.Run(ctx, "get", newPath+"@"+version); err != nil {
		return files, err
	}
	return files, nil
}

func rewriteModuleImports(root, oldPath, newPath string, graph Snapshot, transaction *Transaction, checkpoint ModuleCheckpoint) ([]string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	var changed []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path == root {
				return nil
			}
			if strings.HasPrefix(entry.Name(), ".") || entry.Name() == "vendor" || entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				return filepath.SkipDir
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		positions := token.NewFileSet()
		file, err := parser.ParseFile(positions, path, data, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse imports in %s: %w", path, err)
		}
		for index := len(file.Imports) - 1; index >= 0; index-- {
			literal := file.Imports[index].Path
			importPath, err := strconv.Unquote(literal.Value)
			if err != nil {
				return err
			}
			if !belongsToModule(importPath, oldPath, graph) {
				continue
			}
			start, end := positions.Position(literal.Pos()).Offset, positions.Position(literal.End()).Offset
			replacement := strconv.Quote(newPath + strings.TrimPrefix(importPath, oldPath))
			data = append(data[:start], append([]byte(replacement), data[end:]...)...)
		}
		original, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if string(data) == string(original) {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if err := transaction.WriteSource(checkpoint, relative, data); err != nil {
			return err
		}
		changed = append(changed, filepath.ToSlash(relative))
		return nil
	})
	return changed, err
}

func belongsToModule(importPath, oldPath string, graph Snapshot) bool {
	if importPath != oldPath && !strings.HasPrefix(importPath, oldPath+"/") {
		return false
	}
	for path := range graph.Modules {
		if strings.HasPrefix(path, oldPath+"/") && (importPath == path || strings.HasPrefix(importPath, path+"/")) {
			return false
		}
	}
	return true
}

func annotateMigrations(results map[string]*ModuleResult, migrations map[string]migrationTrial) {
	for path, result := range results {
		for index := range result.Attempts {
			attempt := &result.Attempts[index]
			if migration, exists := migrations[path+"@"+attempt.Version]; exists {
				attempt.TargetPath = migration.TargetPath
				attempt.SourceFiles = migration.SourceFiles
			}
		}
	}
}

func isMigrationBuildConstraint(err error) bool {
	if isConstraintFailure(err) {
		return true
	}
	for _, marker := range []string{"undefined:", "cannot use ", "imported and not used", "has no field or method", "too many arguments", "not enough arguments", "import cycle not allowed", "syntax error:"} {
		if strings.Contains(err.Error(), marker) {
			return true
		}
	}
	return false
}
