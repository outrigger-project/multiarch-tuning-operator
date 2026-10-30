package updater

import (
	"fmt"
	"testing"
)

func TestParsePolicyAcceptsFourModes(t *testing.T) {
	tests := []struct {
		name        string
		mode        string
		goTarget    string
		k8sTargets  []string
		wantGo      string
		wantTargets map[string]string
	}{
		{name: "pin both", mode: "pin-both", wantTargets: map[string]string{}},
		{name: "go only", mode: "go-only", goTarget: "1.26.8", wantGo: "1.26.8", wantTargets: map[string]string{}},
		{name: "kubernetes only", mode: "kubernetes-only", k8sTargets: []string{"k8s.io/api=v0.36.6", "k8s.io/apimachinery=v0.36.6"}, wantTargets: map[string]string{"k8s.io/api": "v0.36.6", "k8s.io/apimachinery": "v0.36.6"}},
		{name: "both", mode: "both", goTarget: "1.26.8", k8sTargets: []string{"k8s.io/api=v0.36.6"}, wantGo: "1.26.8", wantTargets: map[string]string{"k8s.io/api": "v0.36.6"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePolicy(tt.mode, tt.goTarget, tt.k8sTargets)
			if err != nil {
				t.Fatalf("ParsePolicy() error = %v", err)
			}
			if got.GoTarget != tt.wantGo {
				t.Errorf("GoTarget = %q, want %q", got.GoTarget, tt.wantGo)
			}
			if len(got.KubernetesTargets) != len(tt.wantTargets) {
				t.Fatalf("KubernetesTargets = %#v, want %#v", got.KubernetesTargets, tt.wantTargets)
			}
			for module, version := range tt.wantTargets {
				if got.KubernetesTargets[module] != version {
					t.Errorf("KubernetesTargets[%q] = %q, want %q", module, got.KubernetesTargets[module], version)
				}
			}
		})
	}
}

func TestParsePolicyRejectsMissingAndMalformedTargets(t *testing.T) {
	tests := []struct {
		name       string
		mode       string
		goTarget   string
		k8sTargets []string
	}{
		{name: "missing Go target", mode: "go-only"},
		{name: "Go target must include patch", mode: "go-only", goTarget: "1.26"},
		{name: "missing Kubernetes target map", mode: "kubernetes-only"},
		{name: "empty Kubernetes module", mode: "kubernetes-only", k8sTargets: []string{"=v0.36.6"}},
		{name: "empty Kubernetes version", mode: "kubernetes-only", k8sTargets: []string{"k8s.io/api="}},
		{name: "Kubernetes version must be exact", mode: "kubernetes-only", k8sTargets: []string{"k8s.io/api=latest"}},
		{name: "duplicate Kubernetes target", mode: "kubernetes-only", k8sTargets: []string{"k8s.io/api=v0.36.5", "k8s.io/api=v0.36.6"}},
		{name: "unknown mode", mode: "update-everything"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParsePolicy(tt.mode, tt.goTarget, tt.k8sTargets); err == nil {
				t.Fatal("ParsePolicy() error = nil, want invalid policy error")
			}
		})
	}
}

func TestParsePolicyRejectsTargetsForPinnedDimensions(t *testing.T) {
	tests := []struct {
		name       string
		mode       string
		goTarget   string
		k8sTargets []string
	}{
		{name: "Go target while Go pinned", mode: "pin-both", goTarget: "1.26.8"},
		{name: "Kubernetes target while Kubernetes pinned", mode: "go-only", goTarget: "1.26.8", k8sTargets: []string{"k8s.io/api=v0.36.6"}},
		{name: "Go target while Go pinned in Kubernetes mode", mode: "kubernetes-only", goTarget: "1.26.8", k8sTargets: []string{"k8s.io/api=v0.36.6"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParsePolicy(tt.mode, tt.goTarget, tt.k8sTargets); err == nil {
				t.Fatal("ParsePolicy() error = nil, want pinned-dimension target error")
			}
		})
	}
}

