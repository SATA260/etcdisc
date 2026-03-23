// validation.go centralizes naming, enum, and flat metadata validation rules for phase 1 resources.
package model

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	apperrors "etcdisc/internal/core/errors"
)

var (
	namespaceNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]$`)
	serviceNamePattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]$`)
	instanceIDPattern    = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
	configKeyPattern     = regexp.MustCompile(`^[a-z0-9._-]{1,128}$`)
	capabilityPattern    = regexp.MustCompile(`^[a-z0-9.-]{1,128}$`)
	protocolPattern      = regexp.MustCompile(`^[a-z0-9._-]{1,32}$`)
	metadataKeyPattern   = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
)

// ValidateNamespaceName checks namespace naming rules.
func ValidateNamespaceName(name string) error {
	if !namespaceNamePattern.MatchString(name) {
		return apperrors.New(apperrors.CodeInvalidArgument, "namespace must match [a-z0-9-] with length 3-64")
	}
	return nil
}

// ValidateServiceName checks service naming rules.
func ValidateServiceName(name string) error {
	if !serviceNamePattern.MatchString(name) {
		return apperrors.New(apperrors.CodeInvalidArgument, "service must match [a-z0-9-] with length 3-64")
	}
	return nil
}

// ValidateInstanceID checks the instance identifier format.
func ValidateInstanceID(id string) error {
	if !instanceIDPattern.MatchString(id) {
		return apperrors.New(apperrors.CodeInvalidArgument, "instanceId must match [A-Za-z0-9._-] with length 1-128")
	}
	return nil
}

// ValidateConfigKey checks config key naming rules.
func ValidateConfigKey(key string) error {
	if !configKeyPattern.MatchString(key) {
		return apperrors.New(apperrors.CodeInvalidArgument, "config key must match [a-z0-9._-] with length 1-128")
	}
	return nil
}

// ValidateCapabilityName checks capability naming rules.
func ValidateCapabilityName(name string) error {
	if !capabilityPattern.MatchString(name) {
		return apperrors.New(apperrors.CodeInvalidArgument, "capability must match [a-z0-9.-] with length 1-128")
	}
	return nil
}

// ValidateProtocolName checks a protocol token used by AgentCard definitions.
func ValidateProtocolName(name string) error {
	if !protocolPattern.MatchString(name) {
		return apperrors.New(apperrors.CodeInvalidArgument, "protocol must match [a-z0-9._-] with length 1-32")
	}
	return nil
}

// ValidateFlatMetadata checks that metadata only contains flat string values and stable keys.
func ValidateFlatMetadata(metadata map[string]string) error {
	for key, value := range metadata {
		if !metadataKeyPattern.MatchString(key) {
			return apperrors.New(apperrors.CodeInvalidArgument, fmt.Sprintf("metadata key %q is invalid", key))
		}
		if strings.ContainsRune(value, '\x00') {
			return apperrors.New(apperrors.CodeInvalidArgument, fmt.Sprintf("metadata value %q contains invalid null byte", key))
		}
	}
	return nil
}

// CopyStringMap copies a flat metadata map into a deterministic new map.
func CopyStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return map[string]string{}
	}
	target := make(map[string]string, len(source))
	keys := make([]string, 0, len(source))
	for key := range source {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		target[key] = source[key]
	}
	return target
}
