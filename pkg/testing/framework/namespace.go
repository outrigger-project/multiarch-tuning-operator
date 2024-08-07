package framework

import (
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func NewEphemeralNamespace() *corev1.Namespace {
	name := NormalizeNameString("t-" + uuid.NewString())
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	return ns
}

func NewEphemeralNamespaceWithOwnerRef(ownerref string) *corev1.Namespace {
	name := NormalizeNameString("t-" + uuid.NewString())
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "config.openshift.io/v1",
					Kind:       ownerref,
					Name:       "version",
					UID:        "example-uid",
				},
			},
		},
	}
	return ns
}

func NewEphemeralOpenshiftOperatorNamespaceWithOwnerRef(ownerref string) (*corev1.Namespace, *corev1.Namespace) {
	uuidstring := uuid.NewString()
	parentname := NormalizeNameString("openshift-" + uuidstring + "-operator")
	name := NormalizeNameString("openshift-" + uuidstring)
	parentns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: parentname,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "config.openshift.io/v1",
					Kind:       ownerref,
					Name:       "version",
					UID:        "example-uid",
				},
			},
		},
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	return parentns, ns
}

func NormalizeNameString(name string) string {
	if len(name) > 63 {
		return name[:63]
	}
	return name
}
