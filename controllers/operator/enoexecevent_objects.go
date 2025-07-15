package operator

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
				Verbs:     []string{GET, CREATE},
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
	return buildDeployment(logVerbosity, utils.EnoexecControllerName, 2, utils.EnoexecControllerName, "",
		"--leader-elect", "--enable-enoexec-event-controllers",
	)
}
