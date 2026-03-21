// service.go implements config item storage, effective config resolution, deletes, and watch streams.
package config

import (
	"context"
	"encoding/json"
	"sort"
	"sync"

	apperrors "etcdisc/internal/core/errors"
	"etcdisc/internal/core/keyspace"
	"etcdisc/internal/core/model"
	"etcdisc/internal/core/port"
	namespacesvc "etcdisc/internal/core/service/namespace"
	"etcdisc/internal/infra/clock"
)

// Service manages raw config items and effective config resolution.
type Service struct {
	store      port.Store
	namespaces *namespacesvc.Service
	clock      clock.Clock
}

// PutInput creates or updates one config item.
type PutInput struct {
	Item             model.ConfigItem `json:"item"`
	ExpectedRevision int64            `json:"expectedRevision"`
}

// DeleteInput removes one config item at a single scope.
type DeleteInput struct {
	Scope            model.ConfigScope `json:"scope"`
	Namespace        string            `json:"namespace,omitempty"`
	Service          string            `json:"service,omitempty"`
	Key              string            `json:"key"`
	ExpectedRevision int64             `json:"expectedRevision"`
}

// ResolveInput selects the effective config view for one namespace and service.
type ResolveInput struct {
	Namespace       string                      `json:"namespace"`
	Service         string                      `json:"service"`
	ClientOverrides map[string]model.ConfigItem `json:"clientOverrides,omitempty"`
	Admin           bool                        `json:"-"`
}

// WatchInput selects the config watch scope for one namespace and service.
type WatchInput struct {
	Namespace string
	Service   string
	Revision  int64
	Admin     bool
}

// NewService creates a config service.
func NewService(store port.Store, namespaces *namespacesvc.Service, clk clock.Clock) *Service {
	if clk == nil {
		clk = clock.RealClock{}
	}
	return &Service{store: store, namespaces: namespaces, clock: clk}
}

// Put creates or updates a config item using CAS semantics.
func (s *Service) Put(ctx context.Context, input PutInput) (model.ConfigItem, error) {
	item := input.Item
	if item.Scope != model.ConfigScopeGlobal {
		if err := s.namespaces.CheckWrite(ctx, item.Namespace, false); err != nil {
			return model.ConfigItem{}, err
		}
	}
	if err := item.Validate(); err != nil {
		return model.ConfigItem{}, err
	}
	now := s.clock.Now()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	payload, err := json.Marshal(item)
	if err != nil {
		return model.ConfigItem{}, apperrors.Wrap(apperrors.CodeInternal, "marshal config item failed", err)
	}
	key := keyspace.ConfigKey(item.Scope, item.Namespace, item.Service, item.Key)
	var revision int64
	if input.ExpectedRevision == 0 {
		revision, err = s.store.Create(ctx, key, payload, 0)
	} else {
		revision, err = s.store.CAS(ctx, key, input.ExpectedRevision, payload, 0)
	}
	if err != nil {
		return model.ConfigItem{}, err
	}
	item.Revision = revision
	return item, nil
}

// Delete removes a config item from the specified scope.
func (s *Service) Delete(ctx context.Context, input DeleteInput) error {
	if input.Scope != model.ConfigScopeGlobal {
		if err := s.namespaces.CheckWrite(ctx, input.Namespace, false); err != nil {
			return err
		}
	}
	if input.ExpectedRevision <= 0 {
		return apperrors.New(apperrors.CodeInvalidArgument, "expectedRevision must be greater than zero")
	}
	_, err := s.store.DeleteCAS(ctx, keyspace.ConfigKey(input.Scope, input.Namespace, input.Service, input.Key), input.ExpectedRevision)
	return err
}

// GetRaw returns all raw config items at one scope.

