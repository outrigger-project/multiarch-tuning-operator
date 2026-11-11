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
	Root       string
	Policy     Policy
	GoBinary   string
	ReportPath string
	MaxPasses  int
	Progress   io.Writer
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
	moduleNotInGoMod       = "not-in-go-mod"

	attemptAccepted = "accepted"
)

func ApplyTargets(
	ctx context.Context,
	runner GoRunner,
	baseline Snapshot,
	policy Policy,
	transaction *Transaction,
) (Snapshot, error) {
	if err := runner.CheckCompiler(ctx, compilerTarget(baseline, policy)); err != nil {
		return Snapshot{}, err
	}
	checkpoint, err := transaction.Checkpoint()
	if err != nil {
		return Snapshot{}, err
	}
	rollback := func(cause error) (Snapshot, error) {
		if restoreErr := transaction.RestoreCheckpoint(checkpoint); restoreErr != nil {
			return Snapshot{}, errors.Join(
				cause,
				fmt.Errorf("restore target checkpoint: %w", restoreErr),
			)
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
	if err := tidyWithTargets(ctx, runner, policy); err != nil {
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
		return rollback(
			fmt.Errorf(
				"requested Go/Kubernetes targets cannot coexist: %w",
				errors.Join(violations...),
			),
		)
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
	migrations := make(map[string]migrationTrial)
	migratedPaths := make(map[string]string)
	committed := false
	defer func() {
		if committed {
			return
		}
		report.RolledBack = true
		if err := transaction.Rollback(); err != nil {
			runErr = &UpdaterError{Code: 2, Err: errors.Join(runErr, fmt.Errorf("rollback dependency update: %w", err))}
			report.Status = runFailed
		}
		if runErr != nil {
			report.Failure = sanitizeEvidence(runErr.Error())
		}
		if baseline.Modules != nil {
			report.Final = baseline
			annotateMigrations(results, migrations)
			finalizeResults(results, baseline, cfg.Policy, true)
			report.Modules = sortedResults(results)
			emitRunSummary(cfg.Progress, report)
		}
		runErr = runErrorWithCode(runErr, report.Status)
	}()

	baseline, err = CaptureSnapshot(ctx, root, cfg.GoBinary)
	if err != nil {
		return report, fmt.Errorf("capture baseline: %w", err)
	}
	progressf(
		cfg.Progress,
		"Inventory: %d modules; Go directive %s; toolchain %s",
		dependencyCount(baseline),
		baseline.GoDirective,
		toolchainLabel(baseline.Toolchain),
	)
	report.Baseline = baseline
	report.Final = baseline
	observeSnapshot(results, baseline, baseline)
	cfg.Policy, report.KubernetesReleaseTargets, err = ResolveKubernetesRelease(
		ctx,
		runner,
		baseline,
		cfg.Policy,
	)
	report.Policy = cfg.Policy
	for _, decision := range report.KubernetesReleaseTargets {
		progressf(
			cfg.Progress,
			"Kubernetes release: %s %s -> %s (%s)",
			decision.Path,
			decision.CurrentVersion,
			decision.TargetVersion,
			decision.Disposition,
		)
	}
	if err != nil {
		return report, fmt.Errorf("resolving Kubernetes release: %w", err)
	}
	emitRequestedTargets(cfg.Progress, baseline, cfg.Policy)
	current, err := ApplyTargets(ctx, runner, baseline, cfg.Policy, transaction)
	if err != nil {
		return report, fmt.Errorf("apply target policy: %w", err)
	}
	observeSnapshot(results, current, baseline)
	if err := recordAppliedTargets(results, baseline, current, cfg.Policy); err != nil {
		return report, err
	}

	trials := dependencyTrials{
		cfg: cfg, runner: runner, transaction: transaction, baseline: baseline,
		current: current, results: results, migrations: migrations, migratedPaths: migratedPaths,
	}
	if err := trials.run(ctx); err != nil {
		if ExitCode(err) == 3 {
			report.Status = runIncomplete
		}
		return report, err
	}
	current = trials.current

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
		return report, fmt.Errorf(
			"policy validation after require-block normalization failed: %w",
			errors.Join(violations...),
		)
	}
	current = normalized

	for _, modulePath := range sortedTargetPaths(cfg.Policy.KubernetesTargets) {
		if _, exists := current.Modules[modulePath]; !exists {
			result := resultFor(results, modulePath, baseline)
			result.Outcome = moduleBlocked
		}
	}
	if len(migratedPaths) != 0 {
		progressf(
			cfg.Progress,
			"Validating migrated packages against the final accepted dependency graph",
		)
		if _, err := runner.Run(ctx, "build", "-mod=readonly", "./..."); err != nil {
			return report, fmt.Errorf("final build after module-path migrations: %w", err)
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
		return report, fmt.Errorf(
			"final exact-policy validation failed: %w",
			errors.Join(violations...),
		)
	}
	report.Status = runCompleted
	report.Final = final
	annotateMigrations(results, migrations)
	finalizeResults(results, final, cfg.Policy, false)
	report.Modules = sortedResults(results)
	if cfg.ReportPath != "" {
		if err := WriteReport(cfg.ReportPath, report); err != nil {
			report.Status = runFailed
			return report, fmt.Errorf("publish dependency report: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		report.Status = runFailed
		return report, fmt.Errorf("commit dependency transaction: %w", err)
	}
	committed = true
	emitRunSummary(cfg.Progress, report)
	return report, nil
}

func recordAppliedTargets(
	results map[string]*ModuleResult,
	baseline, current Snapshot,
	policy Policy,
) error {
	changes := graphChanges(baseline, current)
	for _, path := range sortedTargetPaths(policy.KubernetesTargets) {
		target := policy.KubernetesTargets[path]
		selected, exists := current.Modules[path]
		if !exists {
			continue
		}
		before := baseline.Modules[path]
		if !before.Main && before.Version != target {
			result := resultFor(results, path, baseline)
			result.Attempts = append(
				result.Attempts,
				Attempt{
					Version:  target,
					Outcome:  attemptAccepted,
					Evidence: "explicit Kubernetes target",
					Changes:  changes,
				},
			)
		}
		if selected.Version != target {
			return fmt.Errorf(
				"kubernetes module %q selected %q after target setup, want %q",
				path,
				selected.Version,
				target,
			)
		}
	}
	return nil
}

func runErrorWithCode(err error, status string) error {
	var coded *UpdaterError
	if err == nil || errors.As(err, &coded) {
		return err
	}
	code := ExitCode(err)
	if status == runIncomplete {
		code = 3
	}
	return &UpdaterError{Code: code, Err: err}
}

func tidyWithTargets(ctx context.Context, runner GoRunner, policy Policy) error {
	if _, err := runner.Run(ctx, "mod", "tidy"); err != nil {
		return err
	}
	if len(policy.KubernetesTargets) == 0 {
		return nil
	}
	snapshot, err := CaptureSnapshot(ctx, runner.Root, runner.Binary)
	if err != nil {
		return fmt.Errorf("capturing graph after tidy: %w", err)
	}
	// Tidy drops pins for graph-only modules whose packages are not imported.
	if err := applySelectedTargets(ctx, runner, policy, snapshot); err != nil {
		return fmt.Errorf("reapplying exact targets after tidy: %w", err)
	}
	return nil
}

func applySelectedTargets(
	ctx context.Context,
	runner GoRunner,
	policy Policy,
	snapshot Snapshot,
) error {
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
	var migrationErr *migrationConstraint
	if errors.As(err, &migrationErr) {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "requires go >=") ||
		strings.Contains(text, "excluded by go.mod") ||
		strings.Contains(text, "excluded by the main module") ||
		modulePathMismatch.MatchString(text)
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
	if left.GoDirective != right.GoDirective || left.Toolchain != right.Toolchain ||
		len(left.Modules) != len(right.Modules) {
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

func recordRejectedGraph(
	results map[string]*ModuleResult,
	baseline, before, after Snapshot,
	candidatePath, evidence string,
	changes []Change,
) {
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
		result.Attempts = append(
			result.Attempts,
			Attempt{
				Version:  module.Version,
				Outcome:  moduleBlocked,
				Evidence: "selected by a rejected transitive trial: " + evidence,
				Changes:  changes,
			},
		)
	}
}

func finalizeResults(
	results map[string]*ModuleResult,
	final Snapshot,
	policy Policy,
	rolledBack bool,
) {
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
		if rolledBack && result.InitialVersion == result.FinalVersion &&
			hasAttemptOutcome(result, attemptAccepted) {
			result.Outcome = moduleExecutionFailure
			continue
		}
		if result.MigratedTo != "" {
			if rolledBack {
				result.Outcome = moduleExecutionFailure
			} else {
				result.Outcome = moduleUpdated
			}
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
			if result.InitialVersion == "" && result.FinalVersion == "" &&
				hasAttemptOutcome(result, moduleBlocked) {
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
		} else if exists && !module.Required && len(result.Attempts) == 0 && result.Outcome == "" {
			result.Outcome = moduleNotInGoMod
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
			if module.MigratedTo != "" {
				progressf(
					writer,
					"Migrated %s@%s -> %s (see target module for final version)",
					module.Path,
					versionLabel(module.InitialVersion),
					module.MigratedTo,
				)
				continue
			}
			if module.InitialVersion == "" {
				progressf(
					writer,
					"Added %s: %s -> %s",
					module.Path,
					versionLabel(module.InitialVersion),
					versionLabel(module.FinalVersion),
				)
			} else {
				progressf(writer, "Updated %s: %s -> %s", module.Path,
					versionLabel(module.InitialVersion), versionLabel(module.FinalVersion))
			}
		case moduleRemoved:
			progressf(
				writer,
				"Removed %s: %s -> %s",
				module.Path,
				versionLabel(module.InitialVersion),
				versionLabel(module.FinalVersion),
			)
		default:
			current := module.FinalVersion
			if current == "" {
				current = module.InitialVersion
			}
			if module.CandidateLimitReached {
				progressf(
					writer,
					"No update %s: remains at %s (%s; candidate limit reached after %d attempts)",
					module.Path,
					versionLabel(current),
					module.Outcome,
					maxCandidateAttempts,
				)
			} else {
				progressf(writer, "No update %s: remains at %s (%s)", module.Path, versionLabel(current), module.Outcome)
			}
		}
	}
}

func emitRequestedTargets(writer io.Writer, baseline Snapshot, policy Policy) {
	if policy.Mode == ModeGoOnly || policy.Mode == ModeBoth {
		progressf(
			writer,
			"Go directive target: %s -> %s",
			versionLabel(baseline.GoDirective),
			policy.GoTarget,
		)
	}
	for _, modulePath := range sortedTargetPaths(policy.KubernetesTargets) {
		current := "<absent>"
		if selected, exists := baseline.Modules[modulePath]; exists {
			current = selected.Version
		}
		progressf(
			writer,
			"Kubernetes target: %s %s -> %s",
			modulePath,
			current,
			policy.KubernetesTargets[modulePath],
		)
	}
}

func emitRunSummary(writer io.Writer, report Report) {
	if report.Baseline.GoDirective == report.Final.GoDirective {
		progressf(writer, "Go directive unchanged at %s", versionLabel(report.Final.GoDirective))
	} else {
		progressf(writer, "Go directive: %s -> %s",
			versionLabel(report.Baseline.GoDirective), versionLabel(report.Final.GoDirective))
	}
	if report.Baseline.Toolchain == report.Final.Toolchain {
		progressf(writer, "Toolchain unchanged (%s)", toolchainLabel(report.Final.Toolchain))
	} else {
		progressf(writer, "Toolchain: %s -> %s",
			toolchainLabel(report.Baseline.Toolchain), toolchainLabel(report.Final.Toolchain))
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
