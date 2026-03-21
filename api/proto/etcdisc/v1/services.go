// services.go defines phase 1 gRPC request and response types plus manual service descriptors.
package etcdiscv1

import (
	"context"

	publicapi "etcdisc/pkg/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type RegisterRequest struct {
	Input publicapi.RegisterInput `json:"input"`
}
type RegisterResponse struct {
	Instance publicapi.Instance `json:"instance"`
}
type HeartbeatRequest struct {
	Input publicapi.HeartbeatInput `json:"input"`
}
type HeartbeatResponse struct {
	Instance publicapi.Instance `json:"instance"`
}
type UpdateInstanceRequest struct {
	Input publicapi.UpdateInstanceInput `json:"input"`
}
type UpdateInstanceResponse struct {
	Instance publicapi.Instance `json:"instance"`
}
type DeregisterRequest struct {
	Input publicapi.DeregisterInput `json:"input"`
}
type DeregisterResponse struct {
	Status string `json:"status"`
}

type DiscoverRequest struct {
	Input publicapi.DiscoverySnapshotInput `json:"input"`
}
type DiscoverResponse struct {
	Items []publicapi.Instance `json:"items"`
}
type WatchInstancesRequest struct {
	Input publicapi.DiscoveryWatchInput `json:"input"`
}

type ConfigGetRequest struct {
	Namespace string `json:"namespace"`
	Service   string `json:"service"`
}
type ConfigGetResponse struct {
	EffectiveConfig map[string]publicapi.EffectiveConfigItem `json:"effectiveConfig"`
}
type ConfigPutRequest struct {
	Input publicapi.ConfigPutInput `json:"input"`
}
type ConfigPutResponse struct {
	Item publicapi.ConfigItem `json:"item"`
}
type ConfigDeleteRequest struct {
	Input publicapi.ConfigDeleteInput `json:"input"`
}
type ConfigDeleteResponse struct {
	Status string `json:"status"`
}
type WatchConfigsRequest struct {
	Input publicapi.ConfigWatchInput `json:"input"`
}

type UpsertAgentCardRequest struct {
	Input publicapi.AgentCardPutInput `json:"input"`
}
type UpsertAgentCardResponse struct {
	Card publicapi.AgentCard `json:"card"`
}
type DiscoverCapabilitiesRequest struct {
	Input publicapi.CapabilityDiscoverInput `json:"input"`
}
type DiscoverCapabilitiesResponse struct {
	Items []publicapi.A2ADiscoveryResult `json:"items"`
}

type RegistryServiceClient interface {
	Register(ctx context.Context, in *RegisterRequest, opts ...grpc.CallOption) (*RegisterResponse, error)
	Heartbeat(ctx context.Context, in *HeartbeatRequest, opts ...grpc.CallOption) (*HeartbeatResponse, error)
	UpdateInstance(ctx context.Context, in *UpdateInstanceRequest, opts ...grpc.CallOption) (*UpdateInstanceResponse, error)
	Deregister(ctx context.Context, in *DeregisterRequest, opts ...grpc.CallOption) (*DeregisterResponse, error)
}

type registryServiceClient struct{ cc grpc.ClientConnInterface }

func NewRegistryServiceClient(cc grpc.ClientConnInterface) RegistryServiceClient {
	return &registryServiceClient{cc: cc}
}

func (c *registryServiceClient) Register(ctx context.Context, in *RegisterRequest, opts ...grpc.CallOption) (*RegisterResponse, error) {
	out := new(RegisterResponse)
	err := c.cc.Invoke(ctx, "/etcdisc.v1.RegistryService/Register", in, out, opts...)
	return out, err
}
func (c *registryServiceClient) Heartbeat(ctx context.Context, in *HeartbeatRequest, opts ...grpc.CallOption) (*HeartbeatResponse, error) {
	out := new(HeartbeatResponse)
	err := c.cc.Invoke(ctx, "/etcdisc.v1.RegistryService/Heartbeat", in, out, opts...)
	return out, err
}
func (c *registryServiceClient) UpdateInstance(ctx context.Context, in *UpdateInstanceRequest, opts ...grpc.CallOption) (*UpdateInstanceResponse, error) {
	out := new(UpdateInstanceResponse)
	err := c.cc.Invoke(ctx, "/etcdisc.v1.RegistryService/UpdateInstance", in, out, opts...)
	return out, err
}
func (c *registryServiceClient) Deregister(ctx context.Context, in *DeregisterRequest, opts ...grpc.CallOption) (*DeregisterResponse, error) {
	out := new(DeregisterResponse)
	err := c.cc.Invoke(ctx, "/etcdisc.v1.RegistryService/Deregister", in, out, opts...)
	return out, err
}

