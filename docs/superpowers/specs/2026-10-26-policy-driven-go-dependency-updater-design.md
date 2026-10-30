# Policy-driven Go dependency updater design

## Intent

Replace the current interactive OCP-wide `hack/upgrade-automation/scripts/upgrade.sh` workflow with a deterministic updater for the selected Go module graph. Maintainers can run it manually; the external Prow periodic job can invoke the same command. The updater changes dependency files and writes a machine-readable report in the workspace. Prow branch publishing and pull-request creation are outside this change.

## Goals

- Offer four independent policies: pin Go and Kubernetes; update Go only; update Kubernetes only; update both.
- Require exact, explicit Go and Kubernetes targets when those dimensions are updated. Kubernetes targets are an exact module-path-to-version map.
- Preserve baseline Go directive/toolchain policy and every selected `k8s.io/*` module version whenever pinned.
- Try stable, newer candidates one module at a time only for explicit root `go.mod` requirements, including indirect requirements, while inventorying and validating the full selected graph. Record a disposition for every discovered dependency; unchanged graph-only dependencies are `not-in-go-mod`.
- Roll back rejected trials, validate the selected graph and exact pins after tidy, and report accepted transitive changes.
- Produce deterministic JSON reporting and deterministic fixture-based tests using a local Go module proxy.
- Emit concise progress on stderr with candidate source/target versions, decisions, and final per-module outcomes.
- Finish with exactly two `go.mod` `require` blocks: direct requirements first and indirect requirements second, retaining an empty block when a class has no requirements.
- Keep scheduled-job policy sourcing, scheduling, branch pushes, and PR create/update outside the updater.

## Non-goals

- Updating container images, tool versions, OCP release versions, generated manifests, or application APIs. Root-module import paths may be rewritten transactionally when a candidate explicitly declares a renamed module path.
- Selecting Go/Kubernetes targets or discovering a Kubernetes release family automatically.
- Creating branches, commits, or pull requests.
- Finding a globally optimal combination of dependency versions. The sequential result is constrained by the chosen candidate order.

## Repository integration

- Replace the existing `upgrade.sh` entry point with the new CLI wrapper and intentionally retire its old positional OCP/Go/Kubernetes arguments and multi-commit workflow.
- Rewrite `hack/upgrade-automation/README.md` with the four mode examples, explicit target syntax, report/outcome descriptions, rollback guarantees, limitations, and exit codes.
- Remove old helper scripts only if no remaining code or documentation references them after the replacement.
- Do not add or modify a Prow periodic job. The Prow job consumes the explicit inputs, runs the updater, and publishes the resulting branch/report in its own configuration.
- The updater requires a clean Git working tree before it starts. Generated report defaults to `_output/dependency-upgrade-report.json` (already ignored), so repeat runs do not dirty the source tree by themselves.

## CLI contract

The `upgrade.sh` path delegates to a Go implementation in `hack/upgrade-automation`. Example interface:

```text
upgrade.sh --mode pin-both
upgrade.sh --mode go-only --go-target 1.26.8
upgrade.sh --mode kubernetes-only --k8s-target k8s.io/api=v0.36.6 --k8s-target k8s.io/apimachinery=v0.36.6
upgrade.sh --mode both --go-target 1.26.8 --k8s-target k8s.io/api=v0.36.6
```

`--go-target` requires an exact patch version. `--k8s-target` is repeatable; each value is an exact `module=version` pair. Go-only and both modes require a Go target; Kubernetes-only and both modes require at least one Kubernetes target. Targets are never inferred. `--go-bin` optionally selects the compiler executable for both the shell launcher's `go run` and the updater's module commands; otherwise `go` from `PATH` is used. The selected compiler must be able to build the updater under the repository's current `go.mod` and must match the exact compiler version required by the active policy. The launcher sets `GOTOOLCHAIN=local` before invoking that compiler.

The command runs from the repository root, rejects a dirty worktree, and defaults report output to `_output/dependency-upgrade-report.json`. A custom `--report` path is supported. It does not commit or publish changes.

## Policy and baseline

Before edits, capture `go.mod`'s exact `go` directive, whether a `toolchain` directive exists and its exact value, each selected `k8s.io/*` module/version from `go list -m -json all`, direct/indirect classification for every selected module, and the initial selected graph.

