package updater

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type dependencyTrials struct {
	cfg           Config
	runner        GoRunner
	transaction   *Transaction
	baseline      Snapshot
	current       Snapshot
	results       map[string]*ModuleResult
	migrations    map[string]migrationTrial
	migratedPaths map[string]string
	considered    map[string]struct{}
	attempts      map[string]int
	limited       map[string]struct{}
}

func (s *dependencyTrials) run(ctx context.Context) error {
	maxPasses := s.cfg.MaxPasses
	if maxPasses <= 0 {
		maxPasses = defaultMaxPasses
	}
	s.considered = make(map[string]struct{})
	s.attempts = make(map[string]int)
	s.limited = make(map[string]struct{})
	tracker := graphTracker{}
	tracker.Seen(s.current)
	for pass := 0; pass < maxPasses; pass++ {
		progressf(
			s.cfg.Progress,
			"Pass %d/%d: processing %d modules",
			pass+1,
			maxPasses,
			dependencyCount(s.current),
		)
		changed := false
		for _, path := range sortedModulePaths(s.current) {
			accepted, err := s.updateModule(ctx, path)
			if err != nil {
				return err
			}
			changed = changed || accepted
		}
		if !changed {
			progressf(s.cfg.Progress, "Pass %d complete: no candidates accepted", pass+1)
			if len(s.limited) == 0 {
				return nil
			}
			paths := make([]string, 0, len(s.limited))
			for path := range s.limited {
				paths = append(paths, path)
			}
			sort.Strings(paths)
			return &UpdaterError{
				Code: 3,
				Err: fmt.Errorf(
					"candidate limit of %d attempts per module reached with unchecked candidates: %s",
					maxCandidateAttempts,
					strings.Join(paths, ", "),
				),
			}
		}
		if tracker.Seen(s.current) {
			progressf(
				s.cfg.Progress,
				"Run incomplete: dependency graph cycle after pass %d",
				pass+1,
			)
			return &UpdaterError{
				Code: 3,
				Err:  fmt.Errorf("dependency graph cycle detected after pass %d", pass+1),
			}
		}
	}
	progressf(s.cfg.Progress, "Run incomplete: reached pass limit %d", maxPasses)
	return &UpdaterError{
		Code: 3,
		Err:  fmt.Errorf("dependency update reached pass bound %d", maxPasses),
	}
}

func (s *dependencyTrials) updateModule(ctx context.Context, path string) (bool, error) {
	module, selected := s.current.Modules[path]
	if !selected || module.Main {
		return false, nil
	}
	if target, migrated := s.migratedPaths[path]; migrated {
		progressf(s.cfg.Progress, "Skipping %s: root imports already migrated to %s", path, target)
		return false, nil
	}
	if isProtectedModule(path, s.cfg.Policy) {
		progressf(
			s.cfg.Progress,
			"Pinned %s at %s (policy-pinned)",
			path,
			versionLabel(module.Version),
		)
		return false, nil
	}
	if !module.Required {
		progressf(
			s.cfg.Progress,
			"Skipping %s at %s (not-in-go-mod)",
			path,
			versionLabel(module.Version),
		)
		return false, nil
	}
	progressf(s.cfg.Progress, "Checking %s at %s", path, versionLabel(module.Version))
	candidates, err := ListCandidates(ctx, s.runner, path, module.Version)
	if err != nil {
		resultFor(s.results, path, s.baseline).Outcome = moduleExecutionFailure
		return false, fmt.Errorf("enumerate candidates for %s: %w", path, err)
	}
	if len(candidates) == 0 {
		progressf(
			s.cfg.Progress,
			"No newer stable candidate for %s at %s",
			path,
			versionLabel(module.Version),
		)
	}
	for _, candidate := range candidates {
		key := path + "@" + candidate
		if _, seen := s.considered[key]; seen {
			continue
		}
		if s.attempts[path] >= maxCandidateAttempts {
			resultFor(s.results, path, s.baseline).CandidateLimitReached = true
			if _, reported := s.limited[path]; !reported {
				progressf(
					s.cfg.Progress,
					"Candidate limit reached for %s: %d attempts; older candidates remain unchecked",
					path,
					maxCandidateAttempts,
				)
			}
			s.limited[path] = struct{}{}
			break
		}
		s.considered[key] = struct{}{}
		s.attempts[path]++
		accepted, err := s.tryCandidate(ctx, module, candidate)
		if err != nil || accepted {
			return accepted, err
		}
	}
	return false, nil
}

