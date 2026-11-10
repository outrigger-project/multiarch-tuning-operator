# Dependency updater sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Bring approved updater improvements into this operator without foreign repository references.
**Architecture:** Sync tracked updater sources from the approved endpoint, adapting CLI identity, tests and documentation to MTO. Preserve existing public command entrypoint.
**Tech Stack:** Go, Bash, existing module dependencies.
**Spec:** docs/superpowers/specs/2026-11-10-updater-sync-design.md

## Global Constraints
- Preserve `github.com/openshift/multiarch-tuning-operator` identity.
- No foreign operator references, dependency upgrades, or vendor edits.
- No commits, publishing or actual updater execution against dependencies.

## Review Focus
- Quoted module declarations must validate correctly.
- Wrong repositories must be rejected.
- Failed runs must preserve existing successful reports.
- Launcher must honor compiler selection and readonly module mode.
- Release targets and rollback must retain exact policy pins.

### Task 1: Port and adapt automation
Files: `hack/upgrade-automation/{cmd/updater,updater,scripts,README.md}`.
Interfaces: existing CLI plus `--k8s-release`, existing transactional Run and JSON reporting.
- [x] Add CLI quoted-module and release-flag regressions; run and observe failures on old implementation.
- [x] Sync upstream tracked files, replacing module identity, environment naming and README heading. Use `example.com/other-operator` for negative identity fixture.
- [x] Run full updater suite, including upstream release, rollback, report preservation and launcher tests.

### Task 2: Verify integration
- [x] Run vet/build, Bash syntax, direct help and reference/whitespace checks.
- [x] Attempt repository-wide non-e2e tests and report blockers.
- [x] Compare sync against source endpoint; only MTO adaptations may differ. Confirm module/vendor unchanged.

## Execution record
Inline execution requested by user approval. No delegation. Keep changes in current checkout for user inspection.

- RED: quoted MTO module identity and Kubernetes release flag failed against old implementation; unrelated repository rejected correctly.
- GREEN: `go test ./hack/upgrade-automation/... -count=1` passed both packages (74 top-level tests).
- `go vet ./hack/upgrade-automation/...`, `go build ./hack/upgrade-automation/...`, Bash syntax, direct launcher help and `git diff --check` passed.
- Case-insensitive foreign-operator-reference scan returned no matches in updater sources/docs. All 30 upstream files match approved endpoint after only MTO identity adaptations.
- `git diff -- go.mod go.sum vendor` empty. No actual dependency upgrade performed.
- Broader non-e2e test commands exceeded 120-second harness limit twice, including with per-test timeout 30s. Observed API, cmd, updater and storage packages pass; whole suite not established. Logs: `/tmp/mto-updater-repo-tests.log`, `/tmp/mto-updater-repo-tests-bounded.log`.
- Final review: self-review only, no independent reviewer. Checked port equivalence, module identity boundary, CLI callers, release target resolution and transaction/report flow. No port-specific issues found.
