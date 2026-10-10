# Architecture

## Repository Layout

```text
cmd/
├── main.go                    # Entrypoint — mode flags, manager setup, scheme registration
└── enoexec-daemon/            # Standalone eBPF daemon binary (runs on each node)
api/
├── common/                    # Shared constants (plugin names) and plugin types
│   └── plugins/               # NodeAffinityScoring, CelArchitecturePlacement types
├── v1alpha1/                  # Legacy API with conversion webhook — DO NOT add new fields here
└── v1beta1/                   # Current storage version: CPPC, PPC, ENoExecEvent types + webhooks
internal/controller/
├── operator/                  # Operator mode: deploys/manages all operand resources
├── podplacement/              # Operand mode: pod reconciler, webhook, pod model, metrics
├── podplacementconfig/        # PodPlacementConfig namespace-scoped controller
└── enoexecevent/              # ENoExecEvent handler controller
    └── handler/               # Reconciles ENoExecEvent CRs produced by the eBPF daemon
pkg/
├── image/                     # Container image inspection — registry auth, manifest parsing, caching
├── informers/                 # CPPC singleton informer (runtime config sync for operand modes)
├── testing/                   # Test framework utilities, fluent builders, fake registry
├── utils/                     # Shared constants, labels, resource apply helpers
└── e2e/                       # E2E test suites (require deployed operator + cluster)
config/                        # Kustomize bases — CRDs, RBAC, webhook, samples, prometheus
bundle/                        # OLM bundle manifests (CSV, CRDs) — DO NOT edit CRDs here; run `make manifests`
deploy/                        # Deployment manifests and kustomize overlays
hack/                          # Build scripts, CI helpers, upgrade automation
docs/                          # Metrics reference, alert runbooks, enhancements, OCP release process
```

## Key Domain Concepts

**Scheduling Gate Pattern:** MTO leverages Kubernetes scheduling readiness (KEP-3521) and mutable scheduling directives (KEP-3838). The webhook adds a scheduling gate (`multiarch.openshift.io/scheduling-gate`) to new pods, preventing the scheduler from placing them. The controller inspects container images to determine supported CPU architectures, patches `nodeAffinity` for `kubernetes.io/arch`, then removes the gate.

**Singleton Operator:** `ClusterPodPlacementConfig` (CPPC) is a cluster-scoped singleton — the operator controller only reconciles the name `"cluster"` (`internal/controller/operator/clusterpodplacementconfig_controller.go:169`, `api/common/const.go:3`). A CPPC with a different name would be accepted by the API but never reconciled. It controls the lifecycle of the pod placement operand (controller + webhook deployments).

**Namespace-scoped Config:** `PodPlacementConfig` (PPC) allows per-namespace pod placement rules with label selectors and priority-based precedence. PPCs enable plugins (NodeAffinityScoring, CEL rules) at namespace granularity.

**Plugin System:** Three registered plugins (`api/common/const.go:8-14`):
- `NodeAffinityScoringPluginName` (0): Preferred (soft) architecture affinity based on weights
- `ExecFormatErrorMonitorPluginName` (1): eBPF ENOEXEC monitoring
- `CelArchitecturePlacementPluginName` (2): CEL expression-based required affinity

**End-to-End Flow:**
1. User creates CPPC CR → Operator deploys controller + webhook + RBAC
2. Webhook adds scheduling gate to new pods (except excluded namespaces/pods)
3. Controller watches gated pods (`status.phase=Pending` field selector)
4. Controller inspects container images via `pkg/image/inspector.go`
5. Architecture intersection computed across all containers (`pod_model.go:358-421`)
6. Required nodeAffinity set for `kubernetes.io/arch` + optional preferred affinity
7. Scheduling gate removed → scheduler places pod on compatible node

## Component Details

**Framework:** controller-runtime (`sigs.k8s.io/controller-runtime`) for reconciliation, caching, webhook handling. library-go (`github.com/openshift/library-go/pkg/operator/events`) for OpenShift-style event recording. `containers/image/v5` for registry interaction.

### Execution Modes

Four mutually exclusive modes set via flags in `cmd/main.go:345-360`:

| Mode | Flag | Leader Election ID | Purpose |
|------|------|--------------------|---------|
| Operator | `--enable-operator` | `operator-208d7abd.multiarch.openshift.io` | Deploy/manage operands |
| PPC Controllers | `--enable-ppc-controllers` | `ppc-controllers-208d7abd.multiarch.openshift.io` | Reconcile gated pods |
| PPC Webhook | `--enable-ppc-webhook` | *(none — stateless)* | Add scheduling gates |
| ENoExec Controllers | `--enable-enoexec-event-controllers` | `enoexecevent-controllers-208d7abd.multiarch.openshift.io` | Process eBPF events |

### Operator Controller (`internal/controller/operator/`)

