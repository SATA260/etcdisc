// keys.go defines stable etcd key layouts for namespaces, instances, config, AgentCards, capabilities, and audit logs.
package keyspace

import (
	"fmt"
	"strings"

	"etcdisc/internal/core/model"
)

const root = "/etcdisc"

// NamespaceKey returns the key for namespace metadata.
func NamespaceKey(name string) string {
	return fmt.Sprintf("%s/namespaces/%s", root, name)
}

// NamespacePrefix returns the prefix for namespace metadata.
func NamespacePrefix() string {
	return root + "/namespaces/"
}

// InstanceKey returns the key for a runtime instance.
func InstanceKey(namespace, service, instanceID string) string {
	return fmt.Sprintf("%s/instances/%s/%s/%s", root, namespace, service, instanceID)
}

// InstanceServicePrefix returns the prefix for all instances of a service.
func InstanceServicePrefix(namespace, service string) string {
	return fmt.Sprintf("%s/instances/%s/%s/", root, namespace, service)
}

// InstanceNamespacePrefix returns the prefix for all instances in a namespace.
func InstanceNamespacePrefix(namespace string) string {
	return fmt.Sprintf("%s/instances/%s/", root, namespace)
}

// InstanceRootPrefix returns the root instance prefix.
func InstanceRootPrefix() string {
	return root + "/instances/"
}

// ConfigKey returns the storage key for a config item.
func ConfigKey(scope model.ConfigScope, namespace, service, key string) string {
	switch scope {
	case model.ConfigScopeGlobal:
		return fmt.Sprintf("%s/config/global/%s", root, key)
	case model.ConfigScopeNamespace:
		return fmt.Sprintf("%s/config/namespaces/%s/%s", root, namespace, key)
	default:
		return fmt.Sprintf("%s/config/services/%s/%s/%s", root, namespace, service, key)
	}
}

// ConfigPrefix returns the listing prefix for a config scope.
func ConfigPrefix(scope model.ConfigScope, namespace, service string) string {
	switch scope {
	case model.ConfigScopeGlobal:
		return root + "/config/global/"
	case model.ConfigScopeNamespace:
		return fmt.Sprintf("%s/config/namespaces/%s/", root, namespace)
	default:
		return fmt.Sprintf("%s/config/services/%s/%s/", root, namespace, service)
	}
}

// ConfigRootPrefix returns the full config root.
func ConfigRootPrefix() string {
	return root + "/config/"
}

// AgentCardKey returns the storage key for an AgentCard.
func AgentCardKey(namespace, agentID string) string {
	return fmt.Sprintf("%s/agentcards/%s/%s", root, namespace, agentID)
}

// AgentCardNamespacePrefix returns the prefix for AgentCards in a namespace.
func AgentCardNamespacePrefix(namespace string) string {
	return fmt.Sprintf("%s/agentcards/%s/", root, namespace)
}

// CapabilityKey returns the key for a capability index entry.
func CapabilityKey(namespace, capability, agentID string) string {
	return fmt.Sprintf("%s/capabilities/%s/%s/%s", root, namespace, capability, agentID)
}

// CapabilityPrefix returns the prefix for capability queries.
func CapabilityPrefix(namespace, capability string) string {
	return fmt.Sprintf("%s/capabilities/%s/%s/", root, namespace, capability)
}

// AuditKey returns the storage key for an audit event.
func AuditKey(id string) string {
	return fmt.Sprintf("%s/audit/%s", root, id)
}

// AuditPrefix returns the audit log prefix.
func AuditPrefix() string {
	return root + "/audit/"
}

// ResourceFromKey classifies a stored key for watch output.
func ResourceFromKey(key string) string {
	switch {
	case strings.HasPrefix(key, NamespacePrefix()):
		return "namespace"
	case strings.HasPrefix(key, InstanceRootPrefix()):
		return "instance"
	case strings.HasPrefix(key, ConfigRootPrefix()):
		return "config"
	case strings.HasPrefix(key, root+"/agentcards/"):
		return "agentcard"
	case strings.HasPrefix(key, root+"/capabilities/"):
		return "capability"
	case strings.HasPrefix(key, AuditPrefix()):
		return "audit"
	default:
		return "unknown"
	}
}
