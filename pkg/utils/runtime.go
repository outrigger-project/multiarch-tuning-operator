package utils

import (
	"context"
	"os"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"go.uber.org/zap"
)

var namespace string
var image string
var AtomicLevel zap.AtomicLevel = zap.NewAtomicLevelAt(-5)
var availableResourcesMap = map[schema.GroupVersionResource]bool{}
var rwMutex = sync.RWMutex{}

func init() {
	// Get the namespace from the env variable
	if ns, ok := os.LookupEnv("NAMESPACE"); ok {
		namespace = ns
	} else {
		namespace = "default"
	}
	if img, ok := os.LookupEnv("IMAGE"); ok {
		image = img
	} else {
		image = "quay.io/example/example-operator:latest"
	}
}

// Namespace returns the namespace where the operator's pods are running.
func Namespace() string {
	return namespace
}

// Image returns the image used to run the operator.
func Image() string {
	return image
}

func IsResourceAvailable(ctx context.Context, client *dynamic.DynamicClient, resource schema.GroupVersionResource) bool {
	rwMutex.RLock()
	if v, ok := availableResourcesMap[resource]; ok {
		rwMutex.RUnlock()
		return v
	}
	rwMutex.RUnlock()
	_, err := client.Resource(resource).Namespace(Namespace()).List(ctx, metav1.ListOptions{})
	rwMutex.Lock()
	defer rwMutex.Unlock()
	availableResourcesMap[resource] = err == nil
	return availableResourcesMap[resource]
}

func IsOpenshift(ctx context.Context, client *dynamic.DynamicClient) bool {
	return IsResourceAvailable(ctx, client, schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "clusterversions",
	})
}

func MonitoringLabelKey(ctx context.Context, client *dynamic.DynamicClient) string {
	if !IsOpenshift(ctx, client) {
		return "k8s.io/cluster-monitoring"
	}
	return "openshift.io/cluster-monitoring"
}
