// service.go records and lists audit entries used by admin views and operational troubleshooting.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync/atomic"

	apperrors "etcdisc/internal/core/errors"
	"etcdisc/internal/core/keyspace"
	"etcdisc/internal/core/model"
	"etcdisc/internal/core/port"
	"etcdisc/internal/infra/clock"
)

// Service stores append-only audit entries.
type Service struct {
	store   port.Store
	clock   clock.Clock
	counter atomic.Uint64
}

// NewService creates an audit service.
func NewService(store port.Store, clk clock.Clock) *Service {
	if clk == nil {
		clk = clock.RealClock{}
	}
	return &Service{store: store, clock: clk}
}

// Record appends one audit entry.
func (s *Service) Record(ctx context.Context, entry model.AuditEntry) (model.AuditEntry, error) {
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("audit-%d", s.counter.Add(1))
	}
	if entry.Metadata == nil {
		entry.Metadata = map[string]string{}
	}
	entry.CreatedAt = s.clock.Now()
	payload, err := json.Marshal(entry)
	if err != nil {
		return model.AuditEntry{}, apperrors.Wrap(apperrors.CodeInternal, "marshal audit entry failed", err)
	}
	revision, err := s.store.Create(ctx, keyspace.AuditKey(entry.ID), payload, 0)
	if err != nil {
		return model.AuditEntry{}, err
	}
	entry.Revision = revision
	return entry, nil
}

// List returns audit entries in reverse chronological order.
func (s *Service) List(ctx context.Context) ([]model.AuditEntry, error) {
	records, err := s.store.List(ctx, keyspace.AuditPrefix())
	if err != nil {
		return nil, err
	}
	entries := make([]model.AuditEntry, 0, len(records))
	for _, record := range records {
		var entry model.AuditEntry
		if err := json.Unmarshal(record.Value, &entry); err != nil {
			return nil, apperrors.Wrap(apperrors.CodeInternal, "unmarshal audit entry failed", err)
		}
		entry.Revision = record.Revision
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].CreatedAt.Equal(entries[j].CreatedAt) {
			return entries[i].Revision > entries[j].Revision
		}
		return entries[i].CreatedAt.After(entries[j].CreatedAt)
	})
	return entries, nil
}
