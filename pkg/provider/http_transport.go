// http_transport.go implements the Provider SDK transport over the phase 1 HTTP APIs.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	etcdiscv1 "etcdisc/api/proto/etcdisc/v1"
	publicapi "etcdisc/pkg/api"
	"google.golang.org/grpc"
)

// HTTPTransport calls provider APIs through HTTP JSON endpoints.
type HTTPTransport struct {
	BaseURL string
	Client  *http.Client
}

// Register performs HTTP registration.
func (t HTTPTransport) Register(ctx context.Context, input publicapi.RegisterInput) (publicapi.Instance, error) {
	var instance publicapi.Instance
	err := t.postJSON(ctx, "/v1/registry/register", input, &instance)
	return instance, err
}

// Heartbeat performs an HTTP heartbeat.
func (t HTTPTransport) Heartbeat(ctx context.Context, input publicapi.HeartbeatInput) (publicapi.Instance, error) {
	var instance publicapi.Instance
	err := t.postJSON(ctx, "/v1/registry/heartbeat", input, &instance)
	return instance, err
}

// Deregister performs an HTTP deregistration.
func (t HTTPTransport) Deregister(ctx context.Context, input publicapi.DeregisterInput) error {
	return t.postJSON(ctx, "/v1/registry/deregister", input, nil)
}

func (t HTTPTransport) postJSON(ctx context.Context, path string, input any, output any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(t.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("provider http request failed: %s", resp.Status)
	}
	if output == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(output)
}

func (t HTTPTransport) httpClient() *http.Client {
	if t.Client != nil {
		return t.Client
	}
	return http.DefaultClient
}

// GRPCTransport calls provider APIs through the phase 1 gRPC surface.
type GRPCTransport struct {
	Client etcdiscv1.RegistryServiceClient
}

// NewGRPCTransport builds a Provider SDK gRPC transport from a client connection.
func NewGRPCTransport(conn grpc.ClientConnInterface) GRPCTransport {
	return GRPCTransport{Client: etcdiscv1.NewRegistryServiceClient(conn)}
}

// Register performs gRPC registration.
func (t GRPCTransport) Register(ctx context.Context, input publicapi.RegisterInput) (publicapi.Instance, error) {
	resp, err := t.Client.Register(ctx, etcdiscv1.NewRegisterRequestFromPublic(input))
	if err != nil {
		return publicapi.Instance{}, err
	}
	return resp.GetInstance().ToPublic(), nil
}

// Heartbeat performs a gRPC heartbeat.
func (t GRPCTransport) Heartbeat(ctx context.Context, input publicapi.HeartbeatInput) (publicapi.Instance, error) {
	resp, err := t.Client.Heartbeat(ctx, etcdiscv1.NewHeartbeatRequestFromPublic(input))
	if err != nil {
		return publicapi.Instance{}, err
	}
	return resp.GetInstance().ToPublic(), nil
}

// Deregister performs a gRPC deregistration.
func (t GRPCTransport) Deregister(ctx context.Context, input publicapi.DeregisterInput) error {
	_, err := t.Client.Deregister(ctx, etcdiscv1.NewDeregisterRequestFromPublic(input))
	return err
}
