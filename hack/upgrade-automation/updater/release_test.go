package updater

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestResolveKubernetesRelease(t *testing.T) {
	root, proxy := targetFixture(t, testGoVersion, "")
	setLocalProxy(t, proxy)
	for _, path := range []string{"k8s.io/cloud-provider-vsphere", "k8s.io/provider"} {
		for _, version := range []string{"v1.36.0", "v1.36.6", "v1.37.0"} {
			writeProxyModule(t, proxy, path, version, "1.20", nil)
		}
		writeProxyVersionList(t, proxy, path, []string{"v1.36.0", "v1.36.6"})
	}
	writeProxyVersionList(t, proxy, "k8s.io/cloud-provider-vsphere", []string{"v1.36.0", "v1.37.0"})
	baseline := Snapshot{Modules: map[string]Module{}}
	for path, version := range map[string]string{
		"k8s.io/api": testKubernetesBaseline, "k8s.io/apimachinery": "v0.36.4",
		"k8s.io/provider": "v1.36.0", "k8s.io/cloud-provider-vsphere": "v1.36.0",
		"k8s.io/klog/v2": "v2.140.0", "k8s.io/utils": "v0.0.0-20260707023825-cf1189d6abe3",
		"k8s.io/kube-openapi": "v0.0.0-20260519202549-bbf5c5577288", "k8s.io/gengo/v2": "v2.0.0-20250922181213-ec3ebc5fd46b",
		"k8s.io/newer": "v0.37.0", "k8s.io/current": testKubernetesTarget, "k8s.io/prerelease": "v0.36.0-beta.1",
		"example.test/lib": testInitialVersion,
	} {
		baseline.Modules[path] = Module{Path: path, Version: version}
	}
	policy := Policy{
		Mode:              ModeKubernetesOnly,
		KubernetesRelease: "1.36.6",
		KubernetesTargets: map[string]string{"k8s.io/apimachinery": testKubernetesBaseline},
	}
	got, decisions, err := ResolveKubernetesRelease(
		context.Background(),
		GoRunner{Root: root},
		baseline,
		policy,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"k8s.io/api":          testKubernetesTarget,
		"k8s.io/apimachinery": testKubernetesBaseline,
		"k8s.io/provider":     "v1.36.6",
		"k8s.io/current":      testKubernetesTarget,
	}
	if !reflect.DeepEqual(got.KubernetesTargets, want) {
		t.Fatalf("targets=%v, want %v", got.KubernetesTargets, want)
	}
	if len(policy.KubernetesTargets) != 1 {
		t.Fatal("input policy mutated")
	}
	outcomes := map[string]string{}
	previous := ""
	for _, d := range decisions {
		if d.Path <= previous {
			t.Fatal("decisions not sorted")
		}
		previous = d.Path
		outcomes[d.Path] = d.Disposition
	}
	for path, want := range map[string]string{
		"k8s.io/api": "available", "k8s.io/provider": "available",
		"k8s.io/cloud-provider-vsphere": "unavailable", "k8s.io/newer": "downgrade-skipped",
		"k8s.io/current": "already-current", "k8s.io/klog/v2": "independent-version",
		"k8s.io/prerelease": "independent-version",
	} {
		if outcomes[path] != want {
			t.Errorf("%s: %q, want %q", path, outcomes[path], want)
		}
	}
	baseline.Modules = map[string]Module{
		"k8s.io/missing": {Path: "k8s.io/missing", Version: "v0.36.0"},
	}
	if _, _, err := ResolveKubernetesRelease(context.Background(), GoRunner{Root: root}, baseline, policy); err == nil {
		t.Fatal("failed version listing treated as unavailable")
	}
}

