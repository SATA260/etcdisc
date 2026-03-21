// client_test.go verifies Consumer SDK cache updates and picker strategies.
package consumer

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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

	body, err := json.Marshal(model.Instance{Namespace: "prod", Service: "pay", InstanceID: "node-3", Address: "127.0.0.3", Port: 8080, Weight: 50, Status: model.InstanceStatusHealth})
	require.NoError(t, err)
	require.NoError(t, client.ApplyEvent("prod", "pay", model.WatchEvent{Type: model.WatchEventPut, Value: body}))
	require.Len(t, client.Instances("prod", "pay"), 3)
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
	server := httptest.NewServer(mux)
	defer server.Close()

	transport := HTTPTransport{BaseURL: server.URL, Client: server.Client()}
	items, err := transport.Snapshot(context.Background(), discoverysvc.SnapshotInput{Namespace: "prod", Service: "pay"})
	require.NoError(t, err)
	require.Len(t, items, 1)
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
