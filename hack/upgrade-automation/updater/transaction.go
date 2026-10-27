package updater

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Transaction struct {
	root      string
	backupDir string
	originals map[string]artifactBackup
	sequence  int
	closed    bool
}

type ModuleCheckpoint struct {
	dir       string
	artifacts map[string]artifactBackup
}

type artifactBackup struct {
	path    string
	existed bool
}

var dependencyArtifactPaths = []string{"go.mod", "go.sum", "vendor"}

func BeginTransaction(root string) (*Transaction, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve transaction root: %w", err)
	}
	backupDir, err := os.MkdirTemp("", "mto-upgrade-transaction-")
	if err != nil {
		return nil, fmt.Errorf("create transaction backup: %w", err)
	}
	transaction := &Transaction{root: root, backupDir: backupDir, originals: make(map[string]artifactBackup, len(dependencyArtifactPaths))}
	for _, name := range dependencyArtifactPaths {
		backup := artifactBackup{path: filepath.Join(backupDir, "original", name)}
		if err := backupArtifact(filepath.Join(root, name), backup.path, &backup.existed); err != nil {
			os.RemoveAll(backupDir)
			return nil, fmt.Errorf("backup %s: %w", name, err)
		}
		transaction.originals[name] = backup
	}
	return transaction, nil
}

func (t *Transaction) Checkpoint() (ModuleCheckpoint, error) {
	if t.closed {
		return ModuleCheckpoint{}, errors.New("transaction is closed")
	}
	t.sequence++
	dir := filepath.Join(t.backupDir, "checkpoints", fmt.Sprintf("%06d", t.sequence))
	checkpoint := ModuleCheckpoint{dir: dir, artifacts: make(map[string]artifactBackup, 2)}
	for _, name := range []string{"go.mod", "go.sum"} {
		backup := artifactBackup{path: filepath.Join(dir, name)}
		if err := backupArtifact(filepath.Join(t.root, name), backup.path, &backup.existed); err != nil {
			return ModuleCheckpoint{}, fmt.Errorf("checkpoint %s: %w", name, err)
		}
		checkpoint.artifacts[name] = backup
	}
	return checkpoint, nil
}

func (t *Transaction) RestoreCheckpoint(checkpoint ModuleCheckpoint) error {
	if t.closed {
		return errors.New("transaction is closed")
	}
	var failures []error
	for _, name := range []string{"go.mod", "go.sum"} {
		backup, ok := checkpoint.artifacts[name]
		if !ok {
			failures = append(failures, fmt.Errorf("checkpoint has no %s artifact", name))
			continue
		}
		if err := restoreArtifact(backup, filepath.Join(t.root, name)); err != nil {
			failures = append(failures, fmt.Errorf("restore checkpoint %s: %w", name, err))
		}
	}
	return errors.Join(failures...)
}

func (t *Transaction) Rollback() error {
	if t.closed {
		return nil
	}
	var failures []error
	for _, name := range dependencyArtifactPaths {
		if err := restoreArtifact(t.originals[name], filepath.Join(t.root, name)); err != nil {
			failures = append(failures, fmt.Errorf("restore original %s: %w", name, err))
		}
	}
	if len(failures) != 0 {
		return errors.Join(failures...)
	}
	if err := os.RemoveAll(t.backupDir); err != nil {
		return fmt.Errorf("remove transaction backup: %w", err)
	}
	t.closed = true
	return nil
}

func (t *Transaction) Commit() error {
	if t.closed {
		return errors.New("transaction is closed")
	}
	if err := os.RemoveAll(t.backupDir); err != nil {
		return fmt.Errorf("remove transaction backup: %w", err)
	}
	t.closed = true
	return nil
}

func backupArtifact(source, destination string, existed *bool) error {
	info, err := os.Lstat(source)
	if errors.Is(err, os.ErrNotExist) {
		*existed = false
		return nil
	}
	if err != nil {
		return err
	}
	*existed = true
	return copyArtifact(source, destination, info)
}

func restoreArtifact(backup artifactBackup, destination string) error {
	if err := os.RemoveAll(destination); err != nil {
		return err
	}
	if !backup.existed {
		return nil
	}
	info, err := os.Lstat(backup.path)
	if err != nil {
		return err
	}
	return copyArtifact(backup.path, destination, info)
}

func copyArtifact(source, destination string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return err
		}
		target, err := os.Readlink(source)
		if err != nil {
			return err
		}
		return os.Symlink(target, destination)
	}
	if info.IsDir() {
		if err := os.MkdirAll(destination, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			childSource := filepath.Join(source, entry.Name())
			childInfo, err := os.Lstat(childSource)
			if err != nil {
				return err
			}
			if err := copyArtifact(childSource, filepath.Join(destination, entry.Name()), childInfo); err != nil {
				return err
			}
		}
		return os.Chmod(destination, info.Mode().Perm())
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Chmod(destination, info.Mode().Perm())
}
