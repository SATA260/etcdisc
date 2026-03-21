// etcd_integration_test.go exercises the real-etcd storage, registry, and config paths required by phase 1.
package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"

	appconfig "etcdisc/internal/app/config"
	"etcdisc/internal/core/model"
	configsvc "etcdisc/internal/core/service/config"
	discoverysvc "etcdisc/internal/core/service/discovery"
	healthsvc "etcdisc/internal/core/service/health"
	namespacesvc "etcdisc/internal/core/service/namespace"
	registrysvc "etcdisc/internal/core/service/registry"
	"etcdisc/internal/infra/clock"
	infraetcd "etcdisc/internal/infra/etcd"
)

func TestRealEtcdPhase1Flows(t *testing.T) {
	if os.Getenv("ETCDISC_RUN_INTEGRATION") != "1" {
		t.Skip("set ETCDISC_RUN_INTEGRATION=1 to run real etcd integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg := appconfig.Default()
	client, err := infraetcd.NewClient(cfg)
	require.NoError(t, err)
	defer client.Close()
	store := infraetcd.NewStore(client)
	clk := clock.RealClock{}
	namespaceService := namespacesvc.NewService(store, clk)
	registryService := registrysvc.NewService(store, namespaceService, healthsvc.NewManager(), clk)
	discoveryService := discoverysvc.NewService(store, registryService)
	configService := configsvc.NewService(store, namespaceService, clk)

	_, _ = client.Delete(ctx, "/etcdisc", clientv3.WithPrefix())
	_, err = namespaceService.Create(ctx, namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)

	instance, err := registryService.Register(ctx, registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080}})
	require.NoError(t, err)
	require.NotZero(t, instance.Revision)
	items, err := discoveryService.Snapshot(ctx, discoverysvc.SnapshotInput{Namespace: "prod-core", Service: "payment-api"})
	require.NoError(t, err)
	require.Len(t, items, 1)

	_, err = configService.Put(ctx, configsvc.PutInput{Item: model.ConfigItem{Scope: model.ConfigScopeService, Namespace: "prod-core", Service: "payment-api", Key: "timeout.request", Value: "1000", ValueType: model.ConfigValueDuration}})
	require.NoError(t, err)
	effective, err := configService.Resolve(ctx, configsvc.ResolveInput{Namespace: "prod-core", Service: "payment-api"})
	require.NoError(t, err)
	require.Equal(t, "1000", effective["timeout.request"].Value)
}
