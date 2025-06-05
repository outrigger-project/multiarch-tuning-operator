package operator

import (
	"fmt"
	"os"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

const (
	CREATE = "create"
	UPDATE = "update"
	PATCH  = "patch"
	LIST   = "list"
	WATCH  = "watch"
	GET    = "get"
	USE    = "use"
	DELETE = "delete"

	serviceAccountKind = "ServiceAccount"
	roleKind           = "Role"
	clusterRoleKind    = "ClusterRole"

	// xref: https://github.com/openshift/enhancements/blob/9b5d8a964fc/enhancements/authentication/custom-scc-preemption-prevention.md
	requiredSCCAnnotation   = "openshift.io/required-scc"
	requiredSCCRestrictedV2 = "restricted-v2"
)

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

func buildService(name string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: utils.Namespace(),
			Labels: map[string]string{
				utils.OperandLabelKey:   operandName,
				utils.ControllerNameKey: name,
			},
			Annotations: map[string]string{
				"service.beta.openshift.io/serving-cert-secret-name": name,
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "https",
					Port:       443,
					TargetPort: intstr.FromInt32(9443),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "metrics",
					Port:       8443,
					TargetPort: intstr.FromInt32(8443),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Selector: map[string]string{
				utils.OperandLabelKey:   operandName,
				utils.ControllerNameKey: name,
			},
		},
	}
}

func buildWebhookDeployment(clusterPodPlacementConfig *v1beta1.ClusterPodPlacementConfig) *appsv1.Deployment {
	return buildDeployment(clusterPodPlacementConfig, utils.PodPlacementWebhookName, 3, utils.PodPlacementWebhookName, "",
		"--enable-ppc-webhook", "--enable-cppc-informer",
	)

}

func buildControllerDeployment(clusterPodPlacementConfig *v1beta1.ClusterPodPlacementConfig, requiredSCCHostmoundAnyUID string, seLinuxOptionsType *corev1.SELinuxOptions) *appsv1.Deployment {
	d := buildDeployment(clusterPodPlacementConfig, utils.PodPlacementControllerName, 2, utils.PodPlacementControllerName,
		utils.PodPlacementFinalizerName, "--leader-elect", "--enable-ppc-controllers", "--enable-cppc-informer",
	)
	if d.Spec.Template.Annotations == nil {
		d.Spec.Template.Annotations = map[string]string{}
	}
	d.Spec.Template.Annotations[requiredSCCAnnotation] = requiredSCCHostmoundAnyUID
	d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes,
		corev1.Volume{
			Name: "docker-conf",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/etc/docker/",
					Type: utils.NewPtr(corev1.HostPathDirectoryOrCreate),
				},
			},
		},
		corev1.Volume{
			Name: "containers-conf",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/etc/containers/",
					Type: utils.NewPtr(corev1.HostPathDirectoryOrCreate),
				},
			},
		},
	)
	d.Spec.Template.Spec.Containers[0].VolumeMounts = append(d.Spec.Template.Spec.Containers[0].VolumeMounts,
		corev1.VolumeMount{
			Name:      "docker-conf",
			MountPath: "/etc/docker/",
			ReadOnly:  true,
		},
		corev1.VolumeMount{
			Name:      "containers-conf",
			MountPath: "/etc/containers/",
			ReadOnly:  true,
		},
	)
	if seLinuxOptionsType != nil {
		if d.Spec.Template.Spec.Containers[0].SecurityContext == nil {
			d.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{}
		}
		d.Spec.Template.Spec.Containers[0].SecurityContext.SELinuxOptions = seLinuxOptionsType
	}

	return d
}

