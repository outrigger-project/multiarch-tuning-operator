package operator

import (
	"fmt"
	"os"

	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func buildWebhookDeployment(clusterPodPlacementConfig *v1beta1.ClusterPodPlacementConfig) *appsv1.Deployment {
	return buildDeployment(clusterPodPlacementConfig, utils.PodPlacementWebhookName, 3, utils.PodPlacementWebhookName, "",
		"--enable-ppc-webhook", "--enable-cppc-informer",
	)

}

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
