package updater

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

type Mode string

const (
	ModePinBoth        Mode = "pin-both"
	ModeGoOnly         Mode = "go-only"
	ModeKubernetesOnly Mode = "kubernetes-only"
	ModeBoth           Mode = "both"
)

type Policy struct {
	Mode              Mode              `json:"mode"`
	GoTarget          string            `json:"goTarget,omitempty"`
	KubernetesTargets map[string]string `json:"kubernetesTargets,omitempty"`
}

type Module struct {
	Path     string `json:"path"`
	Version  string `json:"version"`
	Main     bool   `json:"main,omitempty"`
	Direct   bool   `json:"direct"`
	Required bool   `json:"required"`
}

type Snapshot struct {
	GoDirective string            `json:"goDirective"`
	Toolchain   string            `json:"toolchain"`
	Modules     map[string]Module `json:"modules"`
}

var exactGoVersion = regexp.MustCompile(`^1\.[0-9]+\.[0-9]+$`)

func ParsePolicy(mode, goTarget string, k8sTargetArgs []string) (Policy, error) {
	policy := Policy{Mode: Mode(mode), KubernetesTargets: make(map[string]string)}
	switch policy.Mode {
	case ModePinBoth, ModeGoOnly, ModeKubernetesOnly, ModeBoth:
	default:
		return Policy{}, fmt.Errorf("unknown mode %q", mode)
	}

	updatesGo := policy.Mode == ModeGoOnly || policy.Mode == ModeBoth
	updatesKubernetes := policy.Mode == ModeKubernetesOnly || policy.Mode == ModeBoth
	if updatesGo {
		if !exactGoVersion.MatchString(goTarget) {
			return Policy{}, fmt.Errorf("mode %q requires an exact Go target in X.Y.Z form, got %q", mode, goTarget)
		}
		policy.GoTarget = goTarget
	} else if goTarget != "" {
		return Policy{}, fmt.Errorf("mode %q pins Go and does not accept --go-target", mode)
	}

	if !updatesKubernetes && len(k8sTargetArgs) != 0 {
		return Policy{}, fmt.Errorf("mode %q pins Kubernetes and does not accept --k8s-target", mode)
	}
	for _, target := range k8sTargetArgs {
		modulePath, version, ok := strings.Cut(target, "=")
		if !ok || modulePath == "" || version == "" || strings.Contains(version, "=") {
			return Policy{}, fmt.Errorf("invalid Kubernetes target %q: expected module=exact-version", target)
		}
		if err := module.CheckPath(modulePath); err != nil {
			return Policy{}, fmt.Errorf("invalid module path in Kubernetes target %q: %w", target, err)
		}
		if !semver.IsValid(version) {
			return Policy{}, fmt.Errorf("invalid exact module version in Kubernetes target %q", target)
		}
		if _, duplicate := policy.KubernetesTargets[modulePath]; duplicate {
			return Policy{}, fmt.Errorf("duplicate Kubernetes target for module %q", modulePath)
		}
		policy.KubernetesTargets[modulePath] = version
	}
	if updatesKubernetes && len(policy.KubernetesTargets) == 0 {
		return Policy{}, fmt.Errorf("mode %q requires at least one --k8s-target module=version", mode)
	}
	return policy, nil
}

func ValidatePolicy(before, after Snapshot, policy Policy) []error {
	var violations []error
	updatesGo := policy.Mode == ModeGoOnly || policy.Mode == ModeBoth
	wantGo := before.GoDirective
	wantToolchain := before.Toolchain
	if updatesGo {
		wantGo = policy.GoTarget
		if before.Toolchain != "" {
			wantToolchain = "go" + policy.GoTarget
		}
	}
	if after.GoDirective != wantGo {
		violations = append(violations, fmt.Errorf("go directive selected %q, want exact %q", after.GoDirective, wantGo))
	}
	if after.Toolchain != wantToolchain {
		violations = append(violations, fmt.Errorf("toolchain directive selected %q, want %q", after.Toolchain, wantToolchain))
	}

	for _, path := range sortedSnapshotPaths(before) {
		baselineModule := before.Modules[path]
		if !isKubernetesModule(path) {
			continue
		}
		selected, exists := after.Modules[path]
		if !exists {
			violations = append(violations, fmt.Errorf("baseline Kubernetes module %q was removed", path))
			continue
		}
		wantVersion := baselineModule.Version
		if target, requested := policy.KubernetesTargets[path]; requested {
			wantVersion = target
		}
		if selected.Version != wantVersion {
			violations = append(violations, fmt.Errorf("Kubernetes module %q selected %q, want exact %q", path, selected.Version, wantVersion))
		}
	}

	for _, path := range sortedSnapshotPaths(after) {
		selected := after.Modules[path]
		if isKubernetesModule(path) {
			if _, existed := before.Modules[path]; !existed {
				if target, explicitlyAllowed := policy.KubernetesTargets[path]; !explicitlyAllowed {
					violations = append(violations, fmt.Errorf("new Kubernetes module %q has no explicit policy target", path))
				} else if selected.Version != target {
					violations = append(violations, fmt.Errorf("new Kubernetes module %q selected %q, want exact target %q", path, selected.Version, target))
				}
			}
		}
	}
	targetPaths := make([]string, 0, len(policy.KubernetesTargets))
	for path := range policy.KubernetesTargets {
		targetPaths = append(targetPaths, path)
	}
	sort.Strings(targetPaths)
	for _, path := range targetPaths {
		target := policy.KubernetesTargets[path]
		if selected, exists := after.Modules[path]; exists && selected.Version != target {
			// The baseline loop reports k8s.io modules; this also supports an
			// explicitly targeted Kubernetes ecosystem module outside that prefix.
			if !isKubernetesModule(path) {
				violations = append(violations, fmt.Errorf("target module %q selected %q, want exact target %q", path, selected.Version, target))
			}
		}
	}
	return violations
}

func isKubernetesModule(path string) bool {
	return strings.HasPrefix(path, "k8s.io/")
}

func sortedSnapshotPaths(snapshot Snapshot) []string {
	paths := make([]string, 0, len(snapshot.Modules))
	for path := range snapshot.Modules {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}
