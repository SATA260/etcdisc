// client_test.go verifies Consumer SDK cache updates and picker strategies.
package consumer

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	etcdiscv1 "etcdisc/api/proto/etcdisc/v1"
	grpcserver "etcdisc/internal/api/grpcserver"
	discoveryhttp "etcdisc/internal/api/http/discovery"
	"etcdisc/internal/core/model"
	a2asvc "etcdisc/internal/core/service/a2a"
	configsvc "etcdisc/internal/core/service/config"
	discoverysvc "etcdisc/internal/core/service/discovery"
	healthsvc "etcdisc/internal/core/service/health"
	namespacesvc "etcdisc/internal/core/service/namespace"
	registrysvc "etcdisc/internal/core/service/registry"
	publicapi "etcdisc/pkg/api"
	"etcdisc/test/testkit"
)

type fakeTransport struct {
	items  []model.Instance
	stream chan model.WatchEvent
}

func (f *fakeTransport) Snapshot(context.Context, discoverysvc.SnapshotInput) ([]model.Instance, error) {
	return append([]model.Instance(nil), f.items...), nil
}

func (f *fakeTransport) Watch(context.Context, discoverysvc.WatchInput) (<-chan model.WatchEvent, error) {
	return f.stream, nil
}

func TestSyncApplyEventAndPickers(t *testing.T) {
	t.Parallel()

	transport := &fakeTransport{items: []model.Instance{{Namespace: "prod", Service: "pay", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080, Weight: 100, Status: model.InstanceStatusHealth}, {Namespace: "prod", Service: "pay", InstanceID: "node-2", Address: "127.0.0.2", Port: 8080, Weight: 200, Status: model.InstanceStatusHealth}}, stream: make(chan model.WatchEvent, 2)}
	client := NewClient(transport)
	require.NoError(t, client.Sync(context.Background(), discoverysvc.SnapshotInput{Namespace: "prod", Service: "pay"}))
	require.Len(t, client.Instances("prod", "pay"), 2)

	picked, err := client.Pick("prod", "pay", "round_robin", "")
	require.NoError(t, err)
	require.Equal(t, "node-1", picked.InstanceID)
	picked, err = client.Pick("prod", "pay", "round_robin", "")
	require.NoError(t, err)
	require.Equal(t, "node-2", picked.InstanceID)
	picked, err = client.Pick("prod", "pay", "consistent_hash", "order-1")
	require.NoError(t, err)
	require.NotEmpty(t, picked.InstanceID)
	picked, err = client.Pick("prod", "pay", "weighted_random", "")
	require.NoError(t, err)
	require.NotEmpty(t, picked.InstanceID)
	_, err = client.Pick("prod", "pay", "unknown", "")
	require.Error(t, err)

	body, err := json.Marshal(model.Instance{Namespace: "prod", Service: "pay", InstanceID: "node-3", Address: "127.0.0.3", Port: 8080, Weight: 50, Status: model.InstanceStatusHealth})
	require.NoError(t, err)
	require.NoError(t, client.ApplyEvent("prod", "pay", model.WatchEvent{Type: model.WatchEventPut, Value: body}))
	require.Len(t, client.Instances("prod", "pay"), 3)
}

func TestWatchLoopAndReset(t *testing.T) {
	t.Parallel()

	transport := &fakeTransport{items: []model.Instance{{Namespace: "prod", Service: "pay", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080, Weight: 100, Status: model.InstanceStatusHealth}}, stream: make(chan model.WatchEvent, 2)}
	client := NewClient(transport)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = client.Watch(ctx, discoverysvc.WatchInput{Namespace: "prod", Service: "pay"}) }()
	body, err := json.Marshal(model.Instance{Namespace: "prod", Service: "pay", InstanceID: "node-2", Address: "127.0.0.2", Port: 8080, Weight: 100, Status: model.InstanceStatusHealth})
	require.NoError(t, err)
	transport.stream <- model.WatchEvent{Type: model.WatchEventPut, Value: body}
	require.Eventually(t, func() bool {
		return len(client.Instances("prod", "pay")) == 1 && client.Instances("prod", "pay")[0].InstanceID == "node-2"
	}, 2*time.Second, 20*time.Millisecond)
	transport.stream <- model.WatchEvent{Type: model.WatchEventReset}
	require.Eventually(t, func() bool {
		return len(client.Instances("prod", "pay")) == 1 && client.Instances("prod", "pay")[0].InstanceID == "node-1"
	}, 2*time.Second, 20*time.Millisecond)
	close(transport.stream)
}

