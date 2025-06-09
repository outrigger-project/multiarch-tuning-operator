package storage

import (
	"context"
	"fmt"
	"os"

	"golang.org/x/time/rate"

	"github.com/go-logr/logr"
	"github.com/google/uuid"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes/scheme"

	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/multiarch-tuning-operator/enoexec-daemon/internal/types"
	"github.com/openshift/multiarch-tuning-operator/enoexec-daemon/internal/utils"
	multiarchv1beta1 "github.com/outrigger-project/multiarch-tuning-operator/apis/multiarch/v1beta1"
)

type K8sENOExecEventStorage struct {
	*IWStorageBase
	nodeName  string
	namespace string
	limiter   *rate.Limiter
}

// NewK8sENOExecEventStorage creates a new K8sENOExecEventStorage instance.
func NewK8sENOExecEventStorage(ctx context.Context, limiter *rate.Limiter, channelCapacity int, nodeName, namespace string) (*K8sENOExecEventStorage, error) {
	return &K8sENOExecEventStorage{
		IWStorageBase: &IWStorageBase{
			ctx: ctx,
			ch:  make(chan *types.ENOEXECInternalEvent, channelCapacity),
		},
		nodeName:  nodeName,
		namespace: namespace,
		limiter:   limiter,
	}, nil
}

func (s *K8sENOExecEventStorage) Run() error {
	// This method is intended to be run in a separate goroutine.
	// It should process the data from the channel and handle it accordingly.
	log, err := logr.FromContext(s.ctx)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "Failed to get logger:", err)
		return fmt.Errorf("failed to get logger from context: %w", err)
	}
	log.Info("Starting K8sENOExecEventStorage")
	config, err := controllerruntime.GetConfig()
	if err != nil {
		return err
	}
	if err = registerScheme(scheme.Scheme); err != nil {
		return err
	}
	defer utils.Should(s.close())
	var k8sClient client.Client
	k8sClient, err = client.New(config, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}
	for {
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		case event := <-s.ch:
			if event == nil {
				log.Info("Received nil event, skipping")
				continue
			}
			enoexecEvent, err := s.buildENoExecEvent(event)
			if err != nil {
				log.Error(err, "Failed to build ENOExecEvent from internal event", "event", event)
				continue
			}
			if err := s.limiter.Wait(s.ctx); err != nil {
				log.Error(err, "Rate limiter wait failed, skipping event", "event", enoexecEvent)
				continue
			}
			err = k8sClient.Create(s.ctx, enoexecEvent.DeepCopy())
			if err != nil {
				log.Error(err, "Failed to create ENOExecEvent in Kubernetes", "event", enoexecEvent)
				continue
			}
			log.Info("Successfully created ENOExecEvent in Kubernetes", "event", enoexecEvent.Name, "pod_name", enoexecEvent.Status.PodName, "pod_namespace", enoexecEvent.Status.PodNamespace, "container_id", enoexecEvent.Status.ContainerID)
			enoexecEventObj := &multiarchv1beta1.ENoExecEvent{}
			if err := k8sClient.Get(s.ctx, client.ObjectKey{
				Name:      enoexecEvent.Name,
				Namespace: s.namespace,
			}, enoexecEventObj); err != nil {
				log.Error(err, "Failed to get ENOExecEvent from Kubernetes after creation", "event", enoexecEvent)
			} else {
				log.Info("Successfully retrieved ENOExecEvent from Kubernetes after creation", "event", enoexecEventObj.Name, "pod_name", enoexecEventObj.Status.PodName, "pod_namespace", enoexecEventObj.Status.PodNamespace, "container_id", enoexecEventObj.Status.ContainerID)
			}
			enoexecEventObj.Status = enoexecEvent.Status
			if err := k8sClient.Status().Update(s.ctx, enoexecEventObj); err != nil {
				log.Error(err, "Failed to update ENOExecEvent status in Kubernetes", "event", enoexecEventObj)
			} else {
				log.Info("Successfully updated ENOExecEvent status in Kubernetes", "event", enoexecEventObj.Name, "pod_name", enoexecEventObj.Status.PodName, "pod_namespace", enoexecEventObj.Status.PodNamespace, "container_id", enoexecEventObj.Status.ContainerID)
			}
		}
	}
}

func (s *K8sENOExecEventStorage) buildENoExecEvent(e *types.ENOEXECInternalEvent) (*multiarchv1beta1.ENoExecEvent, error) {
	// Convert the ENOEXECInternalEvent to a multiarchv1beta1.ENOExecEvent.
	// This is a placeholder for actual conversion logic.
	// generate a unique name for the event with uuid
	uid, err := uuid.NewUUID()
	if err != nil {
		// Handle error (e.g., log it, return an error, etc.)
		return nil, err
	}
	name := uid.String()
	if len(name) > 63 {
		name = name[:63]
	}

	return &multiarchv1beta1.ENoExecEvent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: s.namespace,
		},
		Status: multiarchv1beta1.ENoExecEventStatus{
			NodeName:     s.nodeName,
			PodName:      e.PodName,
			PodNamespace: e.PodNamespace,
			ContainerID:  e.ContainerID,
			Command:      "",
		},
	}, nil
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
