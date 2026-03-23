// service.go implements namespace lifecycle and access-mode checks for phase 1 resource isolation.
package namespace

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	apperrors "etcdisc/internal/core/errors"
	"etcdisc/internal/core/keyspace"
	"etcdisc/internal/core/model"
	"etcdisc/internal/core/port"
	"etcdisc/internal/infra/clock"
)

// Service manages namespace lifecycle and access checks.
type Service struct {
	store port.Store
	clock clock.Clock
}

// NewService creates a namespace service.
func NewService(store port.Store, clk clock.Clock) *Service {
	if clk == nil {
		clk = clock.RealClock{}
	}
	return &Service{store: store, clock: clk}
}

// CreateNamespaceInput contains fields accepted by namespace creation.
type CreateNamespaceInput struct {
	Name string `json:"name"`
}

// UpdateAccessModeInput contains the mutable fields for namespace updates.
type UpdateAccessModeInput struct {
	Name             string                    `json:"name"`
	AccessMode       model.NamespaceAccessMode `json:"accessMode"`
	ExpectedRevision int64                     `json:"expectedRevision"`
}

// Create creates a new namespace with the documented default access mode.
func (s *Service) Create(ctx context.Context, input CreateNamespaceInput) (model.Namespace, error) {
	ns := model.Namespace{
		Name:       input.Name,
		AccessMode: model.NamespaceAccessReadWrite,
	}
	now := s.clock.Now()
	ns.CreatedAt = now
	ns.UpdatedAt = now
	if err := ns.Validate(); err != nil {
		return model.Namespace{}, err
	}
	payload, err := json.Marshal(ns)
	if err != nil {
		return model.Namespace{}, apperrors.Wrap(apperrors.CodeInternal, "marshal namespace failed", err)
	}
	revision, err := s.store.Create(ctx, keyspace.NamespaceKey(ns.Name), payload, 0)
	if err != nil {
		return model.Namespace{}, err
	}
	ns.Revision = revision
	return ns, nil
}

// Get returns a namespace by name.
func (s *Service) Get(ctx context.Context, name string) (model.Namespace, error) {
	if err := model.ValidateNamespaceName(name); err != nil {
		return model.Namespace{}, err
	}
	record, err := s.store.Get(ctx, keyspace.NamespaceKey(name))
	if err != nil {
		return model.Namespace{}, err
	}
	var ns model.Namespace
	if err := json.Unmarshal(record.Value, &ns); err != nil {
		return model.Namespace{}, apperrors.Wrap(apperrors.CodeInternal, "unmarshal namespace failed", err)
	}
	ns.Revision = record.Revision
	return ns, nil
}

// List returns all namespaces ordered by name.
func (s *Service) List(ctx context.Context) ([]model.Namespace, error) {
	records, err := s.store.List(ctx, keyspace.NamespacePrefix())
	if err != nil {
		return nil, err
	}
	namespaces := make([]model.Namespace, 0, len(records))
	for _, record := range records {
		var ns model.Namespace
		if err := json.Unmarshal(record.Value, &ns); err != nil {
			return nil, apperrors.Wrap(apperrors.CodeInternal, "unmarshal namespace failed", err)
		}
		ns.Revision = record.Revision
		namespaces = append(namespaces, ns)
	}
	sort.Slice(namespaces, func(i, j int) bool {
		return namespaces[i].Name < namespaces[j].Name
	})
	return namespaces, nil
}

// UpdateAccessMode updates namespace accessMode using CAS.
func (s *Service) UpdateAccessMode(ctx context.Context, input UpdateAccessModeInput) (model.Namespace, error) {
	if err := model.ValidateNamespaceName(input.Name); err != nil {
		return model.Namespace{}, err
	}
	if input.ExpectedRevision <= 0 {
		return model.Namespace{}, apperrors.New(apperrors.CodeInvalidArgument, "expectedRevision must be greater than zero")
	}
	current, err := s.Get(ctx, input.Name)
	if err != nil {
		return model.Namespace{}, err
	}
	current.AccessMode = input.AccessMode
	current.UpdatedAt = s.clock.Now()
	if err := current.Validate(); err != nil {
		return model.Namespace{}, err
	}
	payload, err := json.Marshal(current)
	if err != nil {
		return model.Namespace{}, apperrors.Wrap(apperrors.CodeInternal, "marshal namespace failed", err)
	}
	revision, err := s.store.CAS(ctx, keyspace.NamespaceKey(current.Name), input.ExpectedRevision, payload, 0)
	if err != nil {
		return model.Namespace{}, err
	}
	current.Revision = revision
	return current, nil
}

// CheckRead rejects business read traffic when the namespace blocks reads.
func (s *Service) CheckRead(ctx context.Context, name string, admin bool) error {
	if admin {
		return nil
	}
	ns, err := s.Get(ctx, name)
	if err != nil {
		return err
	}
	if !ns.AccessMode.AllowsRead() {
		return apperrors.New(apperrors.CodeForbidden, "namespace does not allow reads")
	}
	return nil
}

// CheckWrite rejects business write traffic when the namespace blocks writes.
func (s *Service) CheckWrite(ctx context.Context, name string, admin bool) error {
	if admin {
		return nil
	}
	ns, err := s.Get(ctx, name)
	if err != nil {
		return err
	}
	if !ns.AccessMode.AllowsWrite() {
		return apperrors.New(apperrors.CodeForbidden, "namespace does not allow writes")
	}
	return nil
}

// TouchExists verifies that the namespace exists.
func (s *Service) TouchExists(ctx context.Context, name string) error {
	_, err := s.Get(ctx, name)
	return err
}

// NewFixedClock returns a clock suitable for tests.
func NewFixedClock(now time.Time) clock.Clock {
	return fixedClock{now: now.UTC()}
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}
