// convert.go maps generated protobuf types to public phase-1 DTOs and back.
package etcdiscv1

import (
	"encoding/json"
	"slices"
	"time"

	"etcdisc/internal/core/model"
	publicapi "etcdisc/pkg/api"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func NewRegisterRequestFromPublic(input publicapi.RegisterInput) *RegisterRequest {
	return &RegisterRequest{Input: RegisterInputFromPublic(input)}
}

func RegisterInputFromPublic(input publicapi.RegisterInput) *RegisterInput {
	return &RegisterInput{Instance: InstanceFromPublic(input.Instance), LeaseTtlSeconds: input.LeaseTTLSeconds}
}

func (m *RegisterInput) ToPublic() publicapi.RegisterInput {
	if m == nil {
		return publicapi.RegisterInput{}
	}
	return publicapi.RegisterInput{Instance: m.GetInstance().ToPublic(), LeaseTTLSeconds: m.GetLeaseTtlSeconds()}
}

func NewHeartbeatRequestFromPublic(input publicapi.HeartbeatInput) *HeartbeatRequest {
	return &HeartbeatRequest{Input: HeartbeatInputFromPublic(input)}
}

func HeartbeatInputFromPublic(input publicapi.HeartbeatInput) *HeartbeatInput {
	return &HeartbeatInput{Namespace: input.Namespace, Service: input.Service, InstanceId: input.InstanceID}
}

func (m *HeartbeatInput) ToPublic() publicapi.HeartbeatInput {
	if m == nil {
		return publicapi.HeartbeatInput{}
	}
	return publicapi.HeartbeatInput{Namespace: m.GetNamespace(), Service: m.GetService(), InstanceID: m.GetInstanceId()}
}

func NewUpdateInstanceRequestFromPublic(input publicapi.UpdateInstanceInput) *UpdateInstanceRequest {
	return &UpdateInstanceRequest{Input: UpdateInstanceInputFromPublic(input)}
}

func UpdateInstanceInputFromPublic(input publicapi.UpdateInstanceInput) *UpdateInstanceInput {
	out := &UpdateInstanceInput{Namespace: input.Namespace, Service: input.Service, InstanceId: input.InstanceID, Metadata: cloneStringMap(input.Metadata), ExpectedRevision: input.ExpectedRevision}
	if input.Weight != nil {
		out.Weight = wrapperspb.Int32(int32(*input.Weight))
	}
	if input.Version != nil {
		out.Version = wrapperspb.String(*input.Version)
	}
	return out
}

func (m *UpdateInstanceInput) ToPublic() publicapi.UpdateInstanceInput {
	if m == nil {
		return publicapi.UpdateInstanceInput{}
	}
	out := publicapi.UpdateInstanceInput{Namespace: m.GetNamespace(), Service: m.GetService(), InstanceID: m.GetInstanceId(), Metadata: cloneStringMap(m.GetMetadata()), ExpectedRevision: m.GetExpectedRevision()}
	if m.Weight != nil {
		value := int(m.Weight.Value)
		out.Weight = &value
	}
	if m.Version != nil {
		value := m.Version.Value
		out.Version = &value
	}
	return out
}

func NewDeregisterRequestFromPublic(input publicapi.DeregisterInput) *DeregisterRequest {
	return &DeregisterRequest{Input: DeregisterInputFromPublic(input)}
}

func DeregisterInputFromPublic(input publicapi.DeregisterInput) *DeregisterInput {
	return &DeregisterInput{Namespace: input.Namespace, Service: input.Service, InstanceId: input.InstanceID}
}

func (m *DeregisterInput) ToPublic() publicapi.DeregisterInput {
	if m == nil {
		return publicapi.DeregisterInput{}
	}
	return publicapi.DeregisterInput{Namespace: m.GetNamespace(), Service: m.GetService(), InstanceID: m.GetInstanceId()}
}

func NewDiscoverRequestFromPublic(input publicapi.DiscoverySnapshotInput) *DiscoverRequest {
	return &DiscoverRequest{Input: DiscoverySnapshotInputFromPublic(input)}
}

func DiscoverySnapshotInputFromPublic(input publicapi.DiscoverySnapshotInput) *DiscoverySnapshotInput {
	out := &DiscoverySnapshotInput{Namespace: input.Namespace, Service: input.Service, Group: input.Group, Version: input.Version, Metadata: cloneStringMap(input.Metadata)}
	if input.HealthyOnly != nil {
		out.HealthyOnly = wrapperspb.Bool(*input.HealthyOnly)
	}
	return out
}

func (m *DiscoverySnapshotInput) ToPublic() publicapi.DiscoverySnapshotInput {
	if m == nil {
		return publicapi.DiscoverySnapshotInput{}
	}
	out := publicapi.DiscoverySnapshotInput{Namespace: m.GetNamespace(), Service: m.GetService(), Group: m.GetGroup(), Version: m.GetVersion(), Metadata: cloneStringMap(m.GetMetadata())}
	if m.HealthyOnly != nil {
		value := m.HealthyOnly.Value
		out.HealthyOnly = &value
	}
	return out
}

func NewWatchInstancesRequestFromPublic(input publicapi.DiscoveryWatchInput) *WatchInstancesRequest {
	return &WatchInstancesRequest{Input: DiscoveryWatchInputFromPublic(input)}
}

func DiscoveryWatchInputFromPublic(input publicapi.DiscoveryWatchInput) *DiscoveryWatchInput {
	out := &DiscoveryWatchInput{Namespace: input.Namespace, Service: input.Service, Revision: input.Revision}
	if input.HealthyOnly != nil {
		out.HealthyOnly = wrapperspb.Bool(*input.HealthyOnly)
	}
	return out
}

func (m *DiscoveryWatchInput) ToPublic() publicapi.DiscoveryWatchInput {
	if m == nil {
		return publicapi.DiscoveryWatchInput{}
	}
	out := publicapi.DiscoveryWatchInput{Namespace: m.GetNamespace(), Service: m.GetService(), Revision: m.GetRevision()}
	if m.HealthyOnly != nil {
		value := m.HealthyOnly.Value
		out.HealthyOnly = &value
	}
	return out
}

func ConfigPutInputFromPublic(input publicapi.ConfigPutInput) *ConfigPutInput {
	return &ConfigPutInput{Item: ConfigItemFromPublic(input.Item), ExpectedRevision: input.ExpectedRevision}
}

func (m *ConfigPutInput) ToPublic() publicapi.ConfigPutInput {
	if m == nil {
		return publicapi.ConfigPutInput{}
	}
	return publicapi.ConfigPutInput{Item: m.GetItem().ToPublic(), ExpectedRevision: m.GetExpectedRevision()}
}

func ConfigDeleteInputFromPublic(input publicapi.ConfigDeleteInput) *ConfigDeleteInput {
	return &ConfigDeleteInput{Scope: string(input.Scope), Namespace: input.Namespace, Service: input.Service, Key: input.Key, ExpectedRevision: input.ExpectedRevision}
}

func (m *ConfigDeleteInput) ToPublic() publicapi.ConfigDeleteInput {
	if m == nil {
		return publicapi.ConfigDeleteInput{}
	}
	return publicapi.ConfigDeleteInput{Scope: model.ConfigScope(m.GetScope()), Namespace: m.GetNamespace(), Service: m.GetService(), Key: m.GetKey(), ExpectedRevision: m.GetExpectedRevision()}
}

func ConfigWatchInputFromPublic(input publicapi.ConfigWatchInput) *ConfigWatchInput {
	return &ConfigWatchInput{Namespace: input.Namespace, Service: input.Service, Revision: input.Revision}
}

func (m *ConfigWatchInput) ToPublic() publicapi.ConfigWatchInput {
	if m == nil {
		return publicapi.ConfigWatchInput{}
	}
	return publicapi.ConfigWatchInput{Namespace: m.GetNamespace(), Service: m.GetService(), Revision: m.GetRevision()}
}

func AgentCardPutInputFromPublic(input publicapi.AgentCardPutInput) *AgentCardPutInput {
	return &AgentCardPutInput{Card: AgentCardFromPublic(input.Card), ExpectedRevision: input.ExpectedRevision}
}

func (m *AgentCardPutInput) ToPublic() publicapi.AgentCardPutInput {
	if m == nil {
		return publicapi.AgentCardPutInput{}
	}
	return publicapi.AgentCardPutInput{Card: m.GetCard().ToPublic(), ExpectedRevision: m.GetExpectedRevision()}
}

func CapabilityDiscoverInputFromPublic(input publicapi.CapabilityDiscoverInput) *CapabilityDiscoverInput {
	out := &CapabilityDiscoverInput{Namespace: input.Namespace, Capability: input.Capability, Protocol: input.Protocol}
	if input.IncludeUnhealthy {
		out.IncludeUnhealthy = wrapperspb.Bool(true)
	}
	return out
}

func (m *CapabilityDiscoverInput) ToPublic() publicapi.CapabilityDiscoverInput {
	if m == nil {
		return publicapi.CapabilityDiscoverInput{}
	}
	out := publicapi.CapabilityDiscoverInput{Namespace: m.GetNamespace(), Capability: m.GetCapability(), Protocol: m.GetProtocol()}
	if m.IncludeUnhealthy != nil {
		out.IncludeUnhealthy = m.IncludeUnhealthy.Value
	}
	return out
}

func InstanceFromPublic(value publicapi.Instance) *Instance {
	return &Instance{Namespace: value.Namespace, Service: value.Service, Group: value.Group, Version: value.Version, Weight: int32(value.Weight), InstanceId: value.InstanceID, AgentId: value.AgentID, Address: value.Address, Port: int32(value.Port), Metadata: cloneStringMap(value.Metadata), HealthCheckMode: string(value.HealthCheckMode), ProbeConfig: ProbeConfigFromPublic(value.ProbeConfig), Status: string(value.Status), LeaseId: value.LeaseID, ConsecutiveFailures: int32(value.ConsecutiveFailures), ConsecutiveSuccesses: int32(value.ConsecutiveSuccesses), LastHeartbeatAt: timestamp(value.LastHeartbeatAt), LastProbeAt: timestamp(value.LastProbeAt), CreatedAt: timestamp(value.CreatedAt), UpdatedAt: timestamp(value.UpdatedAt), StatusUpdatedAt: timestamp(value.StatusUpdatedAt), Revision: value.Revision}
}

func (m *Instance) ToPublic() publicapi.Instance {
	if m == nil {
		return publicapi.Instance{}
	}
	return publicapi.Instance{Namespace: m.GetNamespace(), Service: m.GetService(), Group: m.GetGroup(), Version: m.GetVersion(), Weight: int(m.GetWeight()), InstanceID: m.GetInstanceId(), AgentID: m.GetAgentId(), Address: m.GetAddress(), Port: int(m.GetPort()), Metadata: cloneStringMap(m.GetMetadata()), HealthCheckMode: model.HealthCheckMode(m.GetHealthCheckMode()), ProbeConfig: m.GetProbeConfig().ToPublic(), Status: model.InstanceStatus(m.GetStatus()), LeaseID: m.GetLeaseId(), ConsecutiveFailures: int(m.GetConsecutiveFailures()), ConsecutiveSuccesses: int(m.GetConsecutiveSuccesses()), LastHeartbeatAt: timeFromProto(m.GetLastHeartbeatAt()), LastProbeAt: timeFromProto(m.GetLastProbeAt()), CreatedAt: timeFromProto(m.GetCreatedAt()), UpdatedAt: timeFromProto(m.GetUpdatedAt()), StatusUpdatedAt: timeFromProto(m.GetStatusUpdatedAt()), Revision: m.GetRevision()}
}

func ProbeConfigFromPublic(value model.ProbeConfig) *ProbeConfig {
	return &ProbeConfig{Address: value.Address, Port: int32(value.Port), Path: value.Path}
}

func (m *ProbeConfig) ToPublic() model.ProbeConfig {
	if m == nil {
		return model.ProbeConfig{}
	}
	return model.ProbeConfig{Address: m.GetAddress(), Port: int(m.GetPort()), Path: m.GetPath()}
}

func ConfigItemFromPublic(value publicapi.ConfigItem) *ConfigItem {
	return &ConfigItem{Namespace: value.Namespace, Service: value.Service, Scope: string(value.Scope), Key: value.Key, Value: value.Value, ValueType: string(value.ValueType), CreatedAt: timestamp(value.CreatedAt), UpdatedAt: timestamp(value.UpdatedAt), Revision: value.Revision}
}

func (m *ConfigItem) ToPublic() publicapi.ConfigItem {
	if m == nil {
		return publicapi.ConfigItem{}
	}
	return publicapi.ConfigItem{Namespace: m.GetNamespace(), Service: m.GetService(), Scope: model.ConfigScope(m.GetScope()), Key: m.GetKey(), Value: m.GetValue(), ValueType: model.ConfigValueType(m.GetValueType()), CreatedAt: timeFromProto(m.GetCreatedAt()), UpdatedAt: timeFromProto(m.GetUpdatedAt()), Revision: m.GetRevision()}
}

func EffectiveConfigItemFromPublic(value publicapi.EffectiveConfigItem) *EffectiveConfigItem {
	return &EffectiveConfigItem{Key: value.Key, Value: value.Value, ValueType: string(value.ValueType), Scope: string(value.Scope)}
}

func (m *EffectiveConfigItem) ToPublic() publicapi.EffectiveConfigItem {
	if m == nil {
		return publicapi.EffectiveConfigItem{}
	}
	return publicapi.EffectiveConfigItem{Key: m.GetKey(), Value: m.GetValue(), ValueType: model.ConfigValueType(m.GetValueType()), Scope: model.ConfigScope(m.GetScope())}
}

func AgentCardFromPublic(value publicapi.AgentCard) *AgentCard {
	return &AgentCard{Namespace: value.Namespace, AgentId: value.AgentID, Service: value.Service, Capabilities: slices.Clone(value.Capabilities), Protocols: slices.Clone(value.Protocols), AuthMode: string(value.AuthMode), Metadata: cloneStringMap(value.Metadata), CreatedAt: timestamp(value.CreatedAt), UpdatedAt: timestamp(value.UpdatedAt), Revision: value.Revision}
}

func (m *AgentCard) ToPublic() publicapi.AgentCard {
	if m == nil {
		return publicapi.AgentCard{}
	}
	return publicapi.AgentCard{Namespace: m.GetNamespace(), AgentID: m.GetAgentId(), Service: m.GetService(), Capabilities: slices.Clone(m.GetCapabilities()), Protocols: slices.Clone(m.GetProtocols()), AuthMode: model.AuthMode(m.GetAuthMode()), Metadata: cloneStringMap(m.GetMetadata()), CreatedAt: timeFromProto(m.GetCreatedAt()), UpdatedAt: timeFromProto(m.GetUpdatedAt()), Revision: m.GetRevision()}
}

func A2ADiscoveryResultFromPublic(value publicapi.A2ADiscoveryResult) *A2ADiscoveryResult {
	return &A2ADiscoveryResult{Namespace: value.Namespace, AgentId: value.AgentID, Service: value.Service, Address: value.Address, Port: int32(value.Port), Protocols: slices.Clone(value.Protocols), AuthMode: string(value.AuthMode), Metadata: cloneStringMap(value.Metadata), Status: string(value.Status)}
}

func (m *A2ADiscoveryResult) ToPublic() publicapi.A2ADiscoveryResult {
	if m == nil {
		return publicapi.A2ADiscoveryResult{}
	}
	return publicapi.A2ADiscoveryResult{Namespace: m.GetNamespace(), AgentID: m.GetAgentId(), Service: m.GetService(), Address: m.GetAddress(), Port: int(m.GetPort()), Protocols: slices.Clone(m.GetProtocols()), AuthMode: model.AuthMode(m.GetAuthMode()), Metadata: cloneStringMap(m.GetMetadata()), Status: model.InstanceStatus(m.GetStatus())}
}

func WatchEventFromPublic(value publicapi.WatchEvent) *WatchEvent {
	return &WatchEvent{Type: string(value.Type), Resource: value.Resource, Key: value.Key, Revision: value.Revision, Value: slices.Clone(value.Value)}
}

func (m *WatchEvent) ToPublic() publicapi.WatchEvent {
	if m == nil {
		return publicapi.WatchEvent{}
	}
	return publicapi.WatchEvent{Type: publicapi.WatchEventType(m.GetType()), Resource: m.GetResource(), Key: m.GetKey(), Revision: m.GetRevision(), Value: slices.Clone(m.GetValue())}
}

func NamespaceFromPublic(value publicapi.Namespace) *Namespace {
	return &Namespace{Name: value.Name, AccessMode: string(value.AccessMode), CreatedAt: timestamp(value.CreatedAt), UpdatedAt: timestamp(value.UpdatedAt), Revision: value.Revision}
}

func (m *Namespace) ToPublic() publicapi.Namespace {
	if m == nil {
		return publicapi.Namespace{}
	}
	return publicapi.Namespace{Name: m.GetName(), AccessMode: model.NamespaceAccessMode(m.GetAccessMode()), CreatedAt: timeFromProto(m.GetCreatedAt()), UpdatedAt: timeFromProto(m.GetUpdatedAt()), Revision: m.GetRevision()}
}

func EffectiveConfigMapFromPublic(value map[string]publicapi.EffectiveConfigItem) map[string]*EffectiveConfigItem {
	out := make(map[string]*EffectiveConfigItem, len(value))
	for key, item := range value {
		out[key] = EffectiveConfigItemFromPublic(item)
	}
	return out
}

func EffectiveConfigMapToPublic(value map[string]*EffectiveConfigItem) map[string]publicapi.EffectiveConfigItem {
	out := make(map[string]publicapi.EffectiveConfigItem, len(value))
	for key, item := range value {
		out[key] = item.ToPublic()
	}
	return out
}

func MarshalWatchValue(value any) []byte {
	body, _ := json.Marshal(value)
	return body
}

func timestamp(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
}

func timeFromProto(value *timestamppb.Timestamp) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.AsTime()
}

func cloneStringMap(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]string, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
}
