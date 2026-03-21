// client_test.go verifies Config SDK snapshot caching and watch event refresh behavior.
package configsdk

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
	confighttp "etcdisc/internal/api/http/config"
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
	effective map[string]model.EffectiveConfigItem
	stream    chan model.WatchEvent
}

func (f *fakeTransport) Effective(context.Context, string, string) (map[string]model.EffectiveConfigItem, error) {
	copyItems := make(map[string]model.EffectiveConfigItem, len(f.effective))
	for key, item := range f.effective {
		copyItems[key] = item
	}
	return copyItems, nil
}

func (f *fakeTransport) Watch(context.Context, configsvc.WatchInput) (<-chan model.WatchEvent, error) {
	return f.stream, nil
}

func TestConfigClient(t *testing.T) {
	t.Parallel()

	transport := &fakeTransport{effective: map[string]model.EffectiveConfigItem{"timeout.request": {Key: "timeout.request", Value: "1000", ValueType: model.ConfigValueDuration, Scope: model.ConfigScopeService}}, stream: make(chan model.WatchEvent, 2)}
	client := NewClient(transport)
	require.NoError(t, client.Sync(context.Background(), "prod", "pay"))
	require.Equal(t, "1000", client.EffectiveConfig("prod", "pay")["timeout.request"].Value)

	body, err := json.Marshal(model.ConfigItem{Scope: model.ConfigScopeService, Key: "timeout.request", Value: "2000", ValueType: model.ConfigValueDuration})
	require.NoError(t, err)
	require.NoError(t, client.ApplyEvent(context.Background(), "prod", "pay", model.WatchEvent{Type: model.WatchEventPut, Value: body}))
	require.Equal(t, "2000", client.EffectiveConfig("prod", "pay")["timeout.request"].Value)

	transport.effective["timeout.request"] = model.EffectiveConfigItem{Key: "timeout.request", Value: "1500", ValueType: model.ConfigValueDuration, Scope: model.ConfigScopeNamespace}
	require.NoError(t, client.ApplyEvent(context.Background(), "prod", "pay", model.WatchEvent{Type: model.WatchEventDelete}))
	require.Equal(t, "1500", client.EffectiveConfig("prod", "pay")["timeout.request"].Value)
}

func TestWatchLoop(t *testing.T) {
	t.Parallel()

	transport := &fakeTransport{effective: map[string]model.EffectiveConfigItem{"timeout.request": {Key: "timeout.request", Value: "1000", ValueType: model.ConfigValueDuration, Scope: model.ConfigScopeService}}, stream: make(chan model.WatchEvent, 2)}
	client := NewClient(transport)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = client.Watch(ctx, "prod", "pay", 0) }()
	body, err := json.Marshal(model.ConfigItem{Scope: model.ConfigScopeService, Key: "timeout.request", Value: "2000", ValueType: model.ConfigValueDuration})
	require.NoError(t, err)
	transport.stream <- model.WatchEvent{Type: model.WatchEventPut, Value: body}
	require.Eventually(t, func() bool { return client.EffectiveConfig("prod", "pay")["timeout.request"].Value == "2000" }, 2*time.Second, 20*time.Millisecond)
	close(transport.stream)
}