func (s *dependencyTrials) prepareCandidate(
	ctx context.Context,
	module Module,
	candidate string,
	checkpoint ModuleCheckpoint,
) (Snapshot, string, error) {
	path := module.Path
	target := path
	_, err := s.runner.Run(ctx, "get", path+"@"+candidate)
	if err != nil {
		if newPath := renamedModulePath(err, path); newPath != "" {
			if restoreErr := s.transaction.RestoreCheckpoint(checkpoint); restoreErr != nil {
				return Snapshot{}, target, fmt.Errorf(
					"restore before module-path migration: %w",
					restoreErr,
				)
			}
			target = newPath
			progressf(
				s.cfg.Progress,
				"Migrating %s@%s -> %s@%s: rewriting root-module imports",
				path,
				module.Version,
				newPath,
				candidate,
			)
			var files []string
			files, err = migrateModule(
				ctx,
				s.runner,
				path,
				newPath,
				candidate,
				s.current,
				s.cfg.Policy,
				s.transaction,
				checkpoint,
			)
			s.migrations[path+"@"+candidate] = migrationTrial{
				TargetPath:  newPath,
				SourceFiles: files,
			}
		}
	}
	if err != nil {
		return Snapshot{}, target, fmt.Errorf("execute candidate %s@%s: %w", path, candidate, err)
	}
	trial, err := CaptureSnapshot(ctx, s.runner.Root, s.runner.Binary)
	if err != nil {
		return Snapshot{}, target, fmt.Errorf("reload graph after %s@%s: %w", path, candidate, err)
	}
	if err := applySelectedTargets(ctx, s.runner, s.cfg.Policy, trial); err != nil {
		return Snapshot{}, target, fmt.Errorf(
			"apply explicit target after %s@%s: %w",
			path,
			candidate,
			err,
		)
	}
	if err := tidyWithTargets(ctx, s.runner, s.cfg.Policy); err != nil {
		return Snapshot{}, target, fmt.Errorf("tidy candidate %s@%s: %w", path, candidate, err)
	}
	if err := SetToolchainDirective(ctx, s.runner, expectedToolchain(s.baseline, s.cfg.Policy)); err != nil {
		return Snapshot{}, target, fmt.Errorf(
			"enforce toolchain after %s@%s: %w",
			path,
			candidate,
			err,
		)
	}
	after, err := CaptureSnapshot(ctx, s.runner.Root, s.runner.Binary)
	if err != nil {
		return Snapshot{}, target, fmt.Errorf(
			"reload graph after tidy for %s@%s: %w",
			path,
			candidate,
			err,
		)
	}
	return after, target, nil
}

func (s *dependencyTrials) rejectError(
	module Module,
	candidate string,
	checkpoint ModuleCheckpoint,
	cause error,
) (bool, error) {
	if err := s.transaction.RestoreCheckpoint(checkpoint); err != nil {
		return false, errors.Join(
			cause,
			fmt.Errorf("restore candidate %s@%s: %w", module.Path, candidate, err),
		)
	}
	result := resultFor(s.results, module.Path, s.baseline)
	outcome := moduleExecutionFailure
	if isConstraintFailure(cause) {
		outcome = moduleBlocked
	} else {
		result.Outcome = outcome
	}
	result.Attempts = append(
		result.Attempts,
		Attempt{Version: candidate, Outcome: outcome, Evidence: cause.Error()},
	)
	progressf(
		s.cfg.Progress,
		"Rejected %s: %s -> %s (%s: %s)",
		module.Path,
		versionLabel(module.Version),
		candidate,
		outcome,
		cause.Error(),
	)
	if outcome == moduleBlocked {
		return false, nil
	}
	return false, cause
}

