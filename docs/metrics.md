# Metrics

All the components expose the default metrics of the controller-runtime project.

Refer to https://book.kubebuilder.io/reference/metrics-reference.

It is responsibility of the user to install the monitoring operator and configure the monitoring stack to be able to scrape the metrics 
from the multi-arch tuning operator components.

In Openshift, when deploying through the OperatorHub or the Operator Lifecycle Manager, the monitoring stack is automatically installed and configured.
Users can enable the monitoring of the multi-arch tuning operator by labeling the namespace where the operator is deployed with `openshift.io/cluster-monitoring: "true"`.
If installing from the UI, the users can opt-in to enable monitoring in the recommended namespace `openshift-multiarch-tuning-operator` via the available checkbox at installation time.

## Pod Placement Operand

The following metrics are exposed by the Pod Placement Operand:

| Metric                                            | Type      | Controller               | Description                                                                                                     |
|---------------------------------------------------|-----------|--------------------------|-----------------------------------------------------------------------------------------------------------------|
| `mto_ppo_ctrl_time_to_process_pod_seconds`        | Histogram | pod placement controller | The time taken to process any pod.                                                                              |
| `mto_ppo_ctrl_time_to_process_gated_pod_seconds`  | Histogram | pod placement controller | The time taken to process a pod that is gated (includes inspection).                                            |
| `mto_ppo_ctrl_time_to_inspect_image_seconds`      | Histogram | pod placement controller | The time taken to inspect an image (it may include the time to retrieve the info from a cache).                 |
| `mto_ppo_ctrl_time_to_inspect_pod_images_seconds` | Histogram | pod placement controller | The time taken to inspect all the images in a pod (it may include the time to retrieve this info from a cache). |
| `mto_ppo_ctrl_processed_pods_total`               | Counter   | pod placement controller | The total number of pods processed by the pod placement controller that had a scheduling gate                   |
| `mto_ppo_ctrl_failed_image_inspection_total`      | Counter   | pod placement controller | The total number of image inspections that failed.                                                              |
| `mto_ppo_pods_gated`                              | Gauge     | controller and webhook   | The current number of gated pods (this metric is not considered reliable yet). It should converge to 0.         |
| `mto_ppo_wh_pods_processed_total`                 | Counter   | mutating webhook         | The total number of pods processed by the webhook.                                                              |
| `mto_ppo_wh_pods_gated_total`                     | Counter   | mutating webhook         | The total number of pods gated by the webhook.                                                                  |
| `mto_ppo_wh_response_time_seconds`                | Histogram | mutating webhook         | The response time of the webhook.                                                                               |

## Exec Format Error Operand

The following metrics are exposed by the Exec Format Error Operand:

| Metric                      | Type    | Controller      | Description                                                                                  |
|-----------------------------|---------|-----------------|----------------------------------------------------------------------------------------------|
| `mto_enoexecevents`         | Counter | enoexec handler | The total number of exec format error detected and reported                                  |
| `mto_enoexecevents_invalid` | Counter | enoexec handler | The counter for ENoExecEvents objects that faled the reconciliation and report as pod events |


## Example queries

The following queries can be used to monitor the pod placement operand:

```sql
-- Memory usage
sum(container_memory_rss{namespace='openshift-multiarch-tuning-operator', container=""}) BY (pod)

-- CPU Usage
pod:container_cpu_usage:sum{namespace='openshift-multiarch-tuning-operator'}


-- 50th percentile time to process pods
-- Gated pods
histogram_quantile(0.5, sum by (le) (rate(mto_ppo_ctrl_time_to_process_gated_pod_seconds_bucket[5m])))
-- Any pods in the controller
histogram_quantile(0.5, sum by (le) (rate(mto_ppo_ctrl_time_to_process_pod_seconds_bucket[5m])))
-- Image inspection
histogram_quantile(0.5, sum by (le) (rate(mto_ppo_ctrl_time_to_inspect_image_seconds_bucket[5m])))
-- All images in a pod insection
histogram_quantile(0.5, sum by (le) (rate(mto_ppo_ctrl_time_to_inspect_pod_images_seconds_bucket[5m])))
-- Time to schedule a pod (kubernetes metrics)
histogram_quantile(0.5, sum by (le) (rate(scheduler_pod_scheduling_sli_duration_seconds_bucket[5m])))


-- Total pods processed by the webhooks
sum(mto_ppo_wh_pods_processed_total)
-- Total pods gated by the webhooks
sum(mto_ppo_wh_pods_gated_total)
-- Total gated pods processed by the controller
sum(mto_ppo_ctrl_processed_pods_total)
-- Failed image inspection
sum(mto_ppo_ctrl_failed_image_inspection_total)

-- Current number of gated pods (with the multiarch tuning operator scheduling gate)
sum(mto_ppo_pods_gated)
-- Current number of gated pods (with any scheduling gate)
sum(scheduler_pending_pods{queue="gated"})
sum(scheduler_pending_pods) by (queue)

-- Distribution of the time to inspect an image
sum by(le) (rate(mto_ppo_ctrl_time_to_inspect_pod_images_seconds_bucket[5m]))

-- Rate of increase of exec format errors in the last 6h
irate(mto_enoexecevents_total[6h])

```
