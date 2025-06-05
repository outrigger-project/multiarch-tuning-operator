---
title: Introducing the namespace-scoped `PodPlacementConfig`
authors:
  - "@aleskandro"
reviewers:
  - "@AnnaZivkovic"
  - "@jeffdyoung"
  - "@lwan-wanglin"
  - "@Prashanth684"
  - "@prb112"
approvers:
  - "@Prashanth684"
creation-date: 2025-02-04
last-updated: 2025-02-04
tracking-link:
  - https://issues.redhat.com/browse/MULTIARCH-4252
see-also: []
---

# Introducing the namespace-scoped `PodPlacementConfig`

## Release Signoff Checklist

- [x] Enhancement is `implementable`
- [x] Design details are appropriately documented from clear requirements
- [x] Test plan is defined
- [x] Graduation criteria for dev preview, tech preview, GA
- [ ] User-facing documentation is created in [openshift-docs](https://github.com/openshift/openshift-docs/)

## Summary
Currently, the Multiarch Tuning Operator exposes a cluster-scoped Custom Resource Definition (CRD) that allows users to
(a) enable the pod placement controller, (b) define a namespace selector to filter the namespaces whose pods should be
patched with the scheduling gate at admission time, and (c) have all the matching pods in the selected namespaces patched
with the node selector term for the compatible architectures to influence the pod scheduling's strong predicates run by the 
node affinity filtering plugin.
In [MULTIARCH-4970](https://issues.redhat.com/browse/MULTIARCH-4970), we introduced new fields in the `PodPlacementConfig` that
allow users to define cluster-wide preferences to influence the node affinity scoring plugin with respect to the pod's
preferred architectures. However, users may want to have more granular control over the pod placement controller's behavior
by defining different preferences for different namespaces and sub-set of pods within a namespace.
This enhancement proposes to introduce a namespace-scoped `PodPlacementConfig` CRD that allows users to define the pod placement
controller's behavior at a namespace level. The namespace-scoped `PodPlacementConfig` CRD will include fields similar to the ones defined 
in the current version of the cluster-scoped `PodPlacementConfig` CRD, and label selectors to refine the scope of the configuration
to subset of pods within a namespace matching the label selector.


## Motivation
- Users want to have more granular control over the pod placement controller's behavior by defining different preferences 
for different namespaces and a subset of pods within a namespace, given they are aware some pods can perform better on certain 
architectures or support cost-reduction strategies by running pods on cheaper nodes such as arm64 ones.

### User Stories
- As a cluster administrator, I want to define different preferences for different namespaces and a subset of pods within a namespace
so that I can influence the node affinity scoring plugin with respect to the pod's preferred architectures at a namespace level.

### Goals
- Provide a namespace-scoped resource to define the pod placement controller's behavior at a namespace level for the node affinity 
  scoring plugin
- Provide a namespace-scoped resource to define the pod placement controller's behavior for the node affinity 
  scoring plugin on a subset of pods within a namespace

### Non-Goals
- Allow users to define multiple pod placement configurations that tune the placement of pods across multiple namespaces
- Allow users to define which namespaces should get the node affinity filtering plugin's behavior managed by the cluster-scoped 
  `ClusterPodPlacementConfig` CRD: the cluster-scoped `PodPlacementConfig` CRD will continue to be used for this purpose and will be
  a dependency for the namespace-scoped `PodPlacementConfig` CRD

## Proposal

We introduce a new namespace-scoped `PodPlacementConfig` CRD that allows users to define the pod placement controller's 
behavior at a namespace level.

Its development is expected to ship in the following phases:

- Phase 1 - initial introduction of the namespace-scoped `PodPlacementConfig` CRD:
	- The multiarch tuning operator will refuse the creation of the namespace-scoped `PodPlacementConfig` CRD if the cluster-scoped
	  `ClusterPodPlacementConfig` CRD is not present
	- The pod placement controller deployed when the `ClusterPodPlacementConfig` CRD is present will be able to also apply 
      the configuration to the pods in the selected namespaces, if any namespace-scoped `PodPlacementConfig` CRD is present.
    - The namespace-scoped `PodPlacementConfig` CRD will include fields similar to the ones defined in the current version of the 
	  cluster-scoped `ClusterPodPlacementConfig` CRD, and label selectors to refine the scope of the configuration to subset of pods within 
	  a namespace matching the label selector.
    - The namespace-scoped `PodPlacementConfig` CRD will take precedence over the cluster-scoped `ClusterPodPlacementConfig` CRD when 
	  both configure the same plugins for the same pods.
- Phase 2 - Sharding the pod placement controller into per-namespace deployments:
	- When at least one namespace-scoped `PodPlacementConfig` CRD is present in a given namespace, the multiarch tuning operator
      deploys a local pod placement controller in the namespace that will make use of an additional scheduling gate to patch
      the pods specifications with the node affinity terms defined by the enabled plugins in the namespace-scoped `PodPlacementConfig` CRD.

### Phase 1

#### Namespace-scoped `PodPlacementConfig` CRD

The namespace-scoped `PodPlacementConfig` CRD will include fields similar to the ones defined in the current version of the
cluster-scoped `ClusterPodPlacementConfig` CRD, and label selectors to refine the scope of the configuration to subset of pods within
the namespace.

The following `yaml` snippet shows the structure of the namespace-scoped `PodPlacementConfig` CRD:

```yaml
apiVersion: multiarch.openshift.io/v1beta1
kind: `PodPlacementConfig`
metadata:
  name: my-namespace-config
  namespace: my-namespace
spec:
  labelSelector:
      matchExpressions:
        - key: app
          operator: In
          values:
            - my-label-for-apps-performing-better-on-arm64
  priority: 100
  plugins:
    nodeAffinityScoring:
      enabled: true
      platforms:
        - architecture: amd64
          weight: 25
        - architecture: arm64
          weight: 75
```

The `plugins.nodeAffinityScoring` fields are defined as in the current version of the cluster-scoped `ClusterPodPlacementConfig` CRD,
implemented at https://github.com/openshift/multiarch-tuning-operator/pull/369.

The `priority` field is an integer that defines the priority of the namespace-scoped `PodPlacementConfig` CRD. The higher the value, the higher the priority.
If multiple namespace-scoped `PodPlacementConfig` objects are present in a namespace, the one with the highest priority will take precedence over the others when
a pod matches the label selector of multiple `PodPlacementConfig` objects.

#### Changes to the Multiarch Tuning Operator

A new validating webhook will be added to the multiarch tuning operator to validate the namespace-scoped `PodPlacementConfig`.
In particular, the webhook will deny the creation of the namespace-scoped `PodPlacementConfig` if the cluster-scoped 
`ClusterPodPlacementConfig` CRD is not present.
This validating webhook will also be the base for the multi-version support of the `PodPlacementConfig` CRD.
The `ClusterPodPlacementConfig` validating webhook also has to deny the deletion of the `ClusterPodPlacementConfig` object if there are any `PodPlacementConfig` objects in the cluster.
The webhook is deployed as part of the multiarch tuning operator deployment, similarly to how the current version of the operator 
deploys the validating webhook for the cluster-scoped `ClusterPodPlacementConfig`.

#### Changes to the Pod Placement Controller

In order to apply the configurations defined by the namespace-scoped `PodPlacementConfig` CRD, the pod placement controller will be changed such that it applies any
matching namespace-scoped `PodPlacementConfig` CRD to the pods in the selected namespaces, sorted by descending priority, before applying the cluster-scoped
`ClusterPodPlacementConfig` configuration.

Given a pod hitting the pod reconciliation loop:
1. Verify the pod has the scheduling gate (return if not)
2. Get all the pod placement configurations in the pod's namespace, sorted by descending priority
3. For each pod placement configuration:
	- Check if the pod matches the label selector
	- Apply the configuration to the pod.
    - If a proposed configuration overlaps with the current pod specification, it is not applied and an event
      is recorded in the pod's event log. This ensures that the operator does not overwrite the user's configuration and 
      that a `PodPlacementConfig` CRD with a higher priority gets precedence over the ones with lower priority.
4. Apply the cluster-scoped `ClusterPodPlacementConfig` configuration to the pod. As the namespace-scoped configuration 
   has already been applied, the cluster-scoped configuration will not overwrite any overlapping namespace-scoped configuration.
   We can also garantee that the cluster-scoped `ClusterPodPlacementConfig`'s strong predicates will be applied to the pods as they
   are not handled by the namespace-scoped resource, which is focused on the scheduling scoring plugins.

#### Changes to the Mutating Webhook

No changes are required to the mutating webhook, as the namespace-scoped `PodPlacementConfig` CRD depends on the cluster-scoped `ClusterPodPlacementConfig` CRD and its business logic is implemented by the same controller responsible for reconciling through its scheduling gate.

#### RBAC

We will introduce rules to allow the
multiarch tuning operator and pod placement controller to, respectively, reconcile (CRUD) and read 
the namespace-scoped `PodPlacementConfig` CRD.

#### Considerations about uninstallation

No changes in the current process are required. The users are still recommended to uninstall the Multiarch Tuning Operator resources before proceeding with the uninstallation: in the case such resources are not deleted before the operator is uninstalled, the controllers will continue to be able to reconcile the pods, but changes (creation and deletion in particular) will not guarantee the expected behavior.

### Phase 2: Sharding the pod placement controller into per-namespace instances

In phase 2, we will partition the pod placement controller into per-namespace instances and a cluster-wide one that is mainly responsible for the required affinity. When at least one namespace-scoped `PodPlacementConfig` CRD is present in a given namespace, the multiarch tuning operator will deploy a local pod placement controller and mutating webhook in the namespace that will make use of an additional scheduling gate (`multiarch.openshift.io/local-scheduling-gate`) to patch the pods specifications with the node affinity terms defined by the enabled plugins in the namespace-scoped `PodPlacementConfig` CRD.

#### Changes to the Multiarch Tuning Operator

A reconciler for the namespace-scoped `PodPlacementConfig` CRD will be added to the multiarch tuning operator.

When the first `PodPlacementConfig` is created in a namespace, the multiarch tuning operator will deploy in that namespace:
- a pod placement controller
- a mutating webhook
- a mutating webhook configuration
- service accounts and RBAC rules to allow the pod placement controller to reconcile the pods and the mutating webhook to mutate the pods

When the last `PodPlacementConfig` is deleted in a namespace, the multiarch tuning operator will delete the resources deployed in that namespace.

The Log Verbosity level of the pod placement controller will be set according to the cluster-scoped `ClusterPodPlacementConfig` CRD.

#### Changes to the Pod Placement Controller

The current pod reconciler will have some dedicated flags to allow the pod placement controller to run in a per-namespace mode. The pod placement controller will be able to run in a per-namespace mode by setting the `--namespace` flag to the namespace where the controller is running. The controller will then only reconcile the pods in the namespace where it is running.

The cluster-wide pod placement controller will not process a pod and remove the related scheduling gate (`multiarch.openshift.io/scheduling-gate`) until the local `multiarch.openshift.io/local-scheduling-gate` scheduling gate is removed.

The behavior of the pod placement controller will be the same as in the first phase, but split into two different instances: one for the namespace-scoped configuration, applied first and considering the available `PodPlacementConfig` sorted by descending priority, and the other for the cluster-scoped configuration, applied after the namespace-scoped configuration.

#### Changes to the Mutating Webhook

The Mutating Webhook will be deployed in the namespace where the pod placement controller is running. The Mutating Webhook will be configured to mutate the pods in the namespace where it is running and will add a different scheduling gate to the pods (`multiarch.openshift.io/local-scheduling-gate`).

The Mutating Webhook Configuration will be responsible for instructing the kube-apiserver to call the Mutating Webhook in the namespace that has a `PodPlacementConfig` for the pods created in that namespace.
Each `PodPlacementConfig` will have a Mutating Webhook Configuration rule with the object selector to match the pods in the namespace where the `PodPlacementConfig` is created.

#### Consideration about uninstallation

When the last `PodPlacementConfig` is deleted in a namespace, the multiarch tuning operator will delete the resources deployed in that namespace.
The users are still recommended to uninstall the Multiarch Tuning Operator resources before proceeding with the uninstallation: in the case such resources are not deleted before the operator is uninstalled, the controllers will continue to be able to reconcile the pods, but changes (creation and deletion in particular) are not guaranteed to take effect as expected.

#### RBAC

All the RBAC rules for the local pod placement controller and the local mutating webhook will be created by the multiarch tuning operator when the first `PodPlacementConfig` is created in a namespace. Such rules will include `Role`, `RoleBinding` and `ServiceAccount` resources. No cluster-scoped RBAC rules should be required for the local pod placement controller and the local mutating webhook.

#### Summary of the rules
##### Pod Placement Controller

---------------

| Resource                                         | Methods                         | comments                                                                                                            |
|--------------------------------------------------|---------------------------------|---------------------------------------------------------------------------------------------------------------------|
| pods                                             | get, list, watch, patch, update |                                                                                                                     | 
| pods/status                                      | update                          |                                                                                                                     |
| security.openshift.io/securitycontextconstraints | use                             | it's used in case the pods are created by a user/SA that needs some privileges                                      |
| events                                           | create, patch                   |                                                                                                                     |
| podplacementconfigs.multiarch.openshift.io       | get, list, watch                |                                                                                                                     |
| authorization.openshift.io/subjectaccessreviews  | create                          | automatically created by kubebuilder (required for leader election)                                                 |
| authentication.k8s.io/tokenreviews               | create                          | automatically created by kubebuilder (required for leader election)                                                 |
| configmaps, coordination.k8s.io/leases           | create, get, update, delete     | automatically created by kubebuilder (required for leader election)                                                 |

##### Pod Mutating Webhook

---------------

| Resource                                        | Methods                     | comments                                                                        |
|-------------------------------------------------|-----------------------------|---------------------------------------------------------------------------------|
| pods                                            | list, watch, get            |                                                                                 | 
| pods/status                                     | update                      |                                                                                 |
| events                                          | create, patch               |                                                                                 |
| podplacementconfigs.multiarch.openshift.io      | get, list, watch            |                                                                                 |
| authorization.openshift.io/subjectaccessreviews | create                      | automatically created by kubebuilder (required for leader election)             |
| authentication.k8s.io/tokenreviews              | create                      | automatically created by kubebuilder (required for leader election)             |
| configmaps, coordination.k8s.io/leases          | create, get, update, delete | automatically created by kubebuilder (required for leader election)             |


### Implementation Details/Notes/Constraints

- The priority field is an 8-bit unsigned integer, ranging from 0 to 255. The higher the value, the higher the priority.
- When a Pod Placement Config is created, the operator will validate the priority field to ensure it is within the valid range and that no other Pod Placement Config in the same namespace has the same priority.
- If the `priority` field is omitted, the operator will default to `0`.

### Risks and Mitigations

### Drawbacks

### Open Questions

### Test Plan

#### Unit Testing and Integration Test Suites

- Unit Testing: Test each new function, method, and feature in isolation to ensure correctness, reliability, and
  robustness. Verify that the new code paths are covered by the unit tests and that the code behaves as expected
  under different conditions.
- Integration Test Suite: Run integration tests against a simulated control plane using the operator SDK's envtest
  facilities. We will add the necessary test cases to ensure the reconciliation loop of the new `PodPlacementConfig` is working as expected and that pods are reconciled according to both the cluster-scoped and namespace-scoped configurations in the correct order.

#### Functional Test Suite

##### Phase 1

- The operator should refuse the creation of a namespace-scoped `PodPlacementConfig` CRD if the cluster-scoped `ClusterPodPlacementConfig` CRD is not present.
- The operator should refuse the deletion of the cluster-scoped `ClusterPodPlacementConfig` CRD if there are any namespace-scoped `PodPlacementConfig` CRDs in the cluster.
- The pod placement controller should apply the configuration defined by the namespace-scoped `PodPlacementConfig` CRD to the pods in the selected namespaces, and the cluster-scoped `ClusterPodPlacementConfig` configuration for preferred affinities should be ignored
- The pod placement controller should apply the configuration defined by the highest-priority namespace-scoped `PodPlacementConfig` CRD to pods matching multiple `PodPlacementConfig` CRDs in the same namespace
- No PPC with the same priority should be allowed in the same namespace
- If the `priority` field is omitted, the operator should default to `0`
- The pod placement controller should not process a pod with the configuration of the namespace-scoped `PodPlacementConfig` CRD if the pod does not match its label selector

##### Phase 2
- The operator should deploy a local pod placement controller and mutating webhook in the namespace where the first namespace-scoped `PodPlacementConfig` CRD is created
- The operator should delete the resources deployed in the namespace when the last namespace-scoped `PodPlacementConfig` CRD is deleted
- The local pod placement controller should reconcile the pods in the namespace where it is running
- The cluster pod placement controller should not process a pod and remove the related scheduling gate until the local scheduling gate is removed

### Graduation Criteria

The `PodPlacementConfig` API will be introduced as version v1beta1.


### Upgrade / Downgrade Strategy
- No special upgrade/downgrade strategy is required for this enhancement. The operator will be updated to support the
  new `PodPlacementConfig` API.

### Version Skew Strategy

### Operational Aspects of API Extensions

#### Failure Modes
- Webhook failure - The mutating admission webhook has the "FailPolicy=Ignore"
  setting. The creation or scheduling of pods will not be blocked if the webhook
  is down. However, there would be an event in the pod events log to notify the
  administrator about this condition.
- Operator/controller failure - Any operator/controller failure will be
  localized to the operator namespace and will not affect the other,
  especially core, components. Pods might be in a gated state waiting
  to be scheduled. Once the controller/operator recovers, these pods will be
  evaluated and will proceed to be scheduled. If the operator or controller
  cannot recover, the scheduling gate has to be removed manually by patching the
  pod spec.

#### Support Procedures
- Webhook
	- If the webhook fails to deploy and run for whatever reason, alerts will
	  notify the administrator about the problem.
	- The mutating admission webhook has `FailPolicy=Ignore` and hence will not
	  block the pod from being scheduled if any errors occur when calling the
	  webhook.
	- When the webhook is online, operations will proceed as usual, and pods
	  will start being intercepted and gated depending on the configuration
- Pods are gated, and the controller is down
	- If the webhook has gated certain pods and the controller unexpectedly goes
	  down, pods will be gated till it recovers
	- The scheduling gate can be manually removed from the pods to continue
	  normal operations. Pods that are gated can be identified by their status,
	  which would be "SchedulingGated" instead of "Running" or "Pending"
	- Redeploying the operator if it does not recover should start the
	  controller which would resume processing the gated pods.
	- Information about the local controllers status will be available in all the `PodPlacementConfig` of that namespace.
- Health checking on the controller will throw alerts if the controller cannot
  be reached
- Metrics can also be used to identify faulty behaviors of the controller and
  the webhook

## Documentation Plan

A new section will be added to the Multiarch Tuning Operator documentation to explain the new namespace-scoped `PodPlacementConfig` CRD and how to use it to define the pod placement controller's behavior at a namespace level.


## Implementation History

In Progress:
  - API implementation: https://github.com/openshift/multiarch-tuning-operator/pull/625, MULTIARCH-5365[^1]
Not Started:
  - Validating webhook for CPPC/PPC: MULTIARCH-5366[^2], MULTIARCH-5367[^3], MULTIARCH-5368[^4]
  - Core controller and implementation logic: MULTIARCH-5369[^5], MULTIARCH-5370[^6]
  - Integration and e2e tests: MULTIARCH-5424[^7]
  - Documentation: MULTIARCH-5425[^8]

## Alternatives
- As an alternative to `phase 2`, we may consider sharding the pods reconciliation among different instances of the cluster-wide pod placement controller. In fact, the current implementation of the pod placement controller is inherently stateless and ca be easily scaled horizontally. Howver, Kubernetes, KubeBuilder and OperatorSDK do not currently support deployments of parallel reconcilers in a active-active replicas configuration. This would require a custom implementation of the operator to handle the sharding of the pods reconciliation among different instances of the pod placement controller, leveraging some shared state to assign the pods to different instances (and ignore their processing in others). This might be a more complex solution and would require more development effort, but would allow for a more scalable and fault-tolerant solution. It would be better to consider this alternative if such sharding implementation can be implemented upstream in the OperatorSDK or KubeBuilder projects. It would have value as the cluster scoped resource still needs to be able to access pods in all namespaces, hence no reduction of the RBAC rules is possible in any case.

## Infrastructure Needed

## Open Questions


In phase2, is it necessary to let the pods go through two scheduling gates? Does it duplicate the work done by the controllers?

- Using two scheduling gates enable a basic distributed transaction following the micro-services patterns, mediated by the scheduling gates set in the pod specs (distributed shared state):
	 - The scheduling gate of the namespace-scoped `PodPlacementConfig` will be used for tuning the preferences (and possibly other fields handled by future plugins) first
	 - Then, the scheduling gate of the ClusterPodPlacementConfig will mainly be used by the cluster-wide controller to handle the required affinity, and the global preferences if no PodPlacementConfig handles it already.
     - The controllers of the namespace-scoped `PodPlacementConfig` will not handle the strong predicates, as (a) we want the users willing to tune their architecture-aware pod placement preferences only after the strong predicates are guarateed, and (b) we would need to allow the service accounts in the user namespace to access secrets like the global pull secret, which we might rather prefer to allow only in the core namespace running the cluster-wide controller and operator.

## References

[^1]: https://issues.redhat.com/browse/MULTIARCH-5365
[^2]: https://issues.redhat.com/browse/MULTIARCH-5366
[^3]: https://issues.redhat.com/browse/MULTIARCH-5367
[^4]: https://issues.redhat.com/browse/MULTIARCH-5368
[^5]: https://issues.redhat.com/browse/MULTIARCH-5369
[^6]: https://issues.redhat.com/browse/MULTIARCH-5370
[^7]: https://issues.redhat.com/browse/MULTIARCH-5424
[^8]: https://issues.redhat.com/browse/MULTIARCH-5425