type RegistryServiceServer interface {
	Register(context.Context, *RegisterRequest) (*RegisterResponse, error)
	Heartbeat(context.Context, *HeartbeatRequest) (*HeartbeatResponse, error)
	UpdateInstance(context.Context, *UpdateInstanceRequest) (*UpdateInstanceResponse, error)
	Deregister(context.Context, *DeregisterRequest) (*DeregisterResponse, error)
}

type UnimplementedRegistryServiceServer struct{}

func (UnimplementedRegistryServiceServer) Register(context.Context, *RegisterRequest) (*RegisterResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method Register not implemented")
}
func (UnimplementedRegistryServiceServer) Heartbeat(context.Context, *HeartbeatRequest) (*HeartbeatResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method Heartbeat not implemented")
}
func (UnimplementedRegistryServiceServer) UpdateInstance(context.Context, *UpdateInstanceRequest) (*UpdateInstanceResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method UpdateInstance not implemented")
}
func (UnimplementedRegistryServiceServer) Deregister(context.Context, *DeregisterRequest) (*DeregisterResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method Deregister not implemented")
}

func RegisterRegistryServiceServer(s grpc.ServiceRegistrar, srv RegistryServiceServer) {
	s.RegisterService(&RegistryService_ServiceDesc, srv)
}

type DiscoveryServiceClient interface {
	Discover(ctx context.Context, in *DiscoverRequest, opts ...grpc.CallOption) (*DiscoverResponse, error)
	WatchInstances(ctx context.Context, in *WatchInstancesRequest, opts ...grpc.CallOption) (DiscoveryService_WatchInstancesClient, error)
}

type discoveryServiceClient struct{ cc grpc.ClientConnInterface }

func NewDiscoveryServiceClient(cc grpc.ClientConnInterface) DiscoveryServiceClient {
	return &discoveryServiceClient{cc: cc}
}

