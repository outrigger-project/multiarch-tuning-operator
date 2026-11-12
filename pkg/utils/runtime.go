package utils

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
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

// resourceAvailabilityTimeout bounds each API request issued by IsResourceAvailable.
// It is a variable so that tests can shorten it.
var resourceAvailabilityTimeout = 10 * time.Second

// IsResourceAvailable reports whether the given resource is served by the API server.
//
// Only definitive answers are cached for the lifetime of the process. Any other error
// (e.g., timeouts, connection refused, 429/500/503, Forbidden while RBAC is propagating)
// is returned to the caller without caching, so that it can be handled gracefully.
func IsResourceAvailable(ctx context.Context, client dynamic.Interface, resource schema.GroupVersionResource) (bool, error) {
	rwMutex.RLock()
	if v, ok := availableResourcesMap[resource]; ok {
		rwMutex.RUnlock()
		return v, nil
	}
	rwMutex.RUnlock()

	ctx, cancel := context.WithTimeout(ctx, resourceAvailabilityTimeout)
	defer cancel()
	// Limit: 1 as we only need to know whether the API is served, not to fetch its objects.
	_, err := client.Resource(resource).Namespace(Namespace()).List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil && !apierrors.IsNotFound(err) && !apierrors.IsMethodNotSupported(err) && !meta.IsNoMatchError(err) {
		// The error does not tell whether the API is served (e.g., it is transient): return it without caching,
		// so that the next call checks again.
		return false, fmt.Errorf("unable to determine whether %s is available: %w", resource.String(), err)
	}

	rwMutex.Lock()
	defer rwMutex.Unlock()
	// err is nil (the API is served) or a definitive "not served" error: cache the result.
	availableResourcesMap[resource] = err == nil
	return availableResourcesMap[resource], nil
}
