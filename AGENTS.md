# Multiarch Tuning Operator (MTO)

**Repository:** `github.com/openshift/multiarch-tuning-operator`
**Purpose:** Architecture-aware pod scheduling for multi-arch OpenShift/Kubernetes clusters. Inspects container images, adds `kubernetes.io/arch` nodeAffinity, and manages scheduling gates so pods land on compatible nodes.

## Critical Warnings

1. **ClusterPodPlacementConfig must be named `"cluster"`** — singleton enforced by the operator controller, which only reconciles this name (`internal/controller/operator/clusterpodplacementconfig_controller.go:169`)
2. **CGO required** — image inspection uses gpgme via `containers/image`; `CGO_ENABLED=1` and `gpgme-devel` are mandatory
3. **Never hand-edit `zz_generated.deepcopy.go`** — run `make generate` after API changes
4. **Only one execution mode per binary** — `--enable-operator`, `--enable-ppc-controllers`, `--enable-ppc-webhook`, or `--enable-enoexec-event-controllers` are mutually exclusive (`cmd/main.go:345-360`)
5. **`openshift-*` namespaces are NOT hardcoded exclusions** — only `kube-*` and the operator namespace are; configure `namespaceSelector` on the CR or the webhook will gate critical workloads

## Architecture at a Glance

Four mutually exclusive binary modes: **Operator** (deploys operands) → **Webhook** (gates pods) → **Controller** (inspects images, patches affinity, ungates) → **ENoExec** (eBPF monitoring). Uses controller-runtime with library-go event recording.

## Documentation

| Need | Start here |
|------|-----------|
| Architecture, controllers, API contracts | [ai-docs/ARCHITECTURE.md](ai-docs/ARCHITECTURE.md) |
| Build, test, deploy, common tasks | [ai-docs/DEVELOPMENT.md](ai-docs/DEVELOPMENT.md) |
| Test suites, patterns, framework | [ai-docs/TESTING.md](ai-docs/TESTING.md) |
| Enhancement proposals and design docs | [ai-docs/ENHANCEMENTS.md](ai-docs/ENHANCEMENTS.md) |
| Metrics reference | [docs/metrics.md](docs/metrics.md) |
| Alert runbooks | [docs/alerts/](docs/alerts/) |
| OCP release process | [docs/ocp-release.md](docs/ocp-release.md) |
| Review guidelines | [REVIEW.md](REVIEW.md) |

## Key Files

| File | Purpose |
|------|---------|
| `cmd/main.go` | Entrypoint, mode flags, leader election |
| `internal/controller/operator/` | Operator mode: CR lifecycle, operand deployment |
| `internal/controller/podplacement/` | Pod reconciler, webhook, pod model |
| `pkg/image/inspector.go` | Image architecture detection |
| `pkg/utils/const.go` | All labels, annotations, constants |
| `api/v1beta1/` | Current API types and webhooks |
| `config/` | Kustomize manifests (CRDs, RBAC, webhook) |

## External References

- [OpenShift Enhancement Proposal](https://github.com/openshift/enhancements/blob/master/enhancements/multi-arch/multiarch-manager-operator.md)
- [KEP-3521: Pod Scheduling Readiness](https://github.com/kubernetes/enhancements/tree/master/keps/sig-scheduling/3521-pod-scheduling-readiness)
- [Platform Conventions (openshift/enhancements)](https://github.com/openshift/enhancements): `dev-guide/`, `guidelines/`, `CONVENTIONS.md`
