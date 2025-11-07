package internal

import (
	"sync"

	multiarchv1beta1 "github.com/openshift/multiarch-tuning-operator/api/v1beta1"
)

var (
	mu                        sync.RWMutex
	clusterPodPlacementConfig *multiarchv1beta1.ClusterPodPlacementConfig
)

func StoreClusterPodPlacementConfig(config *multiarchv1beta1.ClusterPodPlacementConfig) {
	mu.Lock()
	defer mu.Unlock()
	clusterPodPlacementConfig = config.DeepCopy()
}

func DeleteClusterPodPlacementConfig() {
	mu.Lock()
	defer mu.Unlock()
	clusterPodPlacementConfig = nil
}

func GetClusterPodPlacementConfig() *multiarchv1beta1.ClusterPodPlacementConfig {
	mu.RLock()
	defer mu.RUnlock()
	return clusterPodPlacementConfig
}
