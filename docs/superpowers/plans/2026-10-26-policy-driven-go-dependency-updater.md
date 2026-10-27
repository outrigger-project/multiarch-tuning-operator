# Policy-driven Go Dependency Updater Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the OCP-wide `upgrade.sh` workflow with a deterministic Go module updater that applies exact independent Go/Kubernetes policies and emits an auditable report.

**Architecture:** A thin shell entry point delegates to a standard-library Go CLI under `hack/upgrade-automation`. The Go package separates policy/baseline evaluation, Go command execution, version candidates, transactional graph trials, and JSON reporting. Integration tests use temporary modules and a local file module proxy.

**Tech Stack:** Bash wrapper; Go; existing `golang.org/x/mod/semver` dependency; Go module commands; `go test`; local GOPROXY fixtures.

**Spec:** `docs/superpowers/specs/2026-10-26-policy-driven-go-dependency-updater-design.md`

## Global Constraints

- Four modes are `pin-both`, `go-only`, `kubernetes-only`, and `both`.
- Updated Go targets are exact patch versions; Kubernetes targets are exact `module=version` pairs.
- Pinned Go directive/toolchain and every pinned `k8s.io/*` selected version remain exact.
- Each module candidate is an explicit version request without `go get -u`; use `GOTOOLCHAIN=local` and verify the compiler.
- Unsupported local/remote replacements and active workspaces fail before mutation.
- The updater never commits, pushes, or creates a pull request; the report defaults to `_output/dependency-upgrade-report.json`.
- Any failed/incomplete run restores original `go.mod`, `go.sum`, and `vendor`; the report remains available.
- No new module dependencies; no change to Prow configuration.

## Review Focus

- **Exact policy after `go mod tidy`:** test that an implicit go/toolchain or Kubernetes change rejects a candidate and restores the checkpoint (Task 2 and Task 3).
- **Version ordering around pseudo-versions and prereleases:** test stable-only, strictly-newer candidate ordering without path migration (Task 3).
- **Graph changes that introduce Kubernetes modules:** test that unlisted new modules reject trials and listed exact targets allow them (Task 4).
- **Retrieval failure misclassified as incompatibility:** test local proxy/auth errors produce execution failure and run rollback (Task 5).
- **Interrupted or incomplete run leaves mixed vendor state:** test signal/bound cleanup restores all dependency artifacts and retains deterministic report (Task 5).

---

## File Structure

- `hack/upgrade-automation/scripts/upgrade.sh`: same entry path, reduced to a wrapper for the Go CLI.
- `hack/upgrade-automation/cmd/updater/main.go`: parse CLI inputs, invoke updater, write report, map run status to exit code.
- `hack/upgrade-automation/updater/policy.go`: mode/target validation and exact policy representation.
- `hack/upgrade-automation/updater/graph.go`: baseline capture, `go.mod` metadata, module graph, direct/indirect state, graph comparison.
- `hack/upgrade-automation/updater/go.go`: controlled `go` subprocess environment and module command helpers.
- `hack/upgrade-automation/updater/transaction.go`: artifact checkpoints, rollback, signal cleanup, final vendor transaction.
- `hack/upgrade-automation/updater/candidates.go`: stable newer candidate enumeration and ordering.
- `hack/upgrade-automation/updater/engine.go`: target transaction, deterministic passes, candidate acceptance, bounds/cycle handling.
- `hack/upgrade-automation/updater/normalize.go`: final exact two-block `go.mod` normalization using Go module syntax metadata.
- `hack/upgrade-automation/updater/report.go`: report types, outcomes, stable JSON serialization.
- `hack/upgrade-automation/updater/*_test.go`: focused unit tests and local-proxy integration fixtures.
- `hack/upgrade-automation/README.md`: invocation, policy, report, rollback, limitations, exit codes.
- `hack/upgrade-automation/scripts/lib/*.sh`: remove obsolete helpers after confirming no non-documentation callers remain.

## Shared Interfaces

Use these package-level data contracts in `updater`:

