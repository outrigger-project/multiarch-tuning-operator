package builder

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DeploymentBuilder is a builder for appsv1.Deployment objects to be used only in unit tests.
type DeploymentBuilder struct {
	deployment *appsv1.Deployment
}

// NewDeployment returns a new DeploymentBuilder to build appsv1.Deployment objects. It is meant to be used only in unit tests.
func NewDeployment() *DeploymentBuilder {
	return &DeploymentBuilder{
		deployment: &appsv1.Deployment{},
	}
}

func (d *DeploymentBuilder) WithPodSpec(ps corev1.PodSpec) *DeploymentBuilder {
	d.deployment.Spec.Template.Spec = ps
	return d
}

func (d *DeploymentBuilder) WithReplicas(num *int32) *DeploymentBuilder {
	d.deployment.Spec.Replicas = num
	return d
}

func (d *DeploymentBuilder) WithSelectorAndPodLabels(entries map[string]string) *DeploymentBuilder {
	if d.deployment.Spec.Template.Labels == nil && len(entries) > 0 {
		d.deployment.Spec.Template.Labels = make(map[string]string, len(entries))
	}
	if d.deployment.Spec.Selector == nil {
		d.deployment.Spec.Selector = &metav1.LabelSelector{}
	}
	if d.deployment.Spec.Selector.MatchLabels == nil && len(entries) > 0 {
		d.deployment.Spec.Selector.MatchLabels = make(map[string]string, len(entries))
	}
	for k, v := range entries {
		d.deployment.Spec.Template.Labels[k] = v
		d.deployment.Spec.Selector.MatchLabels[k] = v
	}
	return d
}

func (d *DeploymentBuilder) WithName(name string) *DeploymentBuilder {
	d.deployment.Name = name
	return d
}

func (d *DeploymentBuilder) WithNamespace(namespace string) *DeploymentBuilder {
	d.deployment.Namespace = namespace
	return d
}

func (d *DeploymentBuilder) WithPodAnnotations(annotations map[string]string) *DeploymentBuilder {
	if d.deployment.Spec.Template.Annotations == nil {
		d.deployment.Spec.Template.Annotations = make(map[string]string, len(annotations))
	}
	for k, v := range annotations {
		d.deployment.Spec.Template.Annotations[k] = v
	}
	return d
}

func (d *DeploymentBuilder) Build() *appsv1.Deployment {
	return d.deployment
}