- **Pin Go:** retain the exact baseline `go` directive and exact baseline toolchain presence/value.
- **Update Go:** set the `go` directive to the requested exact target. If the baseline had a `toolchain` directive, set it to `go<target>`; if absent, keep it absent. This preserves directive presence while changing its value only when one existed.
- **Pin Kubernetes:** individually retain each baseline `k8s.io/*` selected version. Do not assume that all modules share a version.
- **Update Kubernetes:** apply only the explicitly listed module-to-version targets. All unlisted baseline Kubernetes modules stay individually pinned. A newly selected `k8s.io/*` module is rejected unless the target map explicitly names it. Target-map entries for modules not in the baseline are allowed only if a trial introduces that module.
- In all modes, choose a controlled compiler matching the pinned baseline compiler (baseline toolchain when present, otherwise baseline Go directive) or requested Go target. Set `GOTOOLCHAIN=local` for module commands and verify `go version`; automatic compiler switching cannot mask incompatibility.

Apply the requested Go directive and all explicit Kubernetes module/version pairs in one target-setting transaction (one multi-module `go get` request), run tidy, reload the graph, and validate exact target/pin rules before dependency trials. If targets cannot coexist, restore the baseline and report the evidence; never relax either target.

## Candidate engine

Separate policy evaluation, candidate selection, trial execution, and reporting into independently testable Go components.

1. Require a clean worktree, capture baseline `go.mod`/`go.sum`, and inventory `go list -m -json all`.
2. Sort module paths lexically. Skip individual candidate trials for modules not explicitly required in the current root `go.mod`. For each eligible module, request available versions from Go tooling, order newest first, and consider only stable versions newer than current. Do not guess module-path or major-version migrations or downgrade. If an explicit candidate fails because it declares a renamed module path, verify that declaration under the new path, rewrite matching root-module imports, and try the same exact version there within the candidate transaction. Protected module paths cannot be migrated; existing differently versioned new-path requirements cannot be silently overridden.
3. Apply each candidate from the last accepted checkpoint using `go get <module>@<exact-version>` without `-u`, then `go mod tidy`.
4. Reload the entire graph and verify the requested module (or verified renamed path) selected the candidate, Go/toolchain policy is exact, all protected Kubernetes versions are exact, and no unapproved new Kubernetes module appeared. Migration trials must additionally pass a readonly module-mode build of root packages for the current platform/build configuration. Known compiler/API incompatibilities reject the migration; retrieval/environment errors remain fatal execution failures. Tests are not executed by the migration build.
5. Compare the complete selected graph to the checkpoint. Record all transitive additions, removals, and version changes for each accepted trial.
6. On a policy/constraint rejection, restore `go.mod`, `go.sum`, and any rewritten Go sources before considering the next candidate. Record rejection evidence and migration source/target paths and affected files. Retrieval/authentication errors are execution failures, never evidence of incompatibility; abort and roll back the run rather than treating the failed lookup as “no compatible version.”
7. Refresh inventory after accepted trials. Newly discovered modules enter deterministic processing; removed modules remain in the report. Track considered module/version pairs, detect graph cycles, and stop at a fixed bounded number of passes. Limit each module to 20 candidate trials across all passes; if older candidates remain unchecked, mark its `candidateLimitReached` flag and continue processing other modules before ending the run as incomplete. A cycle or bound hit is `incomplete`, not success, and restores the baseline dependency artifacts.
8. After the last tidy, normalize `go.mod` into exactly two `require` blocks (direct then indirect), preserving requirement versions, classifications, comments, and non-require directives. Reload the graph and verify it is unchanged and policy-compliant, then regenerate `vendor` with the repository's normal Go module command. Preserve/restore the original dependency artifacts if normalization, final validation, vendoring, a fatal execution error, an interruption, or the pass bound/cycle check fails.

This is a sequential constrained updater, not a global optimizer. The candidate set/order and each accepted transitive change are part of the report.

## Module edge cases

