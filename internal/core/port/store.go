// store.go defines the storage and watch port implemented by etcd and test fakes.
package port

import "context"

// LeaseID identifies an etcd style lease used by heartbeat mode instances.
type LeaseID int64

// EventType identifies a storage watch event.
type EventType string

const (
	// EventPut signals a create or update.
	EventPut EventType = "put"
	// EventDelete signals a deletion.
	EventDelete EventType = "delete"
	// EventReset signals that the caller must rebuild from a fresh snapshot.
	EventReset EventType = "reset"
)

// Record is a raw key-value document loaded from persistent storage.
type Record struct {
	Key      string
	Value    []byte
	Revision int64
	LeaseID  LeaseID
}

// Event is a low-level watched change emitted by the store implementation.
type Event struct {
	Type            EventType
	Key             string
	Value           []byte
	Revision        int64
	CompactRevision int64
	Err             error
}

// Store provides the minimal persistence operations required by phase 1 services.
type Store interface {
	Status(ctx context.Context) error
	Create(ctx context.Context, key string, value []byte, leaseID LeaseID) (int64, error)
	Put(ctx context.Context, key string, value []byte, leaseID LeaseID) (int64, error)
	CAS(ctx context.Context, key string, expectedRevision int64, value []byte, leaseID LeaseID) (int64, error)
	Get(ctx context.Context, key string) (Record, error)
	List(ctx context.Context, prefix string) ([]Record, error)
	Delete(ctx context.Context, key string) (int64, error)
	DeleteCAS(ctx context.Context, key string, expectedRevision int64) (int64, error)
	Watch(ctx context.Context, prefix string, startRevision int64) <-chan Event
	GrantLease(ctx context.Context, ttlSeconds int64) (LeaseID, error)
	KeepAliveOnce(ctx context.Context, leaseID LeaseID) error
	RevokeLease(ctx context.Context, leaseID LeaseID) error
}
