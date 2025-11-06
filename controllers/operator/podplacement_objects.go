package operator

import (
	"fmt"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

// buildMutatingWebhookConfiguration creates the MutatingWebhookConfiguration for the pod placement webhook.
func buildMutatingWebhookConfiguration(clusterPodPlacementConfig *v1beta1.ClusterPodPlacementConfig) *admissionv1.MutatingWebhookConfiguration {
	return &admissionv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: utils.PodMutatingWebhookConfigurationName,
			Labels: map[string]string{
				utils.OperandLabelKey:   operandName,
				utils.ControllerNameKey: utils.PodPlacementWebhookName,
			},
			Annotations: map[string]string{
				"service.beta.openshift.io/inject-cabundle": "true",
			},
		},
		Webhooks: []admissionv1.MutatingWebhook{
			{
				AdmissionReviewVersions: []string{"v1"},
				ClientConfig: admissionv1.WebhookClientConfig{
					Service: &admissionv1.ServiceReference{
						Name:      utils.PodPlacementWebhookName,
						Namespace: utils.Namespace(),
						Path:      utils.NewPtr("/add-pod-scheduling-gate"),
					},
				},
				NamespaceSelector: clusterPodPlacementConfig.Spec.NamespaceSelector,
				FailurePolicy:     utils.NewPtr(admissionv1.Ignore),
				SideEffects:       utils.NewPtr(admissionv1.SideEffectClassNone),
				Name:              utils.PodMutatingWebhookName,
				Rules: []admissionv1.RuleWithOperations{
					{
						Operations: []admissionv1.OperationType{
							admissionv1.Create,
						},
						Rule: admissionv1.Rule{
							APIGroups:   []string{""},
							APIVersions: []string{"v1"},
							Resources:   []string{"pods"},
						},
					},
				},
			},
		},
	}
}

// buildWebhookDeployment creates the specific deployment for the pod-placement-webhook.
func buildWebhookDeployment(clusterPodPlacementConfig *v1beta1.ClusterPodPlacementConfig) *appsv1.Deployment {
	d := buildDeployment(clusterPodPlacementConfig.Spec.LogVerbosity.ToZapLevelInt(), utils.PodPlacementWebhookName, 3, utils.PodPlacementWebhookName, "",
		"--enable-ppc-webhook", "--enable-cppc-informer",
	)
	additionalVolumes := []corev1.Volume{
		{
			Name: "webhook-server-cert",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  utils.PodPlacementWebhookName,
					DefaultMode: utils.NewPtr(int32(420)),
				},
			},
		},
	}
	additionalMounts := []corev1.VolumeMount{
		{
			Name:      "webhook-server-cert",
			MountPath: "/var/run/manager/tls",
			ReadOnly:  true,
		},
	}

	// 3. Append the additional items to the base slices.
	d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, additionalVolumes...)
	d.Spec.Template.Spec.Containers[0].VolumeMounts = append(d.Spec.Template.Spec.Containers[0].VolumeMounts, additionalMounts...)

	return d
}

// buildControllerDeployment creates the Deployment for the cluster pod placement config controller.
func buildControllerDeployment(clusterPodPlacementConfig *v1beta1.ClusterPodPlacementConfig, requiredSCCHostmoundAnyUID string, seLinuxOptionsType *corev1.SELinuxOptions) *appsv1.Deployment {
	d := buildDeployment(clusterPodPlacementConfig.Spec.LogVerbosity.ToZapLevelInt(), utils.PodPlacementControllerName, 2, utils.PodPlacementControllerName,
		utils.PodPlacementFinalizerName, "--leader-elect", "--enable-ppc-controllers", "--enable-cppc-informer",
	)
	if d.Spec.Template.Annotations == nil {
		d.Spec.Template.Annotations = map[string]string{}
	}
	d.Spec.Template.Annotations[requiredSCCAnnotation] = requiredSCCHostmoundAnyUID

	// 2. Define all additional volumes and mounts needed for this specific controller.
	additionalVolumes := []corev1.Volume{
		{
			Name: "webhook-server-cert",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  utils.PodPlacementControllerName,
					DefaultMode: utils.NewPtr(int32(420)),
				},
			},
		},
		{
			Name: "docker-conf",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/etc/docker/",
					Type: utils.NewPtr(corev1.HostPathDirectoryOrCreate),
				},
			},
		},
		{
			Name: "containers-conf",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/etc/containers/",
					Type: utils.NewPtr(corev1.HostPathDirectoryOrCreate),
				},
			},
		},
		{
			Name: "shortnames-cache",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	additionalMounts := []corev1.VolumeMount{
		{
			Name:      "webhook-server-cert",
			MountPath: "/var/run/manager/tls",
			ReadOnly:  true,
		},
		{
			Name:      "docker-conf",
			MountPath: "/etc/docker/",
			ReadOnly:  true,
		},
		{
			Name:      "containers-conf",
			MountPath: "/etc/containers/",
			ReadOnly:  true,
		},
		{
			Name:      "shortnames-cache",
			MountPath: "/tmp/container/cache",
		},
	}
	additionalEnv := []corev1.EnvVar{
		{
			Name:  "XDG_CACHE_HOME",
			Value: "/tmp/container/cache",
		},
	}

	// 3. Append the additional volumes and mounts to the base ones from the generic builder.
	d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, additionalVolumes...)
	d.Spec.Template.Spec.Containers[0].Env = append(d.Spec.Template.Spec.Containers[0].Env, additionalEnv...)
	d.Spec.Template.Spec.Containers[0].VolumeMounts = append(d.Spec.Template.Spec.Containers[0].VolumeMounts, additionalMounts...)

	if seLinuxOptionsType != nil {
		if d.Spec.Template.Spec.Containers[0].SecurityContext == nil {
			d.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{}
		}
		d.Spec.Template.Spec.Containers[0].SecurityContext.SELinuxOptions = seLinuxOptionsType
	}

	return d
}

