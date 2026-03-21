// model_test.go verifies validation, defaults, and enum helpers for core phase 1 models.
package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNamespaceValidate(t *testing.T) {
	t.Parallel()

	ns := Namespace{Name: "prod-core", AccessMode: NamespaceAccessReadWrite}
	require.NoError(t, ns.Validate())
	require.True(t, ns.AccessMode.AllowsRead())
	require.True(t, ns.AccessMode.AllowsWrite())

	ns.Name = "Bad_Name"
	require.Error(t, ns.Validate())
}

func TestInstanceNormalizeAndValidate(t *testing.T) {
	t.Parallel()

	instance := Instance{
		Namespace: "prod",
		Service:   "payment-api",
		Address:   "127.0.0.1",
		Port:      8080,
	}

	instance.Normalize()

	require.Equal(t, DefaultGroup, instance.Group)
	require.Equal(t, DefaultWeight, instance.Weight)
	require.Equal(t, HealthCheckHeartbeat, instance.HealthCheckMode)
	require.Equal(t, InstanceStatusHealth, instance.Status)
	require.NoError(t, instance.Validate())
}

func TestInstanceAppliesProbeDefaults(t *testing.T) {
	t.Parallel()

	instance := Instance{
		Namespace:       "prod",
		Service:         "payment-api",
		Address:         "10.0.0.8",
		Port:            8080,
		HealthCheckMode: HealthCheckHTTPProbe,
	}

	instance.Normalize()

	require.Equal(t, "/etcdisc/http/health", instance.ProbeConfig.Path)
	require.Equal(t, "10.0.0.8", instance.ProbeConfig.Address)
	require.Equal(t, 8080, instance.ProbeConfig.Port)
	require.NoError(t, instance.Validate())
}

func TestInstanceValidateRejectsInvalidWeight(t *testing.T) {
	t.Parallel()

	instance := Instance{
		Namespace:       "prod",
		Service:         "payment-api",
		InstanceID:      "node-1",
		Address:         "localhost",
		Port:            8080,
		Weight:          10001,
		HealthCheckMode: HealthCheckHeartbeat,
		Status:          InstanceStatusHealth,
		Metadata:        map[string]string{"zone": "cn-hz-a"},
	}

	require.Error(t, instance.Validate())
}

func TestConfigItemValidate(t *testing.T) {
	t.Parallel()

	item := ConfigItem{
		Scope:     ConfigScopeService,
		Namespace: "prod",
		Service:   "payment-api",
		Key:       "timeout.request",
		Value:     "3000",
		ValueType: ConfigValueDuration,
	}

	require.NoError(t, item.Validate())

	item.Value = "abc"
	require.Error(t, item.Validate())
}

func TestAgentCardValidate(t *testing.T) {
	t.Parallel()

	card := AgentCard{
		Namespace:    "prod",
		AgentID:      "assistant-1",
		Service:      "agent-api",
		Capabilities: []string{"chat.answer", "tool.search"},
		Protocols:    []string{"http", "grpc"},
		AuthMode:     AuthModeStaticToken,
		Metadata:     map[string]string{"team": "copilot"},
	}

	require.NoError(t, card.Validate())

	card.Capabilities = []string{"Bad Capability"}
	require.Error(t, card.Validate())
}

func TestValidationHelpers(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateNamespaceName("core-prod"))
	require.NoError(t, ValidateServiceName("order-api"))
	require.NoError(t, ValidateCapabilityName("tool.search"))
	require.NoError(t, ValidateConfigKey("timeout.request"))
	require.NoError(t, ValidateFlatMetadata(map[string]string{"k-1": "v"}))

	require.Error(t, ValidateNamespaceName("UPPER"))
	require.Error(t, ValidateServiceName("ab"))
	require.Error(t, ValidateCapabilityName("bad capability"))
	require.Error(t, ValidateConfigKey("Bad.Key"))
	require.Error(t, ValidateFlatMetadata(map[string]string{"bad key!": "v"}))
}
