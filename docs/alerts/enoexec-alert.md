# ExecFormatErrorsDetected

## Meaning

The `ExecFormatErrorsDetected` alert fires when the Exec Format Error plugin is enabled and has detected **exec format errors** in one or more pods during the last 6 hours.

## Impact

Affected pods are trying to execute a binary that is not built for the host node’s CPU architecture.

**Possible causes include:**

1. **Incorrect scheduling** – The pod lacks the right node affinity and is scheduled onto an unsupported architecture. In this case, the entrypoint often fails and the pod enters `CrashLoopBackOff`.
2. **Wrong image metadata** – The image manifest reports an incorrect architecture (e.g., published as `arm64` but actually built for `amd64`).
3. **Incorrect binary in the image** – A build step (e.g., a `RUN` layer in the Dockerfile) downloads or installs a binary for the wrong architecture, regardless of the target.
4. **Binary injected at runtime** – A binary is introduced dynamically, for example through a `PersistentVolume`, `ImageVolume`, or by being downloaded into ephemeral storage by the entrypoint or application logic.

## Diagnosis

1. **List recent events** reported by the Multiarch Tuning Operator:
   
```bash
kubectl get events --field-selector involvedObject.kind=Pod,reportingController=multiarch-tuning-operator
```

2. **Inspect and check the logs of the affected pods**:
	* Verify if node affinity is set.
		* If not, check whether the pod should have been patched automatically by the Pod Placement Operand, or whether it is excluded via `ClusterPodPlacementConfig`.
		* If exclusion is intentional, manually configure node affinity to restrict scheduling to supported architectures.
		* If node affinity should have been added automatically, review Pod Placement Operand documentation to confirm cluster and registry configurations are correct.
	* If the pod is on a supported architecture but still fails (e.g., in `CrashLoopBackOff`), check container logs to identify which binary is causing the error.
	* If the issue comes from a binary bundled at runtime, update the pod or its owning resource to avoid introducing incompatible binaries.
	* If the issue originates from the image itself, contact the image provider and request a corrected build.

## Mitigation

While working on a permanent fix, you may mitigate by **forcing the pod to run on a node with a CPU architecture compatible with both the problematic binary and the rest of the containers**.