func buildDeployment(clusterPodPlacementConfig *v1beta1.ClusterPodPlacementConfig,
	name string, replicas int32, serviceAccount string, finalizer string, args ...string) *appsv1.Deployment {
	finalizers := make([]string, 0)
	if finalizer != "" {
		finalizers = append(finalizers, finalizer)
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: utils.Namespace(),
			Labels: map[string]string{
				utils.OperandLabelKey:   operandName,
				utils.ControllerNameKey: name,
			},
			Finalizers: finalizers,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: utils.NewPtr(replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					utils.OperandLabelKey:   operandName,
					utils.ControllerNameKey: name,
				},
			},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxSurge:       utils.NewPtr(intstr.FromString("25%")),
					MaxUnavailable: utils.NewPtr(intstr.FromString("25%")),
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						utils.OperandLabelKey:   operandName,
						utils.ControllerNameKey: name,
					},
					Annotations: map[string]string{
						// See https://github.com/openshift/enhancements/blob/c5b9aea25e/enhancements/workload-partitioning/management-workload-partitioning.md
						"target.workload.openshift.io/management": "{\"effect\": \"PreferredDuringScheduling\"}",
						requiredSCCAnnotation:                     requiredSCCRestrictedV2,
					},
				},
				Spec: corev1.PodSpec{
					AutomountServiceAccountToken: utils.NewPtr(true),
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      utils.ArchLabel,
												Operator: corev1.NodeSelectorOpIn,
												Values: []string{
													utils.ArchitectureAmd64,
													utils.ArchitectureArm64,
													utils.ArchitectureS390x,
													utils.ArchitecturePpc64le,
												},
											},
										},
									},
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:            name,
							Image:           utils.Image(),
							ImagePullPolicy: corev1.PullIfNotPresent,
							Env: []corev1.EnvVar{
								{
									Name:  "NAMESPACE",
									Value: utils.Namespace(),
								},
								{
									Name:  "HTTP_PROXY",
									Value: os.Getenv("HTTP_PROXY"),
								},
								{
									Name:  "HTTPS_PROXY",
									Value: os.Getenv("HTTPS_PROXY"),
								},

								{
									Name:  "NO_PROXY",
									Value: os.Getenv("NO_PROXY"),
								},
							},
							Args: append([]string{
								"--health-probe-bind-address=:8081",
								"--metrics-bind-address=:8443",
								fmt.Sprintf("--initial-log-level=%d",
									clusterPodPlacementConfig.Spec.LogVerbosity.ToZapLevelInt()),
							}, args...),
							Command: []string{
								"/manager",
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/healthz",
										Port:   intstr.FromInt32(8081),
										Scheme: corev1.URISchemeHTTP,
									},
								},
								InitialDelaySeconds: 15,
								TimeoutSeconds:      1,
								PeriodSeconds:       20,
								SuccessThreshold:    1,
								FailureThreshold:    3,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/readyz",
										Port:   intstr.FromInt32(8081),
										Scheme: corev1.URISchemeHTTP,
									},
								},
								InitialDelaySeconds: 5,
								TimeoutSeconds:      1,
								PeriodSeconds:       10,
								SuccessThreshold:    1,
								FailureThreshold:    3,
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("10m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
							},
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: utils.NewPtr(false),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{
										"ALL",
									},
								},
								Privileged:             utils.NewPtr(false),
								ReadOnlyRootFilesystem: utils.NewPtr(true),
								RunAsNonRoot:           utils.NewPtr(true),
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "webhook-server-cert",
									MountPath: "/var/run/manager/tls",
									ReadOnly:  true,
								},
								{
									Name:      "trusted-ca",
									MountPath: "/etc/pki/ca-trust/extracted/pem",
									ReadOnly:  true,
								},
							},
						},
					},
					PriorityClassName:  priorityClassName,
					ServiceAccountName: serviceAccount,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: utils.NewPtr(true),
					},
					TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
						{
							MaxSkew:           1,
							TopologyKey:       "kubernetes.io/hostname",
							WhenUnsatisfiable: corev1.ScheduleAnyway,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									utils.OperandLabelKey:   operandName,
									utils.ControllerNameKey: name,
								},
							},
							MatchLabelKeys: []string{"pod-template-hash"},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "webhook-server-cert",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName:  name,
									DefaultMode: utils.NewPtr(int32(420)),
								},
							},
						},
						{
							Name: "trusted-ca",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "multiarch-tuning-operator-trusted-ca",
									},
									Items: []corev1.KeyToPath{
										{
											Key:  "ca-bundle.crt",
											Path: "tls-ca-bundle.pem",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func buildClusterRole(name string, rules []rbacv1.PolicyRule) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				utils.OperandLabelKey:   operandName,
				utils.ControllerNameKey: name,
			},
		},
		Rules: rules,
	}
}

func buildClusterRoleBinding(name string, roleRef rbacv1.RoleRef, subjects []rbacv1.Subject) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				utils.OperandLabelKey:   operandName,
				utils.ControllerNameKey: name,
			},
		},
		RoleRef:  roleRef,
		Subjects: subjects,
	}
}

func buildRoleBinding(name string, roleRef rbacv1.RoleRef, subjects []rbacv1.Subject) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: utils.Namespace(),
			Labels: map[string]string{
				utils.OperandLabelKey:   operandName,
				utils.ControllerNameKey: name,
			},
		},
		RoleRef:  roleRef,
		Subjects: subjects,
	}
}

func buildServiceAccount(name string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: utils.Namespace(),
			Labels: map[string]string{
				utils.OperandLabelKey:   operandName,
				utils.ControllerNameKey: name,
			},
		},
		AutomountServiceAccountToken: utils.NewPtr(false),
	}
}

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

func buildServiceMonitor(name string) *monitoringv1.ServiceMonitor {
	return &monitoringv1.ServiceMonitor{
		TypeMeta: metav1.TypeMeta{
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
			Kind:       monitoringv1.ServiceMonitorsKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: utils.Namespace(),
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Endpoints: []monitoringv1.Endpoint{
				{
					HonorLabels:     true,
					Path:            "/metrics",
					Port:            "metrics",
					Scheme:          "https",
					BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
					TLSConfig: &monitoringv1.TLSConfig{
						CAFile: "/etc/prometheus/configmaps/serving-certs-ca-bundle/service-ca.crt",
						SafeTLSConfig: monitoringv1.SafeTLSConfig{
							ServerName: utils.NewPtr(fmt.Sprintf("%s.%s.svc", name, utils.Namespace())),
						},
					},
				},
			},
			NamespaceSelector: monitoringv1.NamespaceSelector{
				MatchNames: []string{utils.Namespace()},
			},
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					utils.ControllerNameKey: name,
				},
			},
		},
	}
}

func buildAvailabilityAlertRule() *monitoringv1.PrometheusRule {
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
