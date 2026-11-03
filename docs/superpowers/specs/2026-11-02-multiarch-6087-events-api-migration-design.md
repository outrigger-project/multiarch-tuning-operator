# MULTIARCH-6087: native Kubernetes events API migration

Status: design draft for approval; application changes and test execution are not part of this document-writing task.

## Intent and agreed decisions

Migrate the pod-placement controller, pod-placement webhook, ENoExec event controller, their shared models, and affected tests from `k8s.io/client-go/tools/record` to `k8s.io/client-go/tools/events`.

The supplied MULTIARCH-6087 description is the requirements source. PR #264 is explicitly excluded. The user approved using the native broadcaster rather than `EventBroadcasterAdapter`: the vendored adapter interface and the native broadcaster's old `StartRecordingToSink` method are themselves deprecated. Use `EventRecorderLogger`, `NewBroadcaster`, and `StartRecordingToSinkWithContext` instead.

## Goals

- Remove first-party `tools/record` dependencies and the associated `GetEventRecorderFor` calls in the migrated components and tests.
- Publish real `events.k8s.io/v1.Event` objects with explicit semantic actions.
- Preserve each existing event's reason, type, message text, regarding-object identity, namespace, and emission conditions.
- Own broadcaster startup, cancellation, and shutdown explicitly; webhook event jobs must not outlive the recorder they use.
- Update runtime operand RBAC and tests so successful compilation is not mistaken for successful event publication.
- Replace legacy fake-recorder string assertions with native-broadcaster event assertions.
- Remove only deprecation suppressions made obsolete by this migration.

## Scope boundaries

The operator controller's `github.com/openshift/library-go/pkg/operator/events.Recorder` is a different interface. Retain it and the core-event permissions it still needs. Do not broaden the migration into library-go, controller-runtime internals, leader-election recorders, dependency updates, or application behavior changes. Do not migrate `fake.NewSimpleClientset` merely because an unrelated comment also references MULTIARCH-6087. Do not change ENoExec error-label/retry behavior or add previously nonexistent event emissions.

## Repository inventory

| Area | Files / change |
|---|---|
| Production wiring | `cmd/main.go`: three operand setup functions currently call `mgr.GetEventRecorderFor`; webhook pool cleanup uses FIFO `postFuncs` |
| Shared pod wrapper | `pkg/models/pod.go`: recorder field, constructor, accessor, `PublishEvent` |
| Pod-placement controller | `internal/controller/podplacement/pod_reconciler.go`, `pod_model.go`, `cel_integration.go`: recorder injection and every `PublishEvent` call, including CEL paths |
| Webhook | `internal/controller/podplacement/scheduling_gate_mutating_webhook.go`: recorder field/constructor and delayed post-admission gate event |
| ENoExec | `internal/controller/enoexecevent/handler/enoexecevent_controller.go`, `enoexecevent_model.go`: recorder injection, wrappers, pod event |
| Runtime RBAC | `internal/controller/operator/podplacement_objects.go`: controller and webhook event rules; `enoexecevent_objects.go`: handler event rule |
| Generated/operator RBAC | Existing RBAC markers, `config/rbac/role.yaml`, `bundle/manifests/multiarch-tuning-operator.clusterserviceversion.yaml`, and generation sources as needed |
| Shared assertions | `pkg/testing/framework/events.go`, `pkg/testing/framework/pod.go`: core event queries and object-UID matching |
| Unit-style tests | `pkg/models/pod_test.go`; pod-placement `pod_model_test.go`, `cel_evaluator_test.go`, `cel_webhook_handle_test.go`, `cel_controller_integration_test.go`, `regression_test.go` |
| envtest suites | Pod-placement and ENoExec `suite_test.go`; associated controller tests |
| E2E assertions | `pkg/e2e/operator/pod_placement_config_test.go` and callers of shared event helpers, including `pkg/e2e/podplacement` |

The implementer must search all callers again before editing; this inventory reflects the current checkout. Application APIs are unchanged, so CRD/deepcopy edits are not expected. Never hand-edit generated deepcopy files.

## Recorder and publication contract

Use the import alias `clientgoevents "k8s.io/client-go/tools/events"` consistently to distinguish it from API event types and library-go events.

Every migrated recorder field, constructor parameter, and accessor returns or accepts `clientgoevents.EventRecorderLogger`. Keep recorder-nil no-op behavior; retain the additional nil-pod guard in `ENoExecEvent.PublishEventOnPod`.

The shared publication methods gain an explicit action argument:

```go
func (pod *Pod) PublishEvent(eventType, reason, action, message string)
func (e *ENoExecEvent) PublishEvent(eventType, reason, action, message string)
func (e *ENoExecEvent) PublishEventOnPod(pod *corev1.Pod, eventType, reason, action, message string)
```

At the boundary, publish with:

```go
recorder.WithLogger(log.FromContext(ctx)).Eventf(object, nil, eventType, reason, action, "%s", message)
```

