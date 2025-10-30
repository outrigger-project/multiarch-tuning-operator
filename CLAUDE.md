# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

The Multiarch Tuning Operator enhances operational experience within multi-architecture clusters and single-architecture clusters migrating to multi-architecture compute configurations. It provides architecture-aware scheduling of workloads by automatically adding node affinity requirements based on container image architectures.

This is a Kubernetes operator built with Kubebuilder and operator-sdk, primarily targeting OpenShift clusters.

## Development Commands

### Building

```bash
# Local build (requires CGO_ENABLED=1, gpgme-devel/libgpgme-dev)
make build

# Single-arch container image
make docker-build IMG=<registry>/multiarch-tuning-operator:tag

# Multi-arch container image (requires qemu-user-static)
make docker-buildx IMG=<registry>/multiarch-tuning-operator:tag

# To preserve buildx instance between builds
touch .persistent-buildx
```

### Testing

```bash
# Run all tests (includes manifests, generate, fmt, vet, goimports, gosec, lint, unit)
make test

# Unit tests only
make unit

# E2E tests (requires deployed operator)
KUBECONFIG=/path/to/kubeconfig NAMESPACE=openshift-multiarch-tuning-operator make e2e

# Individual checks
make lint        # golangci-lint
make gosec       # SAST security scanning
make vet         # go vet
make goimports   # goimports check
make fmt         # gofmt
```

**Running tests locally vs containerized:**
- By default, tests run in a containerized environment using BUILD_IMAGE
- To run locally: `NO_DOCKER=1 make test`
- Or add `NO_DOCKER=1` to a `.env` file (see dotenv.example)

**Running a single test:**
```bash
# Set GINKGO_ARGS to target specific tests
GINKGO_ARGS="-v --focus='your test pattern'" make unit
```

### Deployment

```bash
# Deploy operator to cluster
make deploy IMG=<registry>/multiarch-tuning-operator:tag

# Undeploy operator
make undeploy

# Install CRDs only
make install

# Uninstall CRDs
make uninstall
```

### API Changes

```bash
# Generate manifests (CRDs, RBAC, etc.) after editing API definitions
make manifests

# Generate DeepCopy implementations
make generate
```

### Bundle Operations

```bash
# Generate operator bundle
make bundle VERSION=x.y.z

# Verify bundle generation
make bundle-verify

# Build and push bundle
make bundle-build BUNDLE_IMG=<registry>/bundle:tag
make bundle-push BUNDLE_IMG=<registry>/bundle:tag
```

## Architecture

### Binary Modes

The operator runs in different modes controlled by flags (see cmd/main-binary/main.go:286-312):

1. **Operator mode** (`--enable-operator`): Manages ClusterPodPlacementConfig CR and deploys operands
2. **Pod Placement Controllers** (`--enable-ppc-controllers`): Reconciles pods with scheduling gates
3. **Pod Placement Webhook** (`--enable-ppc-webhook`): Mutating webhook that adds scheduling gates to pods
4. **ENoExecEvent Controllers** (`--enable-enoexec-event-controllers`): Monitors exec format errors via eBPF

Only one mode can be active at a time. Each mode has its own leader election ID.

### Core Components

**Operator Controller** (controllers/operator/):
- Reconciles ClusterPodPlacementConfig singleton CR (name must be "cluster")
- Deploys/manages pod placement operands (controllers, webhook, RBAC, etc.)
- Handles ordered deletion to ensure pods are ungated before operand removal
- Creates ServiceMonitor for metrics scraping

**Pod Placement Operand** (controllers/podplacement/):
- **PodReconciler**: Watches pods with scheduling gate, inspects container images, adds architecture-based nodeAffinity
- **PodSchedulingGateMutatingWebHook**: Adds `multiarch.openshift.io/scheduling-gate` to new pods
- **GlobalPullSecretSyncer**: Syncs pull secrets for image inspection

**ENoExecEvent System** (controllers/enoexecevent/):
- **Daemon** (cmd/enoexec-daemon/): eBPF-based monitoring of exec format errors on nodes
- **Handler Controller**: Processes ENoExecEvent CRs created by the daemon

### Key Workflows

**Pod Placement Flow:**
1. Webhook adds scheduling gate to pod, preventing scheduling
2. PodReconciler watches gated pods
3. Inspects container images to determine supported architectures
4. Adds nodeAffinity requirement for kubernetes.io/arch
5. Removes scheduling gate, allowing scheduler to place pod

**Image Inspection** (pkg/image/):
- Supports multi-arch manifests (OCI, Docker v2.2)
- Handles pull secrets and registry certificates
- Uses caching to optimize repeated inspections
- Metrics tracking for inspection operations

### API Versions

- **v1alpha1**: Original API version with conversion webhook
- **v1beta1**: Current stable API (hub version for conversions)
- Conversion webhooks in apis/multiarch/v1beta1/clusterpodplacementconfig_webhook.go

**ClusterPodPlacementConfig** is a singleton cluster-scoped resource. Only one instance named "cluster" is allowed.

### Namespace Exclusions

System namespaces are excluded from pod placement by default (cannot be overridden):
- `openshift-*`
- `kube-*`
- `hypershift-*`

Additional namespaces can be excluded via namespaceSelector with label `multiarch.openshift.io/exclude-pod-placement`.

### Testing Infrastructure

**Unit Tests:**
- Located alongside source files (*_test.go)
- Use Ginkgo/Gomega framework
- envtest provides fake API server
- Test helpers in pkg/testing/builder/ (fluent builders for K8s objects)
- Test framework utilities in pkg/testing/framework/

**E2E Tests:**
- Located in pkg/e2e/
- Require deployed operator in cluster
- Separate test suites for operator and pod placement

## Environment Variables

Create a `.env` file in the repository root to customize build settings:
- `NO_DOCKER=1`: Run builds/tests locally instead of in container
- `FORCE_DOCKER=1`: Force Docker instead of Podman
- `BUILD_IMAGE`: Override builder image
- `RUNTIME_IMAGE`: Override runtime base image

## Important Implementation Notes

### Image Inspection Dependencies

Image inspection requires the containers/image library which has CGO dependencies on gpgme. This means:
- Local builds require gpgme-devel (RHEL/Fedora) or libgpgme-dev (Debian)
- CGO_ENABLED=1 is required
- Multi-arch builds use platform-specific builder images with these dependencies

### Vendoring

This project uses Go vendoring (`GOFLAGS=-mod=vendor`). After modifying dependencies:
```bash
make vendor
```

### Metrics

The operator exposes Prometheus metrics on port 8080 (secured with authentication/authorization). Key metrics include:
- Pod placement controller performance
- Image inspection operations
- Webhook operations
- ENoExecEvent monitoring

See docs/metrics.md for full metric definitions.

### Konflux/CI Integration

The repository includes .tekton/ pipeline definitions for Konflux CI/CD. Bundle Dockerfiles have konflux-specific variants (bundle.konflux.Dockerfile).
