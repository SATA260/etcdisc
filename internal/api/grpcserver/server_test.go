// server_test.go verifies the gRPC access layer reuses the shared phase 1 services.
package grpcserver

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
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

	updated, err := registryClient.Heartbeat(ctx, &etcdiscv1.HeartbeatRequest{Input: registrysvc.HeartbeatInput{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-1"}})
	require.NoError(t, err)
	require.Equal(t, "node-1", updated.Instance.InstanceID)
	weight := 200
	updatedInstance, err := registryClient.UpdateInstance(ctx, &etcdiscv1.UpdateInstanceRequest{Input: registrysvc.UpdateInput{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-1", Weight: &weight, ExpectedRevision: updated.Instance.Revision}})
	require.NoError(t, err)
	require.Equal(t, 200, updatedInstance.Instance.Weight)

	putResp, err := configClient.PutConfig(ctx, &etcdiscv1.ConfigPutRequest{Input: configsvc.PutInput{Item: model.ConfigItem{Scope: model.ConfigScopeService, Namespace: "prod-core", Service: "payment-api", Key: "retry.count", Value: "3", ValueType: model.ConfigValueInt}}})
	require.NoError(t, err)

	deleteResp, err := configClient.DeleteConfig(ctx, &etcdiscv1.ConfigDeleteRequest{Input: configsvc.DeleteInput{Scope: model.ConfigScopeService, Namespace: "prod-core", Service: "payment-api", Key: "retry.count", ExpectedRevision: putResp.Item.Revision}})
	require.NoError(t, err)
	require.Equal(t, "deleted", deleteResp.Status)

	_, err = registryClient.Register(ctx, &etcdiscv1.RegisterRequest{Input: registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-2", Address: "127.0.0.2", Port: 8080}}})
	require.NoError(t, err)
	deregisterResp, err := registryClient.Deregister(ctx, &etcdiscv1.DeregisterRequest{Input: registrysvc.DeregisterInput{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-2"}})
	require.NoError(t, err)
	require.Equal(t, "deleted", deregisterResp.Status)
}

func TestServerStreamingMethodsDirectly(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	clk := namespacesvc.NewFixedClock(time.Now())
	nsService := namespacesvc.NewService(store, clk)
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	registry := registrysvc.NewService(store, nsService, healthsvc.NewManager(), clk)
	configService := configsvc.NewService(store, nsService, clk)
	srv := &server{services: Services{Registry: registry, Discovery: discoverysvc.NewService(store, registry), Config: configService, A2A: a2asvc.NewService(store, nsService, registry, clk)}}

	watchCtx, cancel := context.WithCancel(context.Background())
	instanceStream := &fakeDiscoveryStream{ctx: watchCtx}
	go func() {
		time.Sleep(20 * time.Millisecond)
		_, _ = registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080}})
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	require.NoError(t, srv.WatchInstances(&etcdiscv1.WatchInstancesRequest{Input: discoverysvc.WatchInput{Namespace: "prod-core", Service: "payment-api"}}, instanceStream))
	require.NotEmpty(t, instanceStream.events)

	watchCtx, cancel = context.WithCancel(context.Background())
	configStream := &fakeConfigStream{ctx: watchCtx}
	go func() {
		time.Sleep(20 * time.Millisecond)
		_, _ = configService.Put(context.Background(), configsvc.PutInput{Item: model.ConfigItem{Scope: model.ConfigScopeService, Namespace: "prod-core", Service: "payment-api", Key: "timeout.request", Value: "1000", ValueType: model.ConfigValueDuration}})
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	require.NoError(t, srv.WatchConfigs(&etcdiscv1.WatchConfigsRequest{Input: configsvc.WatchInput{Namespace: "prod-core", Service: "payment-api"}}, configStream))
	require.NotEmpty(t, configStream.events)
}

func TestServerErrorMapping(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	clk := namespacesvc.NewFixedClock(time.Now())
	nsService := namespacesvc.NewService(store, clk)
	srv := &server{services: Services{Registry: registrysvc.NewService(store, nsService, healthsvc.NewManager(), clk), Discovery: discoverysvc.NewService(store, registrysvc.NewService(store, nsService, healthsvc.NewManager(), clk)), Config: configsvc.NewService(store, nsService, clk), A2A: a2asvc.NewService(store, nsService, registrysvc.NewService(store, nsService, healthsvc.NewManager(), clk), clk)}}

	_, err := srv.Register(context.Background(), &etcdiscv1.RegisterRequest{Input: registrysvc.RegisterInput{Instance: model.Instance{Namespace: "bad", Service: "pay", Address: "127.0.0.1", Port: 8080}}})
	require.Error(t, err)
	_, err = srv.Heartbeat(context.Background(), &etcdiscv1.HeartbeatRequest{Input: registrysvc.HeartbeatInput{Namespace: "prod-core", Service: "pay", InstanceID: "missing"}})
	require.Error(t, err)
	_, err = srv.UpdateInstance(context.Background(), &etcdiscv1.UpdateInstanceRequest{Input: registrysvc.UpdateInput{Namespace: "prod-core", Service: "pay", InstanceID: "missing"}})
	require.Error(t, err)
	_, err = srv.Deregister(context.Background(), &etcdiscv1.DeregisterRequest{Input: registrysvc.DeregisterInput{Namespace: "prod-core", Service: "pay", InstanceID: "missing"}})
	require.Error(t, err)
	_, err = srv.Discover(context.Background(), &etcdiscv1.DiscoverRequest{Input: discoverysvc.SnapshotInput{Namespace: "prod-core", Service: "pay"}})
	require.Error(t, err)
	_, err = srv.GetEffectiveConfig(context.Background(), &etcdiscv1.ConfigGetRequest{Namespace: "prod-core", Service: "pay"})
	require.Error(t, err)
	_, err = srv.PutConfig(context.Background(), &etcdiscv1.ConfigPutRequest{Input: configsvc.PutInput{Item: model.ConfigItem{Scope: model.ConfigScopeService, Namespace: "prod-core", Service: "pay", Key: "timeout.request", Value: "1000", ValueType: model.ConfigValueDuration}}})
	require.Error(t, err)
	_, err = srv.DeleteConfig(context.Background(), &etcdiscv1.ConfigDeleteRequest{Input: configsvc.DeleteInput{Scope: model.ConfigScopeService, Namespace: "prod-core", Service: "pay", Key: "timeout.request", ExpectedRevision: 1}})
	require.Error(t, err)
	_, err = srv.UpsertAgentCard(context.Background(), &etcdiscv1.UpsertAgentCardRequest{Input: a2asvc.PutInput{Card: model.AgentCard{Namespace: "prod-core", AgentID: "agent-1", Service: "agent-api", Capabilities: []string{"tool.search"}, Protocols: []string{"grpc"}, AuthMode: model.AuthModeStaticToken}}})
	require.Error(t, err)
	_, err = srv.DiscoverCapabilities(context.Background(), &etcdiscv1.DiscoverCapabilitiesRequest{Input: a2asvc.DiscoverInput{Namespace: "prod-core", Capability: "tool.search"}})
	require.Error(t, err)
}

type fakeDiscoveryStream struct {
	grpc.ServerStream
	ctx    context.Context
	events []model.WatchEvent
}

func (f *fakeDiscoveryStream) Context() context.Context { return f.ctx }
func (f *fakeDiscoveryStream) Send(event *model.WatchEvent) error {
	f.events = append(f.events, *event)
	return nil
}
func (f *fakeDiscoveryStream) SetHeader(metadata.MD) error  { return nil }
func (f *fakeDiscoveryStream) SendHeader(metadata.MD) error { return nil }
func (f *fakeDiscoveryStream) SetTrailer(metadata.MD)       {}
func (f *fakeDiscoveryStream) SendMsg(any) error            { return nil }
func (f *fakeDiscoveryStream) RecvMsg(any) error            { return io.EOF }

type fakeConfigStream struct {
	grpc.ServerStream
	ctx    context.Context
	events []model.WatchEvent
}

func (f *fakeConfigStream) Context() context.Context { return f.ctx }
func (f *fakeConfigStream) Send(event *model.WatchEvent) error {
	f.events = append(f.events, *event)
	return nil
}
func (f *fakeConfigStream) SetHeader(metadata.MD) error  { return nil }
func (f *fakeConfigStream) SendHeader(metadata.MD) error { return nil }
func (f *fakeConfigStream) SetTrailer(metadata.MD)       {}
func (f *fakeConfigStream) SendMsg(any) error            { return nil }
func (f *fakeConfigStream) RecvMsg(any) error            { return io.EOF }
