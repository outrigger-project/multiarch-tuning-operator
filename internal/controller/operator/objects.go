package operator

import (
	"fmt"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

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

func buildDeployment(logVerbosity int,
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
									logVerbosity),
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
							// Generic volume mounts that all deployments need
							VolumeMounts: []corev1.VolumeMount{
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
					// Generic volumes that all deployments need
					Volumes: []corev1.Volume{
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
