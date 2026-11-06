package framework

import (
	"context"

	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func GetEventsForObject(ctx context.Context, client kubernetes.Interface, objectName string, objectNamespace string) ([]eventsv1.Event, error) {
	events, err := client.EventsV1().Events(objectNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var matching []eventsv1.Event
	for _, event := range events.Items {
		if event.Regarding.Name == objectName && event.Regarding.Namespace == objectNamespace {
			matching = append(matching, event)
		}
	}
	return matching, nil
}
