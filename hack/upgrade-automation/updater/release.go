package updater

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"golang.org/x/mod/semver"
)

// ResolveKubernetesRelease expands a release into exact targets using each
// selected module's major version. Unpublished targets retain baseline pins.
func ResolveKubernetesRelease(
	ctx context.Context,
	runner GoRunner,
	baseline Snapshot,
	policy Policy,
) (Policy, []ReleaseTargetResult, error) {
	policy.KubernetesTargets = maps.Clone(policy.KubernetesTargets)
	if policy.KubernetesTargets == nil {
		policy.KubernetesTargets = map[string]string{}
	}
	var decisions []ReleaseTargetResult
	if policy.KubernetesRelease == "" {
		return policy, decisions, nil
	}
	if !exactGoVersion.MatchString(policy.KubernetesRelease) ||
		!semver.IsValid("v"+policy.KubernetesRelease) {
		return policy, decisions, fmt.Errorf(
			"invalid Kubernetes release %q",
			policy.KubernetesRelease,
		)
	}
	suffix := strings.TrimPrefix(policy.KubernetesRelease, "1")
	for _, path := range sortedSnapshotPaths(baseline) {
		current := baseline.Modules[path]
		if current.Main || !isKubernetesModule(path) {
			continue
		}
		if _, explicit := policy.KubernetesTargets[path]; explicit {
			continue
		}
		decision := ReleaseTargetResult{
			Path:           path,
			CurrentVersion: current.Version,
			Disposition:    "independent-version",
		}
		switch path {
		case "k8s.io/klog/v2", "k8s.io/utils", "k8s.io/kube-openapi", "k8s.io/gengo/v2":
			decisions = append(decisions, decision)
			continue
		}
		if !semver.IsValid(current.Version) || semver.Prerelease(current.Version) != "" {
			decisions = append(decisions, decision)
			continue
		}
		target := semver.Major(current.Version) + suffix
		decision.TargetVersion = target
		switch semver.Compare(target, current.Version) {
		case -1:
			decision.Disposition = "downgrade-skipped"
		case 0:
			decision.Disposition = "already-current"
			policy.KubernetesTargets[path] = target
		default:
			output, err := runner.Run(ctx, "list", "-m", "-versions", path)
			if err != nil {
				return policy, decisions, fmt.Errorf(
					"listing release versions for %s: %w",
					path,
					err,
				)
			}
			fields := strings.Fields(string(output))
			if len(fields) == 0 || fields[0] != path {
				return policy, decisions, fmt.Errorf(
					"unexpected release version list for %s: %q",
					path,
					string(output),
				)
			}
			decision.Disposition = "unavailable"
			for _, version := range fields[1:] {
				if version == target {
					decision.Disposition = "available"
					policy.KubernetesTargets[path] = target
					break
				}
			}
		}
		decisions = append(decisions, decision)
	}
	return policy, decisions, nil
}
