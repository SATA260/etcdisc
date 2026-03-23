// store.go implements the phase 1 persistence port on top of the etcd v3 client.
package etcd

import (
	"context"
	"fmt"

	apperrors "etcdisc/internal/core/errors"
	"etcdisc/internal/core/port"
	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// Store adapts the raw etcd client to the application's storage port.
type Store struct {
	client *clientv3.Client
}

// NewStore creates a store backed by the provided etcd client.
func NewStore(client *clientv3.Client) *Store {
	return &Store{client: client}
}

// Status checks store connectivity.
func (s *Store) Status(ctx context.Context) error {
	resp, err := s.client.Get(ctx, "health")
	if err != nil {
		return mapEtcdError(err, "store status failed")
	}
	_ = resp
	return nil
}

// Create inserts a key only when it does not already exist.
func (s *Store) Create(ctx context.Context, key string, value []byte, leaseID port.LeaseID) (int64, error) {
	op := clientv3.OpPut(key, string(value), withLease(leaseID)...)
	resp, err := s.client.Txn(ctx).
		If(clientv3.Compare(clientv3.Version(key), "=", 0)).
		Then(op).
		Else(clientv3.OpGet(key)).
		Commit()
	if err != nil {
		return 0, mapEtcdError(err, "create failed")
	}
	if !resp.Succeeded {
		return 0, apperrors.New(apperrors.CodeAlreadyExists, fmt.Sprintf("key %q already exists", key))
	}
	return resp.Header.Revision, nil
}

// Put writes a value with last-write-wins semantics.
func (s *Store) Put(ctx context.Context, key string, value []byte, leaseID port.LeaseID) (int64, error) {
	resp, err := s.client.Put(ctx, key, string(value), withLease(leaseID)...)
	if err != nil {
		return 0, mapEtcdError(err, "put failed")
	}
	return resp.Header.Revision, nil
}

// CAS updates a key when the expected revision matches the current mod revision.
func (s *Store) CAS(ctx context.Context, key string, expectedRevision int64, value []byte, leaseID port.LeaseID) (int64, error) {
	resp, err := s.client.Txn(ctx).
		If(clientv3.Compare(clientv3.ModRevision(key), "=", expectedRevision)).
		Then(clientv3.OpPut(key, string(value), withLease(leaseID)...)).
		Else(clientv3.OpGet(key)).
		Commit()
	if err != nil {
		return 0, mapEtcdError(err, "cas failed")
	}
	if !resp.Succeeded {
		return 0, apperrors.New(apperrors.CodeConflict, fmt.Sprintf("cas conflict on key %q", key))
	}
	return resp.Header.Revision, nil
}

// Get returns a single stored record.
func (s *Store) Get(ctx context.Context, key string) (port.Record, error) {
	resp, err := s.client.Get(ctx, key)
	if err != nil {
		return port.Record{}, mapEtcdError(err, "get failed")
	}
	if len(resp.Kvs) == 0 {
		return port.Record{}, apperrors.New(apperrors.CodeNotFound, fmt.Sprintf("key %q not found", key))
	}
	kv := resp.Kvs[0]
	return port.Record{Key: string(kv.Key), Value: kv.Value, Revision: kv.ModRevision, LeaseID: port.LeaseID(kv.Lease)}, nil
}

// List returns all records under a prefix.
func (s *Store) List(ctx context.Context, prefix string) ([]port.Record, error) {
	resp, err := s.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, mapEtcdError(err, "list failed")
	}
	records := make([]port.Record, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		records = append(records, port.Record{Key: string(kv.Key), Value: kv.Value, Revision: kv.ModRevision, LeaseID: port.LeaseID(kv.Lease)})
	}
	return records, nil
}

// Delete removes a key unconditionally.
func (s *Store) Delete(ctx context.Context, key string) (int64, error) {
	resp, err := s.client.Delete(ctx, key)
	if err != nil {
		return 0, mapEtcdError(err, "delete failed")
	}
	if resp.Deleted == 0 {
		return resp.Header.Revision, apperrors.New(apperrors.CodeNotFound, fmt.Sprintf("key %q not found", key))
	}
	return resp.Header.Revision, nil
}

