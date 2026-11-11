package updater

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestTransactionRestoresCheckpointAndWholeRunArtifacts(t *testing.T) {
	root := t.TempDir()
	writeDependencyArtifacts(t, root)
	transaction, err := BeginTransaction(root)
	if err != nil {
		t.Fatalf("BeginTransaction() error = %v", err)
	}
	checkpoint, err := transaction.Checkpoint()
	if err != nil {
		t.Fatalf("Checkpoint() error = %v", err)
	}
	writeFile(t, filepath.Join(root, "go.mod"), "module fixture.test/root\ngo 1.26.8\n")
	writeFile(t, filepath.Join(root, "go.sum"), "new checksum\n")
	if err := transaction.RestoreCheckpoint(checkpoint); err != nil {
		t.Fatalf("RestoreCheckpoint() error = %v", err)
	}
	assertFile(t, filepath.Join(root, "go.mod"), "module fixture.test/root\ngo 1.26.7\n")
	assertFile(t, filepath.Join(root, "go.sum"), "baseline checksum\n")
	assertFile(t, filepath.Join(root, "vendor", "fixture.txt"), "baseline vendor\n")

	writeFile(t, filepath.Join(root, "go.mod"), "partial trial\n")
	writeFile(t, filepath.Join(root, "go.sum"), "partial sum\n")
	writeFile(t, filepath.Join(root, "vendor", "fixture.txt"), "partial vendor\n")
	writeFile(t, filepath.Join(root, "vendor", "new.txt"), "created by failed vendor\n")
	if err := transaction.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	assertFile(t, filepath.Join(root, "go.mod"), "module fixture.test/root\ngo 1.26.7\n")
	assertFile(t, filepath.Join(root, "go.sum"), "baseline checksum\n")
	assertFile(t, filepath.Join(root, "vendor", "fixture.txt"), "baseline vendor\n")
	if _, err := os.Stat(filepath.Join(root, "vendor", "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("failed vendor output remains: stat error = %v", err)
	}
}

func TestTransactionRollbackRemovesNewGoSum(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module fixture.test/root\ngo 1.26.7\n")
	transaction, err := BeginTransaction(root)
	if err != nil {
		t.Fatalf("BeginTransaction() error = %v", err)
	}
	writeFile(t, filepath.Join(root, "go.sum"), "created during trial\n")
	if err := transaction.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.sum")); !os.IsNotExist(err) {
		t.Fatalf("new go.sum remains: stat error = %v", err)
	}
}

func TestTransactionRollbackAfterCancellation(t *testing.T) {
	root := t.TempDir()
	writeDependencyArtifacts(t, root)
	transaction, err := BeginTransaction(root)
	if err != nil {
		t.Fatalf("BeginTransaction() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ctx.Err() == nil {
		t.Fatal("context was not cancelled")
	}
	writeFile(t, filepath.Join(root, "go.mod"), "interrupted trial\n")
	if err := transaction.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	assertFile(t, filepath.Join(root, "go.mod"), "module fixture.test/root\ngo 1.26.7\n")
}

func writeDependencyArtifacts(t *testing.T, root string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "go.mod"), "module fixture.test/root\ngo 1.26.7\n")
	writeFile(t, filepath.Join(root, "go.sum"), "baseline checksum\n")
	writeFile(t, filepath.Join(root, "vendor", "fixture.txt"), "baseline vendor\n")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path, content string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != content {
		t.Fatalf("%s = %q, want %q", path, got, content)
	}
}