- Reconciles CPPC singleton; only fetches the name `"cluster"` — other names are never reconciled (`clusterpodplacementconfig_controller.go:169`)
- Uses `APIReader` fallback for cache consistency (`clusterpodplacementconfig_controller.go:174`)
- Manages three finalizers:
  - `finalizers.multiarch.openshift.io/pod-placement` — main cleanup
  - `finalizers.multiarch.openshift.io/no-pod-placement-config` — blocks deletion if PPCs exist
  - `finalizers.multiarch.openshift.io/enoexec-events` — ENoExecEvent cleanup
- Ordered deletion: waits for webhook service deletion, then waits for pods to be ungated, then removes controller (`handleDelete()`, lines 395-583)
- SCC selection based on K8s version (`getCorrectHostmountAnyUIDSCC()`, lines 321-352):
  - K8s < 1.30: `hostmount-anyuid`
  - K8s 1.30-1.31: `privileged` with `spc_t` SELinux (MCO regression workaround)
  - K8s >= 1.32: `hostmount-anyuid-v2` with `spc_t` SELinux

### Pod Reconciler (`internal/controller/podplacement/pod_reconciler.go`)

- **Concurrency:** `NumCPU() * 4` max concurrent reconciles (line 344) — I/O-bound image inspection
- **Cache optimization:** Field selector `status.phase=Pending` configured in `main.go:123-127`
- Watches PodPlacementConfigs — re-queues gated pods on PPC create/delete to handle informer cache lag (lines 364-367)
- Max retry count for image inspection: **5** (`pod_model.go:47`)
- Falls back to `fallbackArchitecture` after max retries if configured

### Mutating Webhook (`internal/controller/podplacement/scheduling_gate_mutating_webhook.go`)

- Adds `multiarch.openshift.io/scheduling-gate` to new pods
- Uses ants worker pool: 16 workers for event publishing (`cmd/main.go:264-265`) with exponential backoff (`scheduling_gate_mutating_webhook.go:164-172`)
- CEL architecture constraints applied in webhook (not controller) because Kubernetes rejects post-persistence NodeSelectorTerm mutations (OPENSHIFTP-636, lines 201-209)
- Informer cache fallback: `getCacheWithAPIFallback()` uses APIReader when cache not yet synced

## Resource Management

All operand resources applied via `utils.ApplyResources()` using server-side apply with owner references. Automatic cleanup via finalizers and controller reference chain.

| Controller | Apply Method | Code Reference |
|-----------|-------------|----------------|
| Operator | `utils.ApplyResources()` (SSA) | `clusterpodplacementconfig_controller.go:847` |
| Pod Reconciler | `r.Client.Update()` (pod patch) | `pod_reconciler.go:104-121` |
| Webhook | In-memory mutation (admission response) | `scheduling_gate_mutating_webhook.go:137-142` |
| ENoExec Handler | `r.Client.Update()` (pod label) + `r.Client.Delete()` (event cleanup) | `enoexecevent_controller.go:165-168` |

## Error Classification

| Error Type | Requeue Behavior | Status Effect |
|-----------|-----------------|---------------|
| Image inspection failure | Requeue with backoff, max 5 retries | `image-inspect-error` label + annotation |
| Max retries exceeded | Uses `fallbackArchitecture` if set; else pod stays gated | `fallback-arch` label if fallback used |
| Pod not found | No requeue (deleted) | None |
| Namespace excluded | No requeue (ignored) | None |
| CPPC not found | Operator recreates if deleted | `Degraded` condition |

## OpenShift Integration Points

| Integration | How | Code Reference |
|------------|-----|----------------|
| Global pull secret | Synced from `openshift-config/pull-secret` | `cmd/main.go:353-354` |
| Registry certificates | ConfigMap `image-registry-certificates` | `cmd/main.go:355` |
| Cluster monitoring | ServiceMonitor + `openshift.io/cluster-monitoring` label | Operator controller reconcile |
| Proxy support | `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY` propagated to operands | Operator controller |
| OLM lifecycle | CSV with `spec.replaces`, `relatedImages`, install modes | `bundle/manifests/` |
| SCC management | Version-aware SCC selection for enoexec daemon | `getCorrectHostmountAnyUIDSCC()` |
| Status conditions | `Available`, `Progressing`, `Degraded`, `Deprovisioning` | `api/v1beta1/clusterpodplacementconfig_types.go:78-109` |

## Generated Code Inventory

| Path | Generator | Regenerate Command |
|------|----------|-------------------|
| `api/*/zz_generated.deepcopy.go` | controller-gen | `make generate` |
| `config/crd/bases/` | controller-gen | `make manifests` |
| `config/rbac/role.yaml` | controller-gen (from RBAC markers) | `make manifests` |
| `config/webhook/` | controller-gen | `make manifests` |
| `bundle/manifests/` | operator-sdk | `make bundle` |

