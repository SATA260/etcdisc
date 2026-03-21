// services_test.go verifies the manual gRPC descriptors, clients, codecs, and handler shims.
package etcdiscv1

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	publicapi "etcdisc/pkg/api"
)

func TestJSONCodec(t *testing.T) {
	t.Parallel()

	codec := jsonCodec{}
	require.Equal(t, "json", JSONCodecName())
	require.Equal(t, "json", codec.Name())
	body, err := codec.Marshal(RegisterRequest{Input: publicapi.RegisterInput{Instance: publicapi.Instance{InstanceID: "node-1"}}})
	require.NoError(t, err)
	var decoded RegisterRequest
	require.NoError(t, codec.Unmarshal(body, &decoded))
	require.Equal(t, "node-1", decoded.Input.Instance.InstanceID)
}

func TestClientWrappers(t *testing.T) {
	t.Parallel()

	stream := &fakeClientStream{recvQueue: []any{
		&publicapi.WatchEvent{Type: publicapi.WatchEventPut, Key: "instance"},
		&publicapi.WatchEvent{Type: publicapi.WatchEventPut, Key: "config"},
	}}
	conn := &fakeClientConn{stream: stream}

	registryClient := NewRegistryServiceClient(conn)
	registerResp, err := registryClient.Register(context.Background(), &RegisterRequest{Input: publicapi.RegisterInput{Instance: publicapi.Instance{InstanceID: "node-1"}}})
	require.NoError(t, err)
	require.Equal(t, "node-1", registerResp.Instance.InstanceID)
	_, err = registryClient.Heartbeat(context.Background(), &HeartbeatRequest{Input: publicapi.HeartbeatInput{InstanceID: "node-1"}})
	require.NoError(t, err)
	_, err = registryClient.UpdateInstance(context.Background(), &UpdateInstanceRequest{Input: publicapi.UpdateInstanceInput{InstanceID: "node-1"}})
	require.NoError(t, err)
	_, err = registryClient.Deregister(context.Background(), &DeregisterRequest{Input: publicapi.DeregisterInput{InstanceID: "node-1"}})
	require.NoError(t, err)

	discoveryClient := NewDiscoveryServiceClient(conn)
	discoverResp, err := discoveryClient.Discover(context.Background(), &DiscoverRequest{Input: publicapi.DiscoverySnapshotInput{Namespace: "prod"}})
	require.NoError(t, err)
	require.Len(t, discoverResp.Items, 1)
	watchStream, err := discoveryClient.WatchInstances(context.Background(), &WatchInstancesRequest{Input: publicapi.DiscoveryWatchInput{Namespace: "prod"}})
	require.NoError(t, err)
	event, err := watchStream.Recv()
	require.NoError(t, err)
	require.Equal(t, "instance", event.Key)

	configClient := NewConfigServiceClient(conn)
	_, err = configClient.GetEffectiveConfig(context.Background(), &ConfigGetRequest{Namespace: "prod", Service: "svc"})
	require.NoError(t, err)
	_, err = configClient.PutConfig(context.Background(), &ConfigPutRequest{Input: publicapi.ConfigPutInput{Item: publicapi.ConfigItem{Key: "timeout.request"}}})
	require.NoError(t, err)
	_, err = configClient.DeleteConfig(context.Background(), &ConfigDeleteRequest{Input: publicapi.ConfigDeleteInput{Key: "timeout.request"}})
	require.NoError(t, err)
	configWatch, err := configClient.WatchConfigs(context.Background(), &WatchConfigsRequest{Input: publicapi.ConfigWatchInput{Namespace: "prod", Service: "svc"}})
	require.NoError(t, err)
	event, err = configWatch.Recv()
	require.NoError(t, err)
	require.Equal(t, "config", event.Key)

	a2aClient := NewA2AServiceClient(conn)
	_, err = a2aClient.UpsertAgentCard(context.Background(), &UpsertAgentCardRequest{Input: publicapi.AgentCardPutInput{Card: publicapi.AgentCard{AgentID: "agent-1"}}})
	require.NoError(t, err)
	capResp, err := a2aClient.DiscoverCapabilities(context.Background(), &DiscoverCapabilitiesRequest{Input: publicapi.CapabilityDiscoverInput{Capability: "tool.search"}})
	require.NoError(t, err)
	require.Len(t, capResp.Items, 1)
}