func (s *dependencyTrials) tryCandidate(
	ctx context.Context,
	module Module,
	candidate string,
) (bool, error) {
	path := module.Path
	progressf(s.cfg.Progress, "Trying %s: %s -> %s", path, versionLabel(module.Version), candidate)
	checkpoint, err := s.transaction.Checkpoint()
	if err != nil {
		return false, fmt.Errorf("checkpoint %s@%s: %w", path, candidate, err)
	}
	after, target, err := s.prepareCandidate(ctx, module, candidate, checkpoint)
	if err != nil {
		return s.rejectError(module, candidate, checkpoint, err)
	}
	changes := graphChanges(s.current, after)
	violations := ValidatePolicy(s.baseline, after, s.cfg.Policy)
	if selected, exists := after.Modules[target]; !exists || selected.Version != candidate {
		violations = append(
			violations,
			fmt.Errorf("requested %s@%s selected %q", target, candidate, selected.Version),
		)
	}
	if target != path && len(violations) == 0 {
		progressf(
			s.cfg.Progress,
			"Building packages to validate module-path migration to %s",
			target,
		)
		if _, err := s.runner.Run(ctx, "build", "-mod=readonly", "./..."); err != nil {
			if isMigrationBuildConstraint(err) {
				violations = append(
					violations,
					fmt.Errorf("renamed module does not compile with root packages: %w", err),
				)
			} else {
				return s.rejectError(module, candidate, checkpoint,
					fmt.Errorf("build renamed module %s@%s: %w", target, candidate, err))
			}
		}
	}
	result := resultFor(s.results, path, s.baseline)
	if len(violations) != 0 {
		evidence := joinErrors(violations)
		result.Attempts = append(
			result.Attempts,
			Attempt{
				Version:  candidate,
				Outcome:  moduleBlocked,
				Evidence: evidence,
				Changes:  changes,
			},
		)
		recordRejectedGraph(s.results, s.baseline, s.current, after, path, evidence, changes)
		progressf(
			s.cfg.Progress,
			"Rejected %s: %s -> %s (blocked-by-constraints: %s)",
			path,
			versionLabel(module.Version),
			candidate,
			evidence,
		)
		if err := s.transaction.RestoreCheckpoint(checkpoint); err != nil {
			return false, fmt.Errorf(
				"restore policy-rejected candidate %s@%s: %w",
				path,
				candidate,
				err,
			)
		}
		return false, nil
	}
	result.Attempts = append(
		result.Attempts,
		Attempt{Version: candidate, Outcome: attemptAccepted, Changes: changes},
	)
	if target != path {
		result.MigratedTo = target
		s.migratedPaths[path] = target
	}
	for _, targetPath := range sortedTargetPaths(s.cfg.Policy.KubernetesTargets) {
		if targetPath == path ||
			s.current.Modules[targetPath].Version == after.Modules[targetPath].Version {
			continue
		}
		targetResult := resultFor(s.results, targetPath, s.baseline)
		targetResult.Attempts = append(
			targetResult.Attempts,
			Attempt{
				Version:  s.cfg.Policy.KubernetesTargets[targetPath],
				Outcome:  attemptAccepted,
				Evidence: "explicit Kubernetes target introduced during candidate trial",
				Changes:  changes,
			},
		)
	}
	progressf(
		s.cfg.Progress,
		"Accepted %s: %s -> %s@%s (%d graph changes)",
		path,
		versionLabel(module.Version),
		target,
		after.Modules[target].Version,
		len(changes),
	)
	observeSnapshot(s.results, after, s.baseline)
	s.current = after
	return true, nil
}
