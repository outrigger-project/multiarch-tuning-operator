package types

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"

	multiarchv1beta1 "github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1beta1"
)

// ENOEXECInternalEvent is an event that is emitted when the eBPF tracepoint
// detects an ENOEXEC error. This event contains information about the pod that
// encountered the error, and is sent to the internal event bus to be processed by
// the consumer process running the storage implementation and then adapted into an ENoExecEvent K8S object.
type ENOEXECInternalEvent struct {
	PodName      string `yaml:"podName,omitempty"`
	PodNamespace string `yaml:"podNamespace,omitempty"`
	ContainerID  string `yaml:"containerID,omitempty"`
}

// ToENoExecEvent converts the ENOEXECInternalEvent to a multiarchv1beta1.ENOExecEvent that can be stored in Kubernetes.
func (e *ENOEXECInternalEvent) ToENoExecEvent(namespace string, nodeName string) (*multiarchv1beta1.ENoExecEvent, error) {
	return &multiarchv1beta1.ENoExecEvent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      string(uuid.NewUUID()),
			Namespace: namespace,
		},
		Status: multiarchv1beta1.ENoExecEventStatus{
			NodeName:     nodeName,
			PodName:      e.PodName,
			PodNamespace: e.PodNamespace,
			ContainerID:  e.ContainerID,
		},
	}, nil
}
