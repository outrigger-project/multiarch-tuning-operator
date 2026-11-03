# MULTIARCH-6087 Events API Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish the existing operand events through native `events.k8s.io/v1` broadcasters with explicit actions, correct permissions, and producer-before-recorder shutdown.

**Architecture:** Share one small API-backed recorder constructor and one native watcher-based test helper. Give delayed webhook jobs an owned, cancellable lifetime, then migrate recorder types, publication sites, production wiring, and tests together. Keep library-go operator recording separate.

**Tech Stack:** Go, vendored Kubernetes client-go `tools/events`, controller-runtime, ants MultiPool, Ginkgo/Gomega, envtest, controller-gen, operator-sdk.

**Spec:** `docs/superpowers/specs/2026-11-02-multiarch-6087-events-api-migration-design.md`

## Global Constraints

- Use `clientgoevents "k8s.io/client-go/tools/events"` and `clientgoevents.EventRecorderLogger` consistently; no dependency updates.
- Use `NewBroadcaster`, `EventSinkImpl{Interface: client.EventsV1()}`, and `StartRecordingToSinkWithContext`; no adapter, deprecated factory, native fake recorder, or legacy shim.
- Preserve reason, type, message text, regarding identity/namespace, emission conditions, and nil-recorder behavior; retain the ENoExec nil-pod guard.
- Publish using `recorder.WithLogger(log.FromContext(ctx)).Eventf(object, nil, eventType, reason, action, "%s", message)`.
- Reporting controllers remain `utils.OperatorName` and `utils.EnoexecControllerName`; native client-go owns reporting-instance identity.
- Retain the operator's library-go recorder and necessary core-event/leader-election permissions. Do not change ENoExec labels/retries or add CR/webhook CEL emissions.
- Use the exact eight action identifiers and values in the spec; select actions explicitly at each call site.
- Webhook jobs retain the two-minute timeout and existing backoff parameters; shutdown uses `ReleaseTimeout(30 * time.Second)`, cancels and joins on timeout, and reports that timeout.
- All three operand event-writer rules grant `create`, `patch`, and `update` on `events.k8s.io/events`; preserve unrelated permissions.
- No PR #264 input, unrelated `fake.NewSimpleClientset` migration, API/CRD changes, or hand-edited generated deepcopy files.
- Preserve `CGO_ENABLED=1`, gpgme, vendoring, and envtest setup. Run repository generation targets and report unavailable/failed checks explicitly.
- Do not commit without a direct user request. Task checkpoints below are review points, not authorization to commit.

## Review Focus

1. Literal `%` in image/CEL messages must survive unchanged, including warning and retry-exhaustion events (Task 3).
2. Canceled admission requests must not cancel accepted delayed jobs; shutdown cancellation must interrupt both backoff and an in-flight API read (Tasks 2 and 3).
3. Concurrent admission, rejected pool submissions, and shutdown must never permit an untracked producer or `WaitGroup.Add` after joining begins (Task 2).
4. Recreated pods with the same name must not satisfy UID-based assertions for an older pod; event series must not cause brittle exact-count assertions (Task 5).
5. Restricted operand accounts must be able to write events, while library-go/leader-election consumers retain core-event access (Tasks 4 and 5).

## File Map and Sequence

1. **Native recorder foundation:** create `pkg/events/recorder.go`, `pkg/events/recorder_test.go`, `pkg/testing/framework/event_recorder.go`, and `pkg/testing/framework/event_recorder_test.go`.
2. **Webhook producer lifetime:** modify `internal/controller/podplacement/scheduling_gate_mutating_webhook.go`; create `internal/controller/podplacement/webhook_event_jobs_test.go`; update existing webhook fixture cleanup.
3. **Atomic recorder migration:** modify `pkg/utils/const.go`, `pkg/models/pod.go`, `pkg/models/pod_test.go`, pod-placement recorder consumers/tests, ENoExec recorder consumers/tests, `cmd/main.go`, and both operand envtest suites; create `cmd/main_test.go` and `internal/controller/enoexecevent/handler/enoexecevent_model_test.go`.
4. **RBAC and artifacts:** modify the three runtime event rules and operator RBAC markers; create `internal/controller/operator/events_rbac_test.go`; regenerate `config/rbac/role.yaml`, the bundle CSV, and any affected generation outputs.
5. **API observation and verification:** migrate `pkg/testing/framework/events.go`, `pkg/testing/framework/pod.go`, ENoExec `ensureEvent`, and E2E event assertions; create framework query tests and add an envtest core-conversion assertion.