**NEVER hand-edit generated files.** Edit source annotations/markers, then run the appropriate `make` target.

## API Behavioral Contracts

- **Singleton constraint:** Only one CPPC named `"cluster"` is reconciled; the operator controller hardcodes this name (`api/common/const.go:3`) — other names are accepted by the API but ignored
- **API versions:** `v1alpha1` (legacy, conversion webhook) → `v1beta1` (current hub/storage version)
- **Pod ignore rules** (`shouldIgnorePod()` at `pod_model.go:471-578`):
  - **Hardcoded:** operator namespace, `kube-*` prefixed namespaces, pods with `spec.nodeName`, control-plane nodeSelector, DaemonSet-owned pods
  - **Not hardcoded:** `openshift-*`, `hypershift-*` — must use `namespaceSelector` on CPPC
- **Architecture intersection:** All containers in a pod must share at least one architecture; nodeAffinity uses the intersection set
- **Preferred affinity tracking:** Annotation `multiarch.openshift.io/preferred-affinity-sources` records `arch:weight:source[:skipped]` for audit
- **Max retry count:** 5 retries for image inspection before fallback (`pod_model.go:47`)
- **Webhook CEL evaluation:** CEL rules evaluated in webhook (not controller) due to Kubernetes NodeSelectorTerm immutability post-persistence

## Labels and Annotations

| Key | Values | Purpose |
|-----|--------|---------|
| `multiarch.openshift.io/scheduling-gate` | `gated`/`removed` | Scheduling gate status |
| `multiarch.openshift.io/node-affinity` | `set`/`overriden` | Required affinity applied (note: `overriden` spelling is intentional per code review) |
| `multiarch.openshift.io/preferred-node-affinity` | `set` | Preferred affinity applied |
| `multiarch.openshift.io/single-arch` | `<arch>` | Pod supports one architecture |
| `multiarch.openshift.io/multi-arch` | *(present)* | Pod supports multiple architectures |
| `multiarch.openshift.io/image-inspect-error` | *(present)* | Image inspection failed |
| `multiarch.openshift.io/fallback-arch` | `<arch>` | Fallback architecture used |
| `multiarch.openshift.io/operand` | *(present)* | Marks operand resources |

## Status Conditions

| Condition | Meaning |
|-----------|---------|
| `Available` | All operand components have ≥1 ready replica |
| `Progressing` | Components are rolling out |
| `Degraded` | No replicas available, not in deprovisioning |
| `Deprovisioning` | CR deletion in progress |
| `PodPlacementControllerNotRolledOut` | Controller deployment not ready or not up-to-date |
| `PodPlacementWebhookNotRolledOut` | Webhook deployment not ready or not up-to-date |
| `MutatingWebhookConfigurationNotAvailable` | Webhook configuration missing |

## Deployment Topology

| Component | Replicas | HA Model |
|-----------|----------|----------|
| Operator | 2 | Active-passive (leader election) |
| Pod Placement Controller | 2 | Active-passive (leader election) |
| Pod Placement Webhook | 3 | Active-active (stateless) |
| ENoExec Daemon | DaemonSet | One per node |
| ENoExec Handler | 2 | Active-passive (leader election) |

## Design References

**Scheduling Gate Architecture:** The scheduling gate pattern was chosen over mutating admission (which would block on image inspection latency) to decouple webhook response time from image inspection. The webhook adds the gate in microseconds; the controller processes asynchronously with high concurrency (`NumCPU * 4`). See [MTO-0001](../docs/enhancements/MTO-0001.md).

**CEL in Webhook, Not Controller:** Kubernetes rejects post-persistence mutations to `NodeSelectorTerms` (only additions are allowed). CEL architecture rules must therefore be applied in the webhook before the pod is persisted. This is documented in `scheduling_gate_mutating_webhook.go:201-209` referencing OPENSHIFTP-636.

**Ordered Deletion:** When CPPC is deleted, the operator waits for the webhook to be removed first, then waits for all gated pods to be processed before deleting the controller. This prevents pods from being permanently stuck in `SchedulingGated` state. See `handleDelete()` at `clusterpodplacementconfig_controller.go:395-583`.

## Platform Documentation

For generic OpenShift development conventions, see:
- [openshift/enhancements](https://github.com/openshift/enhancements): `dev-guide/` for development conventions, `guidelines/` for enhancement process, `CONVENTIONS.md` for coding standards

## SME Review Recommended

- Plugin priority conflict resolution when multiple PPCs match the same pod
- Exact image caching invalidation behavior (TODO noted in `pkg/image/inspector.go` for ICSP/IDMS/ITMS watch)
- Non-OCP deployment configuration details (Phase 1 of MTO-0003 is in progress)
