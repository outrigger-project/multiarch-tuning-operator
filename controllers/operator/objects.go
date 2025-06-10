package operator

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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

func BuildServiceAccount(name string) *corev1.ServiceAccount {
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