func (c *discoveryServiceClient) Discover(ctx context.Context, in *DiscoverRequest, opts ...grpc.CallOption) (*DiscoverResponse, error) {
	out := new(DiscoverResponse)
	err := c.cc.Invoke(ctx, "/etcdisc.v1.DiscoveryService/Discover", in, out, opts...)
	return out, err
}
func (c *discoveryServiceClient) WatchInstances(ctx context.Context, in *WatchInstancesRequest, opts ...grpc.CallOption) (DiscoveryService_WatchInstancesClient, error) {
	stream, err := c.cc.NewStream(ctx, &DiscoveryService_ServiceDesc.Streams[0], "/etcdisc.v1.DiscoveryService/WatchInstances", opts...)
	if err != nil {
		return nil, err
	}
	x := &discoveryServiceWatchInstancesClient{ClientStream: stream}
	if err := x.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type DiscoveryService_WatchInstancesClient interface {
	Recv() (*publicapi.WatchEvent, error)
	grpc.ClientStream
}

type discoveryServiceWatchInstancesClient struct{ grpc.ClientStream }

func (x *discoveryServiceWatchInstancesClient) Recv() (*publicapi.WatchEvent, error) {
	m := new(publicapi.WatchEvent)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

type DiscoveryServiceServer interface {
	Discover(context.Context, *DiscoverRequest) (*DiscoverResponse, error)
	WatchInstances(*WatchInstancesRequest, DiscoveryService_WatchInstancesServer) error
}

type DiscoveryService_WatchInstancesServer interface {
	Send(*publicapi.WatchEvent) error
	grpc.ServerStream
}

type UnimplementedDiscoveryServiceServer struct{}

func (UnimplementedDiscoveryServiceServer) Discover(context.Context, *DiscoverRequest) (*DiscoverResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method Discover not implemented")
}
func (UnimplementedDiscoveryServiceServer) WatchInstances(*WatchInstancesRequest, DiscoveryService_WatchInstancesServer) error {
	return status.Error(codes.Unimplemented, "method WatchInstances not implemented")
}

func RegisterDiscoveryServiceServer(s grpc.ServiceRegistrar, srv DiscoveryServiceServer) {
	s.RegisterService(&DiscoveryService_ServiceDesc, srv)
}

type ConfigServiceClient interface {
	GetEffectiveConfig(ctx context.Context, in *ConfigGetRequest, opts ...grpc.CallOption) (*ConfigGetResponse, error)
	PutConfig(ctx context.Context, in *ConfigPutRequest, opts ...grpc.CallOption) (*ConfigPutResponse, error)
	DeleteConfig(ctx context.Context, in *ConfigDeleteRequest, opts ...grpc.CallOption) (*ConfigDeleteResponse, error)
	WatchConfigs(ctx context.Context, in *WatchConfigsRequest, opts ...grpc.CallOption) (ConfigService_WatchConfigsClient, error)
}

type configServiceClient struct{ cc grpc.ClientConnInterface }

func NewConfigServiceClient(cc grpc.ClientConnInterface) ConfigServiceClient {
	return &configServiceClient{cc: cc}
}

func (c *configServiceClient) GetEffectiveConfig(ctx context.Context, in *ConfigGetRequest, opts ...grpc.CallOption) (*ConfigGetResponse, error) {
	out := new(ConfigGetResponse)
	err := c.cc.Invoke(ctx, "/etcdisc.v1.ConfigService/GetEffectiveConfig", in, out, opts...)
	return out, err
}
func (c *configServiceClient) PutConfig(ctx context.Context, in *ConfigPutRequest, opts ...grpc.CallOption) (*ConfigPutResponse, error) {
	out := new(ConfigPutResponse)
	err := c.cc.Invoke(ctx, "/etcdisc.v1.ConfigService/PutConfig", in, out, opts...)
	return out, err
}
func (c *configServiceClient) DeleteConfig(ctx context.Context, in *ConfigDeleteRequest, opts ...grpc.CallOption) (*ConfigDeleteResponse, error) {
	out := new(ConfigDeleteResponse)
	err := c.cc.Invoke(ctx, "/etcdisc.v1.ConfigService/DeleteConfig", in, out, opts...)
	return out, err
}
func (c *configServiceClient) WatchConfigs(ctx context.Context, in *WatchConfigsRequest, opts ...grpc.CallOption) (ConfigService_WatchConfigsClient, error) {
	stream, err := c.cc.NewStream(ctx, &ConfigService_ServiceDesc.Streams[0], "/etcdisc.v1.ConfigService/WatchConfigs", opts...)
	if err != nil {
		return nil, err
	}
	x := &configServiceWatchConfigsClient{ClientStream: stream}
	if err := x.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type ConfigService_WatchConfigsClient interface {
	Recv() (*publicapi.WatchEvent, error)
	grpc.ClientStream
}

type configServiceWatchConfigsClient struct{ grpc.ClientStream }

func (x *configServiceWatchConfigsClient) Recv() (*publicapi.WatchEvent, error) {
	m := new(publicapi.WatchEvent)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

type ConfigServiceServer interface {
	GetEffectiveConfig(context.Context, *ConfigGetRequest) (*ConfigGetResponse, error)
	PutConfig(context.Context, *ConfigPutRequest) (*ConfigPutResponse, error)
	DeleteConfig(context.Context, *ConfigDeleteRequest) (*ConfigDeleteResponse, error)
	WatchConfigs(*WatchConfigsRequest, ConfigService_WatchConfigsServer) error
}

type ConfigService_WatchConfigsServer interface {
	Send(*publicapi.WatchEvent) error
	grpc.ServerStream
}

type UnimplementedConfigServiceServer struct{}

func (UnimplementedConfigServiceServer) GetEffectiveConfig(context.Context, *ConfigGetRequest) (*ConfigGetResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method GetEffectiveConfig not implemented")
}
func (UnimplementedConfigServiceServer) PutConfig(context.Context, *ConfigPutRequest) (*ConfigPutResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method PutConfig not implemented")
}
func (UnimplementedConfigServiceServer) DeleteConfig(context.Context, *ConfigDeleteRequest) (*ConfigDeleteResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method DeleteConfig not implemented")
}
func (UnimplementedConfigServiceServer) WatchConfigs(*WatchConfigsRequest, ConfigService_WatchConfigsServer) error {
	return status.Error(codes.Unimplemented, "method WatchConfigs not implemented")
}

func RegisterConfigServiceServer(s grpc.ServiceRegistrar, srv ConfigServiceServer) {
	s.RegisterService(&ConfigService_ServiceDesc, srv)
}

type A2AServiceClient interface {
	UpsertAgentCard(ctx context.Context, in *UpsertAgentCardRequest, opts ...grpc.CallOption) (*UpsertAgentCardResponse, error)
	DiscoverCapabilities(ctx context.Context, in *DiscoverCapabilitiesRequest, opts ...grpc.CallOption) (*DiscoverCapabilitiesResponse, error)
}

type a2aServiceClient struct{ cc grpc.ClientConnInterface }

func NewA2AServiceClient(cc grpc.ClientConnInterface) A2AServiceClient {
	return &a2aServiceClient{cc: cc}
}

func (c *a2aServiceClient) UpsertAgentCard(ctx context.Context, in *UpsertAgentCardRequest, opts ...grpc.CallOption) (*UpsertAgentCardResponse, error) {
	out := new(UpsertAgentCardResponse)
	err := c.cc.Invoke(ctx, "/etcdisc.v1.A2AService/UpsertAgentCard", in, out, opts...)
	return out, err
}
func (c *a2aServiceClient) DiscoverCapabilities(ctx context.Context, in *DiscoverCapabilitiesRequest, opts ...grpc.CallOption) (*DiscoverCapabilitiesResponse, error) {
	out := new(DiscoverCapabilitiesResponse)
	err := c.cc.Invoke(ctx, "/etcdisc.v1.A2AService/DiscoverCapabilities", in, out, opts...)
	return out, err
}

type A2AServiceServer interface {
	UpsertAgentCard(context.Context, *UpsertAgentCardRequest) (*UpsertAgentCardResponse, error)
	DiscoverCapabilities(context.Context, *DiscoverCapabilitiesRequest) (*DiscoverCapabilitiesResponse, error)
}

type UnimplementedA2AServiceServer struct{}

func (UnimplementedA2AServiceServer) UpsertAgentCard(context.Context, *UpsertAgentCardRequest) (*UpsertAgentCardResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method UpsertAgentCard not implemented")
}
func (UnimplementedA2AServiceServer) DiscoverCapabilities(context.Context, *DiscoverCapabilitiesRequest) (*DiscoverCapabilitiesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method DiscoverCapabilities not implemented")
}

func RegisterA2AServiceServer(s grpc.ServiceRegistrar, srv A2AServiceServer) {
	s.RegisterService(&A2AService_ServiceDesc, srv)
}

var RegistryService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "etcdisc.v1.RegistryService",
	HandlerType: (*RegistryServiceServer)(nil),
	Methods:     []grpc.MethodDesc{{MethodName: "Register", Handler: _RegistryService_Register_Handler}, {MethodName: "Heartbeat", Handler: _RegistryService_Heartbeat_Handler}, {MethodName: "UpdateInstance", Handler: _RegistryService_UpdateInstance_Handler}, {MethodName: "Deregister", Handler: _RegistryService_Deregister_Handler}},
	Streams:     []grpc.StreamDesc{},
	Metadata:    "api/proto/etcdisc/v1/etcdisc.proto",
}

var DiscoveryService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "etcdisc.v1.DiscoveryService",
	HandlerType: (*DiscoveryServiceServer)(nil),
	Methods:     []grpc.MethodDesc{{MethodName: "Discover", Handler: _DiscoveryService_Discover_Handler}},
	Streams:     []grpc.StreamDesc{{StreamName: "WatchInstances", Handler: _DiscoveryService_WatchInstances_Handler, ServerStreams: true}},
	Metadata:    "api/proto/etcdisc/v1/etcdisc.proto",
}

var ConfigService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "etcdisc.v1.ConfigService",
	HandlerType: (*ConfigServiceServer)(nil),
	Methods:     []grpc.MethodDesc{{MethodName: "GetEffectiveConfig", Handler: _ConfigService_GetEffectiveConfig_Handler}, {MethodName: "PutConfig", Handler: _ConfigService_PutConfig_Handler}, {MethodName: "DeleteConfig", Handler: _ConfigService_DeleteConfig_Handler}},
	Streams:     []grpc.StreamDesc{{StreamName: "WatchConfigs", Handler: _ConfigService_WatchConfigs_Handler, ServerStreams: true}},
	Metadata:    "api/proto/etcdisc/v1/etcdisc.proto",
}

var A2AService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "etcdisc.v1.A2AService",
	HandlerType: (*A2AServiceServer)(nil),
	Methods:     []grpc.MethodDesc{{MethodName: "UpsertAgentCard", Handler: _A2AService_UpsertAgentCard_Handler}, {MethodName: "DiscoverCapabilities", Handler: _A2AService_DiscoverCapabilities_Handler}},
	Streams:     []grpc.StreamDesc{},
	Metadata:    "api/proto/etcdisc/v1/etcdisc.proto",
}