Re-search first-party callers before each relevant edit. The supplied spec is currently untracked user work: preserve it. Tasks are sequential; Tasks 3–5 together form the complete deployable migration.

### Task 1: Native recorders with explicit ownership

**Files:** The four new foundation files in the file map. Read the vendored `tools/events/interfaces.go` and `event_broadcaster.go` before implementation.

**Interfaces:**
- Produces `events.NewRecorder(ctx context.Context, client kubernetes.Interface, scheme *runtime.Scheme, reportingController string) (clientgoevents.EventRecorderLogger, func(), error)`.
- Produces `framework.NewTestEventRecorder(ctx context.Context, scheme *runtime.Scheme, reportingController string) (clientgoevents.EventRecorderLogger, <-chan *eventsv1.Event, func(), error)`.
- Keep constructor failure injection private: `newRecorder(ctx context.Context, broadcaster clientgoevents.EventBroadcaster, scheme *runtime.Scheme, reportingController string) (clientgoevents.EventRecorderLogger, func(), error)`. The public constructor supplies the real broadcaster; this boundary tests ownership without an exported factory/configuration option.

- [ ] **Step 1: Add failing recorder lifecycle and capture tests.**

  `TestNewRecorderPublishesEventsV1` uses a client-go test client and awaits an actual events/v1 create action. Assert namespace, regarding UID, reporter, and action. `TestNewRecorderStartupFailureCleansUp` supplies a broadcaster stub whose start returns a sentinel error; assert its context is canceled and `Shutdown` is called once. `TestNewRecorderCleanupIsIdempotent` invokes cleanup twice and asserts one shutdown. Use an actual broadcaster for the successful path.

  `TestNewTestEventRecorderCapturesStructuredEvent` emits a single named, namespaced pod with UID `pod-uid` and asserts:

  ```go
  g.Expect(event.Regarding).To(Equal(corev1.ObjectReference{
      APIVersion: "v1", Kind: "Pod", Name: "test-pod", Namespace: "test-ns", UID: "pod-uid",
  }))
  g.Expect(event.Related).To(BeNil())
  g.Expect(event.Type).To(Equal(corev1.EventTypeNormal))
  g.Expect(event.Reason).To(Equal("ArchAwareSchedGateAdded"))
  g.Expect(event.Action).To(Equal("SchedulingGateAdd"))
  g.Expect(event.Note).To(Equal("literal 100% %s"))
  g.Expect(event.ReportingController).To(Equal(utils.OperatorName))
  ```

  `TestNewTestEventRecorderCleanupUnblocksCapture` fills the capture buffer, triggers another callback, and verifies cancellation/cleanup returns within a bounded deadline. Repeat cleanup and test parent cancellation. Do not assert that the event channel closes.

- [ ] **Step 2: Run `CGO_ENABLED=1 go test ./pkg/events ./pkg/testing/framework -run 'TestNew(Recorder|TestEventRecorder)' -count=1`.** Expect missing constructors before implementation.
- [ ] **Step 3: Implement the two public constructors and private ownership boundary.** Production cleanup uses `sync.Once`, cancels its child recording context, then shuts down the broadcaster, including on startup failure. Test capture uses `StartEventWatcher`, typed deep copies, a buffer of 64, and a send selecting on owned-context cancellation. Never start nil-sink recording; do not close the capture channel while callbacks could send. Stop the watcher and broadcaster during idempotent cleanup; arrange parent cancellation to stop capture as well.
- [ ] **Step 4: Repeat the Step 2 command with `-race`.** Expect all named tests to pass, including saturated capture cleanup.

