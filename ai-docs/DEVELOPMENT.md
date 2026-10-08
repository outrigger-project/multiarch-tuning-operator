# Development Guide

## Prerequisites

- **Go 1.26.7+** (`go.mod` — `go 1.26.7`)
- **gpgme-devel** (RHEL/Fedora) or **libgpgme-dev** (Debian) — CGO dependency for image inspection
- **Podman** or **Docker** — containerized builds run by default
- **qemu-user-static** — required only for multi-arch builds via `docker buildx`

## Build Commands

```bash
make build                  # Local build (requires gpgme-devel, CGO_ENABLED=1)
make docker-build IMG=<img> # Single-arch container image
make docker-buildx IMG=<img> # Multi-arch image (linux/arm64,linux/amd64)
```

**Containerized vs local execution:** All builds and tests run inside a container (`BUILD_IMAGE`) by default. Override with `NO_DOCKER=1` in environment or `.env` file. See `dotenv.example` for all options.

## Code Quality Pipeline

`make test` runs the full pipeline in order:

```bash
make manifests    # Regenerate CRDs, RBAC, webhook config (controller-gen)
make generate     # Regenerate DeepCopy implementations (controller-gen)
make fmt          # gofmt -s (hack/go-fmt.sh)
make vet          # go vet ./...
make goimports    # Import grouping check (hack/goimports.sh)
make gosec        # SAST security scan (hack/gosec.sh)
make lint         # golangci-lint v2.12.2 (hack/golangci-lint.sh)
make unit         # Ginkgo unit/integration tests with coverage
```

**CI enforcement:** `make verify-diff` (`hack/verify-diff.sh`) checks for untracked files and uncommitted changes — run after any generation target.

## Common Tasks

### Add or modify API fields

1. Edit types in `api/v1beta1/` (e.g., `clusterpodplacementconfig_types.go`)
2. Run `make manifests generate` — regenerates CRDs, RBAC, DeepCopy
3. Run `make verify-diff` — ensure generated files are committed
4. If adding validation, update the webhook in `api/v1beta1/clusterpodplacementconfig_webhook.go`

### Add a new label or annotation

1. Add constant to `pkg/utils/const.go` (follow existing `multiarch.openshift.io/` prefix pattern)
2. Update usages in `internal/controller/podplacement/pod_model.go`
3. Update RBAC markers if the label affects cluster-scoped resources

### Modify pod placement logic

1. Core logic lives in `internal/controller/podplacement/pod_model.go`
2. Image inspection in `pkg/image/inspector.go`
3. Webhook gate logic in `scheduling_gate_mutating_webhook.go`
4. Test with the fluent builders in `pkg/testing/builder/`

### Update operator-deployed resources

1. Modify resource construction in `clusterpodplacementconfig_controller.go` (`reconcile()`, lines 746-863)
2. Resources are applied via `utils.ApplyResources()` — SSA with owner references
3. Update RBAC markers (comment annotations above the reconciler) for any new resource types
4. Run `make manifests` to regenerate RBAC

### Update vendored dependencies

```bash
# Edit go.mod, then:
make vendor       # Runs hack/go-mod.sh — tidies and vendors
make verify-diff  # Ensure vendor changes are committed
```

**Vendoring is mandatory** — `GOFLAGS=-mod=vendor` is exported in the Makefile (line 111-112).

## Bundle and Catalog

```bash
make bundle VERSION=<ver>         # Generate OLM bundle manifests
make bundle-verify                # Verify bundle generation is deterministic
make bundle-build BUNDLE_IMG=<img> # Build bundle image
make bundle-push BUNDLE_IMG=<img>  # Push bundle image
make catalog-build CATALOG_IMG=<img> # Build catalog image
make catalog-push CATALOG_IMG=<img>  # Push catalog image
```

**Bundle verify** (`make bundle-verify`) regenerates the bundle and runs `verify-diff` to ensure deterministic output. It preserves `createdAt` timestamps in CSV files (Makefile lines 346-361).

## Deployment

```bash
make install      # Install CRDs into cluster
make deploy IMG=<img>  # Deploy operator
make undeploy     # Remove operator
make uninstall    # Remove CRDs
```

After deploying, create the singleton CR to activate pod placement:

```yaml
apiVersion: multiarch.openshift.io/v1beta1
kind: ClusterPodPlacementConfig
metadata:
  name: cluster
spec:
  logVerbosityLevel: Normal
  namespaceSelector:
    matchExpressions:
      - key: multiarch.openshift.io/exclude-pod-placement
        operator: DoesNotExist
```

## Environment Configuration

Create a `.env` file (see `dotenv.example`):

| Variable | Effect |
|----------|--------|
| `NO_DOCKER=1` | Run builds/tests locally (not in container) |
| `FORCE_DOCKER=1` | Force Docker instead of Podman |
| `BUILD_IMAGE=<img>` | Override builder image (default: `ubi9/go-toolset:1.26`) |
| `RUNTIME_IMAGE=<img>` | Override runtime base image (default: `ubi9/ubi-minimal:latest`) |

## Key Makefile Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `VERSION` | `1.3.4` | Operator version |
| `IMAGE_TAG_BASE` | `registry.ci.openshift.org/origin/multiarch-tuning-operator` | Image registry base |
| `GOLINT_VERSION` | `v2.12.2` | golangci-lint version |
| `CONTROLLER_TOOLS_VERSION` | `v0.21.0` | controller-gen version |
| `OPERATOR_SDK_VERSION` | `v1.42.3` | operator-sdk version |
| `KUSTOMIZE_VERSION` | `v5.8.1` | kustomize version |

## Metrics and Debugging

All components expose Prometheus metrics at `:8080/metrics`. See [docs/metrics.md](../docs/metrics.md) for the full metric list and example queries.

**Health probes:** `:8081/healthz` (liveness), `:8081/readyz` (readiness).

**TLS certs directory:** `/var/run/manager/tls` (`cmd/main.go:351`).

## Common Mistakes

1. **Forgetting `make manifests` after RBAC marker changes** — markers in controller comments generate `config/rbac/role.yaml`
2. **Editing CRDs in `bundle/` directly** — always edit API types in `api/v1beta1/`, then `make manifests && make bundle`
3. **Assuming `openshift-*` namespaces are excluded** — they require explicit `namespaceSelector` configuration
4. **Running tests without gpgme** — image inspection tests fail without `gpgme-devel` installed (or use `NO_DOCKER=0` to run in container)
5. **Using `--depth 1` clone** — vendoring and CI require full git history

## OCP Release Process

See [docs/ocp-release.md](../docs/ocp-release.md) for the complete downstream release workflow including Konflux integration, FBC publishing, and development stream branching.

## Platform Documentation

For generic OpenShift development conventions: [openshift/enhancements](https://github.com/openshift/enhancements) — `dev-guide/`, `guidelines/`, `CONVENTIONS.md`

## SME Review Recommended

- Konflux pipeline configuration specifics for this operator
- Non-OCP deployment flow (MTO-0003 Phase 1 in progress; see [docs/support-non-okd.md](../docs/support-non-okd.md))
