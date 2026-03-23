// watch_event.go defines the generic watch event envelope shared by registry and config streams.
package model

import "encoding/json"

// WatchEventType identifies the type of change sent to watch consumers.
type WatchEventType string

const (
	// WatchEventPut signals a create or update event.
	WatchEventPut WatchEventType = "put"
	// WatchEventDelete signals a resource deletion.
	WatchEventDelete WatchEventType = "delete"
	// WatchEventReset signals that the client must reload a full snapshot.
	WatchEventReset WatchEventType = "reset"
)

// WatchEvent is a generic event envelope with JSON payload data.
type WatchEvent struct {
	Type     WatchEventType  `json:"type"`
	Resource string          `json:"resource"`
	Key      string          `json:"key"`
	Revision int64           `json:"revision"`
	Value    json.RawMessage `json:"value,omitempty"`
}
