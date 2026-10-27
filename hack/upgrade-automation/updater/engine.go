package updater

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

type Config struct {
	Root      string
	Policy    Policy
	GoBinary  string
	MaxPasses int
	Progress  io.Writer
}

const defaultMaxPasses = 5
const maxCandidateAttempts = 20

const (
	runCompleted  = "completed"
	runIncomplete = "incomplete"
	runFailed     = "failed"

	moduleUpdated          = "updated"
	moduleAlreadyCurrent   = "already-current"
	modulePolicyPinned     = "policy-pinned"
	moduleBlocked          = "blocked-by-constraints"
	moduleRemoved          = "removed"
	moduleExecutionFailure = "execution-failure"

	attemptAccepted = "accepted"
)

func ApplyTargets(ctx context.Context, runner GoRunner, baseline Snapshot, policy Policy, transaction *Transaction) (Snapshot, error) {
	if err := runner.CheckCompiler(ctx, compilerTarget(baseline, policy)); err != nil {
		return Snapshot{}, err
	}
	checkpoint, err := transaction.Checkpoint()
	if err != nil {
		return Snapshot{}, err
	}
	rollback := func(cause error) (Snapshot, error) {
		if restoreErr := transaction.RestoreCheckpoint(checkpoint); restoreErr != nil {
			return Snapshot{}, errors.Join(cause, fmt.Errorf("restore target checkpoint: %w", restoreErr))
		}
		return Snapshot{}, cause
	}

	if policy.Mode == ModeGoOnly || policy.Mode == ModeBoth {
		if _, err := runner.Run(ctx, "mod", "edit", "-go="+policy.GoTarget); err != nil {
			return rollback(fmt.Errorf("set Go target: %w", err))
		}
		if baseline.Toolchain != "" {
			if _, err := runner.Run(ctx, "mod", "edit", "-toolchain=go"+policy.GoTarget); err != nil {
				return rollback(fmt.Errorf("set Go toolchain target: %w", err))
			}
		}
	}
	if len(policy.KubernetesTargets) != 0 {
		paths := make([]string, 0, len(policy.KubernetesTargets))
		for modulePath := range policy.KubernetesTargets {
			if _, selected := baseline.Modules[modulePath]; selected {
				paths = append(paths, modulePath)
			}
		}
		sort.Strings(paths)
		if len(paths) != 0 {
			args := []string{"get"}
			for _, modulePath := range paths {
				args = append(args, modulePath+"@"+policy.KubernetesTargets[modulePath])
			}
			if _, err := runner.Run(ctx, args...); err != nil {
				return rollback(fmt.Errorf("apply exact Kubernetes targets: %w", err))
			}
		}
	}
	if _, err := runner.Run(ctx, "mod", "tidy"); err != nil {
		return rollback(fmt.Errorf("tidy target dependency graph: %w", err))
	}
	wantToolchain := baseline.Toolchain
	if (policy.Mode == ModeGoOnly || policy.Mode == ModeBoth) && wantToolchain != "" {
		wantToolchain = "go" + policy.GoTarget
	}
	if err := SetToolchainDirective(ctx, runner, wantToolchain); err != nil {
		return rollback(fmt.Errorf("enforce toolchain policy: %w", err))
	}
	after, err := CaptureSnapshot(ctx, runner.Root, runner.Binary)
	if err != nil {
		return rollback(fmt.Errorf("reload graph after target setup: %w", err))
	}
	if violations := ValidatePolicy(baseline, after, policy); len(violations) != 0 {
		return rollback(fmt.Errorf("requested Go/Kubernetes targets cannot coexist: %w", errors.Join(violations...)))
	}
	return after, nil
}

func compilerTarget(baseline Snapshot, policy Policy) string {
	if policy.Mode == ModeGoOnly || policy.Mode == ModeBoth {
		return policy.GoTarget
	}
	if baseline.Toolchain != "" {
		return baseline.Toolchain
	}
	return baseline.GoDirective
}

