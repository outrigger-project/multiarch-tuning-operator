package storage

import (
	"context"

	"github.com/openshift/multiarch-tuning-operator/enoexec-daemon/internal/types"
)

// IWStorage is the interface that defines the methods for writeable storage implementations.
type IWStorage interface {
	IStorage
	Store(*types.ENOEXECInternalEvent) error
}

// IStorage is the interface that defines the methods for storage implementations.
// Storage implementations should provide methods to store data, retrieve data, or both,
// implementing either IWStorage or IRStorage as needed.
// The implementation of IStorage is expected to be concurrent-safe and should run in a separate goroutine,
// implementing the Run method to start the storage process.
type IStorage interface {
	Run() error
}

type IWStorageBase struct {
	ch  chan *types.ENOEXECInternalEvent
	ctx context.Context
}

// Store writes data to the FIFO pipe.
// Users of this method should ensure that a goroutine is running to process the data
// from the channel, as this method will not block until the data is written to the FIFO pipe.
// This method is non-blocking and will return immediately, queuing the data for later processing.
func (i *IWStorageBase) Store(evt *types.ENOEXECInternalEvent) error {
	select {
	case i.ch <- evt:
		return nil
	case <-i.ctx.Done():
		return i.ctx.Err()
	}
}

// close closes the FIFO file if it is open.
func (i *IWStorageBase) close() error {
	close(i.ch)
	if i.ctx != nil {
		if cancelFunc, ok := i.ctx.Value("cancelFunc").(context.CancelFunc); ok {
			cancelFunc()
		}
		i.ctx = nil
	}
	return nil
}
