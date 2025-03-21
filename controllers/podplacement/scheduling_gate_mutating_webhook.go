/*
Copyright 2023 Red Hat, Inc.

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
	"sync"
	"time"

	"net/http"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"

	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/panjf2000/ants/v2"

	"github.com/openshift/multiarch-tuning-operator/controllers/podplacement/metrics"
	"github.com/openshift/multiarch-tuning-operator/pkg/informers/clusterpodplacementconfig"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

// [disabled:operator]kubebuilder:webhook:path=/add-pod-scheduling-gate,mutating=true,sideEffects=None,admissionReviewVersions=v1,failurePolicy=ignore,groups="",resources=pods,verbs=create,versions=v1,name=pod-placement-scheduling-gate.multiarch.openshift.io

// PodSchedulingGateMutatingWebHook annotates Pods
type PodSchedulingGateMutatingWebHook struct {
	client     client.Client
	clientSet  *kubernetes.Clientset
	decoder    admission.Decoder
	once       sync.Once
	scheme     *runtime.Scheme
	recorder   record.EventRecorder
	workerPool *ants.MultiPool
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
	pod := &Pod{
		ctx:      ctx,
		recorder: nil, // do we want to publish events if the pod is ignored?
	}
	err := a.decoder.Decode(req, &pod.Pod)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	log := ctrllog.FromContext(ctx).WithValues("namespace", pod.Namespace, "name", pod.Name)

	cppc := clusterpodplacementconfig.GetClusterPodPlacementConfig()
	if cppc != nil && cppc.Spec.Plugins != nil && cppc.Spec.Plugins.NodeAffinityScoring.IsEnabled() {
		pod.ensureLabel(utils.PreferredNodeAffinityLabel, utils.LabelValueNotSet)
	}
	pod.ensureLabel(utils.NodeAffinityLabel, utils.LabelValueNotSet)
	pod.ensureLabel(utils.SchedulingGateLabel, utils.LabelValueNotSet)

	if pod.shouldIgnorePod(cppc) {
		log.V(3).Info("Ignoring the pod")
		return a.patchedPodResponse(&pod.Pod, req)
	}

	pod.ensureSchedulingGate()
	// We also add a label to the pod to indicate that the scheduling gate was added
	// and this pod expects processing by the operator. That's useful for testing and debugging, but also gives the user
	// an indication that the pod is waiting for processing and can support kubectl queries to find out which pods are
	// waiting for processing, for example when the operator is being uninstalled.
	pod.Labels[utils.SchedulingGateLabel] = utils.SchedulingGateLabelValueGated
	// we don't care about this goroutine, it's informational,
	// we know it will finish eventually by design, and we don't need to block the response as we
	// are right in the admission pipeline, before the pod is persisted.
	log.V(3).Info("Scheduling gate added to the pod, launching the event creation goroutine")
	a.delayedSchedulingGatedEvent(ctx, pod.DeepCopy())
	metrics.GatedPods.Inc()
	metrics.GatedPodsGauge.Inc()
	log.V(2).Info("Accepting pod")
	return a.patchedPodResponse(&pod.Pod, req)
}

func (a *PodSchedulingGateMutatingWebHook) delayedSchedulingGatedEvent(ctx context.Context, pod *corev1.Pod) {
	err := a.workerPool.Submit(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		log := ctrllog.FromContext(ctx).WithValues("namespace", pod.Namespace, "name", pod.Name,
			"function", "delayedSchedulingGatedEvent")
		// We try to get the pod from the API with exponential backoff until we find it or a timeout is reached
		err := wait.ExponentialBackoff(wait.Backoff{
			// The maximum time, excluding the time for the execution of the request,
			// is the sum of a geometric series with factor != 1.
			// maxTime = duration * (factor^steps - 1) / (factor - 1)
			// maxTime = 2e-3s * (2^15 - 1) = 65.534s
			Duration: 2 * time.Millisecond,
			Factor:   2,
			Steps:    15,
		}, func() (bool, error) {
			createdPod, err := a.clientSet.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
			if err == nil {
				log.V(2).Info("Pod was found", "namespace", pod.Namespace, "name", pod.Name)
				a.recorder.Event(createdPod, corev1.EventTypeNormal, ArchitectureAwareSchedulingGateAdded, SchedulingGateAddedMsg)
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
		ctrllog.FromContext(ctx).WithValues("namespace", pod.Namespace, "name", pod.Name,
			"function", "delayedSchedulingGatedEvent").Error(err, "Failed to submit the delayedSchedulingGatedEvent job")
	}
}

func NewPodSchedulingGateMutatingWebHook(client client.Client, clientSet *kubernetes.Clientset,
	scheme *runtime.Scheme, recorder record.EventRecorder, workerPool *ants.MultiPool) *PodSchedulingGateMutatingWebHook {
	a := &PodSchedulingGateMutatingWebHook{
		client:     client,
		clientSet:  clientSet,
		scheme:     scheme,
		recorder:   recorder,
		workerPool: workerPool,
	}
	metrics.InitWebhookMetrics()
	return a
}
