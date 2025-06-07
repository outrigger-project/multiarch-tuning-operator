package models

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"

	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

type Pod struct {
	corev1.Pod
	ctx      context.Context
	recorder record.EventRecorder
}

func NewPod(pod *corev1.Pod, ctx context.Context, recorder record.EventRecorder) *Pod {
	return &Pod{
		Pod:      *pod,
		ctx:      ctx,
		recorder: recorder,
	}
}

// Ctx returns the context associated with the Pod.
func (pod *Pod) Ctx() context.Context {
	return pod.ctx
}

// Recorder returns the EventRecorder associated with the Pod.
func (pod *Pod) Recorder() record.EventRecorder {
	return pod.recorder
}

func (pod *Pod) PodObject() *corev1.Pod {
	return &pod.Pod
}

// PublishEvent publishes an event for the pod using the EventRecorder.
func (pod *Pod) PublishEvent(eventType, reason, message string) {
	if pod.recorder != nil {
		pod.recorder.Event(&pod.Pod, eventType, reason, message)
	}
}

// HasGate checks if the pod has a specific scheduling gate.
func (pod *Pod) HasGate(gateName string) bool {
	if pod.Spec.SchedulingGates == nil {
		return false
	}
	for _, gate := range pod.Spec.SchedulingGates {
		if gate.Name == gateName {
			return true
		}
	}
	return false
}

// AddGate adds a scheduling gate to the pod if it does not already exist.
func (pod *Pod) AddGate(gateName string) {
	// https://github.com/kubernetes/enhancements/tree/master/keps/sig-scheduling/3521-pod-scheduling-readiness
	if pod.HasGate(gateName) {
		// If the gate is already present, we return
		return
	}
	if pod.Spec.SchedulingGates == nil {
		pod.Spec.SchedulingGates = make([]corev1.PodSchedulingGate, 0)
	}
	pod.Spec.SchedulingGates = append(pod.Spec.SchedulingGates, corev1.PodSchedulingGate{Name: gateName})
}

// RemoveGate removes a scheduling gate from the pod if it exists.
func (pod *Pod) RemoveGate(gateName string) {
	if !pod.HasGate(gateName) {
		// If the gate is not present, we return
		return
	}
	filtered := make([]corev1.PodSchedulingGate, 0, len(pod.Spec.SchedulingGates)-1)
	for _, schedulingGate := range pod.Spec.SchedulingGates {
		if schedulingGate.Name != gateName {
			filtered = append(filtered, schedulingGate)
		}
	}
	pod.Spec.SchedulingGates = filtered
}

// EnsureLabel ensures that the pod has the given label with the given value.
func (pod *Pod) EnsureLabel(label string, value string) {
	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}
	pod.Labels[label] = value
}

// EnsureNoLabel ensures that the pod does not have the given label.
func (pod *Pod) EnsureNoLabel(label string) {
	if pod.Labels == nil {
		return
	}
	delete(pod.Labels, label)
}

// EnsureAnnotation ensures that the pod has the given annotation with the given value.
func (pod *Pod) EnsureAnnotation(annotation string, value string) {
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	pod.Annotations[annotation] = value
}

// EnsureAndIncrementLabel ensures that the pod has the given label with the given value.
// If the label is already set, it increments the value.
func (pod *Pod) EnsureAndIncrementLabel(label string) {
	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}
	if _, ok := pod.Labels[label]; !ok {
		pod.Labels[label] = "1"
		return
	}
	cur, err := strconv.ParseInt(pod.Labels[label], 10, 32)
	if err != nil {
		pod.Labels[label] = "1"
	} else {
		pod.Labels[label] = fmt.Sprintf("%d", cur+1)
	}
}

// HasControlPlaneNodeSelector returns true if the pod has a node selector that matches the control plane nodes.
func (pod *Pod) HasControlPlaneNodeSelector() bool {
	if pod.Spec.NodeSelector == nil {
		return false
	}
	requiredSelectors := []string{utils.MasterNodeSelectorLabel, utils.ControlPlaneNodeSelectorLabel}
	for _, value := range requiredSelectors {
		if _, ok := pod.Spec.NodeSelector[value]; ok {
			return true
		}
	}
	return false
}

// IsFromDaemonSet returns true if the pod is from a daemonSet.
func (pod *Pod) IsFromDaemonSet() bool {
	// Check all ownerRef
	for _, ownerRef := range pod.OwnerReferences {
		if ownerRef.Kind == "DaemonSet" && ownerRef.Controller != nil && *ownerRef.Controller {
			return true
		}
	}
	return false
}

func (pod *Pod) ContainerNameFor(containerID string) (string, error) {
	// The containerID is in the format: "runtime://<64-hex-chars>"
	matched, err := regexp.MatchString(`^.+://[a-f0-9]{64}$`, containerID)
	if err != nil || !matched {
		return "", fmt.Errorf("invalid container ID format: %s", containerID)
	}
	for _, container := range pod.PodObject().Status.ContainerStatuses {
		if container.ContainerID == containerID {
			return container.Name, nil
		}
	}
	return "", fmt.Errorf("container with ID %s not found in pod %s", containerID, pod.Name)
}
