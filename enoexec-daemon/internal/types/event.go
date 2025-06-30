package types

// ENOEXECInternalEvent is an event that is emitted when the eBPF tracepoint
// detects an ENOEXEC error. This event contains information about the pod that
// encountered the error, and is sent to the internal event bus to be processed by
// the consumer process running the storage implementation and then adapted into an ENoExecEvent K8S object.
type ENOEXECInternalEvent struct {
	PodName      string `yaml:"podName,omitempty"`
	PodNamespace string `yaml:"podNamespace,omitempty"`
	ContainerID  string `yaml:"containerID,omitempty"`
	ProcessName  string `yaml:"processName,omitempty"`
}