### Task 2: Quiesce delayed webhook producers

**Files:** Webhook implementation, new `webhook_event_jobs_test.go`, and `cel_webhook_handle_test.go` fixture cleanup. Keep the existing recorder type until Task 3.

**Interfaces:**
- Produces `func (a *PodSchedulingGateMutatingWebHook) ShutdownEventJobs(timeout time.Duration) error`.
- Existing constructor arguments remain unchanged in this task. Initialize a webhook-owned event-job context and cancellation function in the constructor; it is independent of admission-request cancellation. Preserve request logger values explicitly when deriving job contexts.
- Existing `delayedSchedulingGatedEvent(ctx context.Context, pod *corev1.Pod)` becomes a tracked producer. No caller can publish after `ShutdownEventJobs` returns.

- [ ] **Step 1: Write standard Go tests for the shutdown contract.** Use real ants pools and an `httptest` API endpoint/clientset; no new production pool/client interface is needed. Channels signal request start, cancellation, and completion; deadlines guard hangs, rather than using sleeps to establish ordering.

  | Test | Required assertions |
  |---|---|
  | `TestWebhookEventJobsDrain` | An accepted job can finish during drain; shutdown returns nil only after its producer finishes. |
  | `TestWebhookEventJobsIgnoreAdmissionCancellation` | Cancel the request after submission; the job still obtains the persisted pod. |
  | `TestWebhookEventJobsTimeoutCancelsAPIRead` | Block a GET until its HTTP context cancels; a short shutdown returns a timeout only after the job exits. |
  | `TestWebhookEventJobsTimeoutCancelsBackoff` | Return NotFound; timeout interrupts retry waiting and joins the job. |
  | `TestWebhookEventJobsRejectedSubmission` | Submit to a released/nonblocking saturated pool; shutdown does not hang on a leaked tracking slot. |
  | `TestWebhookEventJobsAdmissionShutdownRace` | Race submissions with shutdown repeatedly; all accepted jobs exit, late jobs are rejected, no post-boundary publication or WaitGroup misuse. |
  | `TestWebhookEventJobsShutdownIsIdempotent` | Repeated/concurrent shutdown callers observe completion safely. |

- [ ] **Step 2: Run `CGO_ENABLED=1 go test ./internal/controller/podplacement -run '^TestWebhookEventJobs' -count=1`.** Expect the missing shutdown method/contract to fail.
- [ ] **Step 3: Implement the owned job lifetime.** Synchronize admission closure and job registration with one mutex; add tracking before submission and undo it on submission failure. Do not hold that mutex during a blocking pool submission or while waiting for jobs. Derive the two-minute job timeout from the owned context, use `wait.ExponentialBackoffWithContext`, and pass its context to pod GETs. Preserve the existing backoff and persisted-pod publication. Shutdown closes admission, attempts pool drain, cancels on timeout, joins every tracked job, cancels on success too, and returns the drain error. Ensure nil-recorder jobs remain no-ops when Task 3 migrates publication. Replace the inaccurate “we don't care about this goroutine” comment.
- [ ] **Step 4: Register fixture cleanup as jobs first, recorder second.** Existing fixtures using only nil clientsets still release their pools through the same boundary where possible. Check all direct webhook struct literals and constructors for initialization/cleanup needs.
- [ ] **Step 5: Run the Step 2 command with `-race -count=10`.** Expect bounded completion and no races; review the late-submission synchronization directly as well.

### Task 3: Migrate publication, callers, and process/suite wiring atomically

