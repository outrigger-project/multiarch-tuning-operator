package updater

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type progressCallback func(string)

func (f progressCallback) Write(data []byte) (int, error) {
	f(string(data))
	return len(data), nil
}

func TestIncompleteRunRollbackFailureReturnsExecutionExit(t *testing.T) {
	for _, failRollback := range []bool{false, true} {
		name := "successful rollback"
		if failRollback {
			name = "failed rollback"
		}
		t.Run(name, func(t *testing.T) {
			root, proxy := engineFixture(t)
			for _, version := range []string{testInitialVersion, testUpdatedVersion} {
				writeProxyModule(t, proxy, "example.test/lib", version, "1.20", nil)
			}
			writeProxyVersionList(t, proxy, "example.test/lib", []string{testInitialVersion, testUpdatedVersion})
			writeModule(t, root, "module fixture.test/root\n\ngo "+testGoVersion+"\nrequire example.test/lib v1.0.0\n")
			writeFile(t, filepath.Join(root, "fixture.go"), "package fixture\nimport _ \"example.test/lib\"\n")
			setLocalProxy(t, proxy)
			backupRoot := t.TempDir()
			t.Setenv("TMPDIR", backupRoot)
			injected := false
			progress := progressCallback(func(message string) {
				if !failRollback || !strings.Contains(message, "Run incomplete: reached pass limit") {
					return
				}
				backups, err := filepath.Glob(filepath.Join(backupRoot, "mto-upgrade-transaction-*", "original", "go.mod"))
				if err != nil || len(backups) != 1 {
					t.Fatalf("find backup: %v, %v", backups, err)
				}
				if err := os.Remove(backups[0]); err != nil {
					t.Fatal(err)
				}
				injected = true
			})
			report, err := Run(context.Background(), Config{
				Root: root, Policy: mustPolicy(t, "pin-both", nil), MaxPasses: 1, Progress: progress,
			})
			wantStatus, wantCode := runIncomplete, 3
			if failRollback {
				wantStatus, wantCode = runFailed, 2
			}
			if err == nil || report.Status != wantStatus || ExitCode(err) != wantCode || !report.RolledBack {
				t.Fatalf("Run: status=%s exit=%d rolledBack=%v err=%v", report.Status, ExitCode(err), report.RolledBack, err)
			}
			if failRollback && (!injected || !strings.Contains(err.Error(), "rollback dependency update")) {
				t.Fatalf("rollback failure was not preserved: %v", err)
			}
		})
	}
}