func Run(ctx context.Context, cfg Config) (report Report, runErr error) {
	root := cfg.Root
	if root == "" {
		root = "."
	}
	runner := GoRunner{Root: root, Binary: cfg.GoBinary}
	report = Report{Status: runFailed, Policy: cfg.Policy, Modules: []ModuleResult{}}
	transaction, err := BeginTransaction(root)
	if err != nil {
		return report, err
	}
	baseline := Snapshot{}
	results := make(map[string]*ModuleResult)
	committed := false
	defer func() {
		if committed {
			return
		}
		report.RolledBack = true
		if err := transaction.Rollback(); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("rollback dependency update: %w", err))
			report.Status = runFailed
		}
		if runErr != nil {
			report.Failure = sanitizeEvidence(runErr.Error())
		}
		if baseline.Modules != nil {
			report.Final = baseline
			finalizeResults(results, baseline, cfg.Policy, true)
			report.Modules = sortedResults(results)
			emitRunSummary(cfg.Progress, report)
		}
		if runErr != nil {
			var coded *UpdaterError
			if !errors.As(runErr, &coded) {
				code := 2
				if report.Status == runIncomplete {
					code = 3
				} else {
					var precondition *PreconditionError
					if errors.As(runErr, &precondition) {
						code = 1
					}
				}
				runErr = &UpdaterError{Code: code, Err: runErr}
			}
		}
	}()

	baseline, err = CaptureSnapshot(ctx, root, cfg.GoBinary)
	if err != nil {
		return report, fmt.Errorf("capture baseline: %w", err)
	}
	progressf(cfg.Progress, "Inventory: %d modules; Go directive %s; toolchain %s", dependencyCount(baseline), baseline.GoDirective, toolchainLabel(baseline.Toolchain))
	emitRequestedTargets(cfg.Progress, baseline, cfg.Policy)
	report.Baseline = baseline
	report.Final = baseline
	observeSnapshot(results, baseline, baseline)
	current, err := ApplyTargets(ctx, runner, baseline, cfg.Policy, transaction)
	if err != nil {
		return report, fmt.Errorf("apply target policy: %w", err)
	}
	observeSnapshot(results, current, baseline)
	targetChanges := graphChanges(baseline, current)
	for _, modulePath := range sortedTargetPaths(cfg.Policy.KubernetesTargets) {
		targetVersion := cfg.Policy.KubernetesTargets[modulePath]
		selected, exists := current.Modules[modulePath]
		if !exists {
			continue
		}
		before := baseline.Modules[modulePath]
		if !baseline.Modules[modulePath].Main && before.Version != targetVersion {
			result := resultFor(results, modulePath, baseline)
			result.Attempts = append(result.Attempts, Attempt{Version: targetVersion, Outcome: attemptAccepted, Evidence: "explicit Kubernetes target", Changes: targetChanges})
		}
		if selected.Version != targetVersion {
			return report, fmt.Errorf("Kubernetes module %q selected %q after target setup, want %q", modulePath, selected.Version, targetVersion)
		}
	}

	maxPasses := cfg.MaxPasses
	if maxPasses <= 0 {
		maxPasses = defaultMaxPasses
	}
	tracker := graphTracker{}
	tracker.Seen(current)
	considered := make(map[string]struct{})
	candidateAttempts := make(map[string]int)
	limitedModules := make(map[string]struct{})
	completed := false
	for pass := 0; pass < maxPasses; pass++ {
		progressf(cfg.Progress, "Pass %d/%d: processing %d modules", pass+1, maxPasses, dependencyCount(current))
		changedThisPass := false
		for _, modulePath := range sortedModulePaths(current) {
			module, selected := current.Modules[modulePath]
			if !selected || module.Main {
				continue
			}
			if isProtectedModule(modulePath, cfg.Policy) {
				progressf(cfg.Progress, "Pinned %s at %s (policy-pinned)", modulePath, versionLabel(module.Version))
				continue
			}
			progressf(cfg.Progress, "Checking %s at %s", modulePath, versionLabel(module.Version))
			candidates, err := ListCandidates(ctx, runner, modulePath, module.Version)
			if err != nil {
				result := resultFor(results, modulePath, baseline)
				result.Outcome = moduleExecutionFailure
				return report, fmt.Errorf("enumerate candidates for %s: %w", modulePath, err)
			}
			if len(candidates) == 0 {
				progressf(cfg.Progress, "No newer stable candidate for %s at %s", modulePath, versionLabel(module.Version))
			}
			accepted := false
			for _, candidate := range candidates {
				consideredKey := modulePath + "@" + candidate
				if _, seen := considered[consideredKey]; seen {
					continue
				}
				if candidateAttempts[modulePath] >= maxCandidateAttempts {
					resultFor(results, modulePath, baseline).CandidateLimitReached = true
					if _, alreadyReported := limitedModules[modulePath]; !alreadyReported {
						progressf(cfg.Progress, "Candidate limit reached for %s: %d attempts; older candidates remain unchecked", modulePath, maxCandidateAttempts)
					}
					limitedModules[modulePath] = struct{}{}
					break
				}
				considered[consideredKey] = struct{}{}
				candidateAttempts[modulePath]++
				progressf(cfg.Progress, "Trying %s: %s -> %s", modulePath, versionLabel(module.Version), candidate)
				checkpoint, err := transaction.Checkpoint()
				if err != nil {
					return report, fmt.Errorf("checkpoint %s@%s: %w", modulePath, candidate, err)
				}
				beforeTrial := current
				if _, err := runner.Run(ctx, "get", modulePath+"@"+candidate); err != nil {
					if restoreErr := transaction.RestoreCheckpoint(checkpoint); restoreErr != nil {
						return report, errors.Join(fmt.Errorf("restore rejected candidate %s@%s: %w", modulePath, candidate, err), restoreErr)
					}
					if isConstraintFailure(err) {
						result := resultFor(results, modulePath, baseline)
						result.Attempts = append(result.Attempts, Attempt{Version: candidate, Outcome: moduleBlocked, Evidence: err.Error()})
						progressf(cfg.Progress, "Rejected %s: %s -> %s (blocked-by-constraints: %s)", modulePath, versionLabel(module.Version), candidate, err.Error())
						continue
					}
					result := resultFor(results, modulePath, baseline)
					result.Attempts = append(result.Attempts, Attempt{Version: candidate, Outcome: moduleExecutionFailure, Evidence: err.Error()})
					result.Outcome = moduleExecutionFailure
					progressf(cfg.Progress, "Execution failure for %s: %s -> %s (%s)", modulePath, versionLabel(module.Version), candidate, err.Error())
					return report, fmt.Errorf("execute candidate %s@%s: %w", modulePath, candidate, err)
				}
				trial, err := CaptureSnapshot(ctx, root, cfg.GoBinary)
				if err != nil {
					if restoreErr := transaction.RestoreCheckpoint(checkpoint); restoreErr != nil {
						return report, errors.Join(fmt.Errorf("reload graph after %s@%s: %w", modulePath, candidate, err), restoreErr)
					}
					if isConstraintFailure(err) {
						result := resultFor(results, modulePath, baseline)
						result.Attempts = append(result.Attempts, Attempt{Version: candidate, Outcome: moduleBlocked, Evidence: err.Error()})
						progressf(cfg.Progress, "Rejected %s: %s -> %s (blocked-by-constraints: %s)", modulePath, versionLabel(module.Version), candidate, err.Error())
						continue
					}
					result := resultFor(results, modulePath, baseline)
					result.Attempts = append(result.Attempts, Attempt{Version: candidate, Outcome: moduleExecutionFailure, Evidence: err.Error()})
					result.Outcome = moduleExecutionFailure
					progressf(cfg.Progress, "Execution failure for %s: %s -> %s (%s)", modulePath, versionLabel(module.Version), candidate, err.Error())
					return report, fmt.Errorf("reload graph after %s@%s: %w", modulePath, candidate, err)
				}
				if err := applySelectedTargets(ctx, runner, cfg.Policy, trial); err != nil {
					if restoreErr := transaction.RestoreCheckpoint(checkpoint); restoreErr != nil {
						return report, errors.Join(fmt.Errorf("apply explicit target after %s@%s: %w", modulePath, candidate, err), restoreErr)
					}
					if isConstraintFailure(err) {
						result := resultFor(results, modulePath, baseline)
						result.Attempts = append(result.Attempts, Attempt{Version: candidate, Outcome: moduleBlocked, Evidence: err.Error()})
						progressf(cfg.Progress, "Rejected %s: %s -> %s (blocked-by-constraints: %s)", modulePath, versionLabel(module.Version), candidate, err.Error())
						continue
					}
					result := resultFor(results, modulePath, baseline)
					result.Attempts = append(result.Attempts, Attempt{Version: candidate, Outcome: moduleExecutionFailure, Evidence: err.Error()})
					result.Outcome = moduleExecutionFailure
					progressf(cfg.Progress, "Execution failure for %s: %s -> %s (%s)", modulePath, versionLabel(module.Version), candidate, err.Error())
					return report, fmt.Errorf("apply explicit target after %s@%s: %w", modulePath, candidate, err)
				}
				if _, err := runner.Run(ctx, "mod", "tidy"); err != nil {
					if restoreErr := transaction.RestoreCheckpoint(checkpoint); restoreErr != nil {
						return report, errors.Join(fmt.Errorf("restore failed tidy for %s@%s: %w", modulePath, candidate, err), restoreErr)
					}
					if isConstraintFailure(err) {
						result := resultFor(results, modulePath, baseline)
						result.Attempts = append(result.Attempts, Attempt{Version: candidate, Outcome: moduleBlocked, Evidence: err.Error()})
						progressf(cfg.Progress, "Rejected %s: %s -> %s (blocked-by-constraints: %s)", modulePath, versionLabel(module.Version), candidate, err.Error())
						continue
					}
					result := resultFor(results, modulePath, baseline)
					result.Attempts = append(result.Attempts, Attempt{Version: candidate, Outcome: moduleExecutionFailure, Evidence: err.Error()})
					result.Outcome = moduleExecutionFailure
					progressf(cfg.Progress, "Execution failure for %s: %s -> %s (%s)", modulePath, versionLabel(module.Version), candidate, err.Error())
					return report, fmt.Errorf("tidy candidate %s@%s: %w", modulePath, candidate, err)
				}
				if err := SetToolchainDirective(ctx, runner, expectedToolchain(baseline, cfg.Policy)); err != nil {
					if restoreErr := transaction.RestoreCheckpoint(checkpoint); restoreErr != nil {
						return report, errors.Join(fmt.Errorf("restore toolchain update for %s@%s: %w", modulePath, candidate, err), restoreErr)
					}
					result := resultFor(results, modulePath, baseline)
					result.Attempts = append(result.Attempts, Attempt{Version: candidate, Outcome: moduleExecutionFailure, Evidence: err.Error()})
					result.Outcome = moduleExecutionFailure
					progressf(cfg.Progress, "Execution failure enforcing toolchain for %s: %s", modulePath, err.Error())
					return report, fmt.Errorf("enforce toolchain after %s@%s: %w", modulePath, candidate, err)
				}
				after, err := CaptureSnapshot(ctx, root, cfg.GoBinary)
				if err != nil {
					if restoreErr := transaction.RestoreCheckpoint(checkpoint); restoreErr != nil {
						return report, errors.Join(fmt.Errorf("reload graph after tidy for %s@%s: %w", modulePath, candidate, err), restoreErr)
					}
					if isConstraintFailure(err) {
						result := resultFor(results, modulePath, baseline)
						result.Attempts = append(result.Attempts, Attempt{Version: candidate, Outcome: moduleBlocked, Evidence: err.Error()})
						progressf(cfg.Progress, "Rejected %s: %s -> %s (blocked-by-constraints: %s)", modulePath, versionLabel(module.Version), candidate, err.Error())
						continue
					}
					result := resultFor(results, modulePath, baseline)
					result.Attempts = append(result.Attempts, Attempt{Version: candidate, Outcome: moduleExecutionFailure, Evidence: err.Error()})
					result.Outcome = moduleExecutionFailure
					progressf(cfg.Progress, "Execution failure for %s: %s -> %s (%s)", modulePath, versionLabel(module.Version), candidate, err.Error())
					return report, fmt.Errorf("reload graph after tidy for %s@%s: %w", modulePath, candidate, err)
				}

				changes := graphChanges(beforeTrial, after)
				violations := ValidatePolicy(baseline, after, cfg.Policy)
				if selected, exists := after.Modules[modulePath]; !exists || selected.Version != candidate {
					violations = append(violations, fmt.Errorf("requested %s@%s selected %q", modulePath, candidate, selected.Version))
				}
				result := resultFor(results, modulePath, baseline)
				if len(violations) != 0 {
					evidence := joinErrors(violations)
					result.Attempts = append(result.Attempts, Attempt{Version: candidate, Outcome: moduleBlocked, Evidence: evidence, Changes: changes})
					recordRejectedGraph(results, baseline, beforeTrial, after, modulePath, evidence, changes)
					progressf(cfg.Progress, "Rejected %s: %s -> %s (blocked-by-constraints: %s)", modulePath, versionLabel(module.Version), candidate, evidence)
					if err := transaction.RestoreCheckpoint(checkpoint); err != nil {
						return report, fmt.Errorf("restore policy-rejected candidate %s@%s: %w", modulePath, candidate, err)
					}
					continue
				}
				result.Attempts = append(result.Attempts, Attempt{Version: candidate, Outcome: attemptAccepted, Changes: changes})
				for _, targetPath := range sortedTargetPaths(cfg.Policy.KubernetesTargets) {
					if targetPath == modulePath || beforeTrial.Modules[targetPath].Version == after.Modules[targetPath].Version {
						continue
					}
					targetResult := resultFor(results, targetPath, baseline)
					targetResult.Attempts = append(targetResult.Attempts, Attempt{Version: cfg.Policy.KubernetesTargets[targetPath], Outcome: attemptAccepted, Evidence: "explicit Kubernetes target introduced during candidate trial", Changes: changes})
				}
				progressf(cfg.Progress, "Accepted %s: %s -> %s (%d graph changes)", modulePath, versionLabel(module.Version), after.Modules[modulePath].Version, len(changes))
				observeSnapshot(results, after, baseline)
				current = after
				changedThisPass = true
				accepted = true
				break
			}
			if accepted {
				continue
			}
		}
		if !changedThisPass {
			completed = true
			progressf(cfg.Progress, "Pass %d complete: no candidates accepted", pass+1)
			break
		}
		if tracker.Seen(current) {
			report.Status = runIncomplete
			progressf(cfg.Progress, "Run incomplete: dependency graph cycle after pass %d", pass+1)
			return report, fmt.Errorf("dependency graph cycle detected after pass %d", pass+1)
		}
		if pass+1 == maxPasses {
			report.Status = runIncomplete
			progressf(cfg.Progress, "Run incomplete: reached pass limit %d", maxPasses)
			return report, fmt.Errorf("dependency update reached pass bound %d", maxPasses)
		}
	}
	if !completed {
		report.Status = runIncomplete
		return report, fmt.Errorf("dependency update did not converge within %d passes", maxPasses)
	}
	if len(limitedModules) != 0 {
		paths := make([]string, 0, len(limitedModules))
		for path := range limitedModules {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		report.Status = runIncomplete
		return report, fmt.Errorf("candidate limit of %d attempts per module reached with unchecked candidates: %s", maxCandidateAttempts, strings.Join(paths, ", "))
	}

	progressf(cfg.Progress, "Normalizing go.mod into direct and indirect require blocks")
	if err := NormalizeRequireBlocks(root); err != nil {
		return report, fmt.Errorf("normalize direct/indirect require blocks: %w", err)
	}
	normalized, err := CaptureSnapshot(ctx, root, cfg.GoBinary)
	if err != nil {
		return report, fmt.Errorf("reload graph after require-block normalization: %w", err)
	}
	if !sameSnapshot(current, normalized) {
		return report, fmt.Errorf("require-block normalization changed the selected module graph")
	}
	if violations := ValidatePolicy(baseline, normalized, cfg.Policy); len(violations) != 0 {
		return report, fmt.Errorf("policy validation after require-block normalization failed: %w", errors.Join(violations...))
	}
	current = normalized

	for _, modulePath := range sortedTargetPaths(cfg.Policy.KubernetesTargets) {
		if _, exists := current.Modules[modulePath]; !exists {
			result := resultFor(results, modulePath, baseline)
			result.Outcome = moduleBlocked
		}
	}
	if _, err := runner.Run(ctx, "mod", "vendor"); err != nil {
		return report, fmt.Errorf("vendor accepted module graph: %w", err)
	}
	final, err := CaptureSnapshot(ctx, root, cfg.GoBinary)
	if err != nil {
		return report, fmt.Errorf("capture final graph: %w", err)
	}
	if violations := ValidatePolicy(baseline, final, cfg.Policy); len(violations) != 0 {
		return report, fmt.Errorf("final exact-policy validation failed: %w", errors.Join(violations...))
	}
	if err := transaction.Commit(); err != nil {
		return report, fmt.Errorf("commit dependency transaction: %w", err)
	}
	committed = true
	report.Status = runCompleted
	report.Final = final
	finalizeResults(results, final, cfg.Policy, false)
	report.Modules = sortedResults(results)
	emitRunSummary(cfg.Progress, report)
	return report, nil
}

