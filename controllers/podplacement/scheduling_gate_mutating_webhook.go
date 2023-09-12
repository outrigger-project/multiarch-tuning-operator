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
	"net/http"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/json"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	// SchedulingGateName is the name of the Scheduling Gate
	schedulingGateName = "multi-arch.openshift.io/scheduling-gate"
)

var schedulingGate = corev1.PodSchedulingGate{
	Name: schedulingGateName,
}

// +kubebuilder:webhook:path=/add-pod-scheduling-gate,mutating=true,sideEffects=None,admissionReviewVersions=v1,failurePolicy=ignore,groups="",resources=pods,verbs=create,versions=v1,name=pod-placement-scheduling-gate.multiarch.openshift.io
// TODO: failurePolicy?

// PodSchedulingGateMutatingWebHook annotates Pods
type PodSchedulingGateMutatingWebHook struct {
	Client  client.Client
	decoder *admission.Decoder
	Scheme  *runtime.Scheme
}

func (a *PodSchedulingGateMutatingWebHook) patchedPodResponse(pod *corev1.Pod, req admission.Request) admission.Response {
	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func (a *PodSchedulingGateMutatingWebHook) Handle(ctx context.Context, req admission.Request) admission.Response {
	if a.decoder == nil {
		a.decoder = admission.NewDecoder(a.Scheme)
	}
	pod := &corev1.Pod{}
	err := a.decoder.Decode(req, pod)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	// ignore the openshift-* namespace as those are infra components
	if strings.HasPrefix(pod.Namespace, "openshift-") || strings.HasPrefix(pod.Namespace, "hypershift-") || strings.HasPrefix(pod.Namespace, "kube-") {
		return a.patchedPodResponse(pod, req)
	}

	// https://github.com/kubernetes/enhancements/tree/master/keps/sig-scheduling/3521-pod-scheduling-readiness
	if pod.Spec.SchedulingGates == nil {
		pod.Spec.SchedulingGates = []corev1.PodSchedulingGate{}
	}

	// if the gate is already present, do not try to patch (it would fail)
	for _, schedulingGate := range pod.Spec.SchedulingGates {
		if schedulingGate.Name == schedulingGateName {
			return a.patchedPodResponse(pod, req)
		}
	}

	pod.Spec.SchedulingGates = append(pod.Spec.SchedulingGates, schedulingGate)

	// Temporary workaround. TODO[aleskandro]: remove when kubernetes/kubernetes#118052 is fixed.
	if pod.Spec.Affinity == nil {
		pod.Spec.Affinity = &corev1.Affinity{}
	}

	return a.patchedPodResponse(pod, req)
}