func TestValidatePolicyPreservesIndependentGoAndKubernetesPins(t *testing.T) {
	baseline := Snapshot{
		GoDirective: "1.26.7",
		Toolchain:   "go1.26.7",
		Modules: map[string]Module{
			"k8s.io/api":          {Path: "k8s.io/api", Version: "v0.36.5"},
			"k8s.io/apimachinery": {Path: "k8s.io/apimachinery", Version: "v0.36.4"},
			"example.com/lib":     {Path: "example.com/lib", Version: "v1.2.3"},
		},
	}
	policy, err := ParsePolicy("pin-both", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	after := Snapshot{
		GoDirective: "1.26.7",
		Toolchain:   "go1.26.7",
		Modules: map[string]Module{
			"k8s.io/api":          {Path: "k8s.io/api", Version: "v0.36.6"},
			"k8s.io/apimachinery": {Path: "k8s.io/apimachinery", Version: "v0.36.4"},
			"example.com/lib":     {Path: "example.com/lib", Version: "v1.3.0"},
		},
	}

	violations := ValidatePolicy(baseline, after, policy)
	if len(violations) != 1 {
		t.Fatalf("ValidatePolicy() violations = %v, want one independently pinned Kubernetes violation", violations)
	}
}

func TestValidatePolicyKeepsToolchainPresenceExact(t *testing.T) {
	baseline := Snapshot{GoDirective: "1.26.7", Modules: map[string]Module{}}
	policy, err := ParsePolicy("go-only", "1.26.8", nil)
	if err != nil {
		t.Fatal(err)
	}
	after := Snapshot{GoDirective: "1.26.8", Toolchain: "go1.26.8", Modules: map[string]Module{}}
	if violations := ValidatePolicy(baseline, after, policy); len(violations) == 0 {
		t.Fatal("ValidatePolicy() accepted an added toolchain directive, want presence mismatch")
	}
}

func TestValidatePolicyUpdatesExistingToolchainWithGoTarget(t *testing.T) {
	baseline := Snapshot{GoDirective: "1.26.7", Toolchain: "go1.26.7", Modules: map[string]Module{}}
	policy, err := ParsePolicy("go-only", "1.26.8", nil)
	if err != nil {
		t.Fatal(err)
	}
	after := Snapshot{GoDirective: "1.26.8", Toolchain: "go1.26.8", Modules: map[string]Module{}}
	if violations := ValidatePolicy(baseline, after, policy); len(violations) != 0 {
		t.Fatalf("ValidatePolicy() violations = %v, want none", violations)
	}
}

func TestValidatePolicyRejectsNewUnlistedKubernetesModule(t *testing.T) {
	baseline := Snapshot{GoDirective: "1.26.7", Modules: map[string]Module{
		"k8s.io/api": {Path: "k8s.io/api", Version: "v0.36.5"},
	}}
	policy, err := ParsePolicy("kubernetes-only", "", []string{"k8s.io/api=v0.36.6"})
	if err != nil {
		t.Fatal(err)
	}
	after := Snapshot{GoDirective: "1.26.7", Modules: map[string]Module{
		"k8s.io/api":     {Path: "k8s.io/api", Version: "v0.36.6"},
		"k8s.io/new-api": {Path: "k8s.io/new-api", Version: "v0.1.0"},
	}}
	if violations := ValidatePolicy(baseline, after, policy); len(violations) == 0 {
		t.Fatal("ValidatePolicy() accepted a new unlisted Kubernetes module")
	}
}

func TestValidatePolicyOrdersKubernetesViolationsDeterministically(t *testing.T) {
	baseline := Snapshot{GoDirective: "1.26.7", Modules: make(map[string]Module)}
	after := Snapshot{GoDirective: "1.26.7", Modules: make(map[string]Module)}
	for index := 0; index < 16; index++ {
		modulePath := fmt.Sprintf("k8s.io/api-%02d", index)
		baseline.Modules[modulePath] = Module{Path: modulePath, Version: "v0.36.5"}
		after.Modules[modulePath] = Module{Path: modulePath, Version: "v0.36.6"}
	}
	policy, err := ParsePolicy("pin-both", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	violations := ValidatePolicy(baseline, after, policy)
	for index := 1; index < len(violations); index++ {
		if violations[index-1].Error() > violations[index].Error() {
			t.Fatalf("policy violations are not stable-sorted: %v", violations)
		}
	}
}
