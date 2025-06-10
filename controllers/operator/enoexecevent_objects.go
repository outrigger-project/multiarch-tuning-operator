package operator

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

func BuildEnoexecClusterRoleController() *rbacv1.ClusterRole {
	return buildClusterRole(utils.EnoexecControllerName, []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"pods"},
			Verbs:     []string{LIST, GET},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"events"},
			Verbs:     []string{CREATE, PATCH},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"nodes"},
			Verbs:     []string{UPDATE, PATCH},
		},
	})
}

func BuildEnoexecClusterRoleBindingController() *rbacv1.ClusterRoleBinding {
	return buildClusterRoleBinding(
		utils.EnoexecControllerName,
		rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     clusterRoleKind,
			Name:     utils.EnoexecControllerName,
		},
		[]rbacv1.Subject{
			{
				Kind:      serviceAccountKind,
				Name:      utils.EnoexecControllerName,
				Namespace: utils.Namespace(),
			},
		},
	)
}

func BuildEnoexecRoleController() *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.EnoexecControllerName,
			Namespace: utils.Namespace(),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"ENoExecEvents"},
				Verbs:     []string{LIST, WATCH, GET, UPDATE, PATCH, CREATE, DELETE},
			},
		},
	}
}

func BuildEnoexecRoleBindingController() *rbacv1.ClusterRoleBinding {
	return buildClusterRoleBinding(
		utils.EnoexecControllerName,
		rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     roleKind,
			Name:     utils.EnoexecControllerName,
		},
		[]rbacv1.Subject{
			{
				Kind:      serviceAccountKind,
				Name:      utils.EnoexecControllerName,
				Namespace: utils.Namespace(),
			},
		},
	)
}

func BuildEnoexecRoleDaemonSet() *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.EnoexecDaemonSet,
			Namespace: utils.Namespace(),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"ENoExecEvent"},
				Verbs:     []string{GET, CREATE},
			},
		},
	}
}

func BuildEnoexecRoleBindingDaemonSet() *rbacv1.ClusterRoleBinding {
	return buildClusterRoleBinding(
		utils.EnoexecDaemonSet,
		rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     roleKind,
			Name:     utils.EnoexecDaemonSet,
		},
		[]rbacv1.Subject{
			{
				Kind:      serviceAccountKind,
				Name:      utils.EnoexecDaemonSet,
				Namespace: utils.Namespace(),
			},
		},
	)
}

// BuildEnoexecDaemonSet returns the DaemonSet object for ENoExecEvent
func BuildEnoexecDaemonSet(serviceAccount string) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.EnoexecDaemonSet,
			Namespace: utils.Namespace(),
			Labels: map[string]string{
				"app": utils.EnoexecDaemonSet,
			},
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": utils.EnoexecDaemonSet,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": utils.EnoexecDaemonSet,
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: serviceAccount,
					Containers: []corev1.Container{
						{
							//TODO: this will need to be updated
							Name:            "bpftrace",
							Image:           "quay.io/iovisor/bpftrace:latest",
							ImagePullPolicy: corev1.PullIfNotPresent,

							//TODO: this should probably be set
							Command: []string{"/bin/sh", "-c"},
							Args: []string{
								`bpftrace -e 'tracepoint:syscalls:sys_exit_execve /args->ret == -8/ { printf("Exec format error detected for PPID %d\n", curtask->real_parent->pid); }'`,
							},
							//Args: append([]string{
							//	"--health-probe-bind-address=:8081",
							//	"--metrics-bind-address=:8443",
							//}, args...),
							//TODO: this should probably be set
							Env: []corev1.EnvVar{},
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
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: utils.NewPtr(false),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{
										"ALL",
									},
								},
								Privileged:             utils.NewPtr(true),
								ReadOnlyRootFilesystem: utils.NewPtr(true),
								RunAsNonRoot:           utils.NewPtr(true),
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "debugfs",
									MountPath: "/sys/kernel/debug",
								},
								{
									Name:      "tracingfs",
									MountPath: "/sys/kernel/tracing",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "debugfs",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/sys/kernel/debug",
									Type: utils.NewPtr(corev1.HostPathDirectory),
								},
							},
						},
						{
							Name: "tracingfs",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/sys/kernel/tracing",
									Type: utils.NewPtr(corev1.HostPathDirectory),
								},
							},
						},
					},
				},
			},
		},
	}
}

// BuildDeployment returns a minimal Deployment object matching your YAML
func BuildDeployment(name string, replicas int32, serviceAccount string,
	args ...string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: utils.Namespace(),
			Labels: map[string]string{
				utils.OperandLabelKey:   operandName,
				utils.ControllerNameKey: name,
			},
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
				},
				Spec: corev1.PodSpec{
					ServiceAccountName:           serviceAccount,
					AutomountServiceAccountToken: utils.NewPtr(true),
					Containers: []corev1.Container{
						{
							Name:            name,
							Image:           utils.Image(),
							ImagePullPolicy: corev1.PullIfNotPresent,
							Args: append([]string{
								"--health-probe-bind-address=:8081",
								"--metrics-bind-address=:8443",
							}, args...),
							//TODO: this should probably be set
							Command: []string{},
							//TODO: this should probably be set
							Env: []corev1.EnvVar{},
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
						},
					},
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
				},
			},
		},
	}
}
