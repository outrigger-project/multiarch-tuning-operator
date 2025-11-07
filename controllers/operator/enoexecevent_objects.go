package operator

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/intstr"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/common/plugins"
	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

func buildClusterRoleENoExecEventsController() *rbacv1.ClusterRole {
	return buildClusterRole(utils.EnoexecControllerName, []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"pods", "nodes"},
			Verbs:     []string{LIST, GET},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"events"},
			Verbs:     []string{CREATE, PATCH},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"pods", "pods/status"},
			Verbs:     []string{UPDATE},
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

func buildRoleENoExecEventController() *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.EnoexecControllerName,
			Namespace: utils.Namespace(),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{v1beta1.GroupVersion.Group},
				Resources: []string{v1beta1.ENoExecEventResource},
				Verbs:     []string{LIST, WATCH, GET, UPDATE, PATCH, CREATE, DELETE},
			},
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

func buildClusterRoleENoExecEventsDaemonSet() *rbacv1.ClusterRole {
	return buildClusterRole(utils.EnoexecDaemonSet, []rbacv1.PolicyRule{
		{
			APIGroups:     []string{"security.openshift.io"},
			Resources:     []string{"securitycontextconstraints"},
			ResourceNames: []string{"privileged"},
			Verbs:         []string{USE},
		},
	})
}

func buildRoleENoExecEventDaemonSet() *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.EnoexecDaemonSet,
			Namespace: utils.Namespace(),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{v1beta1.GroupVersion.Group},
				Resources: []string{v1beta1.ENoExecEventResource},
				Verbs:     []string{GET, CREATE, DELETE},
			},
			{
				APIGroups: []string{v1beta1.GroupVersion.Group},
				Resources: []string{v1beta1.ENoExecEventResource + "/status"},
				Verbs:     []string{UPDATE},
			},
		},
	}
}

// buildDaemonSet returns the DaemonSet object for ENoExecEvent
func buildDaemonSetENoExecEvent(serviceAccount string, name string, logVerbosity int, args ...string) *appsv1.DaemonSet {
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
					ServiceAccountName:           serviceAccount,
					AutomountServiceAccountToken: utils.NewPtr(true),
					HostPID:                      true,
					Containers: []corev1.Container{
						{
							Name:            name,
							Image:           utils.Image(),
							ImagePullPolicy: corev1.PullIfNotPresent,

							Args: args,
							Command: []string{
								"/enoexec-daemon",
								fmt.Sprintf("--initial-log-level=%d",
									logVerbosity),
							},
							Env: []corev1.EnvVar{
								{
									Name: "NAMESPACE",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											APIVersion: "v1",
											FieldPath:  "metadata.namespace",
										},
									},
								},
								{
									Name: "NODE_NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											APIVersion: "v1",
											FieldPath:  "spec.nodeName",
										},
									},
								},
							},
							SecurityContext: &corev1.SecurityContext{
								Privileged: utils.NewPtr(true),
								// The bpftrace tool requires root privileges to interact with the kernel.
								RunAsUser:              utils.NewPtr(int64(0)),
								ReadOnlyRootFilesystem: utils.NewPtr(true),
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "debugfs",
									MountPath: "/sys/kernel/debug",
									ReadOnly:  true,
								},
								{
									Name:      "tracingfs",
									MountPath: "/sys/kernel/tracing",
									ReadOnly:  true,
								},
								{
									Name:      "crio",
									MountPath: "/var/run/crio/crio.sock",
									ReadOnly:  true,
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
						{
							Name: "crio",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/run/crio/crio.sock",
									Type: utils.NewPtr(corev1.HostPathSocket),
								},
							},
						},
					},
				},
			},
		},
	}
}

