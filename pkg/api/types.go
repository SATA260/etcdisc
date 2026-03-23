// types.go exposes the public phase 1 DTOs used by SDKs and transport contracts.
package api

import (
	"etcdisc/internal/core/model"
	a2asvc "etcdisc/internal/core/service/a2a"
	configsvc "etcdisc/internal/core/service/config"
	discoverysvc "etcdisc/internal/core/service/discovery"
	registrysvc "etcdisc/internal/core/service/registry"
)

type Namespace = model.Namespace
type Instance = model.Instance
type ConfigItem = model.ConfigItem
type EffectiveConfigItem = model.EffectiveConfigItem
type AgentCard = model.AgentCard
type A2ADiscoveryResult = model.A2ADiscoveryResult
type WatchEvent = model.WatchEvent
type WatchEventType = model.WatchEventType

const (
	WatchEventPut    = model.WatchEventPut
	WatchEventDelete = model.WatchEventDelete
	WatchEventReset  = model.WatchEventReset
)

type RegisterInput = registrysvc.RegisterInput
type UpdateInstanceInput = registrysvc.UpdateInput
type HeartbeatInput = registrysvc.HeartbeatInput
type DeregisterInput = registrysvc.DeregisterInput

type DiscoverySnapshotInput = discoverysvc.SnapshotInput
type DiscoveryWatchInput = discoverysvc.WatchInput

type ConfigPutInput = configsvc.PutInput
type ConfigDeleteInput = configsvc.DeleteInput
type ConfigWatchInput = configsvc.WatchInput

type AgentCardPutInput = a2asvc.PutInput
type CapabilityDiscoverInput = a2asvc.DiscoverInput
