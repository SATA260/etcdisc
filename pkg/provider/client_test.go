// client_test.go verifies Provider SDK register, heartbeat loop, and deregister behaviors.
package provider

import (
	"context"
	"errors"
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
	registryhttp "etcdisc/internal/api/http/registry"
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
	registerCalls   int
	heartbeatCalls  int
	deregisterCalls int
	heartbeatErr    error
}

func (f *fakeTransport) Register(_ context.Context, input registrysvc.RegisterInput) (model.Instance, error) {
	f.registerCalls++
	return input.Instance, nil
}

func (f *fakeTransport) Heartbeat(_ context.Context, _ registrysvc.HeartbeatInput) (model.Instance, error) {
	f.heartbeatCalls++
	return model.Instance{}, f.heartbeatErr
}

func (f *fakeTransport) Deregister(_ context.Context, _ registrysvc.DeregisterInput) error {
	f.deregisterCalls++
	return nil
}

func TestClient(t *testing.T) {
	t.Parallel()

	transport := &fakeTransport{}
	client := NewClient(transport)
	_, err := client.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod", Service: "pay"}})
	require.NoError(t, err)
	require.Equal(t, 1, transport.registerCalls)

	require.NoError(t, client.Deregister(context.Background(), registrysvc.DeregisterInput{Namespace: "prod", Service: "pay", InstanceID: "node-1"}))
	require.Equal(t, 1, transport.deregisterCalls)
}

func TestRunHeartbeatLoopStopsOnError(t *testing.T) {
	t.Parallel()

	transport := &fakeTransport{heartbeatErr: errors.New("boom")}
	client := NewClient(transport)
	err := client.RunHeartbeatLoop(context.Background(), registrysvc.HeartbeatInput{Namespace: "prod", Service: "pay", InstanceID: "node-1"}, time.Millisecond)
	require.Error(t, err)
	require.Equal(t, 1, transport.heartbeatCalls)
}

func TestHTTPTransport(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	clk := namespacesvc.NewFixedClock(time.Now())
	nsService := namespacesvc.NewService(store, clk)
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod"})
	require.NoError(t, err)
	service := registrysvc.NewService(store, nsService, healthsvc.NewManager(), clk)
	api := registryhttp.API{Service: service}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/registry/register", api.Register)
	mux.HandleFunc("/v1/registry/heartbeat", api.Heartbeat)
	mux.HandleFunc("/v1/registry/deregister", api.Deregister)
	server := httptest.NewServer(mux)
	defer server.Close()

	transport := HTTPTransport{BaseURL: server.URL, Client: server.Client()}
	instance, err := transport.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod", Service: "pay", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080}})
	require.NoError(t, err)
	require.Equal(t, "node-1", instance.InstanceID)
	_, err = transport.Heartbeat(context.Background(), registrysvc.HeartbeatInput{Namespace: "prod", Service: "pay", InstanceID: "node-1"})
	require.NoError(t, err)
	require.NoError(t, transport.Deregister(context.Background(), registrysvc.DeregisterInput{Namespace: "prod", Service: "pay", InstanceID: "node-1"}))
}

func TestGRPCTransport(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	clk := namespacesvc.NewFixedClock(time.Now())
	nsService := namespacesvc.NewService(store, clk)
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod"})
	require.NoError(t, err)
	registry := registrysvc.NewService(store, nsService, healthsvc.NewManager(), clk)
	listener := bufconn.Listen(1024 * 1024)
	server := grpcserver.New(grpcserver.Services{Registry: registry, Discovery: discoverysvc.NewService(store, registry), Config: configsvc.NewService(store, nsService, clk), A2A: a2asvc.NewService(store, nsService, registry, clk)})
	defer server.Stop()
	go func() { _ = server.Serve(listener) }()
	conn, err := grpc.DialContext(context.Background(), "bufnet", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithDefaultCallOptions(grpc.CallContentSubtype(etcdiscv1.JSONCodecName())))
	require.NoError(t, err)
	defer conn.Close()

	transport := NewGRPCTransport(conn)
	instance, err := transport.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod", Service: "pay", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080}})
	require.NoError(t, err)
	require.Equal(t, "node-1", instance.InstanceID)
}