**Files:**
- `pkg/utils/const.go`; `pkg/models/pod.go`, `pod_test.go`.
- Pod placement: `pod_reconciler.go`, `pod_model.go`, `cel_integration.go`, `scheduling_gate_mutating_webhook.go`; `pod_model_test.go`, `cel_evaluator_test.go`, `cel_webhook_handle_test.go`, `cel_controller_integration_test.go`, `regression_test.go`, `pod_reconciler_test.go`, `scheduling_gate_mutating_webhook_test.go`, `webhook_event_jobs_test.go`, `suite_test.go`.
- ENoExec handler: `enoexecevent_controller.go`, `enoexecevent_model.go`, new `enoexecevent_model_test.go`, `suite_test.go`.
- `cmd/main.go`, new `cmd/main_test.go`.

**Interfaces:**
- Consumes both Task 1 constructors and Task 2 shutdown.
- All migrated recorder fields/parameters/accessors use `clientgoevents.EventRecorderLogger`.
- Produces `(*Pod).PublishEvent(eventType, reason, action, message string)`, `(*ENoExecEvent).PublishEvent(eventType, reason, action, message string)`, and `(*ENoExecEvent).PublishEventOnPod(pod *corev1.Pod, eventType, reason, action, message string)`.
- Change the three operand `Run…` setup functions to `(ctx context.Context, mgr ctrl.Manager) ([]func(), error)`: callbacks must be returned even on setup error.
- Add `runOperand(ctx context.Context, mgr ctrl.Manager, setup func(context.Context, ctrl.Manager) ([]func(), error)) error`. It owns a fresh logger-bearing recording context, invokes setup, defers FIFO callbacks on both setup/start failures, starts the manager with the supplied signal context, and cancels the recording parent only after callbacks. Operator mode retains its current setup; it does not enter operand recorder setup.

- [ ] **Step 1: Add failing structured publication and runner tests.**

  Add or migrate `TestPod_PublishEvent` and add ENoExec model tests covering normal/warning, `literal 100% %s`, nil recorder, nil pod, forwarded action, exact regarding namespace/UID, `Related == nil`, and reporter identity. CR wrapper tests register the API scheme and exercise forwarding only; no production CR emission is added.

  Add `TestRunOperandManagerStartFailureCleansUp`, `TestRunOperandSetupFailureCleansUp`, and `TestRunOperandProducerBeforeRecorderCleanup`: use a manager stub embedding `ctrl.Manager` and overriding only `GetLogger`/`Start`. Assert the original error is returned, callbacks run once in FIFO order, recording remains live during job drain, and recording is canceled afterward. Include success and a drain-timeout callback that has already joined its producer.

- [ ] **Step 2: Run targeted tests and verify failures.**

  ```bash
  CGO_ENABLED=1 go test ./pkg/models ./internal/controller/enoexecevent/handler ./cmd -run 'Test(Pod_PublishEvent|ENoExecEvent.*Publish|RunOperand)' -count=1
  ```

- [ ] **Step 3: Add constants and migrate every publication site.** Use the spec's exact mapping below; preserve existing text and conditions. The same `ArchAwarePredicateSet` reason intentionally has two actions.

  | Constant suffix (`EventAction…`) | Value | Emissions |
  |---|---|---|
  | `SchedulingGateAdd` | `SchedulingGateAdd` | Delayed `ArchAwareSchedGateAdded` |
  | `SchedulingGateRemove` | `SchedulingGateRemove` | Removal success/failure and gated-pod ignored |
  | `NodeAffinityUpdate` | `NodeAffinityUpdate` | Required/preferred/partly-applied affinity, fallback affinity, CEL applied/fallback |
  | `NodeAffinitySkip` | `NodeAffinitySkip` | Preferred skipped message, preferred duplicates, predicate conflict |
  | `ImageArchitectureCheck` | `ImageArchitectureCheck` | No supported architectures |
  | `ImageInspectionFailed` | `ImageInspectionFailed` | Inspection errors including max retries |
  | `CELEvaluation` | `CELEvaluation` | CEL evaluation error |
  | `ExecFormatErrorReport` | `ExecFormatErrorReport` | Existing ENoExec pod warning |

  Use `WithLogger(...).Eventf(..., "%s", message)` at wrapper/direct webhook boundaries. Keep `Related` nil; delayed gate events use the GET result's UID. Remove only obsolete events-migration suppressions.

