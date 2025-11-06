---
title: introducing-an-ebpf-based-monitoring-solution-for-enoexec
authors:
  - "@aleskandro"
reviewers:
  - "@AnnaZivkovic"
  - "@jeffdyoung"
  - "@lwan-wanglin"
  - "@mweckbecker"
  - "@Prashanth684"
  - "@prb112"
approvers:
  - "@Prashanth684"
creation-date: 2025-03-27
last-updated: 2025-03-27
tracking-link:
  - https://issues.redhat.com/browse/MULTIARCH-5010
see-also: []
---

# Introducing an eBPF-based monitoring solution for ENOEXEC (aka Exec Format Error)

## Release Signoff Checklist

- [x] Enhancement is `implementable`
- [x] Design details are appropriately documented from clear requirements
- [x] Test plan is defined
- [x] Graduation criteria for dev preview, tech preview, GA
- [ ] User-facing documentation is created in [openshift-docs](https://github.com/openshift/openshift-docs/)

## Summary

The Multiarch Tuning Operator automatically configures pod node affinities to ensure workloads are scheduled on nodes with compatible CPU architectures based on the architectures supported by the images they use.

However, issues can still arise in two key scenarios: when a node selector explicitly sets an incompatible architecture, or when pods attempt to run binaries that aren't supported by the node's architecture. In such cases, the pod may fail entirely and enter a `CrashLoopBackOff`, or specific processes within the pod may crash silently, failing with an `ENOEXEC`[^1] error, without causing the pod itself to restart. The last case can lead to subtle, harder-to-diagnose failures where the pod appears healthy but essential functionality is broken.

These failures become particularly problematic during the migration of production-grade clusters to a multi-architecture setup, where heterogeneous nodes (e.g., x86_64 and arm64) coexist.

This enhancement proposes an eBPF-based monitoring system that can detect `ENOEXEC` errors. Upon detection, it will emit Kubernetes events, raise alerts, and expose metrics to the monitoring stack. This enables administrators to quickly identify and diagnose architecture compatibility issues without digging through logs, facilitating smoother migrations and providing continuous observability for future workloads.

### User Stories

- As a cluster administrator, I want to be notified when an `ENOEXEC` error occurs during an `execve` syscall, whether it's during pod startup or when the primary process attempts to execute a binary incompatible with the node's architecture so that I can quickly identify and investigate the issue.
- As a cluster administrator migrating from a single-architecture to a multi-architecture cluster, I want visibility into pods affected by `ENOEXEC` errors to detect architecture mismatches early and take corrective actions to ensure a smooth migration.

### Goals

- Introduce a new plugin, `execFormatErrorMonitor`, in the `ClusterPodPlacementConfig` Custom Resource Definition (CRD) to enable an eBPF-based monitoring that detects `ENOEXEC` errors on nodes and emits alerts and Kubernetes events in the affected pod's event stream.
- Integrate `ENOEXEC` error reporting into the cluster’s monitoring stack to support visibility and observability at scale.
- Enable administrators to identify and troubleshoot pods failing due to `ENOEXEC` errors.

### Non-Goals

- Automatically resolving the root cause of `ENOEXEC` errors.
- Detecting the architecture of the binary that triggered the `ENOEXEC` error and modifying the pod's node affinity or scheduling behavior in response.

## Proposal

We introduce a new plugin, `execFormatErrorMonitor`, in the cluster-scoped `ClusterPodPlacementConfig` CRD, along with a new custom resource definition, `ENoExecEvent`.

The plugin enables the operator to deploy a producers-consumer architecture composed of the following components:

1. **`enoexec-event-daemon`**: A `DaemonSet` that runs the eBPF program on each node and produces the event as an `ENoExecEvent` object.
2. **`enoexec-event-handler`**: A `Deployment` that watches for `ENoExecEvent` objects and performs the following actions:
	- Records the error as a warning in the affected pod’s event log.
	- Increments a Prometheus counter to track occurrences of `ENOEXEC` errors over time.
	- Deletes the processed `ENoExecEvent` object.
3. **Alerting rule**: A Prometheus `AlertRule` that triggers an alert when the current counter value deviates from the value recorded in the past 24 hours, signaling a potential spike in architecture-related failures.

## Architecture

<img src="https://raw.githubusercontent.com/openshift/multiarch-tuning-operator/462030e3619b9d75ebfb7e3df6d725ce7a10e275/docs/enhancements/enoexec-monitoring-architecture.svg" alt="multiarch-tuning-operator enoexec monitoring architecture" width="100%" style="max-width: 60vw;display:block"/>

The architecture of the `execFormatErrorMonitor` plugin - illustrated in Figure 1 - follows a leader-elected, single-consumer model within a producer-consumer pattern. It uses the Kubernetes API and the underlying key-value store (etcd) as the communication layer between producers and the consumer.

Producers are the `enoexec-event-daemon` instances, deployed as a `DaemonSet` running on each node. These daemons monitor for `ENOEXEC` errors and create `ENoExecEvent` custom resources when such errors are detected on their respective nodes.

The consumer is a leader-elected instance of the `enoexec-event-handler` `Deployment`. It watches for `ENoExecEvent` objects and, upon receiving one, performs the following actions:
1. Record the error as a warning in the affected pod's event log.
2. Increments a Prometheus counter to track the number of `ENOEXEC` errors.
3. Label the pod with the label `multiarch.openshift.io/exec-format-error=true`
4. Deletes the processed `ENoExecEvent` object to prevent reprocessing.

This design ensures reliable, centralized handling of `ENOEXEC` errors while supporting distributed lightweight and scalable detection across all nodes.

### ENoExecEvent definition

`ENoExecEvent` is a new CRD used to record `ENOEXEC` events detected by the `enoexec-event-daemon` `DaemonSet`. The CRD will have the following structure in YAML format:

```yaml
apiVersion: multiarch.openshift.io/v1beta1
kind: ENoExecEvent
metadata:
  name: <uuid>
  namespace: <multiarch-tuning-operator-namespace>
spec:
status:
  nodeName: <node-name>
  podName: <pod-name>
  podNamespace: <pod-namespace>
  containerID: <container-id>
  command: <command>
```

`ENoExecEvent` resources are internal objects not intended for direct user interaction. In Operator Lifecycle Manager (OLM) deployments, the `ClusterServiceVersion` will be annotated to hide the `ENoExecEvent` CRD from the user interface:

```yaml
  operators.operatorframework.io/internal-objects: '["enoexecevents.multiarch.openshift.io"]'
```

### enoexec-event-daemon

The `enoexec-event-daemon` operates on each node within the cluster to monitor for local `ENOEXEC` errors. Upon detecting such an error, it generates a corresponding `ENoExecEvent` object.

This daemon utilizes a dedicated service account, bound to a namespace-scoped role, which grants it permissions solely to create and retrieve `ENoExecEvent` objects within the namespace housing the Multiarch Tuning Operator.

For every detected `ENOEXEC` error, the daemon instantiates a new `ENoExecEvent` object in the operator's namespace. The object's name is generated as a Universally Unique Identifier (UUID) in compliance with ISO/IEC 9834-8[^2] standards adopted in Kubernetes, ensuring global uniqueness. This object encapsulates pertinent details, including the node name, pod name, container ID, and the command that triggered the error.


### Structure of the Payload for the Ring Buffer

Each event recorded in the BPF ring buffer[^3] adheres to a fixed-size, 24-byte payload structure. This design ensures efficient and consistent event data transmission from the kernel to user space. 

A ring buffer event payload is formatted as follows:

```text
  0                   1                   2                   3
  0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
 +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
 |                 real_parent->tgid (4 bytes)                   |
 +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
 |                   current->tgid (4 bytes)                     |
 +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
 |                                                               |
 |                   current_comm (16 bytes)                     |
 |                                                               |
 +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

where:

- **real_parent->tgid**: A 4-byte field representing the _Thread Group ID_ (TGID) of the parent process.
- **current->tgid**: A 4-byte field indicating the TGID of the process causing `ENOEXEC`.
- **current_comm**: A 16-byte field containing the command name of the current process.

### enoexec-event-handler

The `enoexec-event-handler` is a Deployment that watches `ENoExecEvent` custom resources and performs the following actions:

1. Record the event in the corresponding pod's event log.
2. Increments a Prometheus counter to track occurrences.
3. Label the pod with the label `multiarch.openshift.io/exec-format-error=true`
4. Deletes the processed `ENoExecEvent` object to prevent reprocessing.

## Changes to the ClusterPodPlacementConfig CRD

The `execFormatErrorMonitor` plugin will be introduced as part of the `ClusterPodPlacementConfig` custom resource. It will be disabled by default and will initially expose only the `enabled` field, inherited from the base plugin definition.

```yaml
apiVersion: multiarch.openshift.io/v1beta1
kind: ClusterPodPlacementConfig
metadata:
  name: my-namespace-config
spec:
  # [...]
  plugins:
    execFormatErrorMonitor: ## New plugin
      enabled: true
  # [...]
```

## Changes to the Multiarch Tuning Operator

The Multiarch Tuning Operator will be extended to support the new `execFormatErrorMonitor` plugin defined in the `ClusterPodPlacementConfig` custom resource. When this plugin is enabled, the operator will be responsible for deploying the `enoexec-event-daemon` DaemonSet, the `enoexec-event-handler` Deployment, and the related RBAC resources described in the [Architecture](#architecture) and [RBAC](#rbac) sections.

## Changes to the Pod Placement Controller

None

## Changes to the Mutating Webhook

None

## RBAC

The `enoexec-event-daemon` DaemonSet will run with a service account bound to a namespace-scoped role. This role will grant permissions to create and retrieve `ENoExecEvent` objects within the namespace where the Multiarch Tuning Operator is deployed.

The `enoexec-event-handler` Deployment will use a separate service account with permissions to read, list, and delete `ENoExecEvent` objects in the same namespace. In addition, it will require permission to patch pods across the cluster in order to record the corresponding events in their event logs.

### enoexec-event-daemon


| Resource                             | Methods           | comments |
|--------------------------------------|-------------------|----------|
| enoexecevents.multiarch.openshift.io | create, get, list |          |

##### enoexec-event-handler

| Resource                             | Methods                  | comments         |
|--------------------------------------|--------------------------|------------------|
| pods                                 | list, watch, get, update | (cluster-scoped) | 
| events                               | create, patch            | (cluster-scoped) |
| enoexecevents.multiarch.openshift.io | delete, get, list, watch |                  |


#### Considerations for Uninstallation

No changes are required to the existing uninstallation process. Users are still advised to delete all Multiarch Tuning Operator resources before uninstalling the operator. If these resources are not removed beforehand, the operands (such as DaemonSets or Deployments) may continue to run, but any changes to the configuration objects will no longer produce the expected behavior.

When the `execFormatErrorMonitor` plugin is disabled, the Multiarch Tuning Operator will automatically clean up all resources associated with the plugin.

### Implementation Details and Design Constraints

- The `enoexec-event-daemon` DaemonSet will be deployed in the same namespace as the Multiarch Tuning Operator. The associated eBPF program must be portable across all supported CPU architectures. To ensure consistent behavior across heterogeneous nodes, special consideration must be given to kernel version compatibility and architecture-specific nuances, such as endianness and syscall conventions.

- The eBPF program executed by `enoexec-event-daemon` is responsible for detecting `ENOEXEC` errors. Upon such an event, the program captures the TGID of the faulting process as well as the TGID of its real parent process. The `enoexec-event-daemon` uses the two TGIDs to identify the associated cgroup and infer the originating pod's UID.

- Because the Kubernetes API does not support direct lookup of pods by their UID[^4], the `enoexec-event-daemon`, which runs on the same node where the `ENOEXEC` event is observed, must utilize the Container Runtime Interface (CRI) to resolve the pod name and namespace from the pod UID. It then constructs an `ENoExecEvent` custom resource containing the pod and node names.

- The `enoexec-event-handler` is responsible for consuming `ENoExecEvent` resources and emitting the corresponding Kubernetes event within the affected pod's event log. This ensures visibility of execution failures at the workload level via standard Kubernetes observability mechanisms.

- To enhance the granularity of event reporting, the `enoexec-event-handler` may extract the container ID from the `ENoExecEvent` resource and correlate it with the container statuses listed in the pod's `status` field. This allows the identification of the specific container instance affected by the `ENOEXEC` condition.

- The binary's name that triggered the `ENOEXEC` error is retrieved in the eBPF program using the `bpf_get_current_comm()`[^5] helper. While this value does not contain the absolute path, it provides a process name (up to 16 bytes) that can aid in identifying the executable responsible for the failure.

- The `enoexec-event-handler` may retrieve information such as the node architecture or the ownerReferences of an affected pod to provide additional context for the event. This information can be used to enrich the event data and improve the user experience.

- The `counter` metrics exposed by the `enoexec-event-handler` will be labeled with the pod name, namespace, and container ID. This allows for detailed tracking of `ENOEXEC` errors down to the container level, enabling administrators to monitor and analyze the frequency and distribution of these errors across the cluster.

### Risks and Mitigations

- The process responsible for triggering the `ENOEXEC` condition may terminate before the `enoexec-event-daemon` is able to resolve its pod metadata (e.g., name and namespace) at user-space. In such cases, the daemon will fall back to inspecting the `real_parent` task to attempt resolution via the associated cgroup. If this resolution also fails in user space, due to missing process context or unresolvable cgroup mappings, the `podName` field in the resulting `ENoExecEvent` object will be set to the empty string (`""`). The `enoexec-event-handler` will ignore such incomplete events to prevent false attribution. It will expose a dedicated metric to the monitoring stack to track the number of events that could not be attributed to a pod, allowing for observability and alerting on signal loss due to rapid process termination or resolution failures.

- The `enoexec-event-daemon` requires elevated privileges to operate, as it must attach to tracepoints and interact with BPF-related kernel subsystems. Specifically, the pod must be granted the following Linux capabilities: `CAP_BPF`, `CAP_PERFMON`, and `CAP_SYS_RESOURCE`. Furthermore, non-root users are restricted from attaching to most tracepoints due to the default `perf_event_paranoid=2`[^6] setting on CoreOS kernels. As a result, the container executing the eBPF program must run with root privileges. This is a known and accepted risk within the current design constraints. To mitigate the security impact of running a privileged container, we may adopt a split-container model: a dedicated, minimal sidecar container, running as root, would be responsible solely for loading and executing the eBPF program and capturing relevant event data. A unidirectional FIFO pipe would be used to transmit this data to the main container, which runs as a non-root user and is responsible for interacting with the Kubernetes API to create the corresponding `ENoExecEvent` objects. The sidecar container may be based on a minimal image that does not allow shell execution via rsh. This separation-of-privilege approach aims to reduce the attack surface while maintaining functionality and observability. The feasibility and trade-offs of this design will be further assessed during the implementation phase.

- The `enoexec-event-daemon` can flood the Kubernetes API with `ENoExecEvent` objects if the system is under heavy load or if a large number of pods are affected by `ENOEXEC` errors. To mitigate this risk, we will implement throttling through the Token Bucket implementation by `golang.org/x/time/rate` to delay the creation of `ENoExecEvent` objects. The eBPF ring buffer will also have a fixed size and events will be dropped if the buffer is full. This will help prevent overwhelming the API server with excessive requests and ensure that the system remains responsive. Finally, a `ResourceQuota` object may be created in the Multiarch Tuning Operator namespace to limit the number of `ENoExecEvent` objects that can be created. This will help prevent resource exhaustion in etcd and ensure that the system remains responsive.
 Parameters of the queue network will be chosen at implementation time, by numerical simulations, real-world testing or by resolving the network of queues.

### Drawbacks

Introducing the `ENoExecEvent` custom resource definition (CRD) adds complexity to the Multiarch Tuning Operator and its supporting components. It also centralizes the handling of `ENOEXEC` errors, which may not be relevant for all users or workloads. Nevertheless, this trade-off is justified by the enhanced observability, structured event correlation, and alerting capabilities it enables.
  
In large-scale deployments spanning many nodes, the architecture is intentionally designed to minimize resource consumption at the node level. Rather than requiring each instance of the `enoexec-event-daemon` to expose metrics endpoints to be scraped by Prometheus or perform redundant computation, the system captures only the minimal necessary context in the kernel. It relays the information to the user space, where it is completed and sent to the leader-elected consumer responsible for processing these events. This approach reduces duplication and lowers the per-node overhead and the overall resource footprint of the solution.

### Open Questions

- Is it actually possible to retrieve the name of the process' command that fails to run?

### Test Plan

#### Unit Testing and Integration Test Suites

- Unit Testing: Test each new function, method, and feature in isolation to ensure correctness, reliability, and
  robustness. Ensure that all new code paths are adequately covered and that the code behaves as expected under various conditions.
- Integration Test Suite: Run integration tests against a simulated control plane using the operator SDK's envtest
  facilities. We will add the necessary test cases to ensure the reconciliation loop of the new `ENoExecEvent` CRD is correctly handled

#### Functional Test Suite

- Multi-Architecture Testing: Conduct functional tests across all architectures supported by the Multiarch Tuning Operator, specifically x86_64, aarch64, ppc64le, and s390x. Focus on the `enoexec-event-daemon` DaemonSet to ensure compatibility and correct behavior of the eBPF program across different architectures.
  
- Kernel Version and Endianness: Validate that the eBPF program operates correctly across various kernel versions and handles architecture-specific nuances such as endianness. This is crucial to ensure consistent behavior and reliability across diverse environments.

- Resource Utilization and Performance: Monitor and assess the resource consumption of the `enoexec-event-daemon` across different architectures to identify performance bottlenecks or anomalies.

### Graduation Criteria

The `ENoExecEvent` CRD will be introduced as `v1beta1`.

### Upgrade / Downgrade Strategy

No special upgrade or downgrade procedures are required for this enhancement. Users will upgrade to a new version of the Multiarch Tuning Operator that includes support for the `execFormatErrorMonitor` plugin. Since the plugin is disabled by default, existing configurations will not change behavior unless explicitly updated.

### Operational Aspects of API Extensions

The new `execFormatErrorMonitor` plugin field in the `ClusterPodPlacementConfig` spec will be marked as `omitempty`. As a result, it will not appear in serialized CRD instances unless explicitly set by the user.

The `enabled` field defaults to `false`, ensuring the plugin remains disabled unless explicitly activated. Upgrading the operator from an earlier version will not alter the behavior of existing configurations. The operator will activate the plugin only if the user explicitly includes it in their configuration.


#### Failure Modes

- **Race Conditions Between Kernel and User Space**: A rare but possible failure mode exists where the pod or its processes are deleted between the moment an `ENOEXEC` event is captured in kernel space and the time the user-space daemon processes it. In this case, the `enoexec-event-daemon` may be unable to resolve the pod UID. When this occurs, the `podName` field in the `ENoExecEvent` resource is set to an empty string (`""`).

- **Fallback Behavior**: The `enoexec-event-handler` will detect and ignore events lacking a valid `podName`. These events are excluded from the pod’s event log to prevent confusion or misattribution.

- **Observability**: A dedicated Prometheus metric will be exposed to track such failures, enabling cluster administrators to monitor the frequency of signal loss due to transient lifecycle conditions.

## Documentation Plan

A new section will be added to the Multiarch Tuning Operator documentation to explain the new plugin and its configuration.

## Implementation History

- Started
- 2025/07/21: The initial implementation will not include the command name. It's still unsure if we can get this information in a reliable way for an already-dead process.

## Infrastructure Needed

## Alternatives

- Statically inspecting the pod's entrypoint and command line arguments to identify potential `ENOEXEC` errors. This approach is less reliable, as it may not account for all possible execution paths or runtime conditions that could lead to an `ENOEXEC` error.

## References

[^1]: https://github.com/torvalds/linux/blob/1a1d569a75f3ab2923cb62daf356d102e4df2b86/include/uapi/asm-generic/errno-base.h#L12
[^2]: https://www.iso.org/obp/ui/#iso:std:iso-iec:9834:-8:ed-3:v1:en
[^3]: https://www.kernel.org/doc/html/latest/bpf/ringbuf.html
[^4]: https://github.com/kubernetes/kubernetes/issues/20572
[^5]: https://docs.ebpf.io/linux/helper-function/bpf_get_current_comm/
[^6]: https://man7.org/linux/man-pages/man2/perf_event_open.2.html
[^7]: https://issues.redhat.com/browse/MULTIARCH-5416
[^8]: https://issues.redhat.com/browse/MULTIARCH-5420
[^9]: https://issues.redhat.com/browse/MULTIARCH-5417
[^10]: https://issues.redhat.com/browse/MULTIARCH-5419
[^11]: https://issues.redhat.com/browse/MULTIARCH-5421
[^12]: https://issues.redhat.com/browse/MULTIARCH-5418
[^13]: https://issues.redhat.com/browse/MULTIARCH-5422
[^14]: https://issues.redhat.com/browse/MULTIARCH-5423
