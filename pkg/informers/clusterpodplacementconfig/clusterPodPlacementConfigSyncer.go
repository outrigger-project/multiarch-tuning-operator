package clusterpodplacementconfig

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	multiarchv1beta1 "github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/pkg/informers/clusterpodplacementconfig/internal"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// CPPCSyncer syncs ClusterPodPlacementConfig resources using an informer.
type CPPCSyncer struct {
	mgr manager.Manager
	log logr.Logger
}

// NewCPPCSyncer creates a new CPPCSyncer.
func NewCPPCSyncer(mgr manager.Manager) *CPPCSyncer {
	return &CPPCSyncer{
		mgr: mgr,
	}
}

// Start initializes the CPPC informer and starts syncing.
func (s *CPPCSyncer) Start(ctx context.Context) error {
	s.log = log.FromContext(ctx, "handler", "CPPCSyncer")
	s.log.Info("Starting CPPC Syncer")
	mgr := s.mgr

	// Get informer for ClusterPodPlacementConfig
	CPPCInformer, err := mgr.GetCache().GetInformerForKind(ctx, multiarchv1beta1.GroupVersion.WithKind(multiarchv1beta1.ClusterPodPlacementConfigKind))
	if err != nil {
		s.log.Error(err, "Error getting informer for ClusterPodPlacementConfig")
		return err
	}

	// Register event handlers
	_, err = CPPCInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.onAdd(),
		UpdateFunc: s.onUpdate(),
		DeleteFunc: s.onDelete(),
	})
	if err != nil {
		s.log.Error(err, "Error registering handler for ClusterPodPlacementConfig")
		return err
	}

	return nil
}

// onAdd handles the addition of a ClusterPodPlacementConfig.
func (s *CPPCSyncer) onAdd() func(obj interface{}) {
	return func(obj interface{}) {
		CPPC, ok := obj.(*multiarchv1beta1.ClusterPodPlacementConfig)
		if !ok {
			s.log.Error(errors.New("unexpected type, expected ClusterPodPlacementConfig"), "unexpected type",
				"type", fmt.Sprintf("%T", obj))
			return
		}

		internal.StoreClusterPodPlacementConfig(CPPC)
		s.log.Info("Added ClusterPodPlacementConfig", "CPPC name", CPPC.Name, "namespace", CPPC.Namespace)
	}
}

// onDelete handles the deletion of a ClusterPodPlacementConfig.
func (s *CPPCSyncer) onDelete() func(obj interface{}) {
	return func(obj interface{}) {

		CPPC, ok := obj.(*multiarchv1beta1.ClusterPodPlacementConfig)
		if !ok {
			s.log.Error(errors.New("unexpected type, expected ClusterPodPlacementConfig"), "unexpected type",
				"type", fmt.Sprintf("%T", obj))
			return
		}

		internal.DeleteClusterPodPlacementConfig()
		s.log.Info("Deleted ClusterPodPlacementConfig", "name", CPPC.Name, "namespace", CPPC.Namespace)
	}
}

// onUpdate handles updates to a ClusterPodPlacementConfig.
func (s *CPPCSyncer) onUpdate() func(oldObj, newObj interface{}) {
	return func(oldobj, newobj interface{}) {
		oldConfig, ok := oldobj.(*multiarchv1beta1.ClusterPodPlacementConfig)

		if !ok {
			s.log.Error(errors.New("unexpected type, expected ClusterPodPlacementConfig"), "unexpected type",
				"type", fmt.Sprintf("%T", oldobj))
			return
		}

		newConfig, ok := newobj.(*multiarchv1beta1.ClusterPodPlacementConfig)
		if !ok {
			s.log.Error(errors.New("unexpected type, expected ClusterPodPlacementConfig"), "unexpected type",
				"type", fmt.Sprintf("%T", newobj))
			return
		}

		if oldConfig.ResourceVersion == newConfig.ResourceVersion {
			return
		}

		s.onAdd()(newobj)
	}
}

// GetClusterPodPlacementConfig provides access to the stored config.
func GetClusterPodPlacementConfig() *multiarchv1beta1.ClusterPodPlacementConfig {
	return internal.GetClusterPodPlacementConfig()
}
