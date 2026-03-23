// service.go implements AgentCard storage, capability indexing, and A2A capability discovery.
package a2a

import (
	"context"
	"encoding/json"
	"sort"

	apperrors "etcdisc/internal/core/errors"
	"etcdisc/internal/core/keyspace"
	"etcdisc/internal/core/model"
	"etcdisc/internal/core/port"
	namespacesvc "etcdisc/internal/core/service/namespace"
	registrysvc "etcdisc/internal/core/service/registry"
	"etcdisc/internal/infra/clock"
)

type capabilityIndexEntry struct {
	AgentID string `json:"agentId"`
	Service string `json:"service"`
}

// Service manages AgentCards and A2A discovery.
type Service struct {
	store      port.Store
	namespaces *namespacesvc.Service
	registry   *registrysvc.Service
	clock      clock.Clock
}

// PutInput creates or updates an AgentCard.
type PutInput struct {
	Card             model.AgentCard `json:"card"`
	ExpectedRevision int64           `json:"expectedRevision"`
}

// DiscoverInput selects capability discovery results.
type DiscoverInput struct {
	Namespace        string
	Capability       string
	Protocol         string
	IncludeUnhealthy bool
	Admin            bool
}

// NewService creates an A2A service.
func NewService(store port.Store, namespaces *namespacesvc.Service, registry *registrysvc.Service, clk clock.Clock) *Service {
	if clk == nil {
		clk = clock.RealClock{}
	}
	return &Service{store: store, namespaces: namespaces, registry: registry, clock: clk}
}

// Put creates or updates one AgentCard and refreshes its capability index entries.
func (s *Service) Put(ctx context.Context, input PutInput) (model.AgentCard, error) {
	card := input.Card
	if err := s.namespaces.CheckWrite(ctx, card.Namespace, false); err != nil {
		return model.AgentCard{}, err
	}
	if err := card.Validate(); err != nil {
		return model.AgentCard{}, err
	}
	key := keyspace.AgentCardKey(card.Namespace, card.AgentID)
	var previous *model.AgentCard
	if input.ExpectedRevision > 0 {
		current, err := s.Get(ctx, card.Namespace, card.AgentID, false)
		if err != nil {
			return model.AgentCard{}, err
		}
		previous = &current
		card.CreatedAt = current.CreatedAt
	} else {
		card.CreatedAt = s.clock.Now()
	}
	card.UpdatedAt = s.clock.Now()
	payload, err := json.Marshal(card)
	if err != nil {
		return model.AgentCard{}, apperrors.Wrap(apperrors.CodeInternal, "marshal agent card failed", err)
	}
	var revision int64
	if input.ExpectedRevision == 0 {
		revision, err = s.store.Create(ctx, key, payload, 0)
	} else {
		revision, err = s.store.CAS(ctx, key, input.ExpectedRevision, payload, 0)
	}
	if err != nil {
		return model.AgentCard{}, err
	}
	card.Revision = revision
	if previous != nil {
		if err := s.deleteCapabilityIndexes(ctx, *previous); err != nil {
			return model.AgentCard{}, err
		}
	}
	if err := s.putCapabilityIndexes(ctx, card); err != nil {
		return model.AgentCard{}, err
	}
	return card, nil
}

// Get returns one AgentCard.
func (s *Service) Get(ctx context.Context, namespaceName, agentID string, admin bool) (model.AgentCard, error) {
	if err := s.namespaces.CheckRead(ctx, namespaceName, admin); err != nil {
		return model.AgentCard{}, err
	}
	record, err := s.store.Get(ctx, keyspace.AgentCardKey(namespaceName, agentID))
	if err != nil {
		return model.AgentCard{}, err
	}
	return decodeAgentCard(record)
}

// List returns all AgentCards in a namespace.
func (s *Service) List(ctx context.Context, namespaceName string, admin bool) ([]model.AgentCard, error) {
	if err := s.namespaces.CheckRead(ctx, namespaceName, admin); err != nil {
		return nil, err
	}
	records, err := s.store.List(ctx, keyspace.AgentCardNamespacePrefix(namespaceName))
	if err != nil {
		return nil, err
	}
	items := make([]model.AgentCard, 0, len(records))
	for _, record := range records {
		item, err := decodeAgentCard(record)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].AgentID < items[j].AgentID })
	return items, nil
}

// Discover resolves AgentCards plus runtime instances by capability.
func (s *Service) Discover(ctx context.Context, input DiscoverInput) ([]model.A2ADiscoveryResult, error) {
	if err := s.namespaces.CheckRead(ctx, input.Namespace, input.Admin); err != nil {
		return nil, err
	}
	if err := model.ValidateCapabilityName(input.Capability); err != nil {
		return nil, err
	}
	records, err := s.store.List(ctx, keyspace.CapabilityPrefix(input.Namespace, input.Capability))
	if err != nil {
		return nil, err
	}
	results := make([]model.A2ADiscoveryResult, 0)
	for _, record := range records {
		var entry capabilityIndexEntry
		if err := json.Unmarshal(record.Value, &entry); err != nil {
			return nil, apperrors.Wrap(apperrors.CodeInternal, "unmarshal capability entry failed", err)
		}
		card, err := s.Get(ctx, input.Namespace, entry.AgentID, input.Admin)
		if err != nil {
			return nil, err
		}
		if input.Protocol != "" && !contains(card.Protocols, input.Protocol) {
			continue
		}
		instances, err := s.registry.List(ctx, registrysvc.ListFilter{Namespace: input.Namespace, Service: entry.Service, HealthyOnly: !input.IncludeUnhealthy, Admin: input.Admin})
		if err != nil {
			return nil, err
		}
		for _, instance := range instances {
			if instance.AgentID != card.AgentID {
				continue
			}
			results = append(results, model.A2ADiscoveryResult{
				Namespace: input.Namespace,
				AgentID:   card.AgentID,
				Service:   card.Service,
				Address:   instance.Address,
				Port:      instance.Port,
				Protocols: append([]string(nil), card.Protocols...),
				AuthMode:  card.AuthMode,
				Metadata:  model.CopyStringMap(card.Metadata),
				Status:    instance.Status,
			})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].AgentID == results[j].AgentID {
			return results[i].Address < results[j].Address
		}
		return results[i].AgentID < results[j].AgentID
	})
	return results, nil
}

func (s *Service) putCapabilityIndexes(ctx context.Context, card model.AgentCard) error {
	entry, err := json.Marshal(capabilityIndexEntry{AgentID: card.AgentID, Service: card.Service})
	if err != nil {
		return apperrors.Wrap(apperrors.CodeInternal, "marshal capability entry failed", err)
	}
	for _, capability := range card.Capabilities {
		if _, err := s.store.Put(ctx, keyspace.CapabilityKey(card.Namespace, capability, card.AgentID), entry, 0); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) deleteCapabilityIndexes(ctx context.Context, card model.AgentCard) error {
	for _, capability := range card.Capabilities {
		if _, err := s.store.Delete(ctx, keyspace.CapabilityKey(card.Namespace, capability, card.AgentID)); err != nil && !apperrors.IsCode(err, apperrors.CodeNotFound) {
			return err
		}
	}
	return nil
}

func decodeAgentCard(record port.Record) (model.AgentCard, error) {
	var card model.AgentCard
	if err := json.Unmarshal(record.Value, &card); err != nil {
		return model.AgentCard{}, apperrors.Wrap(apperrors.CodeInternal, "unmarshal agent card failed", err)
	}
	card.Revision = record.Revision
	return card, nil
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
