// service.go implements discovery snapshots and instance watch streams for consumers.
package discovery

import (
	"context"
	"encoding/json"

	apperrors "etcdisc/internal/core/errors"
	"etcdisc/internal/core/keyspace"
	"etcdisc/internal/core/model"
	"etcdisc/internal/core/port"
	registrysvc "etcdisc/internal/core/service/registry"
)

// Service provides discovery snapshots and watch streams.
type Service struct {
	store    port.Store
	registry *registrysvc.Service
}

// SnapshotInput captures discovery filter options.
type SnapshotInput struct {
	Namespace   string
	Service     string
	Group       string
	Version     string
	Metadata    map[string]string
	HealthyOnly *bool
}

// WatchInput captures registry watch options.
type WatchInput struct {
	Namespace   string
	Service     string
	HealthyOnly *bool
	Revision    int64
}

// NewService creates a discovery service.
func NewService(store port.Store, registry *registrysvc.Service) *Service {
	return &Service{store: store, registry: registry}
}

// Snapshot returns the current discovery result set.
func (s *Service) Snapshot(ctx context.Context, input SnapshotInput) ([]model.Instance, error) {
	healthyOnly := true
	if input.HealthyOnly != nil {
		healthyOnly = *input.HealthyOnly
	}
	return s.registry.List(ctx, registrysvc.ListFilter{
		Namespace:   input.Namespace,
		Service:     input.Service,
		Group:       input.Group,
		Version:     input.Version,
		Metadata:    input.Metadata,
		HealthyOnly: healthyOnly,
	})
}

// Watch streams instance changes as watch events.
func (s *Service) Watch(ctx context.Context, input WatchInput) <-chan model.WatchEvent {
	out := make(chan model.WatchEvent, 16)
	prefix := keyspace.InstanceNamespacePrefix(input.Namespace)
	if input.Service != "" {
		prefix = keyspace.InstanceServicePrefix(input.Namespace, input.Service)
	}
	in := s.store.Watch(ctx, prefix, input.Revision)
	go func() {
		defer close(out)
		healthyOnly := true
		if input.HealthyOnly != nil {
			healthyOnly = *input.HealthyOnly
		}
		for event := range in {
			if event.Err != nil {
				if event.Type == port.EventReset {
					out <- model.WatchEvent{Type: model.WatchEventReset, Resource: "instance", Revision: event.CompactRevision}
				}
				return
			}
			instance, err := decodeWatchInstance(event)
			if err != nil {
				return
			}
			if healthyOnly && event.Type == port.EventPut && instance.Status != model.InstanceStatusHealth {
				continue
			}
			payload, err := json.Marshal(instance)
			if err != nil {
				return
			}
			eventType := model.WatchEventPut
			if event.Type == port.EventDelete {
				eventType = model.WatchEventDelete
			}
			out <- model.WatchEvent{Type: eventType, Resource: "instance", Key: event.Key, Revision: event.Revision, Value: payload}
		}
	}()
	return out
}

func decodeWatchInstance(event port.Event) (model.Instance, error) {
	var instance model.Instance
	if err := json.Unmarshal(event.Value, &instance); err != nil {
		return model.Instance{}, apperrors.Wrap(apperrors.CodeInternal, "unmarshal instance watch event failed", err)
	}
	return instance, nil
}
