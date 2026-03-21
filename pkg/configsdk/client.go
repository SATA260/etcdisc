// client.go exposes the Config SDK with effective-config caching and watch-based refresh behavior.
package configsdk

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	etcdiscv1 "etcdisc/api/proto/etcdisc/v1"
	publicapi "etcdisc/pkg/api"
	"google.golang.org/grpc"
)

// Transport defines the operations required by the Config SDK.
type Transport interface {
	Effective(ctx context.Context, namespaceName, serviceName string) (map[string]publicapi.EffectiveConfigItem, error)
	Watch(ctx context.Context, input publicapi.ConfigWatchInput) (<-chan publicapi.WatchEvent, error)
}

// Client caches effective config per namespace and service.
type Client struct {
	transport Transport
	mu        sync.RWMutex
	cache     map[string]map[string]publicapi.EffectiveConfigItem
}

// NewClient creates a Config SDK client.
func NewClient(transport Transport) *Client {
	return &Client{transport: transport, cache: map[string]map[string]publicapi.EffectiveConfigItem{}}
}

// Sync loads the effective config snapshot.
func (c *Client) Sync(ctx context.Context, namespaceName, serviceName string) error {
	effective, err := c.transport.Effective(ctx, namespaceName, serviceName)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[cacheKey(namespaceName, serviceName)] = effective
	return nil
}

// Watch applies config watch events until the context ends or the stream closes.
func (c *Client) Watch(ctx context.Context, namespaceName, serviceName string, revision int64) error {
	stream, err := c.transport.Watch(ctx, publicapi.ConfigWatchInput{Namespace: namespaceName, Service: serviceName, Revision: revision})
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-stream:
			if !ok {
				return nil
			}
			if err := c.ApplyEvent(ctx, namespaceName, serviceName, event); err != nil {
				return err
			}
		}
	}
}

// ApplyEvent updates the effective config cache using one watch event.
func (c *Client) ApplyEvent(ctx context.Context, namespaceName, serviceName string, event publicapi.WatchEvent) error {
	if event.Type == publicapi.WatchEventReset || event.Type == publicapi.WatchEventDelete {
		return c.Sync(ctx, namespaceName, serviceName)
	}
	var item publicapi.ConfigItem
	if err := json.Unmarshal(event.Value, &item); err != nil {
		return err
	}
	key := cacheKey(namespaceName, serviceName)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache[key] == nil {
		c.cache[key] = map[string]publicapi.EffectiveConfigItem{}
	}
	c.cache[key][item.Key] = publicapi.EffectiveConfigItem{Key: item.Key, Value: item.Value, ValueType: item.ValueType, Scope: item.Scope}
	return nil
}

// EffectiveConfig returns a copy of the cached effective config.
func (c *Client) EffectiveConfig(namespaceName, serviceName string) map[string]publicapi.EffectiveConfigItem {
	c.mu.RLock()
	defer c.mu.RUnlock()
	items := c.cache[cacheKey(namespaceName, serviceName)]
	copyItems := make(map[string]publicapi.EffectiveConfigItem, len(items))
	for key, item := range items {
		copyItems[key] = item
	}
	return copyItems
}

// HTTPTransport implements effective config and watch calls over HTTP JSON and SSE.
type HTTPTransport struct {
	BaseURL string
	Client  *http.Client
}

// Effective loads the effective config snapshot.
func (t HTTPTransport) Effective(ctx context.Context, namespaceName, serviceName string) (map[string]publicapi.EffectiveConfigItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/v1/config/effective?namespace=%s&service=%s", strings.TrimRight(t.BaseURL, "/"), namespaceName, serviceName), nil)
	if err != nil {
		return nil, err
	}
	resp, err := t.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload struct {
		EffectiveConfig map[string]publicapi.EffectiveConfigItem `json:"effectiveConfig"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.EffectiveConfig, nil
}

// Watch opens an SSE stream for config changes.
func (t HTTPTransport) Watch(ctx context.Context, input publicapi.ConfigWatchInput) (<-chan publicapi.WatchEvent, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/v1/config/watch?namespace=%s&service=%s", strings.TrimRight(t.BaseURL, "/"), input.Namespace, input.Service), nil)
	if err != nil {
		return nil, err
	}
	resp, err := t.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	out := make(chan publicapi.WatchEvent, 8)
	go func() {
		defer resp.Body.Close()
		defer close(out)
		scanner := bufio.NewScanner(resp.Body)
		var currentType publicapi.WatchEventType
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				currentType = publicapi.WatchEventType(strings.TrimPrefix(line, "event: "))
				continue
			}
			if strings.HasPrefix(line, "data: ") {
				var event publicapi.WatchEvent
				if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event) == nil {
					if event.Type == "" {
						event.Type = currentType
					}
					out <- event
				}
			}
		}
	}()
	return out, nil
}

func (t HTTPTransport) httpClient() *http.Client {
	if t.Client != nil {
		return t.Client
	}
	return http.DefaultClient
}

// GRPCTransport implements config snapshot and watch over the phase 1 gRPC APIs.
type GRPCTransport struct {
	Client etcdiscv1.ConfigServiceClient
}

// NewGRPCTransport builds a Config SDK gRPC transport.
func NewGRPCTransport(conn grpc.ClientConnInterface) GRPCTransport {
	return GRPCTransport{Client: etcdiscv1.NewConfigServiceClient(conn)}
}

// Effective loads effective config through gRPC.
func (t GRPCTransport) Effective(ctx context.Context, namespaceName, serviceName string) (map[string]publicapi.EffectiveConfigItem, error) {
	resp, err := t.Client.GetEffectiveConfig(ctx, &etcdiscv1.ConfigGetRequest{Namespace: namespaceName, Service: serviceName}, grpc.CallContentSubtype(etcdiscv1.JSONCodecName()))
	if err != nil {
		return nil, err
	}
	return resp.EffectiveConfig, nil
}

// Watch opens a gRPC server-streaming config watch.
func (t GRPCTransport) Watch(ctx context.Context, input publicapi.ConfigWatchInput) (<-chan publicapi.WatchEvent, error) {
	stream, err := t.Client.WatchConfigs(ctx, &etcdiscv1.WatchConfigsRequest{Input: input}, grpc.CallContentSubtype(etcdiscv1.JSONCodecName()))
	if err != nil {
		return nil, err
	}
	out := make(chan publicapi.WatchEvent, 8)
	go func() {
		defer close(out)
		for {
			event, err := stream.Recv()
			if err != nil {
				return
			}
			out <- *event
		}
	}()
	return out, nil
}

func cacheKey(namespaceName, serviceName string) string {
	return namespaceName + "/" + serviceName
}
