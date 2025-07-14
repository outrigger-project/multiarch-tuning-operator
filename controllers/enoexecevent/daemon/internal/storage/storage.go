package storage

import (
	"context"

	"github.com/openshift/multiarch-tuning-operator/controllers/enoexecevent/daemon/internal/types"
)

// IWStorage is the interface that defines the methods for writeable storage implementations.
type IWStorage interface {
	IStorage
	Store(*types.ENOEXECInternalEvent) error
}

// IStorage is the interface that defines the methods for storage implementations.
// Storage implementations should provide methods to store data, retrieve data, or both,
// implementing either IWStorage or IRStorage as needed.
// The implementation of IStorage is expected to be concurrency-safe and should run in a separate goroutine,
// implementing the Run() method to start the storage main loop.
type IStorage interface {
	Run() error
}

type IWStorageBase struct {
	ch  chan *types.ENOEXECInternalEvent
	ctx context.Context
}

// close runs the cleanup operations for the storage implementation.
func (i *IWStorageBase) close() error {
	if i.ctx != nil {
		if cancelFunc, ok := i.ctx.Value("cancelFunc").(context.CancelFunc); ok {
			cancelFunc()
		}
		i.ctx = nil
	}
	return nil
}
