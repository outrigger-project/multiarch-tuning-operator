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

	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

const (
	// SchedulingGateName is the name of the Scheduling Gate
	schedulingGateName = "multiarch.openshift.io/scheduling-gate"
)

var schedulingGate = corev1.PodSchedulingGate{
	Name: schedulingGateName,
}

// [disabled:operator]kubebuilder:webhook:path=/add-pod-scheduling-gate,mutating=true,sideEffects=None,admissionReviewVersions=v1,failurePolicy=ignore,groups="",resources=pods,verbs=create,versions=v1,name=pod-placement-scheduling-gate.multiarch.openshift.io

// PodSchedulingGateMutatingWebHook annotates Pods
type PodSchedulingGateMutatingWebHook struct {
	Client  client.Client
	decoder *admission.Decoder
	Scheme  *runtime.Scheme
	NsCache *NamespaceCache
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

	// ignore the hypershift-* and kube-* namespace as those are infra components, and ignore the namespace where the operand is running too
	if utils.Namespace() == pod.Namespace || strings.HasPrefix(pod.Namespace, "hypershift-") || strings.HasPrefix(pod.Namespace, "kube-") ||
		pod.Spec.NodeName != "" {
		return a.patchedPodResponse(pod, req)
	}

	// For openshift namespaces, exclude accrodingly
	//   - check for any namespace having ownerReference of ClusterVersion of Network
	//   - exclude the namespace if it does. If it is a namespace starting with openshift* and does not have the "-operator" suffix, add the "-operator" suffix
	//     and check if the parent operator is managed by CVO. If so ,exclude this operand namespace as well.
	//   - exclude "openshift-route-controller-manager" and "openshift-ingress-canary"
	//   - include the "openshift-marketplace" and "openshift-operators" namespaces
	// TODO: How do we firm up this logic for operand namespaces ?
	if a.NsCache.ShouldExcludeNamespace(pod.Namespace) {
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

	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}
	// We also add a label to the pod to indicate that the scheduling gate was added
	// and this pod expects processing by the operator. That's useful for testing and debugging, but also gives the user
	// an indication that the pod is waiting for processing and can support kubectl queries to find out which pods are
	// waiting for processing, for example when the operator is being uninstalled.
	pod.Labels[utils.SchedulingGateLabel] = utils.SchedulingGateLabelValueGated
	pod.Labels[utils.NodeAffinityLabel] = utils.NodeAffinityLabelValueNotSet
	return a.patchedPodResponse(pod, req)
}
