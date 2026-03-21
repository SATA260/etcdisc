// client.go exposes the Provider SDK used by business services for register, heartbeat, and graceful shutdown.
package provider

import (
	"context"
	"time"

	publicapi "etcdisc/pkg/api"
)

// Transport defines the operations required by the Provider SDK.
type Transport interface {
	Register(ctx context.Context, input publicapi.RegisterInput) (publicapi.Instance, error)
	Heartbeat(ctx context.Context, input publicapi.HeartbeatInput) (publicapi.Instance, error)
	Deregister(ctx context.Context, input publicapi.DeregisterInput) error
}

// Client provides the phase 1 Provider SDK surface.
type Client struct {
	transport Transport
}

// NewClient creates a Provider SDK client from the given transport.
func NewClient(transport Transport) *Client {
	return &Client{transport: transport}
}

// Register registers a runtime instance.
func (c *Client) Register(ctx context.Context, input publicapi.RegisterInput) (publicapi.Instance, error) {
	return c.transport.Register(ctx, input)
}

// Heartbeat sends one heartbeat for a heartbeat-mode instance.
func (c *Client) Heartbeat(ctx context.Context, input publicapi.HeartbeatInput) (publicapi.Instance, error) {
	return c.transport.Heartbeat(ctx, input)
}

// Deregister gracefully removes a runtime instance.
func (c *Client) Deregister(ctx context.Context, input publicapi.DeregisterInput) error {
	return c.transport.Deregister(ctx, input)
}

// RunHeartbeatLoop sends periodic heartbeats until the context is cancelled or one heartbeat fails.
func (c *Client) RunHeartbeatLoop(ctx context.Context, input publicapi.HeartbeatInput, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := c.transport.Heartbeat(ctx, input); err != nil {
				return err
			}
		}
	}
}