```go
type Mode string // PinBoth, GoOnly, KubernetesOnly, Both
type Policy struct {
    Mode Mode
    GoTarget string
    KubernetesTargets map[string]string
}
type Module struct {
    Path, Version string
    Main, Direct bool
}
type Snapshot struct {
    GoDirective string
    Toolchain string // empty only when absent
    Modules map[string]Module
}
type Change struct { Path, Before, After string }
type Attempt struct {
    Version, Outcome, Evidence string
    Changes []Change
}
type ModuleResult struct {
    Path, InitialVersion, FinalVersion string
    InitialDirect, FinalDirect bool
    Outcome string
    Attempts []Attempt
}
type Report struct {
    Status string
    Policy Policy
    Baseline, Final Snapshot
    Modules []ModuleResult // sorted by path
}
type Config struct {
    Root string
    Policy Policy
    GoBinary string
    MaxPasses int // zero uses the fixed production bound
    Progress io.Writer // optional human-readable progress destination
}
```

Primary functions:

```go
func ParsePolicy(mode, goTarget string, k8sTargetArgs []string) (Policy, error)
func CaptureSnapshot(ctx context.Context, root, goBin string) (Snapshot, error)
func ValidatePolicy(before, after Snapshot, policy Policy) []error
func ListCandidates(ctx context.Context, runner GoRunner, modulePath, current string) ([]string, error)
func ApplyTargets(ctx context.Context, runner GoRunner, baseline Snapshot, policy Policy, transaction *Transaction) (Snapshot, error)
func Run(ctx context.Context, cfg Config) (Report, error)
func WriteReport(path string, report Report) error
```

`Config` carries repository root, validated `Policy`, selected Go executable, report path, and a fixed pass bound. Keep command construction injectable enough for unit tests; integration tests run the real Go executable against a local proxy.

---

### Task 1: Policy and baseline inventory

**Files:**
- Create: `hack/upgrade-automation/updater/policy.go`
- Create: `hack/upgrade-automation/updater/graph.go`
- Create: `hack/upgrade-automation/updater/policy_test.go`
- Create: `hack/upgrade-automation/updater/graph_test.go`

**Interfaces:** Implements `ParsePolicy`, `CaptureSnapshot`, `ValidatePolicy`, `Policy`, `Module`, and `Snapshot` above.

- [ ] **Step 1: Write policy tests** for all four modes, missing required exact target, malformed target, duplicate Kubernetes module, and accidental target in pin mode.
- [ ] **Step 2: Run `go test ./hack/upgrade-automation/updater -run 'TestParsePolicy|TestValidatePolicy' -count=1`** and confirm new tests fail for missing functions/behavior.
- [ ] **Step 3: Implement policy parsing and exact policy validation**; Go target requires `X.Y.Z`, every Kubernetes target is exact module/version, and updated target maps are nonempty.
- [ ] **Step 4: Write snapshot/preflight tests** from deterministic fixtures for directive/toolchain presence, selected module versions, direct/indirect classification, local and remote replacements, and active workspace detection.
- [ ] **Step 5: Implement `CaptureSnapshot`** with `go mod edit -json` plus `go list -m -json all`, sorted stable map representation, and early rejection of local/remote replace directives or active workspace mode.
- [ ] **Step 6: Run `go test ./hack/upgrade-automation/updater -count=1`** and confirm policy and inventory tests pass.

### Task 2: Controlled Go execution and whole-run transaction

**Files:**
- Create: `hack/upgrade-automation/updater/go.go`
- Create: `hack/upgrade-automation/updater/transaction.go`
- Create: `hack/upgrade-automation/updater/go_test.go`
- Create: `hack/upgrade-automation/updater/transaction_test.go`
- Modify: `hack/upgrade-automation/updater/graph.go` to use the controlled runner

**Interfaces:** Uses `Snapshot`; provides controlled command runner and transaction helpers to `Run`.

- [ ] **Step 1: Write compiler/environment tests** asserting exact `go version`, `GOTOOLCHAIN=local`, cleared vendor `GOFLAGS`, and correct selected root.
- [ ] **Step 2: Run `go test ./hack/upgrade-automation/updater -run 'TestGoEnvironment|TestCompilerVersion' -count=1`** and confirm failures.
- [ ] **Step 3: Implement Go command execution** so module commands always use the selected exact compiler, `GOTOOLCHAIN=local`, and module mode rather than inherited `-mod=vendor`.
- [ ] **Step 4: Write transaction tests** for checkpoint/restore of `go.mod`, `go.sum`, and `vendor`, including failed get, failed tidy, final vendor failure, and interrupt cleanup.
- [ ] **Step 5: Implement transaction helpers** that preserve original dependency artifacts until full success and restore them on errors, incomplete status, or signals.
- [ ] **Step 6: Run `go test ./hack/upgrade-automation/updater -run 'TestGoEnvironment|TestCompilerVersion|TestTransaction' -count=1`** and confirm pass.

