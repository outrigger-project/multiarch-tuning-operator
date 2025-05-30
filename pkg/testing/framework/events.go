package framework

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func GetEventsForObject(ctx context.Context, client *kubernetes.Clientset, objectName string, objectNamespace string) ([]corev1.Event, error) {
	var events *corev1.EventList
	var err error

	if events, err = client.CoreV1().Events(objectNamespace).List(ctx, metav1.ListOptions{
		FieldSelector: "involvedObject.name=" + objectName,
	}); err == nil {
		return events.Items, nil
	}
	return nil, err
}
