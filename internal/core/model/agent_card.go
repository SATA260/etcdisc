// agent_card.go defines the A2A AgentCard resource and discovery result surface used in phase 1.
package model

import (
	"slices"
	"time"

	apperrors "etcdisc/internal/core/errors"
)

// AuthMode identifies the authentication summary exposed by A2A discovery.
type AuthMode string

const (
	// AuthModeNone means no authentication is required.
	AuthModeNone AuthMode = "none"
	// AuthModeStaticToken means a static token is required.
	AuthModeStaticToken AuthMode = "static_token"
	// AuthModeMTLS means mTLS is required.
	AuthModeMTLS AuthMode = "mtls"
)

// AgentCard represents the capability descriptor for an A2A participant.
type AgentCard struct {
	Namespace    string            `json:"namespace"`
	AgentID      string            `json:"agentId"`
	Service      string            `json:"service"`
	Capabilities []string          `json:"capabilities"`
	Protocols    []string          `json:"protocols"`
	AuthMode     AuthMode          `json:"authMode"`
	Metadata     map[string]string `json:"metadata"`
	CreatedAt    time.Time         `json:"createdAt"`
	UpdatedAt    time.Time         `json:"updatedAt"`
	Revision     int64             `json:"revision"`
}

// A2ADiscoveryResult represents the merged AgentCard plus runtime instance view returned to callers.
type A2ADiscoveryResult struct {
	Namespace string            `json:"namespace"`
	AgentID   string            `json:"agentId"`
	Service   string            `json:"service"`
	Address   string            `json:"address"`
	Port      int               `json:"port"`
	Protocols []string          `json:"protocols"`
	AuthMode  AuthMode          `json:"authMode"`
	Metadata  map[string]string `json:"metadata"`
	Status    InstanceStatus    `json:"status"`
}

// Validate validates the AgentCard shape and enum values.
func (a AgentCard) Validate() error {
	if err := ValidateNamespaceName(a.Namespace); err != nil {
		return err
	}
	if err := ValidateServiceName(a.Service); err != nil {
		return err
	}
	if err := ValidateInstanceID(a.AgentID); err != nil {
		return err
	}
	if len(a.Capabilities) == 0 {
		return apperrors.New(apperrors.CodeInvalidArgument, "agent card must contain at least one capability")
	}
	for _, capability := range a.Capabilities {
		if err := ValidateCapabilityName(capability); err != nil {
			return err
		}
	}
	if len(a.Protocols) == 0 {
		return apperrors.New(apperrors.CodeInvalidArgument, "agent card must contain at least one protocol")
	}
	for _, protocol := range a.Protocols {
		if err := ValidateProtocolName(protocol); err != nil {
			return err
		}
	}
	if !a.AuthMode.Valid() {
		return apperrors.New(apperrors.CodeInvalidArgument, "agent authMode is invalid")
	}
	if err := ValidateFlatMetadata(a.Metadata); err != nil {
		return err
	}
	a.Capabilities = slices.Clone(a.Capabilities)
	a.Protocols = slices.Clone(a.Protocols)
	return nil
}

// Valid reports whether the auth mode is supported.
func (m AuthMode) Valid() bool {
	switch m {
	case AuthModeNone, AuthModeStaticToken, AuthModeMTLS:
		return true
	default:
		return false
	}
}
