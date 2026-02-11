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

package handler

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"

	multiarchv1beta1 "github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

// ENoExecEvent wraps the multiarchv1beta1.ENoExecEvent with additional context and event recording capabilities
type ENoExecEvent struct {
	multiarchv1beta1.ENoExecEvent
	ctx      context.Context
	recorder record.EventRecorder
}

// NewENoExecEvent creates a new ENoExecEvent wrapper
func NewENoExecEvent(event *multiarchv1beta1.ENoExecEvent, ctx context.Context, recorder record.EventRecorder) *ENoExecEvent {
	return &ENoExecEvent{
		ENoExecEvent: *event,
		ctx:          ctx,
		recorder:     recorder,
	}
}

// Ctx returns the context associated with the ENoExecEvent
func (e *ENoExecEvent) Ctx() context.Context {
	return e.ctx
}

// Recorder returns the EventRecorder associated with the ENoExecEvent
func (e *ENoExecEvent) Recorder() record.EventRecorder {
	return e.recorder
}

// ENoExecEventObject returns the underlying ENoExecEvent object
func (e *ENoExecEvent) ENoExecEventObject() *multiarchv1beta1.ENoExecEvent {
	return &e.ENoExecEvent
}

// EnsureLabel ensures that the ENoExecEvent has the given label with the given value
func (e *ENoExecEvent) EnsureLabel(label string, value string) {
	if e.Labels == nil {
		e.Labels = make(map[string]string)
	}
	e.Labels[label] = value
}

// EnsureNoLabel ensures that the ENoExecEvent does not have the given label
func (e *ENoExecEvent) EnsureNoLabel(label string) {
	if e.Labels == nil {
		return
	}
	delete(e.Labels, label)
}

// MarkAsError marks the ENoExecEvent CR as having encountered a reconciliation error
// This allows the cleanup logic to identify and bypass errored CRs during plugin disable/uninstall
func (e *ENoExecEvent) MarkAsError() {
	e.EnsureLabel(ENoExecEventErrorLabel, utils.True)
}

// IsMarkedAsError returns true if the ENoExecEvent has been marked with an error label
func (e *ENoExecEvent) IsMarkedAsError() bool {
	if e.Labels == nil {
		return false
	}
	value, exists := e.Labels[ENoExecEventErrorLabel]
	return exists && value == utils.True
}

// HasErrorLabel returns true if the ENoExecEvent has the error label (regardless of value)
func (e *ENoExecEvent) HasErrorLabel() bool {
	if e.Labels == nil {
		return false
	}
	_, exists := e.Labels[ENoExecEventErrorLabel]
	return exists
}

// PublishEvent publishes a Kubernetes event for the ENoExecEvent CR
func (e *ENoExecEvent) PublishEvent(eventType, reason, message string) {
	if e.recorder != nil {
		e.recorder.Event(&e.ENoExecEvent, eventType, reason, message)
	}
}

// PublishEventOnPod publishes a Kubernetes event on the source pod referenced by this ENoExecEvent
// This provides visibility of the reconciliation error in the pod's event log
func (e *ENoExecEvent) PublishEventOnPod(pod *corev1.Pod, eventType, reason, message string) {
	if e.recorder != nil && pod != nil {
		e.recorder.Event(pod, eventType, reason, message)
	}
}
