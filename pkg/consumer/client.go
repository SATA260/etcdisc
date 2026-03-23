// client.go exposes the Consumer SDK with snapshot, watch, cache, and picker support.
package consumer

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math/rand"
	"net/http"
	"strings"
	"sync"

	etcdiscv1 "etcdisc/api/proto/etcdisc/v1"
	publicapi "etcdisc/pkg/api"
	"google.golang.org/grpc"
)

// Transport defines the operations required by the Consumer SDK.
type Transport interface {
	Snapshot(ctx context.Context, input publicapi.DiscoverySnapshotInput) ([]publicapi.Instance, error)
	Watch(ctx context.Context, input publicapi.DiscoveryWatchInput) (<-chan publicapi.WatchEvent, error)
}

// Client keeps a local cache backed by discovery snapshot and watch streams.
type Client struct {
	transport  Transport
	mu         sync.RWMutex
	cache      map[string][]publicapi.Instance
	rrCounters map[string]int
	rand       *rand.Rand
}

// NewClient creates a Consumer SDK client.
func NewClient(transport Transport) *Client {
	return &Client{transport: transport, cache: map[string][]publicapi.Instance{}, rrCounters: map[string]int{}, rand: rand.New(rand.NewSource(1))}
}

// Sync loads a fresh discovery snapshot into the local cache.
func (c *Client) Sync(ctx context.Context, input publicapi.DiscoverySnapshotInput) error {
	items, err := c.transport.Snapshot(ctx, input)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[cacheKey(input.Namespace, input.Service)] = append([]publicapi.Instance(nil), items...)
	return nil
}

// Watch applies future watch events until the context ends or the stream fails.
func (c *Client) Watch(ctx context.Context, input publicapi.DiscoveryWatchInput) error {
	stream, err := c.transport.Watch(ctx, input)
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
			if err := c.ApplyEvent(input.Namespace, input.Service, event); err != nil {
				return err
			}
		}
	}
}

// ApplyEvent updates the local cache using one discovery watch event.
func (c *Client) ApplyEvent(namespaceName, serviceName string, event publicapi.WatchEvent) error {
	if event.Type == publicapi.WatchEventReset {
		return c.Sync(context.Background(), publicapi.DiscoverySnapshotInput{Namespace: namespaceName, Service: serviceName})
	}
	var instance publicapi.Instance
	if err := json.Unmarshal(event.Value, &instance); err != nil {
		return err
	}
	key := cacheKey(namespaceName, serviceName)
	c.mu.Lock()
	defer c.mu.Unlock()
	items := append([]publicapi.Instance(nil), c.cache[key]...)
	items = removeInstance(items, instance.InstanceID)
	if event.Type == publicapi.WatchEventPut {
		items = append(items, instance)
	}
	c.cache[key] = items
	return nil
}

// Instances returns the cached instances for one namespace and service.
func (c *Client) Instances(namespaceName, serviceName string) []publicapi.Instance {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]publicapi.Instance(nil), c.cache[cacheKey(namespaceName, serviceName)]...)
}

// Pick returns one cached instance using the requested strategy.
func (c *Client) Pick(namespaceName, serviceName, strategy, hashKey string) (publicapi.Instance, error) {
	items := c.Instances(namespaceName, serviceName)
	if len(items) == 0 {
		return publicapi.Instance{}, fmt.Errorf("no instances available")
	}
	switch strategy {
	case "round_robin":
		return c.pickRoundRobin(namespaceName, serviceName, items), nil
	case "random":
		return items[c.rand.Intn(len(items))], nil
	case "weighted_random":
		return c.pickWeightedRandom(items), nil
	case "consistent_hash":
		return pickConsistentHash(items, hashKey), nil
	default:
		return publicapi.Instance{}, fmt.Errorf("unknown strategy %q", strategy)
	}
}

func (c *Client) pickRoundRobin(namespaceName, serviceName string, items []publicapi.Instance) publicapi.Instance {
	key := cacheKey(namespaceName, serviceName)
	c.mu.Lock()
	defer c.mu.Unlock()
	index := c.rrCounters[key] % len(items)
	c.rrCounters[key] = c.rrCounters[key] + 1
	return items[index]
}

func (c *Client) pickWeightedRandom(items []publicapi.Instance) publicapi.Instance {
	total := 0
	for _, item := range items {
		total += item.Weight
	}
	pick := c.rand.Intn(total)
	running := 0
	for _, item := range items {
		running += item.Weight
		if pick < running {
			return item
		}
	}
	return items[len(items)-1]
}

func pickConsistentHash(items []publicapi.Instance, hashKey string) publicapi.Instance {
	best := items[0]
	bestScore := uint64(0)
	for index, item := range items {
		h := fnv.New64a()
		_, _ = h.Write([]byte(hashKey))
		_, _ = h.Write([]byte(item.Endpoint()))
		score := h.Sum64()
		if index == 0 || score < bestScore {
			best = item
			bestScore = score
		}
	}
	return best
}

func removeInstance(items []publicapi.Instance, instanceID string) []publicapi.Instance {
	filtered := items[:0]
	for _, item := range items {
		if item.InstanceID != instanceID {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func cacheKey(namespaceName, serviceName string) string {
	return namespaceName + "/" + serviceName
}

// HTTPTransport implements discovery over HTTP and SSE.
type HTTPTransport struct {
	BaseURL string
	Client  *http.Client
}

// Snapshot performs an HTTP snapshot query.
func (t HTTPTransport) Snapshot(ctx context.Context, input publicapi.DiscoverySnapshotInput) ([]publicapi.Instance, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/v1/discovery/instances?namespace=%s&service=%s", strings.TrimRight(t.BaseURL, "/"), input.Namespace, input.Service), nil)
	if err != nil {
		return nil, err
	}
	resp, err := t.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload struct {
		Items []publicapi.Instance `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

// Watch performs an HTTP SSE watch request.
func (t HTTPTransport) Watch(ctx context.Context, input publicapi.DiscoveryWatchInput) (<-chan publicapi.WatchEvent, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/v1/discovery/watch?namespace=%s&service=%s", strings.TrimRight(t.BaseURL, "/"), input.Namespace, input.Service), nil)
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

// GRPCTransport implements discovery using the phase 1 gRPC APIs.
type GRPCTransport struct {
	Client etcdiscv1.DiscoveryServiceClient
}

// NewGRPCTransport builds a Consumer SDK gRPC transport.
func NewGRPCTransport(conn grpc.ClientConnInterface) GRPCTransport {
	return GRPCTransport{Client: etcdiscv1.NewDiscoveryServiceClient(conn)}
}

// Snapshot performs a gRPC discovery snapshot query.
func (t GRPCTransport) Snapshot(ctx context.Context, input publicapi.DiscoverySnapshotInput) ([]publicapi.Instance, error) {
	resp, err := t.Client.Discover(ctx, etcdiscv1.NewDiscoverRequestFromPublic(input))
	if err != nil {
		return nil, err
	}
	items := make([]publicapi.Instance, 0, len(resp.GetItems()))
	for _, item := range resp.GetItems() {
		items = append(items, item.ToPublic())
	}
	return items, nil
}

// Watch opens a gRPC server-streaming discovery watch.
func (t GRPCTransport) Watch(ctx context.Context, input publicapi.DiscoveryWatchInput) (<-chan publicapi.WatchEvent, error) {
	stream, err := t.Client.WatchInstances(ctx, etcdiscv1.NewWatchInstancesRequestFromPublic(input))
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
			out <- event.ToPublic()
		}
	}()
	return out, nil
}