func TestHTTPTransport(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	clk := namespacesvc.NewFixedClock(time.Now())
	nsService := namespacesvc.NewService(store, clk)
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod"})
	require.NoError(t, err)
	registry := registrysvc.NewService(store, nsService, healthsvc.NewManager(), clk)
	_, err = registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod", Service: "pay", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080}})
	require.NoError(t, err)
	api := discoveryhttp.API{Service: discoverysvc.NewService(store, registry)}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/discovery/instances", api.Snapshot)
	mux.HandleFunc("/v1/discovery/watch", api.Watch)
	server := httptest.NewServer(mux)
	defer server.Close()

	transport := HTTPTransport{BaseURL: server.URL, Client: server.Client()}
	items, err := transport.Snapshot(context.Background(), discoverysvc.SnapshotInput{Namespace: "prod", Service: "pay"})
	require.NoError(t, err)
	require.Len(t, items, 1)
	watchCtx, cancel := context.WithCancel(context.Background())
	watchCh, err := transport.Watch(watchCtx, publicapi.DiscoveryWatchInput{Namespace: "prod", Service: "pay"})
	require.NoError(t, err)
	time.Sleep(20 * time.Millisecond)
	_, err = registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod", Service: "pay", InstanceID: "node-2", Address: "127.0.0.2", Port: 8080}})
	require.NoError(t, err)
	event := <-watchCh
	require.Equal(t, publicapi.WatchEventPut, event.Type)
	cancel()
}

func TestGRPCTransport(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	clk := namespacesvc.NewFixedClock(time.Now())
	nsService := namespacesvc.NewService(store, clk)
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod"})
	require.NoError(t, err)
	registry := registrysvc.NewService(store, nsService, healthsvc.NewManager(), clk)
	_, err = registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod", Service: "pay", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080}})
	require.NoError(t, err)
	listener := bufconn.Listen(1024 * 1024)
	server := grpcserver.New(grpcserver.Services{Registry: registry, Discovery: discoverysvc.NewService(store, registry), Config: configsvc.NewService(store, nsService, clk), A2A: a2asvc.NewService(store, nsService, registry, clk)})
	defer server.Stop()
	go func() { _ = server.Serve(listener) }()
	conn, err := grpc.DialContext(context.Background(), "bufnet", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithDefaultCallOptions(grpc.CallContentSubtype(etcdiscv1.JSONCodecName())))
	require.NoError(t, err)
	defer conn.Close()

	transport := NewGRPCTransport(conn)
	items, err := transport.Snapshot(context.Background(), discoverysvc.SnapshotInput{Namespace: "prod", Service: "pay"})
	require.NoError(t, err)
	require.Len(t, items, 1)
}

func TestGRPCTransportWatch(t *testing.T) {
	t.Parallel()

	transport := GRPCTransport{Client: fakeDiscoveryClient{events: []*publicapi.WatchEvent{{Type: publicapi.WatchEventPut, Key: "node-1"}}}}
	watchCh, err := transport.Watch(context.Background(), publicapi.DiscoveryWatchInput{Namespace: "prod", Service: "pay"})
	require.NoError(t, err)
	event := <-watchCh
	require.Equal(t, publicapi.WatchEventPut, event.Type)
	require.Equal(t, "node-1", event.Key)
}

type fakeDiscoveryClient struct{ events []*publicapi.WatchEvent }

func (f fakeDiscoveryClient) Discover(context.Context, *etcdiscv1.DiscoverRequest, ...grpc.CallOption) (*etcdiscv1.DiscoverResponse, error) {
	return &etcdiscv1.DiscoverResponse{Items: []publicapi.Instance{{InstanceID: "node-1"}}}, nil
}

func (f fakeDiscoveryClient) WatchInstances(context.Context, *etcdiscv1.WatchInstancesRequest, ...grpc.CallOption) (etcdiscv1.DiscoveryService_WatchInstancesClient, error) {
	return &fakeDiscoveryWatchClient{events: f.events}, nil
}

type fakeDiscoveryWatchClient struct {
	grpc.ClientStream
	events []*publicapi.WatchEvent
}

func (f *fakeDiscoveryWatchClient) Header() (metadata.MD, error) { return metadata.MD{}, nil }
func (f *fakeDiscoveryWatchClient) Trailer() metadata.MD         { return metadata.MD{} }
func (f *fakeDiscoveryWatchClient) CloseSend() error             { return nil }
func (f *fakeDiscoveryWatchClient) Context() context.Context     { return context.Background() }
func (f *fakeDiscoveryWatchClient) SendMsg(any) error            { return nil }
func (f *fakeDiscoveryWatchClient) RecvMsg(any) error            { return nil }
func (f *fakeDiscoveryWatchClient) Recv() (*publicapi.WatchEvent, error) {
	if len(f.events) == 0 {
		return nil, io.EOF
	}
	event := f.events[0]
	f.events = f.events[1:]
	return event, nil
}
