package builder

import v1 "k8s.io/api/core/v1"

// ContainerStatusBuilder is a builder for creating container statuses.
type ContainerStatusBuilder struct {
	containerStatus v1.ContainerStatus
}

// NewContainerStatus creates a new instance of ContainerStatusBuilder.
func NewContainerStatus() *ContainerStatusBuilder {
	return &ContainerStatusBuilder{
		containerStatus: v1.ContainerStatus{},
	}
}

// WithName sets the name of the container.
func (b *ContainerStatusBuilder) WithName(name string) *ContainerStatusBuilder {
	b.containerStatus.Name = name
	return b
}

// WithState sets the state of the container.
func (b *ContainerStatusBuilder) WithState(state v1.ContainerState) *ContainerStatusBuilder {
	b.containerStatus.State = state
	return b
}

// WithReady sets the ready status of the container.
func (b *ContainerStatusBuilder) WithReady(ready bool) *ContainerStatusBuilder {
	b.containerStatus.Ready = ready
	return b
}

// WithRestartCount sets the restart count of the container.
func (b *ContainerStatusBuilder) WithRestartCount(count int32) *ContainerStatusBuilder {
	b.containerStatus.RestartCount = count
	return b
}

// WithID sets the ID of the container.
func (b *ContainerStatusBuilder) WithID(id string) *ContainerStatusBuilder {
	b.containerStatus.ContainerID = id
	return b
}

// Build returns the constructed ContainerStatus.
func (b *ContainerStatusBuilder) Build() v1.ContainerStatus {
	return b.containerStatus
}