func applySelectedTargets(ctx context.Context, runner GoRunner, policy Policy, snapshot Snapshot) error {
	var paths []string
	for modulePath, version := range policy.KubernetesTargets {
		if selected, exists := snapshot.Modules[modulePath]; exists && selected.Version != version {
			paths = append(paths, modulePath)
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil
	}
	args := []string{"get"}
	for _, modulePath := range paths {
		args = append(args, modulePath+"@"+policy.KubernetesTargets[modulePath])
	}
	_, err := runner.Run(ctx, args...)
	return err
}

func expectedToolchain(baseline Snapshot, policy Policy) string {
	if (policy.Mode == ModeGoOnly || policy.Mode == ModeBoth) && baseline.Toolchain != "" {
		return "go" + policy.GoTarget
	}
	return baseline.Toolchain
}

func isProtectedModule(path string, policy Policy) bool {
	if isKubernetesModule(path) {
		return true
	}
	_, targeted := policy.KubernetesTargets[path]
	return targeted
}

func isConstraintFailure(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "requires go >=") || strings.Contains(text, "excluded by go.mod") || strings.Contains(text, "excluded by the main module")
}

func graphChanges(before, after Snapshot) []Change {
	paths := make(map[string]struct{}, len(before.Modules)+len(after.Modules))
	for path, module := range before.Modules {
		if !module.Main {
			paths[path] = struct{}{}
		}
	}
	for path, module := range after.Modules {
		if !module.Main {
			paths[path] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	changes := make([]Change, 0)
	for _, path := range ordered {
		oldVersion := before.Modules[path].Version
		newVersion := after.Modules[path].Version
		if oldVersion != newVersion {
			changes = append(changes, Change{Path: path, Before: oldVersion, After: newVersion})
		}
	}
	return changes
}

func graphSignature(snapshot Snapshot) string {
	paths := sortedModulePaths(snapshot)
	var signature strings.Builder
	fmt.Fprintf(&signature, "go=%s\ntoolchain=%s\n", snapshot.GoDirective, snapshot.Toolchain)
	for _, path := range paths {
		module := snapshot.Modules[path]
		if !module.Main {
			fmt.Fprintf(&signature, "%s@%s\n", path, module.Version)
		}
	}
	return signature.String()
}

func sameSnapshot(left, right Snapshot) bool {
	if left.GoDirective != right.GoDirective || left.Toolchain != right.Toolchain || len(left.Modules) != len(right.Modules) {
		return false
	}
	for path, module := range left.Modules {
		if right.Modules[path] != module {
			return false
		}
	}
	return true
}

type graphTracker struct {
	seen map[string]struct{}
}

func (t *graphTracker) Seen(snapshot Snapshot) bool {
	if t.seen == nil {
		t.seen = make(map[string]struct{})
	}
	signature := graphSignature(snapshot)
	if _, exists := t.seen[signature]; exists {
		return true
	}
	t.seen[signature] = struct{}{}
	return false
}

func sortedModulePaths(snapshot Snapshot) []string {
	paths := make([]string, 0, len(snapshot.Modules))
	for path := range snapshot.Modules {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func sortedTargetPaths(targets map[string]string) []string {
	paths := make([]string, 0, len(targets))
	for path := range targets {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func resultFor(results map[string]*ModuleResult, path string, baseline Snapshot) *ModuleResult {
	if result, exists := results[path]; exists {
		return result
	}
	result := &ModuleResult{Path: path}
	if module, exists := baseline.Modules[path]; exists && !module.Main {
		result.InitialVersion = module.Version
		result.InitialDirect = module.Direct
	}
	results[path] = result
	return result
}

func observeSnapshot(results map[string]*ModuleResult, snapshot, baseline Snapshot) {
	for path, module := range snapshot.Modules {
		if !module.Main {
			resultFor(results, path, baseline)
		}
	}
}

func recordRejectedGraph(results map[string]*ModuleResult, baseline, before, after Snapshot, candidatePath, evidence string, changes []Change) {
	for _, path := range sortedModulePaths(after) {
		module := after.Modules[path]
		if module.Main {
			continue
		}
		old, existed := before.Modules[path]
		if path == candidatePath || (existed && old.Version == module.Version) {
			continue
		}
		result := resultFor(results, path, baseline)
		result.Attempts = append(result.Attempts, Attempt{Version: module.Version, Outcome: moduleBlocked, Evidence: "selected by a rejected transitive trial: " + evidence, Changes: changes})
	}
}

func finalizeResults(results map[string]*ModuleResult, final Snapshot, policy Policy, rolledBack bool) {
	for path, result := range results {
		module, exists := final.Modules[path]
		if exists && !module.Main {
			result.FinalVersion = module.Version
			result.FinalDirect = module.Direct
		} else {
			result.FinalVersion = ""
			result.FinalDirect = false
		}
		if rolledBack && hasAttemptOutcome(result, moduleExecutionFailure) {
			result.Outcome = moduleExecutionFailure
			continue
		}
		if rolledBack && result.InitialVersion == result.FinalVersion && hasAttemptOutcome(result, attemptAccepted) {
			result.Outcome = moduleExecutionFailure
			continue
		}
		if target, explicitlyTargeted := policy.KubernetesTargets[path]; explicitlyTargeted {
			if !exists {
				result.Outcome = moduleBlocked
			} else if module.Version == target {
				if result.InitialVersion != target {
					result.Outcome = moduleUpdated
				} else {
					result.Outcome = moduleAlreadyCurrent
				}
			} else {
				result.Outcome = moduleBlocked
			}
			continue
		}
		if isKubernetesModule(path) {
			if result.InitialVersion == "" && result.FinalVersion == "" && hasAttemptOutcome(result, moduleBlocked) {
				result.Outcome = moduleBlocked
			} else {
				result.Outcome = modulePolicyPinned
			}
			continue
		}
		if result.InitialVersion != "" && result.FinalVersion == "" {
			result.Outcome = moduleRemoved
		} else if result.InitialVersion != result.FinalVersion {
			result.Outcome = moduleUpdated
		} else if hasAttemptOutcome(result, moduleExecutionFailure) {
			result.Outcome = moduleExecutionFailure
		} else if hasAttemptOutcome(result, moduleBlocked) {
			result.Outcome = moduleBlocked
		} else if result.Outcome == "" {
			result.Outcome = moduleAlreadyCurrent
		}
	}
}

func hasAttemptOutcome(result *ModuleResult, outcome string) bool {
	for _, attempt := range result.Attempts {
		if attempt.Outcome == outcome {
			return true
		}
	}
	return false
}

func sortedResults(results map[string]*ModuleResult) []ModuleResult {
	paths := make([]string, 0, len(results))
	for path := range results {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	ordered := make([]ModuleResult, 0, len(paths))
	for _, path := range paths {
		ordered = append(ordered, *results[path])
	}
	return ordered
}

func progressf(writer io.Writer, format string, args ...any) {
	if writer == nil {
		return
	}
	message := sanitizeEvidence(fmt.Sprintf(format, args...))
	message = strings.ReplaceAll(message, "\r", " ")
	message = strings.ReplaceAll(message, "\n", " ")
	_, _ = fmt.Fprintln(writer, message)
}

func dependencyCount(snapshot Snapshot) int {
	count := 0
	for _, module := range snapshot.Modules {
		if !module.Main {
			count++
		}
	}
	return count
}

func toolchainLabel(toolchain string) string {
	if toolchain == "" {
		return "absent"
	}
	return toolchain
}

func versionLabel(version string) string {
	if version == "" {
		return "<absent>"
	}
	return version
}

func emitModuleSummaries(writer io.Writer, modules []ModuleResult) {
	for _, module := range modules {
		switch module.Outcome {
		case moduleUpdated:
			if module.InitialVersion == "" {
				progressf(writer, "Added %s: %s -> %s", module.Path, versionLabel(module.InitialVersion), versionLabel(module.FinalVersion))
			} else {
				progressf(writer, "Updated %s: %s -> %s", module.Path, versionLabel(module.InitialVersion), versionLabel(module.FinalVersion))
			}
		case moduleRemoved:
			progressf(writer, "Removed %s: %s -> %s", module.Path, versionLabel(module.InitialVersion), versionLabel(module.FinalVersion))
		default:
			current := module.FinalVersion
			if current == "" {
				current = module.InitialVersion
			}
			if module.CandidateLimitReached {
				progressf(writer, "No update %s: remains at %s (%s; candidate limit reached after %d attempts)", module.Path, versionLabel(current), module.Outcome, maxCandidateAttempts)
			} else {
				progressf(writer, "No update %s: remains at %s (%s)", module.Path, versionLabel(current), module.Outcome)
			}
		}
	}
}

func emitRequestedTargets(writer io.Writer, baseline Snapshot, policy Policy) {
	if policy.Mode == ModeGoOnly || policy.Mode == ModeBoth {
		progressf(writer, "Go directive target: %s -> %s", versionLabel(baseline.GoDirective), policy.GoTarget)
	}
	for _, modulePath := range sortedTargetPaths(policy.KubernetesTargets) {
		current := "<absent>"
		if selected, exists := baseline.Modules[modulePath]; exists {
			current = selected.Version
		}
		progressf(writer, "Kubernetes target: %s %s -> %s", modulePath, current, policy.KubernetesTargets[modulePath])
	}
}

func emitRunSummary(writer io.Writer, report Report) {
	if report.Baseline.GoDirective == report.Final.GoDirective {
		progressf(writer, "Go directive unchanged at %s", versionLabel(report.Final.GoDirective))
	} else {
		progressf(writer, "Go directive: %s -> %s", versionLabel(report.Baseline.GoDirective), versionLabel(report.Final.GoDirective))
	}
	if report.Baseline.Toolchain == report.Final.Toolchain {
		progressf(writer, "Toolchain unchanged (%s)", toolchainLabel(report.Final.Toolchain))
	} else {
		progressf(writer, "Toolchain: %s -> %s", toolchainLabel(report.Baseline.Toolchain), toolchainLabel(report.Final.Toolchain))
	}
	emitModuleSummaries(writer, report.Modules)
}

func joinErrors(errs []error) string {
	parts := make([]string, 0, len(errs))
	for _, err := range errs {
		parts = append(parts, err.Error())
	}
	sort.Strings(parts)
	return strings.Join(parts, "; ")
}
