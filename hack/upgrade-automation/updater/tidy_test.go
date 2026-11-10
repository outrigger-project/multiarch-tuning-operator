package updater

import (
	"context"
	"path/filepath"
	"testing"
)

func TestReleaseRetainsUnusedGraphPinsAcrossTidy(t *testing.T) {
	root, proxy := targetFixture(t, testGoVersion, "")
	setLocalProxy(t, proxy)
	writeProxyModule(
		t,
		proxy,
		"k8s.io/cloud-provider-vsphere",
		"v1.36.0",
		"1.20",
		map[string]string{"k8s.io/cloud-provider": "v0.36.0"},
	)
	writeProxyVersionList(t, proxy, "k8s.io/cloud-provider-vsphere", []string{"v1.36.0"})
	for _, version := range []string{"v0.36.0", testKubernetesTarget} {
		writeProxyModule(t, proxy, "k8s.io/cloud-provider", version, "1.20", nil)
	}
	writeProxyVersionList(
		t,
		proxy,
		"k8s.io/cloud-provider",
		[]string{"v0.36.0", testKubernetesTarget},
	)
	for _, version := range []string{testInitialVersion, testUpdatedVersion} {
		writeProxyModule(t, proxy, "example.test/lib", version, "1.20", nil)
	}
	writeProxyVersionList(
		t,
		proxy,
		"example.test/lib",
		[]string{testInitialVersion, testUpdatedVersion},
	)
	runner := GoRunner{Root: root}
	_, err := runner.Run(context.Background(), "mod", "edit",
		"-require=k8s.io/cloud-provider-vsphere@v1.36.0", "-require=example.test/lib@v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(
		t,
		filepath.Join(root, "fixture.go"),
		"package fixture\nimport (_ \"k8s.io/api\"; _ \"k8s.io/apimachinery\"; "+
			"_ \"k8s.io/cloud-provider-vsphere\"; _ \"example.test/lib\")\n",
	)
	report, err := Run(
		context.Background(),
		Config{Root: root, Policy: Policy{Mode: ModeKubernetesOnly, KubernetesRelease: "1.36.6"}},
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := report.Final.Modules["k8s.io/cloud-provider"].Version; got != testKubernetesTarget {
		t.Fatalf("cloud-provider=%s, want v0.36.6", got)
	}
	if got := report.Final.Modules["example.test/lib"].Version; got != testUpdatedVersion {
		t.Fatalf("lib=%s, want accepted candidate v1.1.0", got)
	}
	if _, err := runner.Run(context.Background(), "build", "-mod=readonly", "./..."); err != nil {
		t.Fatalf("Build: %v", err)
	}
}