// DeleteCAS removes a key only when the expected revision matches.
func (s *Store) DeleteCAS(ctx context.Context, key string, expectedRevision int64) (int64, error) {
	resp, err := s.client.Txn(ctx).
		If(clientv3.Compare(clientv3.ModRevision(key), "=", expectedRevision)).
		Then(clientv3.OpDelete(key)).
		Else(clientv3.OpGet(key)).
		Commit()
	if err != nil {
		return 0, mapEtcdError(err, "delete cas failed")
	}
	if !resp.Succeeded {
		return 0, apperrors.New(apperrors.CodeConflict, fmt.Sprintf("cas conflict on delete for key %q", key))
	}
	return resp.Header.Revision, nil
}

// Watch streams changes for a prefix from the given revision.
func (s *Store) Watch(ctx context.Context, prefix string, startRevision int64) <-chan port.Event {
	out := make(chan port.Event, 16)
	opts := []clientv3.OpOption{clientv3.WithPrefix(), clientv3.WithPrevKV()}
	if startRevision > 0 {
		opts = append(opts, clientv3.WithRev(startRevision))
	}
	watchCh := s.client.Watch(ctx, prefix, opts...)
	go func() {
		defer close(out)
		for resp := range watchCh {
			if err := resp.Err(); err != nil {
				event := port.Event{Err: mapEtcdError(err, "watch failed")}
				if compactRevision := resp.CompactRevision; compactRevision > 0 {
					event.Type = port.EventReset
					event.CompactRevision = compactRevision
				}
				select {
				case out <- event:
				case <-ctx.Done():
				}
				return
			}
			for _, ev := range resp.Events {
				typeValue := port.EventPut
				value := ev.Kv.Value
				if ev.Type == clientv3.EventTypeDelete {
					typeValue = port.EventDelete
					if ev.PrevKv != nil {
						value = ev.PrevKv.Value
					}
				}
				select {
				case out <- port.Event{Type: typeValue, Key: string(ev.Kv.Key), Value: value, Revision: ev.Kv.ModRevision}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out
}

// GrantLease creates a lease used by heartbeat registrations.
func (s *Store) GrantLease(ctx context.Context, ttlSeconds int64) (port.LeaseID, error) {
	resp, err := s.client.Grant(ctx, ttlSeconds)
	if err != nil {
		return 0, mapEtcdError(err, "grant lease failed")
	}
	return port.LeaseID(resp.ID), nil
}

// KeepAliveOnce refreshes a lease one time.
func (s *Store) KeepAliveOnce(ctx context.Context, leaseID port.LeaseID) error {
	_, err := s.client.KeepAliveOnce(ctx, clientv3.LeaseID(leaseID))
	if err != nil {
		return mapEtcdError(err, "keepalive failed")
	}
	return nil
}

// RevokeLease revokes a lease explicitly.
func (s *Store) RevokeLease(ctx context.Context, leaseID port.LeaseID) error {
	_, err := s.client.Revoke(ctx, clientv3.LeaseID(leaseID))
	if err != nil {
		return mapEtcdError(err, "revoke lease failed")
	}
	return nil
}

func withLease(leaseID port.LeaseID) []clientv3.OpOption {
	if leaseID == 0 {
		return nil
	}
	return []clientv3.OpOption{clientv3.WithLease(clientv3.LeaseID(leaseID))}
}

func mapEtcdError(err error, message string) error {
	switch err {
	case nil:
		return nil
	case rpctypes.ErrKeyNotFound:
		return apperrors.Wrap(apperrors.CodeNotFound, message, err)
	case rpctypes.ErrCompacted:
		return apperrors.Wrap(apperrors.CodeFailedPrecondition, message, err)
	default:
		return apperrors.Wrap(apperrors.CodeUnavailable, message, err)
	}
}