- **Direct/indirect modules:** derive classification from `go.mod` requirements and account for every non-main module in the selected graph.
- **Kubernetes version families:** only the explicit module-to-version map defines coordinated changes; no blanket version assignment across `k8s.io/*`.
- **Exclusions:** preserve them and let Go's module resolver enforce them; record excluded/rejected candidate evidence where available.
- **Pseudo-versions:** allow an existing pseudo-version as baseline and use Go module version ordering; only strictly newer stable tagged candidates are attempted.
- **Replacements:** fail before mutation with a clear unsupported-case error for both local and remote `replace` directives.
- **Workspace mode:** fail clearly if an active `go.work`/workspace is detected; do not silently ignore it.
- **Unsupported syntax or graph states:** fail before mutation and report the exact directive/state that prevents complete accounting.

## Transaction and interruption behavior

Each candidate starts from the last accepted checkpoint. Rejected trials restore `go.mod` and `go.sum`; accepted checkpoints remain temporary until the entire run, final validation, and vendoring succeed. Any fatal execution error, incomplete bound/cycle, final validation failure, or interruption restores the original `go.mod`, `go.sum`, and `vendor`, while preserving the report. Signal handling restores the current trial and removes temporary files; forced process termination cannot be guaranteed cleanup and is documented. An incompatible initial Go/Kubernetes target combination restores the original dependency files completely. Prow must publish only a run that exits successfully.

Publish the completed report atomically before committing the dependency transaction and deleting its backup. A report-publication failure rolls back dependency artifacts and rewritten sources and returns exit code `2`. The CLI attempts to publish the resulting failure report after rollback; if the destination remains unwritable, it reports the error on stderr.

## Report

Write stable JSON to the requested path. Include policy inputs; baseline/final Go directive and toolchain; baseline/final Kubernetes map; overall status (`completed`, `incomplete`, `failed`); and a path-sorted module list containing initial/final version, direct/indirect state, attempts, outcome, rejection/failure evidence, and accepted trial transitive changes. Emit progress to stderr while running, including each candidate's selected-to-requested version and a final summary that explicitly states when a module was not updated and why.

Every initial or newly discovered module receives one final disposition: `updated`, `already-current`, `policy-pinned`, `blocked-by-constraints`, `removed`, `execution-failure`, or `not-in-go-mod`. Include both initial and final direct/indirect classification and explicit-requirement membership in snapshots. If the run transaction rolls back, the final versions match baseline and attempted/accepted-then-rolled-back trials remain visible in the attempts section. The report distinguishes an exhausted constrained search from an incomplete bounded/cyclic run and failed validation. No “optimal” claim is made.

Exit codes: `0` completed constrained run; `1` invalid invocation, dirty tree, or unsupported repository state; `2` failed retrieval/execution/final validation; `3` incomplete because of pass bound or cycle. Once a valid invocation starts, write the report atomically on every terminal path.

## Verification

- Unit-test policy evaluation, candidate ordering/filtering, graph diffs, reporting order, and target validation.
- Use deterministic fixture modules served by a local file proxy; never rely on the latest upstream release.
- Cover all four modes, exact patch enforcement, raised Go requirements, transitive Kubernetes changes, latest-rejected/older-accepted, no compatible upgrade, direct/indirect/new/removed modules, independently versioned Kubernetes modules, coordinated exact target maps, incompatible target combinations, local/remote replacements, exclusions, pseudo-versions, workspace rejection, retrieval/auth failures, rollback after failed get/tidy, signal cleanup, bounded passes, cycle detection, and deterministic report output.
- Run the updater against a clean repository worktree in `pin-both` mode, verify exact Go/Kubernetes invariants and complete module accounting, and keep that dependency-refresh diff separate from implementation changes.
- Re-run against the same frozen candidate set for idempotence. Run repository formatting, lint, build, tests, vendor/generated targets, and applicable CI checks; report unavailable checks explicitly.

## Builder review checklist

- Does target setup happen atomically before any dependency trial?
- Can every trial restore the previous accepted state without touching unrelated user files?
- Are modules added by accepted transitive changes later processed and reported?
- Do compiler selection and `GOTOOLCHAIN=local` prevent implicit toolchain upgrades?
- Are unsupported graph features detected before mutation and described in the report?
- Does the wrapper/documentation make the retired OCP arguments and external Prow publishing boundary explicit?