func TestServerRegistrationAndHandlers(t *testing.T) {
	t.Parallel()

	registrar := &fakeRegistrar{}
	RegisterRegistryServiceServer(registrar, testRegistryServer{})
	RegisterDiscoveryServiceServer(registrar, testDiscoveryServer{})
	RegisterConfigServiceServer(registrar, testConfigServer{})
	RegisterA2AServiceServer(registrar, testA2AServer{})
	require.Len(t, registrar.services, 4)

	_, err := _RegistryService_Register_Handler(testRegistryServer{}, context.Background(), decodeFrom(RegisterRequest{Input: publicapi.RegisterInput{Instance: publicapi.Instance{InstanceID: "node-1"}}}), nil)
	require.NoError(t, err)
	_, err = _RegistryService_Register_Handler(testRegistryServer{}, context.Background(), decodeFrom(RegisterRequest{Input: publicapi.RegisterInput{Instance: publicapi.Instance{InstanceID: "node-1"}}}), unaryInterceptorPass)
	require.NoError(t, err)
	_, err = _RegistryService_Heartbeat_Handler(testRegistryServer{}, context.Background(), decodeFrom(HeartbeatRequest{Input: publicapi.HeartbeatInput{InstanceID: "node-1"}}), unaryInterceptorPass)
	require.NoError(t, err)
	_, err = _RegistryService_Heartbeat_Handler(testRegistryServer{}, context.Background(), decodeFrom(HeartbeatRequest{Input: publicapi.HeartbeatInput{InstanceID: "node-1"}}), nil)
	require.NoError(t, err)
	_, err = _RegistryService_UpdateInstance_Handler(testRegistryServer{}, context.Background(), decodeFrom(UpdateInstanceRequest{Input: publicapi.UpdateInstanceInput{InstanceID: "node-1"}}), nil)
	require.NoError(t, err)
	_, err = _RegistryService_UpdateInstance_Handler(testRegistryServer{}, context.Background(), decodeFrom(UpdateInstanceRequest{Input: publicapi.UpdateInstanceInput{InstanceID: "node-1"}}), unaryInterceptorPass)
	require.NoError(t, err)
	_, err = _RegistryService_Deregister_Handler(testRegistryServer{}, context.Background(), decodeFrom(DeregisterRequest{Input: publicapi.DeregisterInput{InstanceID: "node-1"}}), unaryInterceptorPass)
	require.NoError(t, err)
	_, err = _RegistryService_Deregister_Handler(testRegistryServer{}, context.Background(), decodeFrom(DeregisterRequest{Input: publicapi.DeregisterInput{InstanceID: "node-1"}}), nil)
	require.NoError(t, err)

	_, err = _DiscoveryService_Discover_Handler(testDiscoveryServer{}, context.Background(), decodeFrom(DiscoverRequest{Input: publicapi.DiscoverySnapshotInput{Namespace: "prod"}}), nil)
	require.NoError(t, err)
	_, err = _DiscoveryService_Discover_Handler(testDiscoveryServer{}, context.Background(), decodeFrom(DiscoverRequest{Input: publicapi.DiscoverySnapshotInput{Namespace: "prod"}}), unaryInterceptorPass)
	require.NoError(t, err)
	discoveryStream := &fakeServerStream{recvQueue: []any{&WatchInstancesRequest{Input: publicapi.DiscoveryWatchInput{Namespace: "prod"}}}}
	require.NoError(t, _DiscoveryService_WatchInstances_Handler(testDiscoveryServer{}, discoveryStream))
	require.NotEmpty(t, discoveryStream.sent)

	_, err = _ConfigService_GetEffectiveConfig_Handler(testConfigServer{}, context.Background(), decodeFrom(ConfigGetRequest{Namespace: "prod", Service: "svc"}), unaryInterceptorPass)
	require.NoError(t, err)
	_, err = _ConfigService_GetEffectiveConfig_Handler(testConfigServer{}, context.Background(), decodeFrom(ConfigGetRequest{Namespace: "prod", Service: "svc"}), nil)
	require.NoError(t, err)
	_, err = _ConfigService_PutConfig_Handler(testConfigServer{}, context.Background(), decodeFrom(ConfigPutRequest{Input: publicapi.ConfigPutInput{Item: publicapi.ConfigItem{Key: "timeout.request"}}}), nil)
	require.NoError(t, err)
	_, err = _ConfigService_PutConfig_Handler(testConfigServer{}, context.Background(), decodeFrom(ConfigPutRequest{Input: publicapi.ConfigPutInput{Item: publicapi.ConfigItem{Key: "timeout.request"}}}), unaryInterceptorPass)
	require.NoError(t, err)
	_, err = _ConfigService_DeleteConfig_Handler(testConfigServer{}, context.Background(), decodeFrom(ConfigDeleteRequest{Input: publicapi.ConfigDeleteInput{Key: "timeout.request"}}), unaryInterceptorPass)
	require.NoError(t, err)
	_, err = _ConfigService_DeleteConfig_Handler(testConfigServer{}, context.Background(), decodeFrom(ConfigDeleteRequest{Input: publicapi.ConfigDeleteInput{Key: "timeout.request"}}), nil)
	require.NoError(t, err)
	configStream := &fakeServerStream{recvQueue: []any{&WatchConfigsRequest{Input: publicapi.ConfigWatchInput{Namespace: "prod", Service: "svc"}}}}
	require.NoError(t, _ConfigService_WatchConfigs_Handler(testConfigServer{}, configStream))
	require.NotEmpty(t, configStream.sent)

	_, err = _A2AService_UpsertAgentCard_Handler(testA2AServer{}, context.Background(), decodeFrom(UpsertAgentCardRequest{Input: publicapi.AgentCardPutInput{Card: publicapi.AgentCard{AgentID: "agent-1"}}}), nil)
	require.NoError(t, err)
	_, err = _A2AService_UpsertAgentCard_Handler(testA2AServer{}, context.Background(), decodeFrom(UpsertAgentCardRequest{Input: publicapi.AgentCardPutInput{Card: publicapi.AgentCard{AgentID: "agent-1"}}}), unaryInterceptorPass)
	require.NoError(t, err)
	_, err = _A2AService_DiscoverCapabilities_Handler(testA2AServer{}, context.Background(), decodeFrom(DiscoverCapabilitiesRequest{Input: publicapi.CapabilityDiscoverInput{Capability: "tool.search"}}), unaryInterceptorPass)
	require.NoError(t, err)
	_, err = _A2AService_DiscoverCapabilities_Handler(testA2AServer{}, context.Background(), decodeFrom(DiscoverCapabilitiesRequest{Input: publicapi.CapabilityDiscoverInput{Capability: "tool.search"}}), nil)
	require.NoError(t, err)
}