func TestRunKubernetesRelease(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "available", true: "conflict"}[conflict], func(t *testing.T) {
			root, proxy := targetFixture(t, testGoVersion, "")
			setLocalProxy(t, proxy)
			writeProxyModule(t, proxy, "k8s.io/cloud-provider-vsphere", "v1.36.0", "1.20", nil)
			writeProxyVersionList(t, proxy, "k8s.io/cloud-provider-vsphere", []string{"v1.36.0"})
			for _, path := range []string{"k8s.io/client-go", "k8s.io/streaming", "k8s.io/graph-only"} {
				for _, version := range []string{"v0.36.4", testKubernetesTarget} {
					writeProxyModule(t, proxy, path, version, "1.20", nil)
				}
				writeProxyVersionList(t, proxy, path, []string{"v0.36.4", testKubernetesTarget})
			}
			writeProxyModuleWithSource(
				t,
				proxy,
				"k8s.io/api",
				testKubernetesBaseline,
				"1.20",
				map[string]string{"k8s.io/graph-only": "v0.36.4"},
				"package fixture\nimport _ \"k8s.io/graph-only\"\n",
			)
			requirements := map[string]string{
				"k8s.io/client-go":    testKubernetesTarget,
				"k8s.io/streaming":    testKubernetesTarget,
				"k8s.io/apimachinery": testKubernetesTarget,
				"k8s.io/graph-only":   testKubernetesTarget,
			}
			if conflict {
				writeProxyModule(t, proxy, "k8s.io/cloud-provider-vsphere", "v1.36.1", "1.20", nil)
				requirements["k8s.io/cloud-provider-vsphere"] = "v1.36.1"
			}
			writeProxyModuleWithSource(
				t,
				proxy,
				"k8s.io/api",
				testKubernetesTarget,
				"1.20",
				requirements,
				"package fixture\nimport (_ \"k8s.io/client-go\"; _ \"k8s.io/streaming\"; "+
					"_ \"k8s.io/apimachinery\"; _ \"k8s.io/graph-only\")\n",
			)
			runner := GoRunner{Root: root}
			_, err := runner.Run(context.Background(), "mod", "edit",
				"-require=k8s.io/cloud-provider-vsphere@v1.36.0",
				"-require=k8s.io/client-go@v0.36.4", "-require=k8s.io/streaming@v0.36.4")
			if err != nil {
				t.Fatal(err)
			}
			writeFile(
				t,
				filepath.Join(root, "fixture.go"),
				"package fixture\nimport (_ \"k8s.io/api\"; _ \"k8s.io/cloud-provider-vsphere\")\n",
			)
			before, err := os.ReadFile(filepath.Join(root, "go.mod"))
			if err != nil {
				t.Fatal(err)
			}
			var progress bytes.Buffer
			reportPath := filepath.Join(t.TempDir(), "report.json")
			report, err := Run(
				context.Background(),
				Config{
					Root:       root,
					Policy:     Policy{Mode: ModeKubernetesOnly, KubernetesRelease: "1.36.6"},
					ReportPath: reportPath,
					Progress:   &progress,
				},
			)
			if conflict {
				if err == nil || !report.RolledBack {
					t.Fatalf("conflict: err=%v report=%+v", err, report)
				}
				after, _ := os.ReadFile(filepath.Join(root, "go.mod"))
				if !bytes.Equal(before, after) {
					t.Fatal("go.mod not restored")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				for _, path := range []string{
					"k8s.io/api", "k8s.io/apimachinery", "k8s.io/client-go", "k8s.io/streaming", "k8s.io/graph-only",
				} {
					if report.Final.Modules[path].Version != testKubernetesTarget {
						t.Errorf("%s not upgraded", path)
					}
				}
				if report.Final.Modules["k8s.io/cloud-provider-vsphere"].Version != "v1.36.0" {
					t.Fatal("provider changed")
				}
				data, err := os.ReadFile(reportPath)
				if err != nil {
					t.Fatal(err)
				}
				var published Report
				if err = json.Unmarshal(data, &published); err != nil || len(published.KubernetesReleaseTargets) == 0 {
					t.Fatalf("published decisions missing: %v", err)
				}
			}
			if len(report.KubernetesReleaseTargets) == 0 ||
				!strings.Contains(progress.String(), "unavailable") {
				t.Fatal("release decisions missing")
			}
		})
	}
}