func TestHTTPTransport(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	clk := namespacesvc.NewFixedClock(time.Now())
	nsService := namespacesvc.NewService(store, clk)
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod"})
	require.NoError(t, err)
	service := configsvc.NewService(store, nsService, clk)
	item, err := service.Put(context.Background(), configsvc.PutInput{Item: model.ConfigItem{Scope: model.ConfigScopeService, Namespace: "prod", Service: "pay", Key: "timeout.request", Value: "1000", ValueType: model.ConfigValueDuration}})
	require.NoError(t, err)
	api := confighttp.API{Service: service}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/config/effective", api.Effective)
	mux.HandleFunc("/v1/config/watch", api.Watch)
	server := httptest.NewServer(mux)
	defer server.Close()

	transport := HTTPTransport{BaseURL: server.URL, Client: server.Client()}
	effective, err := transport.Effective(context.Background(), "prod", "pay")
	require.NoError(t, err)
	require.Equal(t, "1000", effective["timeout.request"].Value)
	watchCtx, cancel := context.WithCancel(context.Background())
	watchCh, err := transport.Watch(watchCtx, publicapi.ConfigWatchInput{Namespace: "prod", Service: "pay"})
	require.NoError(t, err)
	time.Sleep(20 * time.Millisecond)
	_, err = service.Put(context.Background(), configsvc.PutInput{Item: model.ConfigItem{Scope: model.ConfigScopeService, Namespace: "prod", Service: "pay", Key: "timeout.request", Value: "1200", ValueType: model.ConfigValueDuration}, ExpectedRevision: item.Revision})
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
	configService := configsvc.NewService(store, nsService, clk)
	item, err := configService.Put(context.Background(), configsvc.PutInput{Item: model.ConfigItem{Scope: model.ConfigScopeService, Namespace: "prod", Service: "pay", Key: "timeout.request", Value: "1000", ValueType: model.ConfigValueDuration}})
	require.NoError(t, err)
	listener := bufconn.Listen(1024 * 1024)
	server := grpcserver.New(grpcserver.Services{Registry: registry, Discovery: discoverysvc.NewService(store, registry), Config: configService, A2A: a2asvc.NewService(store, nsService, registry, clk)})
	defer server.Stop()
	go func() { _ = server.Serve(listener) }()
	conn, err := grpc.DialContext(context.Background(), "bufnet", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithDefaultCallOptions(grpc.CallContentSubtype(etcdiscv1.JSONCodecName())))
	require.NoError(t, err)
	defer conn.Close()

	transport := NewGRPCTransport(conn)
	effective, err := transport.Effective(context.Background(), "prod", "pay")
	require.NoError(t, err)
	require.Equal(t, "1000", effective["timeout.request"].Value)
	_ = item
}

func TestGRPCTransportWatch(t *testing.T) {
	t.Parallel()

	transport := GRPCTransport{Client: fakeConfigClient{events: []*publicapi.WatchEvent{{Type: publicapi.WatchEventPut, Key: "timeout.request"}}}}
	watchCh, err := transport.Watch(context.Background(), publicapi.ConfigWatchInput{Namespace: "prod", Service: "pay"})
	require.NoError(t, err)
	event := <-watchCh
	require.Equal(t, publicapi.WatchEventPut, event.Type)
	require.Equal(t, "timeout.request", event.Key)
}

type fakeConfigClient struct{ events []*publicapi.WatchEvent }

func (f fakeConfigClient) GetEffectiveConfig(context.Context, *etcdiscv1.ConfigGetRequest, ...grpc.CallOption) (*etcdiscv1.ConfigGetResponse, error) {
	return &etcdiscv1.ConfigGetResponse{EffectiveConfig: map[string]publicapi.EffectiveConfigItem{"timeout.request": {Key: "timeout.request", Value: "1000"}}}, nil
}

func (f fakeConfigClient) PutConfig(context.Context, *etcdiscv1.ConfigPutRequest, ...grpc.CallOption) (*etcdiscv1.ConfigPutResponse, error) {
	return &etcdiscv1.ConfigPutResponse{Item: publicapi.ConfigItem{Key: "timeout.request"}}, nil
}

func (f fakeConfigClient) DeleteConfig(context.Context, *etcdiscv1.ConfigDeleteRequest, ...grpc.CallOption) (*etcdiscv1.ConfigDeleteResponse, error) {
	return &etcdiscv1.ConfigDeleteResponse{Status: "deleted"}, nil
}

func (f fakeConfigClient) WatchConfigs(context.Context, *etcdiscv1.WatchConfigsRequest, ...grpc.CallOption) (etcdiscv1.ConfigService_WatchConfigsClient, error) {
	return &fakeConfigWatchClient{events: f.events}, nil
}

type fakeConfigWatchClient struct {
	grpc.ClientStream
	events []*publicapi.WatchEvent
}

func (f *fakeConfigWatchClient) Header() (metadata.MD, error) { return metadata.MD{}, nil }
func (f *fakeConfigWatchClient) Trailer() metadata.MD         { return metadata.MD{} }
func (f *fakeConfigWatchClient) CloseSend() error             { return nil }
func (f *fakeConfigWatchClient) Context() context.Context     { return context.Background() }
func (f *fakeConfigWatchClient) SendMsg(any) error            { return nil }
func (f *fakeConfigWatchClient) RecvMsg(any) error            { return nil }
func (f *fakeConfigWatchClient) Recv() (*publicapi.WatchEvent, error) {
	if len(f.events) == 0 {
		return nil, io.EOF
	}
	event := f.events[0]
	f.events = f.events[1:]
	return event, nil
}