func TestUnimplementedServers(t *testing.T) {
	t.Parallel()

	_, err := (UnimplementedRegistryServiceServer{}).Register(context.Background(), &RegisterRequest{})
	require.Equal(t, codes.Unimplemented, status.Code(err))
	_, err = (UnimplementedRegistryServiceServer{}).Heartbeat(context.Background(), &HeartbeatRequest{})
	require.Equal(t, codes.Unimplemented, status.Code(err))
	_, err = (UnimplementedRegistryServiceServer{}).UpdateInstance(context.Background(), &UpdateInstanceRequest{})
	require.Equal(t, codes.Unimplemented, status.Code(err))
	_, err = (UnimplementedRegistryServiceServer{}).Deregister(context.Background(), &DeregisterRequest{})
	require.Equal(t, codes.Unimplemented, status.Code(err))

	_, err = (UnimplementedDiscoveryServiceServer{}).Discover(context.Background(), &DiscoverRequest{})
	require.Equal(t, codes.Unimplemented, status.Code(err))
	err = (UnimplementedDiscoveryServiceServer{}).WatchInstances(&WatchInstancesRequest{}, &fakeDiscoveryWatchServer{})
	require.Equal(t, codes.Unimplemented, status.Code(err))

	_, err = (UnimplementedConfigServiceServer{}).GetEffectiveConfig(context.Background(), &ConfigGetRequest{})
	require.Equal(t, codes.Unimplemented, status.Code(err))
	_, err = (UnimplementedConfigServiceServer{}).PutConfig(context.Background(), &ConfigPutRequest{})
	require.Equal(t, codes.Unimplemented, status.Code(err))
	_, err = (UnimplementedConfigServiceServer{}).DeleteConfig(context.Background(), &ConfigDeleteRequest{})
	require.Equal(t, codes.Unimplemented, status.Code(err))
	err = (UnimplementedConfigServiceServer{}).WatchConfigs(&WatchConfigsRequest{}, &fakeConfigWatchServer{})
	require.Equal(t, codes.Unimplemented, status.Code(err))

	_, err = (UnimplementedA2AServiceServer{}).UpsertAgentCard(context.Background(), &UpsertAgentCardRequest{})
	require.Equal(t, codes.Unimplemented, status.Code(err))
	_, err = (UnimplementedA2AServiceServer{}).DiscoverCapabilities(context.Background(), &DiscoverCapabilitiesRequest{})
	require.Equal(t, codes.Unimplemented, status.Code(err))
}

