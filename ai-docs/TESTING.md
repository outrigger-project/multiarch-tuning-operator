# Testing Guide

## Test Suites

| Suite | Command | Label | Location |
|-------|---------|-------|----------|
| Unit/Integration | `make unit` | `integration` | `internal/controller/*_test.go`, `pkg/*_test.go` |
| E2E | `make e2e` | `e2e` | `pkg/e2e/` |
| Full pipeline | `make test` | — | Runs manifests + generate + fmt + vet + goimports + gosec + lint + unit |

## Running Tests

```bash
# Full quality pipeline (CI equivalent)
make test

# Unit/integration tests only
make unit

# Run a specific test by pattern
GINKGO_ARGS="-v --focus='your test pattern'" make unit

# E2E tests (requires deployed operator + cluster access)
KUBECONFIG=/path/to/kubeconfig NAMESPACE=openshift-multiarch-tuning-operator make e2e

# Run locally (not in container)
NO_DOCKER=1 make unit
```

## Test Framework

**Ginkgo v2 + Gomega** — vendored at `vendor/github.com/onsi/ginkgo/v2/`. Tests use `Describe`/`When`/`Context`/`It` structure with `Label()` for CI filtering.

**Ginkgo arguments** (from `hack/ci-test.sh:26`):
```
-vv --randomize-all --randomize-suites -p -race -trace --keep-going --timeout=60m
```

**envtest** — unit tests use `sigs.k8s.io/controller-runtime/pkg/envtest` for a fake API server. Binary assets discovered via `KUBEBUILDER_ASSETS` (auto-resolved by `setup-envtest`).

## Test Utilities

### Fluent Builders (`pkg/testing/builder/`)

20+ builder files for constructing Kubernetes objects in tests. Key builders:

| Builder | File | Example |
|---------|------|---------|
| `NewClusterPodPlacementConfig()` | `clusterpodplacementconfig.go` | Build CPPC with spec options |
| `NewPodBuilder()` | `pod.go` | Build pods with containers, gates |
| `NewNodeBuilder()` | `node.go` | Build nodes with architecture labels |
| `NewContainerBuilder()` | `container.go` | Build containers with image refs |
| `NewJobBuilder()` | `job.go` | Build batch jobs |

**Pattern:** Builders use fluent method chaining:
```go
pod := builder.NewPodBuilder().
    WithName("test-pod").
    WithNamespace("test-ns").
    WithContainer(builder.NewContainerBuilder().
        WithImage("registry.io/app:latest").
        Build()).
    Build()
```

### Framework Utilities (`pkg/testing/framework/`)

| Utility | File | Purpose |
|---------|------|---------|
| `ValidateDeletion()` | `utils.go` | Async deletion validation |
| `VerifyConditions()` | `utils.go` | Status condition assertion |
| `NodeAffinityEquivalenceMatcher` | `node_affinity_equivalence_matcher.go` | Custom Gomega matcher for affinity comparison |
| Namespace helpers | `namespace.go` | Create/cleanup test namespaces |
| Node helpers | `node.go` | Create/cleanup test nodes |
| Pod helpers | `pod.go` | Pod assertion utilities |

### Fake Registry (`pkg/testing/registry/`)

In-process container registry (`registry.go`) for image inspection tests. Provides controlled image manifests without external registry dependencies.

## Test Suite Setup

### Operator Tests (`internal/controller/operator/suite_test.go`)

- Uses `SynchronizedBeforeSuite`/`SynchronizedAfterSuite` for parallel Ginkgo node coordination
- Starts `envtest.Environment` with CRD paths and webhook config
- Launches the controller manager in a goroutine
- Waits for readiness probe at `:8081/readyz`
- Label: `Label("integration", "operator")`

### Pod Placement Tests (`internal/controller/podplacement/suite_test.go`)

- Sets up fake container registry for image inspection
- Shares registry data across Ginkgo nodes via marshaled data in `SynchronizedBeforeSuite`
- Label: `Label("integration")`

### E2E Tests (`pkg/e2e/`)

- **Require:** Deployed operator in a real cluster, `KUBECONFIG` set
- Operator suite: `pkg/e2e/operator/pod_placement_config_test.go`
- Pod placement suite: `pkg/e2e/podplacement/pod_placement_test.go`
- PodPlacementConfig suite: `pkg/e2e/podplacementconfig/`
- Environment: `NAMESPACE` defaults to `openshift-multiarch-tuning-operator`

## Test Patterns

### Async Assertions

Tests use `Eventually()` with Gomega for async operations (controller reconciliation, status updates):

```go
Eventually(func(g Gomega) {
    pod := &corev1.Pod{}
    g.Expect(k8sClient.Get(ctx, podKey, pod)).To(Succeed())
    g.Expect(pod.Spec.Affinity).NotTo(BeNil())
}).WithTimeout(30 * time.Second).WithPolling(1 * time.Second).Should(Succeed())
```

### Serial and Ordered Tests

Operator tests use `Serial` and `Ordered` decorators since they modify cluster-wide singleton resources:

```go
var _ = Describe("ClusterPodPlacementConfig controller", Serial, Ordered, func() { ... })
```

### Event Assertions

Webhook event publishing tests verify async events via the ants worker pool:

```go
Eventually(func() bool {
    events := &corev1.EventList{}
    k8sClient.List(ctx, events, client.InNamespace(ns))
    return containsEvent(events, "ArchitectureAwareSchedulingGateAdded")
}).Should(BeTrue())
```

## CI Integration

- **OpenShift CI:** Detected via `OPENSHIFT_CI` env var (`hack/ci-test.sh:23`)
- **JUnit output:** `junit_multiarch_tuning_operator.xml` in `ARTIFACT_DIR` (default: `./_output`)
- **Coverage:** `test-unit-coverage.out` and HTML report generated automatically
- **Prow compatibility:** Home directory fix for kubebuilder cache in Prow pods (`hack/ci-test.sh:33-37`)

## Coverage

```bash
# Coverage is generated automatically by `make unit`
# Output: test-unit-coverage.out (text), test-unit-coverage.html (visual)
# Location: ARTIFACT_DIR (default: ./_output)
```

## Platform Documentation

For generic OpenShift testing practices: [openshift/enhancements](https://github.com/openshift/enhancements) — `dev-guide/`
