// server_test.go verifies the gRPC access layer reuses the shared phase 1 services.
package grpcserver

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	etcdiscv1 "etcdisc/api/proto/etcdisc/v1"
	"etcdisc/internal/core/model"
	a2asvc "etcdisc/internal/core/service/a2a"
	configsvc "etcdisc/internal/core/service/config"
	discoverysvc "etcdisc/internal/core/service/discovery"
	healthsvc "etcdisc/internal/core/service/health"
	namespacesvc "etcdisc/internal/core/service/namespace"
	registrysvc "etcdisc/internal/core/service/registry"
	"etcdisc/test/testkit"
)

func TestGRPCServer(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	clk := namespacesvc.NewFixedClock(time.Now())
	nsService := namespacesvc.NewService(store, clk)
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	registry := registrysvc.NewService(store, nsService, healthsvc.NewManager(), clk)
	discovery := discoverysvc.NewService(store, registry)
	configService := configsvc.NewService(store, nsService, clk)
	a2aService := a2asvc.NewService(store, nsService, registry, clk)

	listener := bufconn.Listen(1024 * 1024)
	grpcServer := New(Services{Registry: registry, Discovery: discovery, Config: configService, A2A: a2aService})
	defer grpcServer.Stop()
	go func() { _ = grpcServer.Serve(listener) }()

	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithDefaultCallOptions(grpc.CallContentSubtype("json")))
	require.NoError(t, err)
	defer conn.Close()

	registryClient := etcdiscv1.NewRegistryServiceClient(conn)
	registerResp, err := registryClient.Register(ctx, &etcdiscv1.RegisterRequest{Input: registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080}}})
	require.NoError(t, err)
	require.Equal(t, "node-1", registerResp.Instance.InstanceID)

	discoveryClient := etcdiscv1.NewDiscoveryServiceClient(conn)
	discoverResp, err := discoveryClient.Discover(ctx, &etcdiscv1.DiscoverRequest{Input: discoverysvc.SnapshotInput{Namespace: "prod-core", Service: "payment-api"}})
	require.NoError(t, err)
	require.Len(t, discoverResp.Items, 1)

	configClient := etcdiscv1.NewConfigServiceClient(conn)
	_, err = configClient.PutConfig(ctx, &etcdiscv1.ConfigPutRequest{Input: configsvc.PutInput{Item: model.ConfigItem{Scope: model.ConfigScopeService, Namespace: "prod-core", Service: "payment-api", Key: "timeout.request", Value: "1000", ValueType: model.ConfigValueDuration}}})
	require.NoError(t, err)
	configResp, err := configClient.GetEffectiveConfig(ctx, &etcdiscv1.ConfigGetRequest{Namespace: "prod-core", Service: "payment-api"})
	require.NoError(t, err)
	require.Equal(t, "1000", configResp.EffectiveConfig["timeout.request"].Value)

	_, err = registryClient.Register(ctx, &etcdiscv1.RegisterRequest{Input: registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "agent-api", AgentID: "agent-1", InstanceID: "inst-1", Address: "10.0.0.9", Port: 9090}}})
	require.NoError(t, err)
	a2aClient := etcdiscv1.NewA2AServiceClient(conn)
	_, err = a2aClient.UpsertAgentCard(ctx, &etcdiscv1.UpsertAgentCardRequest{Input: a2asvc.PutInput{Card: model.AgentCard{Namespace: "prod-core", AgentID: "agent-1", Service: "agent-api", Capabilities: []string{"tool.search"}, Protocols: []string{"grpc"}, AuthMode: model.AuthModeStaticToken}}})
	require.NoError(t, err)
	a2aResp, err := a2aClient.DiscoverCapabilities(ctx, &etcdiscv1.DiscoverCapabilitiesRequest{Input: a2asvc.DiscoverInput{Namespace: "prod-core", Capability: "tool.search"}})
	require.NoError(t, err)
	require.Len(t, a2aResp.Items, 1)
	require.Equal(t, "10.0.0.9", a2aResp.Items[0].Address)
}
