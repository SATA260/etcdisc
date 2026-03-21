// service_test.go verifies config CAS updates, effective resolution order, deletes, and watch events.
package config

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	apperrors "etcdisc/internal/core/errors"
	"etcdisc/internal/core/model"
	namespacesvc "etcdisc/internal/core/service/namespace"
	"etcdisc/test/testkit"
)

func TestResolveOrderAndDeleteFallback(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	nsService := namespacesvc.NewService(store, namespacesvc.NewFixedClock(time.Now()))
	created, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	_ = created
	svc := NewService(store, nsService, namespacesvc.NewFixedClock(time.Now()))

	globalItem, err := svc.Put(context.Background(), PutInput{Item: model.ConfigItem{Scope: model.ConfigScopeGlobal, Key: "timeout.request", Value: "1000", ValueType: model.ConfigValueDuration}})
	require.NoError(t, err)
	_, err = svc.Put(context.Background(), PutInput{Item: model.ConfigItem{Scope: model.ConfigScopeNamespace, Namespace: "prod-core", Key: "timeout.request", Value: "2000", ValueType: model.ConfigValueDuration}})
	require.NoError(t, err)
	serviceItem, err := svc.Put(context.Background(), PutInput{Item: model.ConfigItem{Scope: model.ConfigScopeService, Namespace: "prod-core", Service: "payment-api", Key: "timeout.request", Value: "3000", ValueType: model.ConfigValueDuration}})
	require.NoError(t, err)

	resolved, err := svc.Resolve(context.Background(), ResolveInput{Namespace: "prod-core", Service: "payment-api"})
	require.NoError(t, err)
	require.Equal(t, "3000", resolved["timeout.request"].Value)

	require.NoError(t, svc.Delete(context.Background(), DeleteInput{Scope: model.ConfigScopeService, Namespace: "prod-core", Service: "payment-api", Key: "timeout.request", ExpectedRevision: serviceItem.Revision}))
	resolved, err = svc.Resolve(context.Background(), ResolveInput{Namespace: "prod-core", Service: "payment-api"})
	require.NoError(t, err)
	require.Equal(t, "2000", resolved["timeout.request"].Value)
	require.NotZero(t, globalItem.Revision)
}

func TestPutRequiresCASOnUpdate(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	nsService := namespacesvc.NewService(store, namespacesvc.NewFixedClock(time.Now()))
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	svc := NewService(store, nsService, namespacesvc.NewFixedClock(time.Now()))

	item, err := svc.Put(context.Background(), PutInput{Item: model.ConfigItem{Scope: model.ConfigScopeNamespace, Namespace: "prod-core", Key: "feature.enabled", Value: "true", ValueType: model.ConfigValueBool}})
	require.NoError(t, err)
	_, err = svc.Put(context.Background(), PutInput{Item: model.ConfigItem{Scope: model.ConfigScopeNamespace, Namespace: "prod-core", Key: "feature.enabled", Value: "false", ValueType: model.ConfigValueBool}, ExpectedRevision: item.Revision + 1})
	require.Error(t, err)
	require.Equal(t, apperrors.CodeConflict, apperrors.CodeOf(err))
	_, err = svc.Put(context.Background(), PutInput{Item: model.ConfigItem{Scope: model.ConfigScopeNamespace, Namespace: "prod-core", Key: "feature.enabled", Value: "not-bool", ValueType: model.ConfigValueBool}})
	require.Equal(t, apperrors.CodeInvalidArgument, apperrors.CodeOf(err))
}

func TestGetRawAndResolveClientOverrides(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	nsService := namespacesvc.NewService(store, namespacesvc.NewFixedClock(time.Now()))
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	svc := NewService(store, nsService, namespacesvc.NewFixedClock(time.Now()))

	_, err = svc.Put(context.Background(), PutInput{Item: model.ConfigItem{Scope: model.ConfigScopeGlobal, Key: "timeout.request", Value: "1000", ValueType: model.ConfigValueDuration}})
	require.NoError(t, err)
	items, err := svc.GetRaw(context.Background(), model.ConfigScopeGlobal, "", "", false)
	require.NoError(t, err)
	require.Len(t, items, 1)

	resolved, err := svc.Resolve(context.Background(), ResolveInput{Namespace: "prod-core", Service: "payment-api", ClientOverrides: map[string]model.ConfigItem{"timeout.request": {Scope: model.ConfigScopeService, Namespace: "prod-core", Service: "payment-api", Key: "timeout.request", Value: "1500", ValueType: model.ConfigValueDuration}}})
	require.NoError(t, err)
	require.Equal(t, "1500", resolved["timeout.request"].Value)

	err = svc.Delete(context.Background(), DeleteInput{Scope: model.ConfigScopeService, Namespace: "prod-core", Service: "payment-api", Key: "timeout.request"})
	require.Equal(t, apperrors.CodeInvalidArgument, apperrors.CodeOf(err))
}

func TestWatchStreamsSingleKeyEvents(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	nsService := namespacesvc.NewService(store, namespacesvc.NewFixedClock(time.Now()))
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	svc := NewService(store, nsService, namespacesvc.NewFixedClock(time.Now()))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watchCh := svc.Watch(ctx, WatchInput{Namespace: "prod-core", Service: "payment-api"})
	time.Sleep(20 * time.Millisecond)
	_, err = svc.Put(context.Background(), PutInput{Item: model.ConfigItem{Scope: model.ConfigScopeService, Namespace: "prod-core", Service: "payment-api", Key: "timeout.request", Value: "1000", ValueType: model.ConfigValueDuration}})
	require.NoError(t, err)

	event := <-watchCh
	require.Equal(t, model.WatchEventPut, event.Type)
	var item model.ConfigItem
	require.NoError(t, json.Unmarshal(event.Value, &item))
	require.Equal(t, "timeout.request", item.Key)
}