type fakeRegistrar struct{ services []string }

func (f *fakeRegistrar) RegisterService(desc *grpc.ServiceDesc, _ any) {
	f.services = append(f.services, desc.ServiceName)
}

type fakeClientConn struct{ stream *fakeClientStream }

func (f *fakeClientConn) Invoke(_ context.Context, method string, _ any, reply any, _ ...grpc.CallOption) error {
	switch r := reply.(type) {
	case *RegisterResponse:
		r.Instance = publicapi.Instance{InstanceID: "node-1"}
	case *HeartbeatResponse:
		r.Instance = publicapi.Instance{InstanceID: "node-1"}
	case *UpdateInstanceResponse:
		r.Instance = publicapi.Instance{InstanceID: "node-1"}
	case *DeregisterResponse:
		r.Status = "deleted"
	case *DiscoverResponse:
		r.Items = []publicapi.Instance{{InstanceID: "node-1"}}
	case *ConfigGetResponse:
		r.EffectiveConfig = map[string]publicapi.EffectiveConfigItem{"timeout.request": {Key: "timeout.request", Value: "1000"}}
	case *ConfigPutResponse:
		r.Item = publicapi.ConfigItem{Key: "timeout.request"}
	case *ConfigDeleteResponse:
		r.Status = "deleted"
	case *UpsertAgentCardResponse:
		r.Card = publicapi.AgentCard{AgentID: "agent-1"}
	case *DiscoverCapabilitiesResponse:
		r.Items = []publicapi.A2ADiscoveryResult{{AgentID: "agent-1"}}
	default:
		return status.Errorf(codes.Internal, "unexpected reply type for %s", method)
	}
	return nil
}

func (f *fakeClientConn) NewStream(_ context.Context, _ *grpc.StreamDesc, method string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
	f.stream.method = method
	return f.stream, nil
}

type fakeClientStream struct {
	grpc.ClientStream
	method    string
	sent      []any
	recvQueue []any
}

func (f *fakeClientStream) Header() (metadata.MD, error) { return metadata.MD{}, nil }
func (f *fakeClientStream) Trailer() metadata.MD         { return metadata.MD{} }
func (f *fakeClientStream) CloseSend() error             { return nil }
func (f *fakeClientStream) Context() context.Context     { return context.Background() }
func (f *fakeClientStream) SendMsg(m any) error          { f.sent = append(f.sent, m); return nil }
func (f *fakeClientStream) RecvMsg(m any) error {
	if len(f.recvQueue) == 0 {
		return io.EOF
	}
	next := f.recvQueue[0]
	f.recvQueue = f.recvQueue[1:]
	body, err := json.Marshal(next)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, m)
}

type fakeServerStream struct {
	grpc.ServerStream
	recvQueue []any
	sent      []any
}

