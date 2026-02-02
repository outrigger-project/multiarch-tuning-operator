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

const (
	// ENoExecEventErrorLabel is the label key used to mark an ENoExecEvent CR as having encountered an error during reconciliation
	ENoExecEventErrorLabel = "multiarch.openshift.io/enoexec-event-error"
	// Label values for error reasons (stored in ENoExecEventErrorLabel)
	ErrorReasonPodNotFound       = "pod-not-found"
	ErrorReasonNodeNotFound      = "node-not-found"
	ErrorReasonContainerNotFound = "container-not-found"
	ErrorReasonWrongNamespace    = "wrong-namespace"
	ErrorReasonReconciliation    = "reconciliation-error"
)
