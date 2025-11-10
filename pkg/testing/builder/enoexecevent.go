package builder

import (
	"github.com/openshift/multiarch-tuning-operator/api/v1beta1"
)

type ENoExecEventBuilder struct {
	*v1beta1.ENoExecEvent
}

func NewENoExecEvent() *ENoExecEventBuilder {
	return &ENoExecEventBuilder{
		ENoExecEvent: &v1beta1.ENoExecEvent{},
	}
}

func (p *ENoExecEventBuilder) WithName(name string) *ENoExecEventBuilder {
	p.Name = name
	return p
}

func (p *ENoExecEventBuilder) WithNamespace(name string) *ENoExecEventBuilder {
	p.Namespace = name
	return p
}

func (p *ENoExecEventBuilder) WithNodeName(nodeName string) *ENoExecEventBuilder {
	p.Status.NodeName = nodeName
	return p
}

func (p *ENoExecEventBuilder) WithPodName(podName string) *ENoExecEventBuilder {
	p.Status.PodName = podName
	return p
}

func (p *ENoExecEventBuilder) WithPodNamespace(podNamespace string) *ENoExecEventBuilder {
	p.Status.PodNamespace = podNamespace
	return p
}

func (p *ENoExecEventBuilder) WithContainerID(containerID string) *ENoExecEventBuilder {
	p.Status.ContainerID = containerID
	return p
}

func (p *ENoExecEventBuilder) Build() *v1beta1.ENoExecEvent {
	return p.ENoExecEvent
}