// buildEnoexecDeployment returns a minimal Deployment object matching your YAML
func buildDeploymentENoExecEventHandler(logVerbosity int) *appsv1.Deployment {
	d := buildDeployment(logVerbosity, utils.EnoexecControllerName, 2, utils.EnoexecControllerName, "",
		"--leader-elect", "--enable-enoexec-event-controllers",
	)
	additionalVolumes := []corev1.Volume{
		{
			Name: "metrics-cert",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  utils.EnoexecControllerName,
					DefaultMode: utils.NewPtr(int32(420)),
				},
			},
		},
	}
	additionalMounts := []corev1.VolumeMount{
		{
			Name:      "metrics-cert",
			MountPath: "/var/run/manager/tls",
			ReadOnly:  true,
		},
	}

	d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, additionalVolumes...)
	d.Spec.Template.Spec.Containers[0].VolumeMounts = append(d.Spec.Template.Spec.Containers[0].VolumeMounts, additionalMounts...)

	return d
}

func buildExecFormatErrorAvailabilityAlertRule() *monitoringv1.PrometheusRule {
	return &monitoringv1.PrometheusRule{
		TypeMeta: metav1.TypeMeta{
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
			Kind:       monitoringv1.PrometheusRuleKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      strings.ToLower(plugins.ExecFormatErrorMonitorPluginName),
			Namespace: utils.Namespace(),
		},
		Spec: monitoringv1.PrometheusRuleSpec{
			Groups: []monitoringv1.RuleGroup{
				{
					Name: "multiarch-tuning-operator-enoexec.rules",
					Rules: []monitoringv1.Rule{
						{
							Alert: "ExecFormatErrorHandlerDown",
							Expr:  intstr.FromString(fmt.Sprintf("kube_deployment_status_replicas_available{namespace=\"%s\", deployment=\"%s\"} == 0", utils.Namespace(), utils.EnoexecControllerName)),
							For:   utils.NewPtr[monitoringv1.Duration]("1m"),
							Annotations: map[string]string{
								"summary":     "The exec format error handler should have at least 1 replica running and ready.",
								"description": "The exec format error handler has been down for more than 1 minute. ",
								"runbook_url": "https://github.com/outrigger-project/multiarch-tuning-operator/blob/main/docs/alerts/enoexec-event-handler-down.md",
							},
							Labels: map[string]string{
								"severity": "critical",
							},
						},
						{
							Alert: "ExecFormatErrorDaemonDown",
							Expr:  intstr.FromString(fmt.Sprintf("kube_daemonset_status_number_unavailable{namespace=\"%s\", daemonset=\"%s\"} > 0", utils.Namespace(), utils.EnoexecDaemonSet)),
							For:   utils.NewPtr[monitoringv1.Duration]("20m"),
							Annotations: map[string]string{
								"summary": "The exec format error daemon is not available in all the nodes.",
								"description": "Some nodes that should be running the exec format error daemon have none of the daemonset pod running and available for more than 20 minutes. " +
									"Exec Format Errors will not be detected in those nodes",
								"runbook_url": "https://github.com/openshift/multiarch-tuning-operator/blob/main/docs/alerts/enoexec-event-daemon-down.md",
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

func buildExecFormatErrorsDetectedAlertRule() *monitoringv1.PrometheusRule {
	return &monitoringv1.PrometheusRule{
		TypeMeta: metav1.TypeMeta{
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
			Kind:       monitoringv1.PrometheusRuleKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      strings.ToLower(utils.ExecFormatErrorsDetected),
			Namespace: utils.Namespace(),
		},
		Spec: monitoringv1.PrometheusRuleSpec{
			Groups: []monitoringv1.RuleGroup{
				{
					Name: "multiarch-tuning-operator-enoexec-detected.rules",
					Rules: []monitoringv1.Rule{
						{
							Alert: utils.ExecFormatErrorsDetected,
							Expr:  intstr.FromString("rate(mto_enoexecevents_total[6h]) > 0"),
							For:   utils.NewPtr[monitoringv1.Duration]("1m"),
							Annotations: map[string]string{
								"summary":     "Exec Format Errors detected in the past 6 hours.",
								"description": "Exec Format Errors detected in the past 6 hours.",
								"runbook_url": "https://github.com/outrigger-project/multiarch-tuning-operator/blob/main/docs/alerts/enoexec-controller-down.md",
							},
							Labels: map[string]string{
								"severity": "critical",
							},
						},
					},
				},
			},
		},
	}
}
