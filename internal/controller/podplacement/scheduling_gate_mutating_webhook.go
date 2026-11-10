/*
Copyright 2026 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package podplacement

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	clientgoevents "k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/panjf2000/ants/v2"

	"github.com/openshift/multiarch-tuning-operator/api/common"
	multiarchv1beta1 "github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/internal/controller/podplacement/metrics"
	"github.com/openshift/multiarch-tuning-operator/pkg/informers/clusterpodplacementconfig"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

// [disabled:operator]kubebuilder:webhook:path=/add-pod-scheduling-gate,mutating=true,sideEffects=None,admissionReviewVersions=v1,failurePolicy=ignore,groups="",resources=pods,verbs=create,versions=v1,name=pod-placement-scheduling-gate.multiarch.openshift.io

// PodSchedulingGateMutatingWebHook annotates Pods
type PodSchedulingGateMutatingWebHook struct {
	client    client.Client
	apiReader client.Reader
	// ppcCacheSynced returns true once the PodPlacementConfig informer cache has
	// completed its initial list/sync.  When nil it is treated as "not yet synced"
	// so the apiReader fallback is always permitted in that case.
	ppcCacheSynced  func() bool
	clientSet       *kubernetes.Clientset
	decoder         admission.Decoder
	once            sync.Once
	scheme          *runtime.Scheme
	recorder        clientgoevents.EventRecorderLogger
	workerPool      *ants.MultiPool
	eventJobsCtx    context.Context
	cancelEventJobs context.CancelFunc
	eventJobsMu     sync.Mutex
	eventJobsClosed bool
	eventJobs       sync.WaitGroup
	shutdownOnce    sync.Once
	shutdownErr     error
}

func (a *PodSchedulingGateMutatingWebHook) patchedPodResponse(pod *corev1.Pod, req admission.Request) admission.Response {
	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func (a *PodSchedulingGateMutatingWebHook) Handle(ctx context.Context, req admission.Request) admission.Response {
	responseTimeStart := time.Now()
	defer utils.HistogramObserve(responseTimeStart, metrics.ResponseTime)
	metrics.ProcessedPodsWH.Inc()

	a.once.Do(func() {
		a.decoder = admission.NewDecoder(a.scheme)
	})
	pod := newPod(&corev1.Pod{}, ctx, a.recorder)

	err := a.decoder.Decode(req, &pod.Pod)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	log := ctrllog.FromContext(ctx).WithValues("namespace", pod.Namespace, "name", pod.Name)

	cppc := clusterpodplacementconfig.GetClusterPodPlacementConfig()

	// List existing PodPlacementConfigs in the same namespace.
	// Use the informer-backed client first. If the cache has not yet observed a
	// newly-created PodPlacementConfig, fall back to a direct API read so the
	// webhook can apply CEL architecture constraints before the Pod is persisted.
	// This avoids missing CEL placement decisions during informer startup or cache
	// propagation, when later reconciliation cannot reliably add new required
	// NodeSelectorTerms.
	ppcList := &multiarchv1beta1.PodPlacementConfigList{}
	if err := a.client.List(ctx, ppcList, client.InNamespace(pod.Namespace)); err != nil {
		log.Error(err, "Failed to list existing PodPlacementConfigs in namespace")
		// On error, proceed without PPC filtering - fail open
		ppcList.Items = []multiarchv1beta1.PodPlacementConfig{}
	}
	// Fall back to a direct API-server read only when the informer cache has not
	// yet completed its initial sync AND the cache returned no items.  Once the
	// cache is synced, an empty result genuinely means no PPCs exist in this
	// namespace and querying the API server on every Pod would generate unnecessary
	// traffic.
	cacheNotYetSynced := a.ppcCacheSynced == nil || !a.ppcCacheSynced()
	if len(ppcList.Items) == 0 && a.apiReader != nil && cacheNotYetSynced {
		if err := a.apiReader.List(ctx, ppcList, client.InNamespace(pod.Namespace)); err != nil {
			log.Error(err, "Failed to list PodPlacementConfigs from API server (webhook fallback)")
			ppcList.Items = []multiarchv1beta1.PodPlacementConfig{}
		}
	}

	// Filter to only PPCs that match this pod's labels - do this once for efficiency
	matchingPPCs := pod.filterMatchingPPCs(ppcList)

	// Set label to indicate if preferred affinity will be set by CPPC or any matching PPC
	if (cppc != nil && cppc.PluginsEnabled(common.NodeAffinityScoringPluginName)) ||
		pod.hasMatchingPPCWithPlugin(matchingPPCs) {
		pod.EnsureLabel(utils.PreferredNodeAffinityLabel, utils.LabelValueNotSet)
	}
	pod.EnsureLabel(utils.NodeAffinityLabel, utils.LabelValueNotSet)
	pod.EnsureLabel(utils.SchedulingGateLabel, utils.LabelValueNotSet)

	if pod.shouldIgnorePod(cppc, matchingPPCs) {
		log.V(3).Info("Ignoring the pod")
		return a.patchedPodResponse(pod.PodObject(), req)
	}

	// Apply CEL architecture placement in webhook before pod is persisted
	a.applyCELInWebhook(ctx, pod, matchingPPCs)

	pod.ensureSchedulingGate()
	// We also add a label to the pod to indicate that the scheduling gate was added
	// and this pod expects processing by the operator. That's useful for testing and debugging, but also gives the user
	// an indication that the pod is waiting for processing and can support kubectl queries to find out which pods are
	// waiting for processing, for example when the operator is being uninstalled.
	pod.Labels[utils.SchedulingGateLabel] = utils.SchedulingGateLabelValueGated
	// Publish after persistence without blocking admission; shutdown joins these jobs.
	log.V(3).Info("Scheduling gate added to the pod, launching the event creation goroutine")
	a.delayedSchedulingGatedEvent(ctx, pod.DeepCopy())
	metrics.GatedPods.Inc()
	metrics.GatedPodsGauge.Inc()
	log.V(2).Info("Accepting pod")
	return a.patchedPodResponse(pod.PodObject(), req)
}

func (a *PodSchedulingGateMutatingWebHook) delayedSchedulingGatedEvent(ctx context.Context, pod *corev1.Pod) {
	if a.clientSet == nil {
		// The scheduling-gated event is informational-only.
		return
	}
	a.eventJobsMu.Lock()
	if a.eventJobsClosed {
		a.eventJobsMu.Unlock()
		return
	}
	a.eventJobs.Add(1)
	a.eventJobsMu.Unlock()
	jobCtx := ctrllog.IntoContext(a.eventJobsCtx, ctrllog.FromContext(ctx))
	err := a.workerPool.Submit(func() {
		defer a.eventJobs.Done()
		ctx, cancel := context.WithTimeout(jobCtx, 2*time.Minute)
		defer cancel()
		log := ctrllog.FromContext(ctx).WithValues("namespace", pod.Namespace, "name", pod.Name,
			"function", "delayedSchedulingGatedEvent")
		// We try to get the pod from the API with exponential backoff until we find it or a timeout is reached
		err := wait.ExponentialBackoffWithContext(ctx, wait.Backoff{
			// The maximum time, excluding the time for the execution of the request,
			// is the sum of a geometric series with factor != 1.
			// maxTime = duration * (factor^steps - 1) / (factor - 1)
			// maxTime = 2e-3s * (2^15 - 1) = 65.534s
			Duration: 2 * time.Millisecond,
			Factor:   2,
			Steps:    15,
		}, func(ctx context.Context) (bool, error) {
			createdPod, err := a.clientSet.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
			if err == nil {
				log.V(2).Info("Pod was found", "namespace", pod.Namespace, "name", pod.Name)
				if a.recorder != nil {
					a.recorder.WithLogger(log).Eventf(createdPod, nil, corev1.EventTypeNormal, ArchitectureAwareSchedulingGateAdded, utils.EventActionSchedulingGateAdd, "%s", SchedulingGateAddedMsg)
				}
				// Pod was found, return true to stop retrying
				return true, nil
			}
			if apierrors.IsNotFound(err) {
				log.V(3).Info("Pod not found yet", "namespace", pod.Namespace, "name", pod.Name)
				// Pod not found yet, continue retrying
				return false, nil
			}
			// Stop retrying
			log.V(3).Info("Failed to get pod", "error", err)
			return false, err
		})
		if err != nil {
			log.V(2).Info("Failed to get a scheduling gated Pod after retries",
				"error", err)
		}
	})
	if err != nil {
		a.eventJobs.Done()
		ctrllog.FromContext(ctx).WithValues("namespace", pod.Namespace, "name", pod.Name,
			"function", "delayedSchedulingGatedEvent").Error(err, "Failed to submit the delayedSchedulingGatedEvent job")
	}
}

// ShutdownEventJobs closes admission and joins all producers before recorder cleanup.
func (a *PodSchedulingGateMutatingWebHook) ShutdownEventJobs(timeout time.Duration) error {
	a.shutdownOnce.Do(func() {
		a.eventJobsMu.Lock()
		a.eventJobsClosed = true
		a.eventJobsMu.Unlock()
		if a.workerPool != nil {
			a.shutdownErr = a.workerPool.ReleaseTimeout(timeout)
		}
		if a.shutdownErr != nil && a.cancelEventJobs != nil {
			a.cancelEventJobs()
		}
		a.eventJobs.Wait()
		if a.cancelEventJobs != nil {
			a.cancelEventJobs()
		}
	})
	return a.shutdownErr
}

// applyCELInWebhook evaluates and applies CEL architecture placement during pod admission.
//
// CEL architecture placement must occur during admission because Kubernetes rejects updates
// that modify existing NodeSelectorTerms (immutable field constraint). By applying architecture
// constraints before the pod is persisted to etcd, we avoid "no additions/deletions to non-empty
// NodeSelectorTerms list are allowed" API rejections during controller reconciliation.
//
// This is a deliberate deviation from the enhancement document (which specified controller-based
// evaluation) to satisfy OPENSHIFTP-636 requirements.
func (a *PodSchedulingGateMutatingWebHook) applyCELInWebhook(ctx context.Context, pod *Pod, matchingPPCs []multiarchv1beta1.PodPlacementConfig) {
	log := ctrllog.FromContext(ctx)

	// Sort matching PPCs by descending priority to process highest priority first
	sort.Slice(matchingPPCs, func(i, j int) bool {
		return matchingPPCs[i].Spec.Priority > matchingPPCs[j].Spec.Priority
	})

	// Evaluate PPCs in priority order. If a higher-priority PPC has CEL evaluation errors,
	// we continue to the next PPC (soft failure model). This ensures pod admission always
	// succeeds even if some PPCs are misconfigured.
	for _, ppc := range matchingPPCs {
		if !ppc.PluginsEnabled(common.CelArchitecturePlacementPluginName) {
			continue
		}

		celPlugin := ppc.Spec.Plugins.CelArchitecturePlacement
		if celPlugin == nil {
			// Should not occur: webhook validation ensures the plugin is non-nil when enabled.
			log.V(1).Info("CEL plugin enabled but configuration is nil; skipping",
				"PodPlacementConfig", ppc.Name, "pod", pod.Name)
			continue
		}

		// Evaluate CEL rules against the pod
		result, err := evaluateCELArchitecturePlacement(celPlugin.Rules, celPlugin.FallbackArchitectures, pod.PodObject())
		if err != nil {
			// Evaluator initialisation failed (not a CEL expression error).
			// Log and continue to next PPC — don't block pod admission.
			log.Error(err, "Failed to evaluate CEL rules, trying next PPC", "PodPlacementConfig", ppc.Name, "pod", pod.Name)
			continue
		}

		// allRulesErrored means every CEL expression in this PPC failed to compile
		// or evaluate.  The PPC is malformed and must not claim the pod; proceed to
		// lower-priority PPCs so they can still apply their constraints.
		if result.allRulesErrored {
			log.V(1).Info("All CEL rules in PPC failed to evaluate (malformed); skipping PPC",
				"PodPlacementConfig", ppc.Name, "pod", pod.Name)
			continue
		}

		// Apply architecture constraints in-place (no NodeSelectorTerms deletions).
		// We use the same safe path as the reconciler's applyCELArchitecturePlacement:
		//   1. Remove the arch key only from nodeSelector (flat map).
		//   2. Update arch matchExpressions within each existing NodeSelectorTerm.
		// This preserves the term count so that a subsequent controller Update never
		// sends fewer terms than what was persisted here, avoiding the Kubernetes
		// HTTP 422 "no additions/deletions to non-empty NodeSelectorTerms list are
		// allowed" error (KEP-3838 immutability requirement).
		// We deliberately do NOT call applyArchitectureConstraints (which calls
		// removeAllArchitectureConstraints → removeArchitectureFromNodeAffinity)
		// because that function drops arch-only terms, shrinking the term count.
		if len(result.architectures) == 0 {
			log.V(1).Info("CEL plugin produced no architectures; trying next PPC",
				"pod", pod.Name,
				"PodPlacementConfig", ppc.Name)
			continue
		}
		removeArchitectureFromNodeSelector(pod.PodObject())
		applyArchitectureNodeAffinity(pod.PodObject(), result.architectures)

		// CEL successfully applied architecture constraints. Mark the pod so downstream
		// components (e.g. reconciler, e2e tests) can distinguish CEL-placed pods from
		// image-inspection-placed pods. The value "overriden" is intentional per review.
		pod.EnsureLabel(utils.NodeAffinityLabel, utils.NodeAffinityLabelValueOverriden)

		log.V(1).Info("Applied CEL architecture constraints",
			"pod", pod.Name,
			"PodPlacementConfig", ppc.Name,
			"architectures", result.architectures,
			"ruleMatched", result.matched,
			"ruleName", result.ruleName)

		// First PPC that produces architectures wins - stop processing remaining PPCs
		return
	}
}

// NewPodSchedulingGateMutatingWebHook creates a new webhook instance.
// ppcCacheSynced should return true once the PodPlacementConfig informer cache
// has completed its initial sync; pass nil to always allow the apiReader fallback
// (safe for unit tests that do not wire an informer).
func NewPodSchedulingGateMutatingWebHook(ctx context.Context, client client.Client, apiReader client.Reader, ppcCacheSynced func() bool,
	clientSet *kubernetes.Clientset, scheme *runtime.Scheme, recorder clientgoevents.EventRecorderLogger,
	workerPool *ants.MultiPool) *PodSchedulingGateMutatingWebHook {
	eventJobsCtx, cancelEventJobs := context.WithCancel(ctx)
	a := &PodSchedulingGateMutatingWebHook{
		client:          client,
		apiReader:       apiReader,
		ppcCacheSynced:  ppcCacheSynced,
		clientSet:       clientSet,
		scheme:          scheme,
		recorder:        recorder,
		workerPool:      workerPool,
		eventJobsCtx:    eventJobsCtx,
		cancelEventJobs: cancelEventJobs,
	}
	metrics.InitWebhookMetrics()
	return a
}
