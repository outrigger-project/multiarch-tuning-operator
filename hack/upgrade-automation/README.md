# Go dependency updater

`scripts/upgrade.sh` tries dependency updates only for modules explicitly required in the root `go.mod`, including `// indirect` requirements. It still inventories and validates the entire selected module graph. It does not choose Go or Kubernetes target versions, create commits, push branches, or open pull requests. Maintainers can run it manually; the external Prow periodic job can pass the same explicit inputs and publish the workspace changes.

## Modes

Run `hack/upgrade-automation/scripts/upgrade.sh` with exactly one mode:

```bash
# Upgrade eligible dependencies while preserving the baseline Go and Kubernetes pins.
hack/upgrade-automation/scripts/upgrade.sh --mode pin-both

# Set a new exact Go patch target and preserve each baseline Kubernetes module version.
hack/upgrade-automation/scripts/upgrade.sh --mode go-only --go-target 1.26.8

# Set exact Kubernetes module targets and preserve the baseline Go directive/toolchain.
hack/upgrade-automation/scripts/upgrade.sh --mode kubernetes-only \
  --k8s-target k8s.io/api=v0.36.6 \
  --k8s-target k8s.io/apimachinery=v0.36.6

# Set both independent policies.
hack/upgrade-automation/scripts/upgrade.sh --mode both \
  --go-target 1.26.8 \
  --k8s-target k8s.io/api=v0.36.6 \
  --k8s-target k8s.io/apimachinery=v0.36.6
```

`--go-target` is an exact `X.Y.Z` version. Each repeatable `--k8s-target` is an exact `module=version` pair using Go module version syntax, such as `k8s.io/api=v0.36.6`. Go-only and both modes require a Go target; Kubernetes-only and both modes require at least one Kubernetes target. Targets in pinned dimensions and extra positional arguments are rejected.

The Kubernetes map is the release-alignment policy: list every intended module/version pair. There is no automatic “same version for every `k8s.io/*` module” rule. Unlisted baseline Kubernetes modules retain their individual selected versions. A newly introduced Kubernetes module is rejected unless explicitly listed. A target map entry absent from the baseline is applied only if a candidate introduces that module; if it never appears, the report marks the target blocked.

`pin-both` pins only the Go and Kubernetes dimensions. Other eligible modules are still considered for stable, newer versions.

## What the updater does

1. Requires this repository and a clean Git working tree, then records `go.mod`'s Go directive/toolchain and inventories `go list -m -json all`.
2. Applies requested Go and Kubernetes targets as one transaction and checks the full graph before any individual dependency trials.
3. Processes module paths in lexical order. Only modules explicitly required in the current root `go.mod` receive individual candidate trials; graph-only transitive modules are skipped. For each eligible module it lists versions newest-first, skipping prereleases and downgrades. It requests a specific version with `go get module@version`, never `go get -u`. If that release declares a renamed module path, the updater can migrate root-module imports transactionally (see below).
4. Runs `go mod tidy`, reloads the selected graph, and verifies the candidate's actual selected version plus every Go/toolchain/Kubernetes exact pin. Accepted trials include graph-wide additions, removals, and version changes.
5. Refreshes the inventory after accepted trials and processes newly discovered modules. A fixed pass bound and cycle detection prevent unbounded runs.
6. Normalizes `go.mod` to exactly two `require` blocks (direct first, indirect second), rechecks the selected graph and exact pins, then runs `go mod vendor` before accepting the complete transaction.

Progress is written to stderr as the updater runs. Target setup shows the baseline Go/Kubernetes version and each requested target. Each candidate shows the selected version and target (`module: v1.2.0 -> v1.3.0`), followed by an accepted or rejection reason. The final per-module summary shows the initial-to-final version or says `No update` with the disposition, such as `policy-pinned`, `already-current`, or `blocked-by-constraints`. The JSON report remains separate and stable for Prow consumers.

The algorithm is deterministic and sequential, but it does not find a globally optimal dependency-version combination. A module update can be rejected because of Go requirements, exact pins, exclusions, or graph-wide constraints; older candidates are then considered. A retrieval or authentication failure is reported as an execution failure, not as proof that a version is incompatible.

Each eligible module has a maximum of **20 candidate trials across the entire run**, including repeat passes. These are different candidate versions, not retries of the same download. Modules selected transitively can appear in `go list -m all` without an explicit requirement in the root `go.mod`; they are inventoried and policy-checked but receive no individual update trials. They may still change transitively when an eligible dependency is updated. If the limit leaves candidates unchecked, the updater continues accounting for other modules, records `candidateLimitReached: true` for the capped module, then reports an incomplete run (exit `3`) and rolls back the run's dependency changes. It does not claim that no compatible version exists beyond the limit.

## Go compiler and directive rules