func (f *fakeServerStream) SetHeader(metadata.MD) error  { return nil }
func (f *fakeServerStream) SendHeader(metadata.MD) error { return nil }
func (f *fakeServerStream) SetTrailer(metadata.MD)       {}
func (f *fakeServerStream) Context() context.Context     { return context.Background() }
func (f *fakeServerStream) SendMsg(m any) error          { f.sent = append(f.sent, m); return nil }
func (f *fakeServerStream) RecvMsg(m any) error {
	if len(f.recvQueue) == 0 {
		return io.EOF
	}
	next := f.recvQueue[0]
	f.recvQueue = f.recvQueue[1:]
	body, err := json.Marshal(next)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, m)
}

type testRegistryServer struct{}

func (testRegistryServer) Register(context.Context, *RegisterRequest) (*RegisterResponse, error) {
	return &RegisterResponse{Instance: publicapi.Instance{InstanceID: "node-1"}}, nil
}
func (testRegistryServer) Heartbeat(context.Context, *HeartbeatRequest) (*HeartbeatResponse, error) {
	return &HeartbeatResponse{Instance: publicapi.Instance{InstanceID: "node-1"}}, nil
}
func (testRegistryServer) UpdateInstance(context.Context, *UpdateInstanceRequest) (*UpdateInstanceResponse, error) {
	return &UpdateInstanceResponse{Instance: publicapi.Instance{InstanceID: "node-1"}}, nil
}
func (testRegistryServer) Deregister(context.Context, *DeregisterRequest) (*DeregisterResponse, error) {
	return &DeregisterResponse{Status: "deleted"}, nil
}

type testDiscoveryServer struct{}

func (testDiscoveryServer) Discover(context.Context, *DiscoverRequest) (*DiscoverResponse, error) {
	return &DiscoverResponse{Items: []publicapi.Instance{{InstanceID: "node-1"}}}, nil
}
func (testDiscoveryServer) WatchInstances(_ *WatchInstancesRequest, stream DiscoveryService_WatchInstancesServer) error {
	return stream.Send(&publicapi.WatchEvent{Type: publicapi.WatchEventPut, Key: "instance"})
}

type testConfigServer struct{}

func (testConfigServer) GetEffectiveConfig(context.Context, *ConfigGetRequest) (*ConfigGetResponse, error) {
	return &ConfigGetResponse{EffectiveConfig: map[string]publicapi.EffectiveConfigItem{"timeout.request": {Key: "timeout.request", Value: "1000"}}}, nil
}
func (testConfigServer) PutConfig(context.Context, *ConfigPutRequest) (*ConfigPutResponse, error) {
	return &ConfigPutResponse{Item: publicapi.ConfigItem{Key: "timeout.request"}}, nil
}
func (testConfigServer) DeleteConfig(context.Context, *ConfigDeleteRequest) (*ConfigDeleteResponse, error) {
	return &ConfigDeleteResponse{Status: "deleted"}, nil
}
func (testConfigServer) WatchConfigs(_ *WatchConfigsRequest, stream ConfigService_WatchConfigsServer) error {
	return stream.Send(&publicapi.WatchEvent{Type: publicapi.WatchEventPut, Key: "config"})
}

type testA2AServer struct{}

func (testA2AServer) UpsertAgentCard(context.Context, *UpsertAgentCardRequest) (*UpsertAgentCardResponse, error) {
	return &UpsertAgentCardResponse{Card: publicapi.AgentCard{AgentID: "agent-1"}}, nil
}
func (testA2AServer) DiscoverCapabilities(context.Context, *DiscoverCapabilitiesRequest) (*DiscoverCapabilitiesResponse, error) {
	return &DiscoverCapabilitiesResponse{Items: []publicapi.A2ADiscoveryResult{{AgentID: "agent-1"}}}, nil
}

type fakeDiscoveryWatchServer struct{ fakeServerStream }

func (f *fakeDiscoveryWatchServer) Send(event *publicapi.WatchEvent) error { return f.SendMsg(event) }

type fakeConfigWatchServer struct{ fakeServerStream }

func (f *fakeConfigWatchServer) Send(event *publicapi.WatchEvent) error { return f.SendMsg(event) }

func decodeFrom(value any) func(any) error {
	return func(target any) error {
		body, err := json.Marshal(value)
		if err != nil {
			return err
		}
		return json.Unmarshal(body, target)
	}
}

func unaryInterceptorPass(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	return handler(ctx, req)
}