func _RegistryService_Register_Handler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(RegisterRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(RegistryServiceServer).Register(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/etcdisc.v1.RegistryService/Register"}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(RegistryServiceServer).Register(ctx, req.(*RegisterRequest))
	}
	return interceptor(ctx, in, info, handler)
}
func _RegistryService_Heartbeat_Handler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(HeartbeatRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(RegistryServiceServer).Heartbeat(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/etcdisc.v1.RegistryService/Heartbeat"}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(RegistryServiceServer).Heartbeat(ctx, req.(*HeartbeatRequest))
	}
	return interceptor(ctx, in, info, handler)
}
func _RegistryService_UpdateInstance_Handler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(UpdateInstanceRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(RegistryServiceServer).UpdateInstance(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/etcdisc.v1.RegistryService/UpdateInstance"}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(RegistryServiceServer).UpdateInstance(ctx, req.(*UpdateInstanceRequest))
	}
	return interceptor(ctx, in, info, handler)
}
func _RegistryService_Deregister_Handler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(DeregisterRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(RegistryServiceServer).Deregister(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/etcdisc.v1.RegistryService/Deregister"}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(RegistryServiceServer).Deregister(ctx, req.(*DeregisterRequest))
	}
	return interceptor(ctx, in, info, handler)
}
func _DiscoveryService_Discover_Handler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(DiscoverRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DiscoveryServiceServer).Discover(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/etcdisc.v1.DiscoveryService/Discover"}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(DiscoveryServiceServer).Discover(ctx, req.(*DiscoverRequest))
	}
	return interceptor(ctx, in, info, handler)
}
func _DiscoveryService_WatchInstances_Handler(srv any, stream grpc.ServerStream) error {
	in := new(WatchInstancesRequest)
	if err := stream.RecvMsg(in); err != nil {
		return err
	}
	return srv.(DiscoveryServiceServer).WatchInstances(in, &discoveryServiceWatchInstancesServer{ServerStream: stream})
}