// buildClusterRoleWebhook defines the cluster-wide permissions required by the cluster pod placement config webhook.
func buildClusterRoleWebhook() *rbacv1.ClusterRole {
	return buildClusterRole(utils.PodPlacementWebhookName, []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"events"},
			Verbs:     []string{CREATE, PATCH},
		},
		{
			APIGroups: []string{v1beta1.GroupVersion.Group},
			Resources: []string{v1beta1.ClusterPodPlacementConfigResource},
			Verbs:     []string{LIST, WATCH, GET},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"pods"},
			Verbs:     []string{LIST, WATCH, GET},
		},
		{
			APIGroups: []string{"authentication.k8s.io"},
			Resources: []string{"tokenreviews"},
			Verbs:     []string{CREATE},
		},
		{
			APIGroups: []string{"authorization.k8s.io"},
			Resources: []string{"subjectaccessreviews"},
			Verbs:     []string{CREATE},
		},
	})
}

// buildClusterRoleController defines the cluster-wide permissions required by the cluster pod placement controller.
func buildClusterRoleController() *rbacv1.ClusterRole {
	return buildClusterRole(utils.PodPlacementControllerName, []rbacv1.PolicyRule{
		{
			APIGroups: []string{"security.openshift.io"},
			Resources: []string{"securitycontextconstraints"},
			Verbs:     []string{USE},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"pods"},
			Verbs:     []string{LIST, WATCH, GET, UPDATE},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"events"},
			Verbs:     []string{CREATE, PATCH},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"pods/status"},
			Verbs:     []string{UPDATE},
		},
		{
			APIGroups: []string{v1beta1.GroupVersion.Group},
			Resources: []string{v1beta1.ClusterPodPlacementConfigResource},
			Verbs:     []string{LIST, WATCH, GET},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"configmaps", "secrets"},
			Verbs:     []string{LIST, WATCH, GET},
		},
		{
			APIGroups: []string{"authentication.k8s.io"},
			Resources: []string{"tokenreviews"},
			Verbs:     []string{CREATE},
		},
		{
			APIGroups: []string{"authorization.k8s.io"},
			Resources: []string{"subjectaccessreviews"},
			Verbs:     []string{CREATE},
		},
	})
}

// buildRoleController defines the namespace-scoped permissions for the pod placement controller.
// These permissions are primarily for managing leader election leases within the operator's namespace.
func buildRoleController() *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.PodPlacementControllerName,
			Namespace: utils.Namespace(),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"configmaps"},
				Verbs:     []string{LIST, WATCH, GET, UPDATE, PATCH, CREATE, DELETE},
			},
			{
				APIGroups: []string{"coordination.k8s.io"},
				Resources: []string{"leases"},
				Verbs:     []string{LIST, WATCH, GET, UPDATE, PATCH, CREATE, DELETE},
			},
		},
	}
}

func buildCPPCAvailabilityAlertRule() *monitoringv1.PrometheusRule {
	return &monitoringv1.PrometheusRule{
		TypeMeta: metav1.TypeMeta{
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
			Kind:       monitoringv1.PrometheusRuleKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.OperatorName,
			Namespace: utils.Namespace(),
		},
		Spec: monitoringv1.PrometheusRuleSpec{
			Groups: []monitoringv1.RuleGroup{
				{
					Name: "multiarch-tuning-operator.rules",
					Rules: []monitoringv1.Rule{
						{
							Alert: "PodPlacementControllerDown",
							Expr:  intstr.FromString(fmt.Sprintf("kube_deployment_status_replicas_available{namespace=\"%s\", deployment=\"%s\"} == 0", utils.Namespace(), utils.PodPlacementControllerName)),
							For:   utils.NewPtr[monitoringv1.Duration]("1m"),
							Annotations: map[string]string{
								"summary": "The pod placement controller should have at least 1 replica running and ready.",
								"description": "The pod placement controller has been down for more than 1 minute. " +
									"If the controller is not running, no architecture constraints can be set. " +
									"The multiarch.openshift.io/scheduling-gate scheduling gate will not be " +
									"automatically removed from gated pods, and pods may stuck in the Pending state.",
								"runbook_url": "https://github.com/openshift/multiarch-tuning-operator/blob/main/docs/alerts/pod-placement-controller-down.md",
							},
							Labels: map[string]string{
								"severity": "critical",
							},
						},
						{
							Alert: "PodPlacementWebhookDown",
							Expr:  intstr.FromString(fmt.Sprintf("kube_deployment_status_replicas_available{namespace=\"%s\", deployment=\"%s\"} == 0", utils.Namespace(), utils.PodPlacementWebhookName)),
							For:   utils.NewPtr[monitoringv1.Duration]("5m"),
							Annotations: map[string]string{
								"summary": "The pod placement webhook should have at least 1 replica running and ready.",
								"description": "The pod placement webhook has been down for more than 5 minutes. Pods will not be gated. " +
									"Therefore, the architecture-specific constraints will not be enforced and pods may be scheduled on nodes " +
									"that are not supported by their images.",
								"runbook_url": "https://github.com/openshift/multiarch-tuning-operator/blob/main/docs/alerts/pod-placement-webhook-down.md",
							},
							Labels: map[string]string{
								"severity": "warning",
							},
						},
					},
				},
			},
		},
	}
}
