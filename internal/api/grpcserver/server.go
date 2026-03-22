// server.go exposes the phase 1 gRPC access layer backed by the shared service implementations.
package grpcserver

import (
	"context"

	etcdiscv1 "etcdisc/api/proto/etcdisc/v1"
	apperrors "etcdisc/internal/core/errors"
	a2asvc "etcdisc/internal/core/service/a2a"
	configsvc "etcdisc/internal/core/service/config"
	discoverysvc "etcdisc/internal/core/service/discovery"
	registrysvc "etcdisc/internal/core/service/registry"
	"google.golang.org/grpc"
)

// Services groups the service dependencies consumed by the gRPC access layer.
type Services struct {
	Registry  *registrysvc.Service
	Discovery *discoverysvc.Service
	Config    *configsvc.Service
	A2A       *a2asvc.Service
}

type server struct {
	etcdiscv1.UnimplementedRegistryServiceServer
	etcdiscv1.UnimplementedDiscoveryServiceServer
	etcdiscv1.UnimplementedConfigServiceServer
	etcdiscv1.UnimplementedA2AServiceServer
	services Services
}

// New creates a gRPC server with all phase 1 services registered.
func New(services Services, opts ...grpc.ServerOption) *grpc.Server {
	grpcServer := grpc.NewServer(opts...)
	handler := &server{services: services}
	etcdiscv1.RegisterRegistryServiceServer(grpcServer, handler)
	etcdiscv1.RegisterDiscoveryServiceServer(grpcServer, handler)
	etcdiscv1.RegisterConfigServiceServer(grpcServer, handler)
	etcdiscv1.RegisterA2AServiceServer(grpcServer, handler)
	return grpcServer
}

func (s *server) Register(ctx context.Context, req *etcdiscv1.RegisterRequest) (*etcdiscv1.RegisterResponse, error) {
	instance, err := s.services.Registry.Register(ctx, req.GetInput().ToPublic())
	if err != nil {
		return nil, apperrors.GRPCStatusOf(err).Err()
	}
	return &etcdiscv1.RegisterResponse{Instance: etcdiscv1.InstanceFromPublic(instance)}, nil
}

func (s *server) Heartbeat(ctx context.Context, req *etcdiscv1.HeartbeatRequest) (*etcdiscv1.HeartbeatResponse, error) {
	instance, err := s.services.Registry.Heartbeat(ctx, req.GetInput().ToPublic())
	if err != nil {
		return nil, apperrors.GRPCStatusOf(err).Err()
	}
	return &etcdiscv1.HeartbeatResponse{Instance: etcdiscv1.InstanceFromPublic(instance)}, nil
}

func (s *server) UpdateInstance(ctx context.Context, req *etcdiscv1.UpdateInstanceRequest) (*etcdiscv1.UpdateInstanceResponse, error) {
	instance, err := s.services.Registry.Update(ctx, req.GetInput().ToPublic())
	if err != nil {
		return nil, apperrors.GRPCStatusOf(err).Err()
	}
	return &etcdiscv1.UpdateInstanceResponse{Instance: etcdiscv1.InstanceFromPublic(instance)}, nil
}

func (s *server) Deregister(ctx context.Context, req *etcdiscv1.DeregisterRequest) (*etcdiscv1.DeregisterResponse, error) {
	if err := s.services.Registry.Deregister(ctx, req.GetInput().ToPublic()); err != nil {
		return nil, apperrors.GRPCStatusOf(err).Err()
	}
	return &etcdiscv1.DeregisterResponse{Status: "deleted"}, nil
}

func (s *server) Discover(ctx context.Context, req *etcdiscv1.DiscoverRequest) (*etcdiscv1.DiscoverResponse, error) {
	items, err := s.services.Discovery.Snapshot(ctx, req.GetInput().ToPublic())
	if err != nil {
		return nil, apperrors.GRPCStatusOf(err).Err()
	}
	out := make([]*etcdiscv1.Instance, 0, len(items))
	for _, item := range items {
		out = append(out, etcdiscv1.InstanceFromPublic(item))
	}
	return &etcdiscv1.DiscoverResponse{Items: out}, nil
}

func (s *server) WatchInstances(req *etcdiscv1.WatchInstancesRequest, stream etcdiscv1.DiscoveryService_WatchInstancesServer) error {
	for event := range s.services.Discovery.Watch(stream.Context(), req.GetInput().ToPublic()) {
		if err := stream.Send(etcdiscv1.WatchEventFromPublic(event)); err != nil {
			return err
		}
	}
	return nil
}

func (s *server) GetEffectiveConfig(ctx context.Context, req *etcdiscv1.ConfigGetRequest) (*etcdiscv1.ConfigGetResponse, error) {
	effective, err := s.services.Config.Resolve(ctx, configsvc.ResolveInput{Namespace: req.Namespace, Service: req.Service})
	if err != nil {
		return nil, apperrors.GRPCStatusOf(err).Err()
	}
	return &etcdiscv1.ConfigGetResponse{EffectiveConfig: etcdiscv1.EffectiveConfigMapFromPublic(effective)}, nil
}

func (s *server) PutConfig(ctx context.Context, req *etcdiscv1.ConfigPutRequest) (*etcdiscv1.ConfigPutResponse, error) {
	item, err := s.services.Config.Put(ctx, req.GetInput().ToPublic())
	if err != nil {
		return nil, apperrors.GRPCStatusOf(err).Err()
	}
	return &etcdiscv1.ConfigPutResponse{Item: etcdiscv1.ConfigItemFromPublic(item)}, nil
}

func (s *server) DeleteConfig(ctx context.Context, req *etcdiscv1.ConfigDeleteRequest) (*etcdiscv1.ConfigDeleteResponse, error) {
	if err := s.services.Config.Delete(ctx, req.GetInput().ToPublic()); err != nil {
		return nil, apperrors.GRPCStatusOf(err).Err()
	}
	return &etcdiscv1.ConfigDeleteResponse{Status: "deleted"}, nil
}

func (s *server) WatchConfigs(req *etcdiscv1.WatchConfigsRequest, stream etcdiscv1.ConfigService_WatchConfigsServer) error {
	for event := range s.services.Config.Watch(stream.Context(), req.GetInput().ToPublic()) {
		if err := stream.Send(etcdiscv1.WatchEventFromPublic(event)); err != nil {
			return err
		}
	}
	return nil
}

func (s *server) UpsertAgentCard(ctx context.Context, req *etcdiscv1.UpsertAgentCardRequest) (*etcdiscv1.UpsertAgentCardResponse, error) {
	card, err := s.services.A2A.Put(ctx, req.GetInput().ToPublic())
	if err != nil {
		return nil, apperrors.GRPCStatusOf(err).Err()
	}
	return &etcdiscv1.UpsertAgentCardResponse{Card: etcdiscv1.AgentCardFromPublic(card)}, nil
}

func (s *server) DiscoverCapabilities(ctx context.Context, req *etcdiscv1.DiscoverCapabilitiesRequest) (*etcdiscv1.DiscoverCapabilitiesResponse, error) {
	items, err := s.services.A2A.Discover(ctx, req.GetInput().ToPublic())
	if err != nil {
		return nil, apperrors.GRPCStatusOf(err).Err()
	}
	out := make([]*etcdiscv1.A2ADiscoveryResult, 0, len(items))
	for _, item := range items {
		out = append(out, etcdiscv1.A2ADiscoveryResultFromPublic(item))
	}
	return &etcdiscv1.DiscoverCapabilitiesResponse{Items: out}, nil
}