Use `"%s", message` rather than treating existing messages as format strings: image errors and CEL expressions can contain literal percent signs. `related` is nil for the existing emissions; this migration does not invent secondary-object associations. The delayed webhook event continues to refer to the persisted pod obtained from the API, with its UID, not the admission object.

## Action constants and exact mapping

Add named constants in `pkg/utils/const.go`, following the repository's central-constants convention. Values are stable UpperCamelCase action strings; call sites select actions explicitly, not via a reason-to-action fallback.

| Existing event / condition | New action value |
|---|---|
| `ArchAwareSchedGateAdded` (webhook) | `SchedulingGateAdd` |
| `ArchAwareSchedGateRemovalSuccess`, `ArchAwareSchedGateRemovalFailed` | `SchedulingGateRemove` |
| `ArchAwareGatedPodIgnored` (gate removal because pod no longer eligible) | `SchedulingGateRemove` |
| `ArchAwarePredicateSet` when required/preferred affinity is applied or partly applied | `NodeAffinityUpdate` |
| `ArchAwarePredicateSet` with `ArchitecturePreferredPredicateSkippedMsg` | `NodeAffinitySkip` |
| `ArchAwarePreferredAffinityDuplicates`, `ArchAwarePredicatesConflict` | `NodeAffinitySkip` |
| `ArchAwareFallbackPredicateSet` | `NodeAffinityUpdate` |
| `NoSupportedArchitecturesFound` | `ImageArchitectureCheck` |
| `ArchAwareInspectionError`, including max-retry failure | `ImageInspectionFailed` |
| `CELEvaluationError` | `CELEvaluation` |
| `CELArchitecturePlacementApplied`, `CELArchitecturePlacementFallback` | `NodeAffinityUpdate` |
| ENoExec pod event, reason `ExecFormatError` | `ExecFormatErrorReport` |

Use identifiers `EventActionSchedulingGateAdd`, `EventActionSchedulingGateRemove`, `EventActionNodeAffinityUpdate`, `EventActionNodeAffinitySkip`, `EventActionImageArchitectureCheck`, `EventActionImageInspectionFailed`, `EventActionCELEvaluation`, and `EventActionExecFormatErrorReport`.

The ENoExec wrapper methods currently have no production callers emitting events on the CR. Migrate their signatures and test action forwarding; do not add new CR events or extra action constants without a real caller.

## Broadcaster construction and ownership

Use one small shared constructor in `pkg/events/recorder.go`:

```go
func NewRecorder(
    ctx context.Context,
    client kubernetes.Interface,
    scheme *runtime.Scheme,
    reportingController string,
) (clientgoevents.EventRecorderLogger, func(), error)
```

It constructs `NewBroadcaster(&clientgoevents.EventSinkImpl{Interface: client.EventsV1()})`, starts it with `StartRecordingToSinkWithContext`, obtains the logger-aware recorder with `NewRecorder(scheme, reportingController)`, and returns an idempotent cleanup function that cancels the owned recording context and calls `Shutdown`. A startup failure performs cleanup and returns an error. Do not use `context.TODO`, an unowned never-canceled context, or deprecated adapter/factory methods.

The caller owns the parent lifetime. A fresh process/suite recording context, with the manager logger attached, must stay alive until event producers are quiescent. It must not be the admission-request context or be canceled automatically before webhook jobs have drained.

Production reporting-controller names remain the existing values: `utils.OperatorName` for the pod-placement controller/webhook, and `utils.EnoexecControllerName` for ENoExec. Native client-go supplies reporting-instance identity. Do not promise identical event names, timestamps, deprecated source fields, or duplicate-event aggregation internals.

### Shutdown sequence

1. Stop accepting webhook requests / stop the manager and wait for its producers to stop.
2. Close webhook event-job admission and drain the worker pool using the existing `ReleaseTimeout(30 * time.Second)` policy while its recorder remains available. On timeout, cancel outstanding event jobs and join them before recorder cleanup; report the timeout rather than claiming all delayed events were published.
3. Cancel each recording context and call broadcaster cleanup exactly once (cleanup is idempotent).
4. In envtest, stop the API server only after recorder cleanup.

Add a webhook-owned event-job context and an explicit `ShutdownEventJobs(timeout time.Duration) error` boundary. Each admitted job is tracked; failed submissions release their tracking slot. Closing admission and registering a job must be synchronized so shutdown cannot race a late `WaitGroup.Add`. Jobs derive their existing two-minute timeout from that owned context and use `wait.ExponentialBackoffWithContext`, including the canceled context in API reads. Shutdown first stops admissions and attempts the normal pool drain; after a drain timeout it cancels outstanding work, joins all admitted jobs, and returns the timeout. On a successful drain it also releases the owned context. No job can publish after this boundary returns; do not stop the recorder on timeout while jobs still run. The queued event is best effort and may be abandoned on shutdown cancellation.