### Task 3: Candidate selection and exact target transaction

**Files:**
- Create: `hack/upgrade-automation/updater/candidates.go`
- Modify: `hack/upgrade-automation/updater/engine.go`
- Create: `hack/upgrade-automation/updater/candidates_test.go`
- Create: `hack/upgrade-automation/updater/target_integration_test.go`

**Interfaces:** Uses `Policy`, `Snapshot`, `GoRunner`, and `Transaction`; implements `ListCandidates(ctx, runner, modulePath, current)` and atomic `ApplyTargets(ctx, runner, baseline, policy, transaction)`.

- [ ] **Step 1: Write candidate ordering tests** for newest-first stable releases, prerelease filtering, exclusions, no downgrades, same module path, and pseudo-version baselines.
- [ ] **Step 2: Run `go test ./hack/upgrade-automation/updater -run TestListCandidates -count=1`** and confirm failures.
- [ ] **Step 3: Implement candidate listing** using Go module version output plus the already-present `golang.org/x/mod/semver`; order stable versions newest-first and filter versions that are not newer than current.
- [ ] **Step 4: Write target-transaction integration tests** using a local file module proxy for all policy combinations, exact patch enforcement, unavailable Go/Kubernetes target combinations, and toolchain absent/present handling.
- [ ] **Step 5: Implement atomic target setup** with one multi-module `go get` request for Kubernetes targets, exact Go directive/toolchain rules, tidy, graph reload, and exact policy validation.
- [ ] **Step 6: Run `go test ./hack/upgrade-automation/updater -run 'TestListCandidates|TestTarget' -count=1`** and confirm pass.

### Task 4: Sequential graph updater

**Files:**
- Modify: `hack/upgrade-automation/updater/engine.go`
- Create: `hack/upgrade-automation/updater/report.go` for shared report data types
- Create: `hack/upgrade-automation/updater/engine_integration_test.go`

**Interfaces:** Implements `Run(ctx, cfg) (Report, error)` over policy, snapshots, target transaction, candidates, and transaction helpers.

- [ ] **Step 1: Write local-proxy integration cases** for latest candidate rejected/older accepted, no compatible candidate, dependency raising Go, Kubernetes change only transitively, indirect module update, graph add/remove, and independent Kubernetes versions.
- [ ] **Step 2: Run `go test ./hack/upgrade-automation/updater -run TestEngine -count=1`** and confirm failures.
- [ ] **Step 3: Implement deterministic module passes**: lexical module order; explicit candidate requests without `-u`; tidy and full graph reload after each candidate; exact policy checks; checkpoint rollback; record accepted transitive diffs; emit source-to-candidate progress and final no-update reasons to `Config.Progress`.
- [ ] **Step 3a: Normalize final requirements** after the last tidy into exactly two `require` blocks (direct, indirect), preserving empty groups and all versions/comments; reload and verify the graph is unchanged before vendoring.
- [ ] **Step 4: Add Kubernetes-map cases** where a new unlisted `k8s.io/*` module is rejected and an explicitly mapped module/version is accepted; assert excluded candidates are never selected.
- [ ] **Step 5: Add pass-bound/cycle tests** and implement considered-pair tracking, graph-cycle detection, and a fixed maximum pass count that returns incomplete status.
- [ ] **Step 6: Run `go test ./hack/upgrade-automation/updater -run 'TestEngine|TestKubernetesMap|TestPass' -count=1`** and confirm pass.

### Task 5: Reporting and failure distinctions

**Files:**
- Modify: `hack/upgrade-automation/updater/report.go`
- Create: `hack/upgrade-automation/updater/report_test.go`
- Modify: `hack/upgrade-automation/updater/engine.go`
- Modify: `hack/upgrade-automation/updater/policy.go` for stable validation evidence
- Modify: `hack/upgrade-automation/updater/graph.go` for typed repository precondition errors
- Modify: `hack/upgrade-automation/updater/go.go` to redact credentials from command errors

**Interfaces:** Implements report types above, `WriteReport(path, Report) error`, and final outcomes for every initial/new/removed module.

