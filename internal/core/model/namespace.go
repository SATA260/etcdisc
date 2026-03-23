// namespace.go defines the namespace resource and access mode used to isolate phase 1 resources.
package model

import (
	"time"

	apperrors "etcdisc/internal/core/errors"
)

// NamespaceAccessMode describes namespace read and write permissions.
type NamespaceAccessMode string

const (
	// NamespaceAccessReadWrite allows both business reads and writes.
	NamespaceAccessReadWrite NamespaceAccessMode = "read_write"
	// NamespaceAccessReadOnly allows reads but blocks writes.
	NamespaceAccessReadOnly NamespaceAccessMode = "read_only"
	// NamespaceAccessWriteOnly allows writes but blocks reads.
	NamespaceAccessWriteOnly NamespaceAccessMode = "write_only"
	// NamespaceAccessNoReadNoWrite blocks both reads and writes.
	NamespaceAccessNoReadNoWrite NamespaceAccessMode = "no_read_no_write"
)

// Namespace represents a logical isolation boundary in etcdisc.
type Namespace struct {
	Name       string              `json:"name"`
	AccessMode NamespaceAccessMode `json:"accessMode"`
	CreatedAt  time.Time           `json:"createdAt"`
	UpdatedAt  time.Time           `json:"updatedAt"`
	Revision   int64               `json:"revision"`
}

// Validate validates namespace fields.
func (n Namespace) Validate() error {
	if err := ValidateNamespaceName(n.Name); err != nil {
		return err
	}
	if !n.AccessMode.Valid() {
		return apperrors.New(apperrors.CodeInvalidArgument, "namespace accessMode is invalid")
	}
	return nil
}

// Valid reports whether the namespace access mode is supported.
func (m NamespaceAccessMode) Valid() bool {
	switch m {
	case NamespaceAccessReadWrite, NamespaceAccessReadOnly, NamespaceAccessWriteOnly, NamespaceAccessNoReadNoWrite:
		return true
	default:
		return false
	}
}

// AllowsRead reports whether business read requests may access the namespace.
func (m NamespaceAccessMode) AllowsRead() bool {
	switch m {
	case NamespaceAccessReadWrite, NamespaceAccessReadOnly:
		return true
	default:
		return false
	}
}

// AllowsWrite reports whether business write requests may modify namespace resources.
func (m NamespaceAccessMode) AllowsWrite() bool {
	switch m {
	case NamespaceAccessReadWrite, NamespaceAccessWriteOnly:
		return true
	default:
		return false
	}
}