- [ ] **Step 4: Migrate isolated recorder fixtures and assertions.** Use `NewTestEventRecorder` with `t.Cleanup`/`DeferCleanup`. Replace recorder-channel strings and immediate drains with bounded eventual structured checks (two seconds for local capture). Add names/namespaces/UIDs to recording fixtures. Retain existing test intent; negative checks use a bounded consistency interval, not immediate absence. Use the Step 1 field assertion contract throughout, without adding a general assertion framework.

  Extend the existing behavior tests to assert exact actions for gate removal success/error/ignored, required/preferred/partial/fallback affinity, duplicate/skip/conflict, no architectures, inspection errors/max retries, and CEL match/fallback/error. Include a literal percent in image-error and CEL-error fixtures. Keep ENoExec label/retry assertions intact. Update Task 2 tests to capture native events and assert the persisted UID and `SchedulingGateAdd`.

- [ ] **Step 5: Wire native production recorders and error-safe cleanup.** Implement the runner/setup interfaces above. Use `mgr.GetConfig()` and error-returning client creation inside migrated setup boundaries. Immediately retain cleanup after recorder creation, including when controller registration or subsequent `mgr.Add` fails. For webhook setup, create the recorder, create the pool, then construct the handler; return callbacks in job-shutdown-before-recorder order. Log a shutdown timeout after jobs have joined; retain existing global ants release where required. Replace the old pool-only callback and ensure `mgr.Start` errors reach outer `must` only after cleanup. Avoid unrelated operator-mode restructuring.

- [ ] **Step 6: Wire both envtest suites with Task 1's production constructor.** Create recording contexts independently of manager cancellation, attach the suite logger, and use the prescribed reporter names. Initialize `stopMgr` before starting the goroutine. Retain handler/recorder cleanup handles; stop and join managers, drain webhook jobs, clean up recorders, then stop envtest. Treat manager-stop timeout as teardown failure rather than proceeding while producers still run. Cover setup-failure teardown as well as normal suite completion.

- [ ] **Step 7: Run focused checks.** Repeat Step 2 with `-race`, then run Task 2 tests once after the recorder change. Run `CGO_ENABLED=1 go test ./internal/controller/podplacement -run '^TestPod_' -count=1`. Use `GINKGO_ARGS="-v --focus='CEL|scheduling_gate_mutating_webhook'" make unit` for the affected Ginkgo paths. Expected: structured assertions pass without new emissions or changed scheduling outcomes. Full integration verification follows Task 5.

### Task 4: Authorize event writes and regenerate operator artifacts

**Files:** `internal/controller/operator/podplacement_objects.go`, `enoexecevent_objects.go`, `clusterpodplacementconfig_controller.go`, new `events_rbac_test.go`; generated `config/rbac/role.yaml` and `bundle/manifests/multiarch-tuning-operator.clusterserviceversion.yaml` plus genuine target outputs.

**Interfaces:** Existing `buildClusterRoleController()`, `buildClusterRoleWebhook()`, and `buildClusterRoleENoExecEventsController()` continue returning `*rbacv1.ClusterRole`.

