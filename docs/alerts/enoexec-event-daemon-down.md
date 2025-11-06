# ExecFormatErrorDaemonDown

## Meaning

The `ExecFormatErrorDaemonDown` alert is triggered when the Exec Format Error Daemon is not running in some nodes.

## Impact

Any pods causing ENoExec will not be detected in the nodes where the daemon is not running.
This alert might be temporarily due to a new node joining the cluster when joining take longer than expected.

## Diagnosis

### enoexec-event-daemon DaemonSet

Check the status of the DaemonSet.

```shell
kubectl describe DaemonSet -n openshift-multiarch-tuning-operator enoexec-event-daemon
```

Check the events and the status of the nodes missing a running instance of the DaemonSet. 

### Mitigation

No mitigation. This error should be temporary and depend on other components external to the Multiarch Tuning Operator.
