package builder

import (
	corev1 "k8s.io/api/core/v1"
)

// NodeBuilder is a utility to build Kubernetes Node objects.
type NodeBuilder struct {
	node *corev1.Node
}

// NewNodeBuilder creates a new instance of NodeBuilder.
func NewNodeBuilder() *NodeBuilder {
	return &NodeBuilder{
		node: &corev1.Node{},
	}
}

// WithName sets the name of the Node.
func (b *NodeBuilder) WithName(name string) *NodeBuilder {
	b.node.Name = name
	return b
}

// WithLabel adds a label to the Node.
func (b *NodeBuilder) WithLabel(key, value string) *NodeBuilder {
	if b.node.Labels == nil {
		b.node.Labels = make(map[string]string)
	}
	b.node.Labels[key] = value
	return b
}

// WithAnnotation adds an annotation to the Node.
func (b *NodeBuilder) WithAnnotation(key, value string) *NodeBuilder {
	if b.node.Annotations == nil {
		b.node.Annotations = make(map[string]string)
	}
	b.node.Annotations[key] = value
	return b
}

// WithTaint adds a taint to the Node.
func (b *NodeBuilder) WithTaint(taint corev1.Taint) *NodeBuilder {
	b.node.Spec.Taints = append(b.node.Spec.Taints, taint)
	return b
}

// Build finalizes and returns the Node object.
func (b *NodeBuilder) Build() *corev1.Node {
	return b.node
}