type discoveryServiceWatchInstancesServer struct{ grpc.ServerStream }

func (x *discoveryServiceWatchInstancesServer) Send(m *publicapi.WatchEvent) error {
	return x.ServerStream.SendMsg(m)
}
func _ConfigService_GetEffectiveConfig_Handler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(ConfigGetRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConfigServiceServer).GetEffectiveConfig(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/etcdisc.v1.ConfigService/GetEffectiveConfig"}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(ConfigServiceServer).GetEffectiveConfig(ctx, req.(*ConfigGetRequest))
	}
	return interceptor(ctx, in, info, handler)
}
func _ConfigService_PutConfig_Handler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(ConfigPutRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConfigServiceServer).PutConfig(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/etcdisc.v1.ConfigService/PutConfig"}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(ConfigServiceServer).PutConfig(ctx, req.(*ConfigPutRequest))
	}
	return interceptor(ctx, in, info, handler)
}
func _ConfigService_DeleteConfig_Handler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(ConfigDeleteRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConfigServiceServer).DeleteConfig(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/etcdisc.v1.ConfigService/DeleteConfig"}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(ConfigServiceServer).DeleteConfig(ctx, req.(*ConfigDeleteRequest))
	}
	return interceptor(ctx, in, info, handler)
}
func _ConfigService_WatchConfigs_Handler(srv any, stream grpc.ServerStream) error {
	in := new(WatchConfigsRequest)
	if err := stream.RecvMsg(in); err != nil {
		return err
	}
	return srv.(ConfigServiceServer).WatchConfigs(in, &configServiceWatchConfigsServer{ServerStream: stream})
}

type configServiceWatchConfigsServer struct{ grpc.ServerStream }

func (x *configServiceWatchConfigsServer) Send(m *publicapi.WatchEvent) error {
	return x.ServerStream.SendMsg(m)
}
func _A2AService_UpsertAgentCard_Handler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(UpsertAgentCardRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(A2AServiceServer).UpsertAgentCard(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/etcdisc.v1.A2AService/UpsertAgentCard"}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(A2AServiceServer).UpsertAgentCard(ctx, req.(*UpsertAgentCardRequest))
	}
	return interceptor(ctx, in, info, handler)
}
func _A2AService_DiscoverCapabilities_Handler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(DiscoverCapabilitiesRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(A2AServiceServer).DiscoverCapabilities(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/etcdisc.v1.A2AService/DiscoverCapabilities"}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(A2AServiceServer).DiscoverCapabilities(ctx, req.(*DiscoverCapabilitiesRequest))
	}
	return interceptor(ctx, in, info, handler)
}
