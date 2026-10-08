# Enhancement Proposals

## Local Enhancements

| ID | Title | Status | Link |
|----|-------|--------|------|
| MTO-0001 | Multiarch Manager Operator | Implemented (GA) | [docs/enhancements/MTO-0001.md](../docs/enhancements/MTO-0001.md) |
| MTO-0002 | Namespace-scoped PodPlacementConfig | In Progress | [docs/enhancements/MTO-0002-local-pod-placement.md](../docs/enhancements/MTO-0002-local-pod-placement.md) |
| MTO-0003 | Non-OCP Kubernetes Support | Provisional (Phase 1) | [docs/enhancements/MTO-0003-support-non-ocp-clusters.md](../docs/enhancements/MTO-0003-support-non-ocp-clusters.md) |
| MTO-0004 | eBPF-based ENOEXEC Monitoring | Implementable | [docs/enhancements/MTO-0004-enoexec-monitoring.md](../docs/enhancements/MTO-0004-enoexec-monitoring.md) |
| MTO-0005 | CEL Architecture Placement Plugin | Design | [docs/enhancements/MTO-0005-architecture-rules-plugin.md](../docs/enhancements/MTO-0005-architecture-rules-plugin.md) |

## OpenShift Enhancement

| Title | Status | Link |
|-------|--------|------|
| Multiarch Manager Operator | Implemented | [openshift/enhancements — multi-arch/multiarch-manager-operator.md](https://github.com/openshift/enhancements/blob/master/enhancements/multi-arch/multiarch-manager-operator.md) |

## Upstream KEPs

| KEP | Title | Relevance |
|-----|-------|-----------|
| KEP-3521 | Pod Scheduling Readiness | Core dependency — scheduling gate mechanism used by MTO |
| KEP-3838 | Pod Mutable Scheduling Directives | Allows controller to modify nodeAffinity after pod creation |

## Enhancement Summaries

### MTO-0001: Core Operator (GA)

Defines the foundational architecture: webhook adds scheduling gates, controller inspects images, patches nodeAffinity, removes gate. Deployment model: operator (2 replicas, active-passive), webhook (3 replicas, active-active), controller (2 replicas, active-passive). Tracking: MIXEDARCH-215.

### MTO-0002: Namespace-scoped PodPlacementConfig (In Progress)

Introduces `PodPlacementConfig` CRD for per-namespace pod placement rules with label selectors and priority-based precedence (0-255). Phase 1: single cluster-wide controller with namespace config awareness. Phase 2 (planned): per-namespace sharding with local scheduling gates. Tracking: MULTIARCH-4252.

### MTO-0003: Non-OCP Support (Provisional)

Enables MTO on standard Kubernetes clusters (starting with CRI-O). Adds `GlobalPullSecretRef` and `CABundleConfigmapRef` fields to remove hardcoded OpenShift dependencies. Phase 1: CRI-O support with cert-manager TLS. Phase 2 (planned): containerd/Docker runtime support. Tracking: MULTIARCH-5324. See also [docs/support-non-okd.md](../docs/support-non-okd.md).

### MTO-0004: ENOEXEC Monitoring (Implementable)

eBPF-based monitoring for exec format errors. Producer-consumer architecture: `enoexec-event-daemon` (DaemonSet) detects errors via eBPF ring buffer, creates `ENoExecEvent` CRs; `enoexec-event-handler` (Deployment) processes events, labels pods, publishes warnings. Plugin-based opt-in via CPPC. Tracking: MULTIARCH-5010.

### MTO-0005: CEL Architecture Placement (Design)

CEL expression-based architecture rules for `PodPlacementConfig`. Rules evaluated against pod metadata only (no spec/status); first match wins with fallback architectures. Constraints: 1000 rules max, 4 architectures per rule, namespace-scoped only. Applied in webhook (not controller) due to Kubernetes NodeSelectorTerm immutability. Tracking: design phase.