Preserve FIFO `postFuncs` ordering if that mechanism is retained: `ShutdownEventJobs(30 * time.Second)` must be registered before recorder cleanup, replacing the current pool-only callback. `mgr.Start` errors must flow through cleanup before `must`/`os.Exit`; setup failures after a broadcaster is created must also release it. The implementer may use a small error-returning setup/runner boundary to accomplish this, rather than restructuring unrelated modes. Event delivery remains asynchronous and best effort; shutdown is not a transactional flush guarantee.

## RBAC and deployment artifacts

For all three operand event-writer rules, grant `create`, `patch`, and `update` on `events` in API group `events.k8s.io`. The native sink can create, patch, and update events. Keep unrelated pod/configuration permissions intact.

Review operator-generated-role escalation/ownership permissions and add corresponding kubebuilder markers for the new event group so `make manifests` and `make bundle` produce the permissions needed to manage operand roles. Retain core-event permissions where library-go or leader election still requires them; only remove a core-event permission when its particular operand no longer has a consumer. No blanket replacement of every resource named `events`.

Test the three runtime role builders independently of generated operator RBAC. Admin-backed envtest alone cannot demonstrate operand service-account permissions.

## Test migration and observable compatibility

Use native broadcasters in both isolated tests and envtest; do not introduce `EventBroadcasterAdapter`, `tools/events.FakeRecorder`, or a new legacy recorder shim.

For isolated tests add `pkg/testing/framework/event_recorder.go` providing:

```go
func NewTestEventRecorder(
    ctx context.Context,
    scheme *runtime.Scheme,
    reportingController string,
) (clientgoevents.EventRecorderLogger, <-chan *eventsv1.Event, func(), error)
```

Use a native broadcaster with `StartEventWatcher` to capture deep-copied typed events. Do not start sink recording with a nil sink. Register watcher/broadcaster cancellation with `t.Cleanup` or `DeferCleanup`. Capturing callbacks must unblock on cleanup; do not silently drop buffered events or close a channel while a callback can still write to it. Use actual object names, namespaces, and UIDs in event fixtures.

Native publication is asynchronous. Replace immediate nonblocking drains of `FakeRecorder.Events` with bounded eventual assertions on structured events. Assert `Regarding`, `Related`, `Type`, `Reason`, `Action`, `Note`, and reporting-controller identity; do not assert the old fake's string serialization or generated event names. Repeated observations may be represented by event series, so exact object counts are valid only for isolated, single-emission scenarios.

For envtest, create native API-backed broadcasters with the same production constructor and clean them up after manager/worker completion and before `testEnv.Stop`. Query `clientset.EventsV1().Events(namespace)` or `eventsv1.EventList`; match `Regarding.UID`/name and `Note` rather than `InvolvedObject` and `Message`. Update `GetEventsForObject` and all callers together. Prefer namespace-scoped listing plus explicit regarding-object filtering over assuming the legacy `involvedObject.name` field selector remains valid on the new endpoint.

Keep one compatibility assertion that a real event emitted through events/v1 remains visible through the API server's core/v1 conversion used by existing `kubectl describe`/external readers. Its fields are compared semantically; do not retain the core API as the primary assertion path for migrated code.

## Verification requirements for the later implementation

- Recorder/model tests: normal and warning events, exact action mapping, percent signs retained literally, nil recorders, nil pod, regarding namespace/UID and related=nil.
- Broadcaster lifecycle tests: startup failure cleanup, idempotent cleanup, cancellation terminates watchers, manager-start failure cleans up, webhook producer-before-recorder shutdown order, timeout cancels backoff/API reads and joins jobs, failed submission releases tracking, and admission/shutdown race cannot add jobs after joining begins.
- Controller/webhook tests: gate addition/removal and removal error, required/preferred/fallback affinity, duplicate/skip/conflict, inspection errors/max retries, CEL match/fallback/error, ENoExec pod warning and unchanged labels/retries.
- RBAC tests: all three operand role builders authorize `events.k8s.io/events` create/patch/update; unrelated rules remain intact. Validate actual publication with deployed operand credentials where a cluster is available.
- Update generated artifacts using real repository targets: `make vendor` if dependency import changes alter vendor contents, `make manifests`, `make generate`, and `make bundle`; review the full generated diff.
- Run applicable formatting/import checks, vet, `make lint`, `make build`, `make unit`, and E2E when a cluster is available. Preserve required CGO/gpgme and envtest setup.
- Search first-party source for `tools/record`, `GetEventRecorderFor`, `record.NewFakeRecorder`, and obsolete MULTIARCH-6087 events-API suppressions. Vendored/internal dependencies and unrelated ticket comments are not migration failures.
- Report every unavailable or failed check explicitly. This planning task runs no tests and makes no claim that the migration is implemented.

## Decisions left to the builder

Choose narrowly scoped test names and fixture organization, a suitable bounded test-channel capacity, and the smallest error-returning setup boundary that guarantees cleanup. Event semantics, type choice, reporter names, native broadcaster choice, RBAC group/verbs, and the scope exclusions above are fixed for the plan.