- [ ] **Step 1: Write deterministic report tests** for sorted module/attempt/change arrays, all outcomes, baseline/final state, accepted transitive changes, and rollback evidence.
- [ ] **Step 2: Run `go test ./hack/upgrade-automation/updater -run TestReport -count=1`** and confirm failure.
- [ ] **Step 3: Implement stable JSON reporting** with `completed`, `incomplete`, or `failed` status and module outcomes `updated`, `already-current`, `policy-pinned`, `blocked-by-constraints`, `removed`, or `execution-failure`.
- [ ] **Step 4: Write proxy failure tests** for unavailable module and authentication errors; assert the report says execution failure, never incompatibility, and the run transaction rolls back.
- [ ] **Step 5: Implement error classification and report-on-terminal-path**; exact exit mapping is 0 success, 1 invalid invocation/precondition, 2 retrieval/execution/final validation, 3 incomplete.
- [ ] **Step 6: Run `go test ./hack/upgrade-automation/updater -run 'TestReport|TestRetrievalFailure' -count=1`** and confirm pass.

### Task 6: CLI, wrapper, and repository documentation

**Files:**
- Create: `hack/upgrade-automation/cmd/updater/main.go`
- Modify: `hack/upgrade-automation/scripts/upgrade.sh`
- Modify: `hack/upgrade-automation/README.md`
- Delete if unused: `hack/upgrade-automation/scripts/lib/version-discovery.sh`
- Delete if unused: `hack/upgrade-automation/scripts/lib/validations.sh`
- Delete if unused: `hack/upgrade-automation/scripts/lib/file-updates.sh`
- Create: `hack/upgrade-automation/cmd/updater/main_test.go`

**Interfaces:** CLI parses `--mode`, `--go-target`, repeatable `--k8s-target`, `--go-bin`, and `--report`; invokes `ParsePolicy`, `Run`, and `WriteReport`; wrapper `exec`s the Go CLI and propagates exit code.

- [ ] **Step 1: Write CLI tests** for mode/target argument matrix, default report location, report-on-failure, and exit codes.
- [ ] **Step 2: Run `go test ./hack/upgrade-automation/cmd/updater -count=1`** and confirm failures.
- [ ] **Step 3: Implement CLI and wrapper**; require repository root and clean tree before mutation, report to `_output/dependency-upgrade-report.json` by default, and create no git commits/branches.
- [ ] **Step 4: Replace README** with four invocation examples, exact target-map syntax, report schema/outcomes, transaction behavior, unsupported workspace/replacements, version limitations, and exit codes; remove obsolete OCP workflow docs.
- [ ] **Step 5: Remove old libraries** only after searching code and docs to confirm they have no remaining consumers.
- [ ] **Step 6: Run `go test ./hack/upgrade-automation/... -count=1`, `bash -n hack/upgrade-automation/scripts/upgrade.sh`, and README/script caller search**; confirm tests/syntax pass and the old interface is intentionally documented as retired.

### Task 7: End-to-end repository validation

**Files:**
- No implementation files unless a validation defect is found.
- Generated: dependency report and vendor/dependency artifacts in a separate clean worktree, not mixed into the implementation diff.

**Interfaces:** Uses the built updater executable against a clean worktree so the currently dirty development tree and untracked user files are untouched.

- [ ] **Step 1: Build the updater** with the repository Go compiler and run all updater unit/integration tests.
- [ ] **Step 2: Run `pin-both` against a clean detached worktree** using the baseline-exact Go compiler; save the report and dependency diff outside the implementation worktree.
- [ ] **Step 3: Verify report accounting and exact invariants**: compare baseline/final Go directive/toolchain and every `k8s.io/*` selected version; ensure every graph module has a disposition.
- [ ] **Step 4: Snapshot the Go module proxy cache from the first run as a local file proxy, rerun from a clean worktree at the accepted result, and compare dependency output for idempotence against the frozen candidate list.**
- [ ] **Step 5: Run `make vendor`, generation targets, formatting, lint, build, unit tests, and applicable CI checks** in a disposable validation worktree; capture unavailable checks explicitly.
- [ ] **Step 6: Review `git diff --check`, implementation diff, and separate dependency-refresh diff**; verify the untracked `rpms.in.yaml` in the user's worktree remains untouched.