The compiler used for module commands must match the pinned baseline compiler (the baseline `toolchain` directive when present, otherwise the Go directive) or the requested Go target. Use `--go-bin /path/to/go` to select the compiler. Module commands run with `GOTOOLCHAIN=local`, so Go cannot silently switch compilers to satisfy a candidate.

- When Go is pinned, the exact baseline Go directive and toolchain presence/value are preserved.
- When Go is updated, the directive becomes the requested exact target. If a toolchain directive existed, its value becomes `go<target>`; if it was absent, it stays absent.
- Tidy or a dependency cannot silently raise the Go target; an incompatible candidate is rolled back.

## Transaction and interruption behavior

Each rejected candidate is restored to the last accepted checkpoint before the next candidate. The whole run remains transactional until exact validation and vendoring succeed. A failed retrieval, incompatible target set, final validation failure, pass-bound/cycle result, or handled interrupt restores the original `go.mod`, `go.sum`, `vendor`, and any Go source files rewritten by a module-path migration; the report remains in the workspace. Accepted trial details remain in the report even when the overall transaction is rolled back. A forced process termination such as `SIGKILL` cannot run cleanup.

The updater never commits or pushes. For Prow use, configure the periodic job to provide the explicit target flags, run this command, and only on exit code `0` publish the updated workspace and report as the job's single pull request. Scheduling, credentials, branch naming, and pull-request create/update are owned by the external Prow configuration.

After the final tidy, the updater keeps exactly two `require` blocks, including an empty block when one class has no requirements. Module paths are sorted within each block; the normalization preserves requirement versions, direct/indirect classification, comments, and other `go.mod` directives. It then reloads the graph and validates exact pins before vendoring.

## Renamed module paths

For an error such as `github.com/google/cel-go@v0.32.0` declaring `cel.dev/cel-go`, the updater verifies the declaration under the new path and requests **the same exact candidate version** (`cel.dev/cel-go@v0.32.0`). It rewrites matching import paths in root-module `.go` files, including test files, and replaces the old root requirement before tidy and full-graph pin validation. Import aliases, comments, and non-import strings are preserved. Imports owned by separately selected nested modules are left alone; vendor, hidden directories, testdata, and nested Go modules are excluded from source rewriting.

A rename is accepted only if the selected new module version is exact, all policies still hold, and `go build -mod=readonly ./...` succeeds for the current platform/build configuration. Known compiler/API incompatibilities reject the candidate and restore its source and module edits before trying an older candidate. Retrieval or environment failures still abort the run. This build does not execute tests or validate other build configurations.

Protected modules cannot be migrated automatically. A rename with no matching root imports is blocked because upstream/transitive consumers must migrate first. An existing new-path module at a different version is not silently overridden. If other dependencies still require the old path, it may remain in the selected graph as a transitive dependency; migrating root imports does not rewrite upstream modules. Reporting includes `migratedTo` on the original module and `targetPath`/`sourceFiles` on migration attempts.

## Report

The default report is `_output/dependency-upgrade-report.json`; override it with `--report PATH`. It is atomically written JSON and includes:

- policy inputs and baseline/final Go directive, toolchain, and selected Kubernetes module versions;
- overall `completed`, `incomplete`, or `failed` status, plus run-level failure and rollback state;
- a path-sorted result for every initially or subsequently discovered module, with initial/final version, direct/indirect classification, attempted candidates, outcome, rejection evidence, and accepted transitive changes.

Module outcomes are `updated`, `already-current`, `policy-pinned`, `blocked-by-constraints`, `removed`, `execution-failure`, and `not-in-go-mod` (an unchanged graph-only module skipped for candidate trials). Snapshot entries include `required` to distinguish explicit `go.mod` requirements from graph-only modules. Excluded versions omitted by Go's version listing are not attempts; if the resolver returns an excluded version and rejects it, that rejection is recorded. Credential-like URL/query values are redacted from error evidence.

## Supported and unsupported module states

- **Exclusions:** preserved and enforced by Go's resolver.
- **Pseudo-versions:** allowed as existing selected versions; only newer stable tagged versions are candidates.
- **Local or remote replacements:** unsupported; fail before mutation with the replacement directive identified.
- **Active Go workspace (`go.work`):** unsupported; fail clearly rather than silently changing workspace semantics.
- **Module-path renames:** supported only when an explicit candidate declares the new path; root imports are rewritten and validated transactionally. Major-version/import-path migrations are not guessed and versions are not downgraded automatically.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Completed constrained run; individual candidates may be blocked or already current. |
| `1` | Invalid invocation, wrong repository, dirty worktree, active workspace, or unsupported replacement state. |
| `2` | Retrieval/authentication, execution, incompatible target transaction, report writing, or final validation failure. |
| `3` | Incomplete run because the candidate/pass bound was reached or a graph cycle was detected. |

The previous positional OCP release workflow (for example, `upgrade.sh 4.21 1.24 1.34.1`) and its image/tool updates, commits, and post-upgrade steps have been retired from this entry point. Prow and release-image/tool maintenance are outside this dependency updater.
