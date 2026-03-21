// service_test.go verifies HTTP, gRPC, and TCP probing behavior for probe mode instances.
package probe

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"

	"etcdisc/internal/core/model"
)

func TestProbeHTTP(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	host, port := splitHostPort(t, server.Listener.Addr().String())
	service := NewService()
	result := service.Probe(context.Background(), model.Instance{HealthCheckMode: model.HealthCheckHTTPProbe, ProbeConfig: model.ProbeConfig{Address: host, Port: port, Path: "/"}})
	require.True(t, result.Success)

	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer badServer.Close()
	host, port = splitHostPort(t, badServer.Listener.Addr().String())
	result = service.Probe(context.Background(), model.Instance{HealthCheckMode: model.HealthCheckHTTPProbe, ProbeConfig: model.ProbeConfig{Address: host, Port: port, Path: "/"}})
	require.False(t, result.Success)
}

func TestProbeTCP(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()
	host, port := splitHostPort(t, listener.Addr().String())
	service := NewService()
	result := service.Probe(context.Background(), model.Instance{HealthCheckMode: model.HealthCheckTCPProbe, ProbeConfig: model.ProbeConfig{Address: host, Port: port}})
	require.True(t, result.Success)
	_ = listener.Close()
	result = service.Probe(context.Background(), model.Instance{HealthCheckMode: model.HealthCheckTCPProbe, ProbeConfig: model.ProbeConfig{Address: host, Port: port}})
	require.False(t, result.Success)
}

func TestProbeGRPC(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	server := grpc.NewServer()
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(server, healthServer)
	go func() {
		_ = server.Serve(listener)
	}()
	defer server.Stop()
	host, port := splitHostPort(t, listener.Addr().String())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	service := NewService()
	result := service.Probe(ctx, model.Instance{HealthCheckMode: model.HealthCheckGRPCProbe, ProbeConfig: model.ProbeConfig{Address: host, Port: port, Path: "/etcdisc/grpc/health"}})
	require.True(t, result.Success)

	server.Stop()
	result = (*Service)(nil).Probe(context.Background(), model.Instance{HealthCheckMode: model.HealthCheckMode("custom"), ProbeConfig: model.ProbeConfig{Address: host, Port: port}})
	require.False(t, result.Success)
}

func splitHostPort(t *testing.T, address string) (string, int) {
	t.Helper()
	host, portText, err := net.SplitHostPort(address)
	require.NoError(t, err)
	port, err := net.LookupPort("tcp", portText)
	require.NoError(t, err)
	return host, port
}
