package framework

import (
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func NewEphemeralNamespace(prefix ...string) *corev1.Namespace {
	base := "t-" + uuid.NewString()
	if len(prefix) > 0 {
		base = prefix[0] + base
	}
	name := NormalizeNameString(base)

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	return ns
}

func NormalizeNameString(name string) string {
	if len(name) > 63 {
		return name[:63]
	}
	return name
}