func (s *Service) GetRaw(ctx context.Context, scope model.ConfigScope, namespaceName, serviceName string, admin bool) ([]model.ConfigItem, error) {
	if scope != model.ConfigScopeGlobal {
		if err := s.namespaces.CheckRead(ctx, namespaceName, admin); err != nil {
			return nil, err
		}
	}
	records, err := s.store.List(ctx, keyspace.ConfigPrefix(scope, namespaceName, serviceName))
	if err != nil {
		return nil, err
	}
	items := make([]model.ConfigItem, 0, len(records))
	for _, record := range records {
		item, err := decodeConfigItem(record)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items, nil
}

// Resolve returns the effective config after applying global, namespace, service, then client values.
func (s *Service) Resolve(ctx context.Context, input ResolveInput) (map[string]model.EffectiveConfigItem, error) {
	if err := s.namespaces.CheckRead(ctx, input.Namespace, input.Admin); err != nil {
		return nil, err
	}
	resolved := map[string]model.EffectiveConfigItem{}
	merge := func(scope model.ConfigScope, items []model.ConfigItem) {
		for _, item := range items {
			resolved[item.Key] = model.EffectiveConfigItem{Key: item.Key, Value: item.Value, ValueType: item.ValueType, Scope: scope}
		}
	}
	globalItems, err := s.GetRaw(ctx, model.ConfigScopeGlobal, "", "", input.Admin)
	if err != nil {
		return nil, err
	}
	namespaceItems, err := s.GetRaw(ctx, model.ConfigScopeNamespace, input.Namespace, "", input.Admin)
	if err != nil {
		return nil, err
	}
	serviceItems, err := s.GetRaw(ctx, model.ConfigScopeService, input.Namespace, input.Service, input.Admin)
	if err != nil {
		return nil, err
	}
	merge(model.ConfigScopeGlobal, globalItems)
	merge(model.ConfigScopeNamespace, namespaceItems)
	merge(model.ConfigScopeService, serviceItems)
	for key, item := range input.ClientOverrides {
		if err := item.Validate(); err != nil {
			return nil, err
		}
		resolved[key] = model.EffectiveConfigItem{Key: key, Value: item.Value, ValueType: item.ValueType, Scope: item.Scope}
	}
	return resolved, nil
}

// Watch streams config changes from the relevant global, namespace, and service scopes.
func (s *Service) Watch(ctx context.Context, input WatchInput) <-chan model.WatchEvent {
	out := make(chan model.WatchEvent, 32)
	prefixes := []string{
		keyspace.ConfigPrefix(model.ConfigScopeGlobal, "", ""),
		keyspace.ConfigPrefix(model.ConfigScopeNamespace, input.Namespace, ""),
		keyspace.ConfigPrefix(model.ConfigScopeService, input.Namespace, input.Service),
	}
	var wg sync.WaitGroup
	wg.Add(len(prefixes))
	for _, prefix := range prefixes {
		prefix := prefix
		go func() {
			defer wg.Done()
			for event := range s.store.Watch(ctx, prefix, input.Revision) {
				if event.Err != nil {
					if event.Type == port.EventReset {
						select {
						case out <- model.WatchEvent{Type: model.WatchEventReset, Resource: "config", Key: prefix, Revision: event.CompactRevision}:
						case <-ctx.Done():
						}
					}
					return
				}
				configItem, err := decodeConfigItem(port.Record{Key: event.Key, Value: event.Value, Revision: event.Revision})
				if err != nil {
					return
				}
				payload, err := json.Marshal(configItem)
				if err != nil {
					return
				}
				eventType := model.WatchEventPut
				if event.Type == port.EventDelete {
					eventType = model.WatchEventDelete
				}
				select {
				case out <- model.WatchEvent{Type: eventType, Resource: "config", Key: event.Key, Revision: event.Revision, Value: payload}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

func decodeConfigItem(record port.Record) (model.ConfigItem, error) {
	var item model.ConfigItem
	if err := json.Unmarshal(record.Value, &item); err != nil {
		return model.ConfigItem{}, apperrors.Wrap(apperrors.CodeInternal, "unmarshal config item failed", err)
	}
	item.Revision = record.Revision
	return item, nil
}
