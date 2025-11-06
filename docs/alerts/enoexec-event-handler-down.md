# ExecFormatErrorHandlerDown

## Meaning

The `ExecFormatErrorHandlerDown` alert is triggered when the Exec Format Error Controller has no ready replicas to reconcile
the ENoExecEventObjects.

## Impact

ENoExecEventObjects created by the Exec Format Error DaemonSet's instances will not be reconciled and broadcasted as events
in the related pods.

## Diagnosis

### enoexec-event-handler Deployment

Check the status of the Deployment and inspect the logs.

```shell
kubectl describe deployment -n openshift-multiarch-tuning-operator enoexec-event-handler
kubectl logs -n openshift-multiarch-tuning-operator enoexec-event-handler
```

### Mitigation

If the error persists, it is suggested to disable the plugin to avoid flooding the Kube API and the data store with
ENoExecEvent objects that cannot be handled.
