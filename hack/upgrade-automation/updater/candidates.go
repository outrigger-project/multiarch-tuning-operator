package updater

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"golang.org/x/mod/semver"
)

func ListCandidates(ctx context.Context, runner GoRunner, modulePath, current string) ([]string, error) {
	if modulePath == "" {
		return nil, fmt.Errorf("module path is required")
	}
	if !semver.IsValid(current) {
		return nil, fmt.Errorf("current module version %q is invalid", current)
	}
	output, err := runner.Run(ctx, "list", "-m", "-versions", modulePath)
	if err != nil {
		return nil, fmt.Errorf("list versions for %s: %w", modulePath, err)
	}
	fields := strings.Fields(string(output))
	if len(fields) == 0 || fields[0] != modulePath {
		return nil, fmt.Errorf("unexpected version list for %s: %q", modulePath, strings.TrimSpace(string(output)))
	}
	seen := make(map[string]struct{}, len(fields)-1)
	candidates := make([]string, 0, len(fields)-1)
	for _, version := range fields[1:] {
		if !semver.IsValid(version) || semver.Prerelease(version) != "" || semver.Compare(version, current) <= 0 {
			continue
		}
		if _, duplicate := seen[version]; duplicate {
			continue
		}
		seen[version] = struct{}{}
		candidates = append(candidates, version)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return semver.Compare(candidates[i], candidates[j]) > 0
	})
	return candidates, nil
}
