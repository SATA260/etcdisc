// service.go implements HTTP, gRPC, and TCP probe checks used by probe mode instances.
package probe

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"etcdisc/internal/core/model"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
)

var probeResults = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "etcdisc_probe_results_total",
	Help: "Count of probe results by protocol and success state.",
}, []string{"protocol", "success"})

// DialContextFunc defines the network dial function used by TCP probes.
type DialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)

// Service performs one-off probe checks for runtime instances.
type Service struct {
	httpClient *http.Client
	dial       DialContextFunc
	grpcDial   func(ctx context.Context, target string, opts ...grpc.DialOption) (*grpc.ClientConn, error)
}

// Result captures the probe outcome and the checked target.
type Result struct {
	Success  bool
	Target   string
	Protocol string
	Error    string
}

// NewService creates a probe service with production defaults.
func NewService() *Service {
	return &Service{
		httpClient: &http.Client{Timeout: 2 * time.Second},
		dial:       (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
		grpcDial:   grpc.DialContext,
	}
}

// Probe runs one probe based on the instance healthCheckMode.
func (s *Service) Probe(ctx context.Context, instance model.Instance) Result {
	svc := s.ensureDefaults()
	switch instance.HealthCheckMode {
	case model.HealthCheckHTTPProbe:
		return svc.probeHTTP(ctx, instance)
	case model.HealthCheckGRPCProbe:
		return svc.probeGRPC(ctx, instance)
	case model.HealthCheckTCPProbe:
		return svc.probeTCP(ctx, instance)
	default:
		return Result{Success: false, Protocol: string(instance.HealthCheckMode), Error: "unsupported probe mode"}
	}
}

func (s *Service) ensureDefaults() *Service {
	if s == nil {
		return NewService()
	}
	if s.httpClient == nil {
		s.httpClient = &http.Client{Timeout: 2 * time.Second}
	}
	if s.dial == nil {
		s.dial = (&net.Dialer{Timeout: 2 * time.Second}).DialContext
	}
	if s.grpcDial == nil {
		s.grpcDial = grpc.DialContext
	}
	return s
}

func (s *Service) probeHTTP(ctx context.Context, instance model.Instance) Result {
	url := fmt.Sprintf("http://%s:%d%s", instance.ProbeConfig.Address, instance.ProbeConfig.Port, instance.ProbeConfig.Path)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		probeResults.WithLabelValues("http", "false").Inc()
		return Result{Protocol: "http", Target: url, Error: err.Error()}
	}
	_ = resp.Body.Close()
	success := resp.StatusCode >= 200 && resp.StatusCode < 300
	probeResults.WithLabelValues("http", fmt.Sprintf("%t", success)).Inc()
	result := Result{Success: success, Protocol: "http", Target: url}
	if !success {
		result.Error = resp.Status
	}
	return result
}

func (s *Service) probeGRPC(ctx context.Context, instance model.Instance) Result {
	target := fmt.Sprintf("%s:%d", instance.ProbeConfig.Address, instance.ProbeConfig.Port)
	conn, err := s.grpcDial(ctx, target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		probeResults.WithLabelValues("grpc", "false").Inc()
		return Result{Protocol: "grpc", Target: target, Error: err.Error()}
	}
	defer conn.Close()
	client := grpc_health_v1.NewHealthClient(conn)
	_, err = client.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		probeResults.WithLabelValues("grpc", "false").Inc()
		return Result{Protocol: "grpc", Target: target, Error: err.Error()}
	}
	probeResults.WithLabelValues("grpc", "true").Inc()
	return Result{Success: true, Protocol: "grpc", Target: target}
}

func (s *Service) probeTCP(ctx context.Context, instance model.Instance) Result {
	target := fmt.Sprintf("%s:%d", instance.ProbeConfig.Address, instance.ProbeConfig.Port)
	conn, err := s.dial(ctx, "tcp", target)
	if err != nil {
		probeResults.WithLabelValues("tcp", "false").Inc()
		return Result{Protocol: "tcp", Target: target, Error: err.Error()}
	}
	_ = conn.Close()
	probeResults.WithLabelValues("tcp", "true").Inc()
	return Result{Success: true, Protocol: "tcp", Target: target}
}
