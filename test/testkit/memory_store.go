// memory_store.go provides an in-memory store implementation for service-layer unit tests.
package testkit

import (
	"context"
	"sort"
	"strings"
	"sync"

	apperrors "etcdisc/internal/core/errors"
	"etcdisc/internal/core/port"
)

type memoryRecord struct {
	value    []byte
	revision int64
	leaseID  port.LeaseID
}

type watcher struct {
	prefix string
	ch     chan port.Event
}

// MemoryStore implements the persistence port in memory for unit tests.
type MemoryStore struct {
	mu       sync.RWMutex
	revision int64
	data     map[string]memoryRecord
	watchers map[int]watcher
	nextID   int
	leases   map[port.LeaseID]struct{}
}

// NewMemoryStore creates an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		data:     map[string]memoryRecord{},
		watchers: map[int]watcher{},
		leases:   map[port.LeaseID]struct{}{},
	}
}

// Status reports that the in-memory store is always available.
func (s *MemoryStore) Status(context.Context) error {
	return nil
}

// Create inserts a value when the key does not already exist.
func (s *MemoryStore) Create(_ context.Context, key string, value []byte, leaseID port.LeaseID) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[key]; ok {
		return 0, apperrors.New(apperrors.CodeAlreadyExists, "key already exists")
	}
	revision := s.bumpRevision()
	record := memoryRecord{value: append([]byte(nil), value...), revision: revision, leaseID: leaseID}
	s.data[key] = record
	s.broadcastLocked(port.Event{Type: port.EventPut, Key: key, Value: record.value, Revision: revision})
	return revision, nil
}

// Put writes a value with last-write-wins semantics.
func (s *MemoryStore) Put(_ context.Context, key string, value []byte, leaseID port.LeaseID) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	revision := s.bumpRevision()
	record := memoryRecord{value: append([]byte(nil), value...), revision: revision, leaseID: leaseID}
	s.data[key] = record
	s.broadcastLocked(port.Event{Type: port.EventPut, Key: key, Value: record.value, Revision: revision})
	return revision, nil
}

// CAS writes a value only when the current revision matches the expected revision.
func (s *MemoryStore) CAS(_ context.Context, key string, expectedRevision int64, value []byte, leaseID port.LeaseID) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.data[key]
	if !ok || current.revision != expectedRevision {
		return 0, apperrors.New(apperrors.CodeConflict, "cas conflict")
	}
	revision := s.bumpRevision()
	record := memoryRecord{value: append([]byte(nil), value...), revision: revision, leaseID: leaseID}
	s.data[key] = record
	s.broadcastLocked(port.Event{Type: port.EventPut, Key: key, Value: record.value, Revision: revision})
	return revision, nil
}

// Get returns a single stored record.
func (s *MemoryStore) Get(_ context.Context, key string) (port.Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.data[key]
	if !ok {
		return port.Record{}, apperrors.New(apperrors.CodeNotFound, "key not found")
	}
	return port.Record{Key: key, Value: append([]byte(nil), record.value...), Revision: record.revision, LeaseID: record.leaseID}, nil
}

// List returns all records under a prefix ordered by key.
func (s *MemoryStore) List(_ context.Context, prefix string) ([]port.Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]string, 0)
	for key := range s.data {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	records := make([]port.Record, 0, len(keys))
	for _, key := range keys {
		record := s.data[key]
		records = append(records, port.Record{Key: key, Value: append([]byte(nil), record.value...), Revision: record.revision, LeaseID: record.leaseID})
	}
	return records, nil
}

// Delete removes a key unconditionally.
func (s *MemoryStore) Delete(_ context.Context, key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.data[key]
	if !ok {
		return 0, apperrors.New(apperrors.CodeNotFound, "key not found")
	}
	delete(s.data, key)
	revision := s.bumpRevision()
	s.broadcastLocked(port.Event{Type: port.EventDelete, Key: key, Value: append([]byte(nil), record.value...), Revision: revision})
	return revision, nil
}

// DeleteCAS removes a key only when the current revision matches.
func (s *MemoryStore) DeleteCAS(_ context.Context, key string, expectedRevision int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.data[key]
	if !ok || record.revision != expectedRevision {
		return 0, apperrors.New(apperrors.CodeConflict, "cas conflict")
	}
	delete(s.data, key)
	revision := s.bumpRevision()
	s.broadcastLocked(port.Event{Type: port.EventDelete, Key: key, Value: append([]byte(nil), record.value...), Revision: revision})
	return revision, nil
}

// Watch subscribes to future changes for a prefix.
func (s *MemoryStore) Watch(ctx context.Context, prefix string, _ int64) <-chan port.Event {
	out := make(chan port.Event, 16)
	s.mu.Lock()
	id := s.nextID
	s.nextID++
	s.watchers[id] = watcher{prefix: prefix, ch: out}
	s.mu.Unlock()
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		delete(s.watchers, id)
		close(out)
		s.mu.Unlock()
	}()
	return out
}

// GrantLease creates a synthetic lease identifier.
func (s *MemoryStore) GrantLease(_ context.Context, _ int64) (port.LeaseID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := port.LeaseID(s.bumpRevision())
	s.leases[id] = struct{}{}
	return id, nil
}

// KeepAliveOnce verifies the lease exists.
func (s *MemoryStore) KeepAliveOnce(_ context.Context, leaseID port.LeaseID) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.leases[leaseID]; !ok {
		return apperrors.New(apperrors.CodeNotFound, "lease not found")
	}
	return nil
}

// RevokeLease removes a synthetic lease.
func (s *MemoryStore) RevokeLease(_ context.Context, leaseID port.LeaseID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.leases, leaseID)
	return nil
}

func (s *MemoryStore) bumpRevision() int64 {
	s.revision++
	return s.revision
}

func (s *MemoryStore) broadcastLocked(event port.Event) {
	for _, watcher := range s.watchers {
		if !strings.HasPrefix(event.Key, watcher.prefix) {
			continue
		}
		select {
		case watcher.ch <- event:
		default:
		}
	}
}