- [ ] **Step 1: Add `TestOperandEventRBAC` with one subtest per role builder.** Assert an explicit `events.k8s.io`/`events` rule grants all three verbs (not a wildcard). Compare non-event rules to the pre-change expected rules so pod, config, security, and authentication permissions remain intact. Independently test retained core permissions for consumers identified below; do not rely on admin-backed envtest.
- [ ] **Step 2: Run `CGO_ENABLED=1 go test ./internal/controller/operator -run '^TestOperandEventRBAC$' -count=1`.** Expect the new-group assertions to fail.
- [ ] **Step 3: Update the three runtime event-writer rules and add the operator marker `//+kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch;update`.** Review leader-election recorder paths and namespaced role builders before removing any operand core permission. Retain core permissions for actual remaining consumers, including library-go; do not replace all `events` rules globally.
- [ ] **Step 4: Repeat Step 2.** Expect every role subtest and unrelated-permission check to pass.
- [ ] **Step 5: Run `make manifests`, `make generate`, and `make bundle`.** Run `make vendor` only if import reachability requires vendor changes; do not change dependency versions. Review the full generated diff for events-group escalation permissions and accidental version/image/timestamp/API churn. Correct generation inputs rather than hand-editing generated RBAC/deepcopy. Report tool/network failures explicitly.

### Task 5: Observe the native API and verify end to end

**Files:** `pkg/testing/framework/events.go`, `pod.go`, new `events_test.go`; ENoExec `enoexecevent_controller_test.go`; pod-placement `scheduling_gate_mutating_webhook_test.go`; `pkg/e2e/operator/pod_placement_config_test.go`; any newly discovered event-helper callers (including `pkg/e2e/podplacement`).

**Interfaces:**
- Change `GetEventsForObject(ctx context.Context, client kubernetes.Interface, objectName string, objectNamespace string) ([]eventsv1.Event, error)`; retain name/namespace arguments. List namespace-scoped events/v1 and filter `Regarding.Name` and namespace explicitly; callers with an object also compare `Regarding.UID`.
- Keep `VerifyPodsEvents(...) func(g Gomega)`'s existing signature and migrate its internals to events/v1 and UID matching.

- [ ] **Step 1: Add failing API-query tests.** `TestGetEventsForObject` seeds events for another name/namespace and verifies only matching native events are returned; propagate list errors. `TestVerifyPodsEventsUsesRegardingUID` creates old/new pod identities sharing a name: an old-UID event cannot satisfy the new pod. Include unrelated reasons and non-Pod regarding objects. Include a matching event with `Series.Count == 3` and verify it satisfies the pod assertion without requiring three event objects. Do not assert exact event counts for repeated observations.

  Add envtest `It("publishes a gate event through events/v1 and preserves core/v1 conversion")`: create an eligible pod; eventually find its native gate event by UID and assert all structured fields. Fetch that same event via `CoreV1().Events(ns)` and compare semantically:

  ```go
  g.Expect(legacy.InvolvedObject).To(Equal(native.Regarding))
  g.Expect(legacy.Message).To(Equal(native.Note))
  g.Expect(legacy.Reason).To(Equal(native.Reason))
  g.Expect(legacy.Type).To(Equal(native.Type))
  g.Expect(legacy.Action).To(Equal(native.Action))
  g.Expect(legacy.ReportingController).To(Equal(native.ReportingController))
  ```

  Generated event names are used only to retrieve the same object, never compared to fixed expected names. This is the single dedicated core-conversion assertion.

