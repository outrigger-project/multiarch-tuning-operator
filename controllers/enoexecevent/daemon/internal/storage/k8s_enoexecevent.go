package storage

import (
	"context"
	"fmt"
	"os"
	"time"

	"golang.org/x/time/rate"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes/scheme"

	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/go-logr/logr"

	multiarchv1beta1 "github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/controllers/enoexecevent/daemon/internal/types"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

// K8sENOExecEventStorage is a storage implementation that writes ENOExec events to Kubernetes.
type K8sENOExecEventStorage struct {
	*IWStorageBase
	nodeName  string
	namespace string
	limiter   *rate.Limiter
	timeout   time.Duration
	k8sClient client.Client
}

// NewK8sENOExecEventStorage creates a new K8sENOExecEventStorage instance.
func NewK8sENOExecEventStorage(ctx context.Context, limiter *rate.Limiter, ch chan *types.ENOEXECInternalEvent, nodeName, namespace string, timeout time.Duration) (*K8sENOExecEventStorage, error) {
	log, err := logr.FromContext(ctx)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "Failed to get logger:", err)
		return nil, fmt.Errorf("failed to get logger from context: %w", err)
	}

	log.Info("Starting K8sENOExecEventStorage")
	config, err := controllerruntime.GetConfig()
	if err != nil {
		return nil, err
	}

	if err = registerScheme(scheme.Scheme); err != nil {
		return nil, err
	}

	var k8sClient client.Client
	k8sClient, err = client.New(config, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return &K8sENOExecEventStorage{
		IWStorageBase: &IWStorageBase{
			ctx: ctx,
			ch:  ch,
		},
		nodeName:  nodeName,
		namespace: namespace,
		limiter:   limiter,
		timeout:   timeout,
		k8sClient: k8sClient,
	}, nil
}

// Run starts the K8sENOExecEventStorage event loop.
//
// It listens for ENOEXEC events on the internal channel, converts each event to a Kubernetes ENoExecEvent,
// and creates it in the cluster.
// This method is designed to run in a separate goroutine so that the responsibilities of catching ENOEXEC
// and notifying the controller pod are handled concurrently.
func (s *K8sENOExecEventStorage) Run() error {
	log, err := logr.FromContext(s.ctx)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "Failed to get logger:", err)
		return fmt.Errorf("failed to get logger from context: %w", err)
	}
	defer utils.ShouldStdErr(s.close)

	for {
		select {
		case event := <-s.ch:
			if event == nil {
				log.Info("Received nil event, skipping")
				continue
			}
			if err = s.processEvent(event); err != nil {
				log.Error(err, "Failed to process ENOExec event", "event", event)
				continue
			}
		case <-s.ctx.Done():
			return s.ctx.Err()
		}
	}
}

// processEvent processes an ENOEXECInternalEvent by converting it to an ENoExecEvent and creating it in Kubernetes.
// It implements throttling to avoid overwhelming the Kubernetes API server with too many requests when events
// are generated at a high rate (e.g., when the pod is restarted frequently or when the binary that fails to execute
// is frequently restarted within the pod).
// The rate limiter is used to control the number of requests sent to the Kubernetes API server.
// The related context is created with a timeout to ensure that the request does not block indefinitely and
// to defend against potential flooding attacks on the Kubernetes API server and storage.
// TODO: this should be testable in integration tests.
func (s *K8sENOExecEventStorage) processEvent(event *types.ENOEXECInternalEvent) error {
	var (
		enoexecEvent    *multiarchv1beta1.ENoExecEvent
		enoexecEventObj multiarchv1beta1.ENoExecEvent
		err             error
	)

	log, err := logr.FromContext(s.ctx)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "Failed to get logger:", err)
		return fmt.Errorf("failed to get logger from context: %w", err)
	}

	enoexecEvent, err = event.ToENoExecEvent(s.namespace, s.nodeName)
	if err != nil {
		return err
	}
	// We implement throttling to avoid overwhelming the Kubernetes API server with too many requests when
	// events are generate at a high rate (e.g., when the pod is restarted frequently or when the binary that
	// fails to execute is frequently restarted within the pod).
	// The rate limiter is used to control the number of requests sent to the Kubernetes API server.
	// The related context is created with a timeout to ensure that the request does not block indefinitely and
	// to defend against potential flooding attacks on the Kubernetes API server and storage.
	// When the rate limiter is exhausted and the context times out, the internal event is dropped.
	ctx, cancel := context.WithTimeout(s.ctx, s.timeout)
	defer cancel()
	if err = s.limiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limiter wait failed: %w", err)
	}

	err = s.k8sClient.Create(s.ctx, enoexecEvent.DeepCopy())
	if err != nil {
		return fmt.Errorf("failed to create ENOExecEvent in Kubernetes: %w", err)
	}
	// If any further operations fail, we need to ensure that the created ENOExecEvent is deleted to avoid leaving
	// orphaned resources in the cluster. This is done by defining a rollback function that will be called
	// if any subsequent operations fail.
	rollbackFn := func() {
		if rErr := s.k8sClient.Delete(s.ctx, enoexecEvent); rErr != nil {
			log.Error(rErr, "Failed to rollback ENOExecEvent creation", "event", enoexecEvent.Name)
		} else {
			log.Info("Rolled back ENOExecEvent creation", "event", enoexecEvent.Name)
		}
	}

	log.Info("Successfully created ENOExecEvent in Kubernetes", "event", enoexecEvent.Name, "pod_name", enoexecEvent.Status.PodName, "pod_namespace", enoexecEvent.Status.PodNamespace, "container_id", enoexecEvent.Status.ContainerID)
	if err = s.k8sClient.Get(s.ctx, client.ObjectKey{
		Name:      enoexecEvent.Name,
		Namespace: s.namespace,
	}, &enoexecEventObj); err != nil {
		rollbackFn()
		return fmt.Errorf("failed to get ENOExecEvent from Kubernetes after creation: %w", err)
	}
	enoexecEventObj.Status = enoexecEvent.Status
	if err = s.k8sClient.Status().Update(s.ctx, &enoexecEventObj); err != nil {
		rollbackFn()
		return fmt.Errorf("failed to update ENOExecEvent status in Kubernetes: %w", err)
	}
	return nil
}

func registerScheme(s *runtime.Scheme) error {
	var errs []error
	errs = append(errs, corev1.AddToScheme(s))
	errs = append(errs, appsv1.AddToScheme(s))
	errs = append(errs, multiarchv1beta1.AddToScheme(s))
	if len(errs) > 0 {
		return errors.NewAggregate(errs)
	}
	return nil
}
