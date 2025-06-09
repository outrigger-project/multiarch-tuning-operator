package types

// ENOEXECInternalEvent is an event that is emitted when the eBPF tracepoint
// detects an ENOEXEC error. This event contains information about the pod that
// encountered the error, and is sent to the internal event bus (FIFO pipe at the time of writing)
// to be processed by the consumer process and adapted into a ENoExecEvent K8S object.
type ENOEXECInternalEvent struct {
	PodName      string `protobuf:"bytes,1,opt,name=pod_name,json=podName,proto3" json:"pod_name,omitempty"`
	PodNamespace string `protobuf:"bytes,2,opt,name=pod_namespace,json=podNamespace,proto3" json:"pod_namespace,omitempty"`
	ContainerID  string `protobuf:"bytes,3,opt,name=container_id,json=containerId,proto3" json:"container_id,omitempty"`
}
