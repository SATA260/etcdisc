// config_item.go defines typed configuration items, scopes, and effective config records for phase 1.
package model

import (
	"encoding/json"
	"strconv"
	"time"

	apperrors "etcdisc/internal/core/errors"
)

// ConfigScope identifies one layer in the config override chain.
type ConfigScope string

const (
	// ConfigScopeGlobal stores cluster-wide defaults.
	ConfigScopeGlobal ConfigScope = "global"
	// ConfigScopeNamespace stores namespace level overrides.
	ConfigScopeNamespace ConfigScope = "namespace"
	// ConfigScopeService stores namespace+service overrides.
	ConfigScopeService ConfigScope = "service"
)

// ConfigValueType identifies how a config value should be interpreted.
type ConfigValueType string

const (
	// ConfigValueString stores an arbitrary string.
	ConfigValueString ConfigValueType = "string"
	// ConfigValueInt stores a decimal integer.
	ConfigValueInt ConfigValueType = "int"
	// ConfigValueBool stores a boolean literal.
	ConfigValueBool ConfigValueType = "bool"
	// ConfigValueDuration stores duration values as integer milliseconds.
	ConfigValueDuration ConfigValueType = "duration"
	// ConfigValueJSON stores syntactically valid JSON strings.
	ConfigValueJSON ConfigValueType = "json"
)

// ConfigItem represents a typed configuration entry.
type ConfigItem struct {
	Namespace string          `json:"namespace,omitempty"`
	Service   string          `json:"service,omitempty"`
	Scope     ConfigScope     `json:"scope"`
	Key       string          `json:"key"`
	Value     string          `json:"value"`
	ValueType ConfigValueType `json:"valueType"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
	Revision  int64           `json:"revision"`
}

// EffectiveConfigItem represents the resolved config seen by business callers.
type EffectiveConfigItem struct {
	Key       string          `json:"key"`
	Value     string          `json:"value"`
	ValueType ConfigValueType `json:"valueType"`
	Scope     ConfigScope     `json:"scope"`
}

// Validate validates scope, target, and typed value rules.
func (c ConfigItem) Validate() error {
	if err := ValidateConfigKey(c.Key); err != nil {
		return err
	}
	if !c.Scope.Valid() {
		return apperrors.New(apperrors.CodeInvalidArgument, "config scope is invalid")
	}
	if c.Scope != ConfigScopeGlobal {
		if err := ValidateNamespaceName(c.Namespace); err != nil {
			return err
		}
	}
	if c.Scope == ConfigScopeService {
		if err := ValidateServiceName(c.Service); err != nil {
			return err
		}
	}
	if !c.ValueType.Valid() {
		return apperrors.New(apperrors.CodeInvalidArgument, "config valueType is invalid")
	}
	switch c.ValueType {
	case ConfigValueInt, ConfigValueDuration:
		if _, err := strconv.Atoi(c.Value); err != nil {
			return apperrors.New(apperrors.CodeInvalidArgument, "config value must be a base-10 integer")
		}
	case ConfigValueBool:
		if _, err := strconv.ParseBool(c.Value); err != nil {
			return apperrors.New(apperrors.CodeInvalidArgument, "config value must be a boolean")
		}
	case ConfigValueJSON:
		var payload any
		if err := json.Unmarshal([]byte(c.Value), &payload); err != nil {
			return apperrors.New(apperrors.CodeInvalidArgument, "config value must be valid JSON")
		}
	}
	return nil
}

// Valid reports whether the config scope is supported.
func (s ConfigScope) Valid() bool {
	switch s {
	case ConfigScopeGlobal, ConfigScopeNamespace, ConfigScopeService:
		return true
	default:
		return false
	}
}

// Valid reports whether the config value type is supported.
func (t ConfigValueType) Valid() bool {
	switch t {
	case ConfigValueString, ConfigValueInt, ConfigValueBool, ConfigValueDuration, ConfigValueJSON:
		return true
	default:
		return false
	}
}
