package updater

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

type Change struct {
	Path   string `json:"path"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

type Attempt struct {
	Version  string   `json:"version"`
	Outcome  string   `json:"outcome"`
	Evidence string   `json:"evidence,omitempty"`
	Changes  []Change `json:"changes,omitempty"`
}

type ModuleResult struct {
	Path                  string    `json:"path"`
	InitialVersion        string    `json:"initialVersion"`
	FinalVersion          string    `json:"finalVersion"`
	InitialDirect         bool      `json:"initialDirect"`
	FinalDirect           bool      `json:"finalDirect"`
	Outcome               string    `json:"outcome"`
	Attempts              []Attempt `json:"attempts,omitempty"`
	CandidateLimitReached bool      `json:"candidateLimitReached,omitempty"`
}

type Report struct {
	Status     string         `json:"status"`
	Policy     Policy         `json:"policy"`
	Baseline   Snapshot       `json:"baseline"`
	Final      Snapshot       `json:"final"`
	Modules    []ModuleResult `json:"modules"`
	RolledBack bool           `json:"rolledBack"`
	Failure    string         `json:"failure,omitempty"`
}

type PreconditionError struct {
	Err error
}

func (e *PreconditionError) Error() string { return e.Err.Error() }
func (e *PreconditionError) Unwrap() error { return e.Err }

type UpdaterError struct {
	Code int
	Err  error
}

func (e *UpdaterError) Error() string { return e.Err.Error() }
func (e *UpdaterError) Unwrap() error { return e.Err }

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var updaterErr *UpdaterError
	if errors.As(err, &updaterErr) {
		return updaterErr.Code
	}
	var precondition *PreconditionError
	if errors.As(err, &precondition) {
		return 1
	}
	return 2
}

var (
	proxyCredentials = regexp.MustCompile(`(?i)(https?://)[^/@\s]+@`)
	querySecret      = regexp.MustCompile(`(?i)(token|password|passwd|secret|api[_-]?key|access[_-]?token)=([^&\s]+)`)
	bearerSecret     = regexp.MustCompile(`(?i)(authorization:\s*bearer\s+)[^\s]+`)
)

func WriteReport(path string, report Report) error {
	if path == "" {
		return fmt.Errorf("report path is required")
	}
	report = normalizeReport(report)
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	data = append(data, '\n')
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".dependency-upgrade-report-*")
	if err != nil {
		return fmt.Errorf("create temporary report: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return fmt.Errorf("set report permissions: %w", err)
	}
	if _, err := bytes.NewReader(data).WriteTo(temporary); err != nil {
		temporary.Close()
		return fmt.Errorf("write report: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync report: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close report: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish report: %w", err)
	}
	return nil
}

func normalizeReport(report Report) Report {
	if report.Modules == nil {
		report.Modules = []ModuleResult{}
	}
	sort.Slice(report.Modules, func(i, j int) bool { return report.Modules[i].Path < report.Modules[j].Path })
	for i := range report.Modules {
		module := &report.Modules[i]
		for j := range module.Attempts {
			attempt := &module.Attempts[j]
			attempt.Evidence = sanitizeEvidence(attempt.Evidence)
			sort.Slice(attempt.Changes, func(a, b int) bool { return attempt.Changes[a].Path < attempt.Changes[b].Path })
		}
	}
	report.Failure = sanitizeEvidence(report.Failure)
	return report
}

func sanitizeEvidence(evidence string) string {
	evidence = proxyCredentials.ReplaceAllString(evidence, `${1}[REDACTED]@`)
	evidence = querySecret.ReplaceAllString(evidence, `${1}=[REDACTED]`)
	return bearerSecret.ReplaceAllString(evidence, `${1}[REDACTED]`)
}