- [ ] **Step 2: Run `CGO_ENABLED=1 go test ./pkg/testing/framework -run 'Test(GetEventsForObject|VerifyPodsEvents)' -count=1` and `GINKGO_ARGS="-v --focus='publishes a gate event'" make unit`.** Expect legacy-query/structured assertions to fail before migration.
- [ ] **Step 3: Migrate queries and their callers.** Use `EventsV1().Events(namespace)`/`eventsv1.EventList`; remove legacy involved-object field selectors. Filter regarding kind/name/namespace/UID, reason, and reporter as appropriate in memory. ENoExec `ensureEvent` asserts warning, `ExecFormatErrorReport`, exact `Note`, source-pod UID, nil related, and reporter. Update E2E ExecFormatError checks similarly while preserving their existing “at least one event” condition. Update all shared-helper users together.
- [ ] **Step 4: Repeat Step 2, then run the ENoExec integration tests.** `GINKGO_ARGS="-v --focus='ENoExec'" make unit`; expected real events/v1 publication and unchanged label/retry outcomes. Check that this focus selects the intended specs; the complete suite below is authoritative.
- [ ] **Step 5: Run repository checks with the documented build environment.** Run `make fmt`, `make goimports`, `make vet`, `make lint`, `make build`, and `make unit`. Where using local execution, preserve `CGO_ENABLED=1`, gpgme, and `KUBEBUILDER_ASSETS`; use the repository container workflow when appropriate. Inspect exit status/output, report failures, and fix migration-caused failures before claiming completion. Review regenerated outputs again after build targets.
- [ ] **Step 6: Search first-party source and review the diff.** Search `cmd`, `internal`, `pkg`, and `api` for `tools/record`, `GetEventRecorderFor`, `record.NewFakeRecorder`, adapter/native-fake use, and obsolete events-API MULTIARCH-6087 suppressions. The unrelated `pkg/testing/framework/utils_test.go` clientset comment stays. Review `git diff --check`, `git diff --stat`, `git diff`, and `git status --short`, including newly created files; confirm no accidental API/dependency or ENoExec behavior changes.
- [ ] **Step 7: Validate cluster publication when a deployed cluster is available.** Run `make e2e` with the deployed migration and configured cluster. Verify native events from all three operands under their actual service-account credentials, including repeated-event patch/update authorization; use access reviews for each verb as supporting evidence, not as a substitute for observing publication. Without a cluster, explicitly report E2E and deployed-credential publication as unverified.

## Completion Evidence

Summarize changed event/lifecycle/RBAC behavior, targeted and full check results, generation results, residual search matches with reasons, and any unavailable checks. Asynchronous delivery remains best effort; a successful shutdown is not a transactional flush guarantee. Planning alone does not establish implementation or passing tests.

## Execution Results

Implemented inline in the user-approved current checkout. At the user's subsequent request, changes were committed in logical sections: recorder infrastructure, operand migration/lifetimes, RBAC/artifacts, native API assertions, and design/verification documentation.

- Native recorders, explicit action call sites, error-safe setup/runner cleanup, webhook job draining/cancellation, runtime/generated RBAC, and structured event queries/tests are implemented.
- Targeted recorder/model/action/lifecycle/RBAC/query/runner tests passed under `-race` across all seven affected packages. Gate-removal event assertions were made order-independent after reproducing reordered delivery on one CPU; 20 repeated single-CPU race runs passed afterward.
- Focused CEL/webhook envtest, ENoExec envtest, and real events/v1-to-core/v1 compatibility checks passed. Envtest suites must run serially because they share probe port 4980.
- `make manifests`, `make generate`, `make bundle`, formatting/import checks, and vet passed. Changed-package lint passed using the Go-1.26-built golangci-lint from `GOPATH/bin`.
- Initial full build/unit/lint attempts exceeded command deadlines; the initial lint invocation also selected an obsolete binary from PATH. Following user direction, no further `make build` or `make test` was run. Full build/full-suite completion and cluster E2E/deployed-service-account publication remain unverified.
- First-party legacy-recorder search is clear except the explicitly excluded `fake.NewSimpleClientset` ticket comment. Dependencies and API definitions are unchanged; generated RBAC/bundle additions were produced by repository targets, with the existing CSV timestamp retained.
- Independent final review completed. The event-order test finding was fixed. Native client-go aggregates by regarding/type/reason/action/reporter, not note: different diagnostic notes can share a series. The exact action/reason mapping and native aggregation are retained as required by the spec; separate retention of every distinct note is not guaranteed.
- Compatibility fixtures use explicit names: generated-name admission objects can have an empty name in envtest, an existing delayed-event limitation outside this migration. Shutdown remains asynchronous/best effort, not a transactional sink flush.
