// runtime_control.go defines cluster runtime control records such as worker members and assignment leader state.
package model

import "time"

// WorkerMember represents one etcdisc worker registered in the runtime member set.
type WorkerMember struct {
	NodeID    string    `json:"nodeId"`
	HTTPAddr  string    `json:"httpAddr"`
	GRPCAddr  string    `json:"grpcAddr"`
	StartedAt time.Time `json:"startedAt"`
	Revision  int64     `json:"revision"`
}

// AssignmentLeader represents the current cluster assignment leader record.
type AssignmentLeader struct {
	NodeID    string    `json:"nodeId"`
	Epoch     int64     `json:"epoch"`
	ElectedAt time.Time `json:"electedAt"`
	Revision  int64     `json:"revision"`
}

// ServiceOwner represents the authoritative runtime owner for one namespace and service pair.
type ServiceOwner struct {
	Namespace             string    `json:"namespace"`
	Service               string    `json:"service"`
	OwnerNodeID           string    `json:"ownerNodeId"`
	Epoch                 int64     `json:"epoch"`
	AssignedByLeaderEpoch int64     `json:"assignedByLeaderEpoch"`
	Reason                string    `json:"reason"`
	AssignedAt            time.Time `json:"assignedAt"`
	ExpiresAt             time.Time `json:"expiresAt"`
	Revision              int64     `json:"revision"`
}

// ServiceSeed records the first ingress node that observed a service.
type ServiceSeed struct {
	Namespace     string    `json:"namespace"`
	Service       string    `json:"service"`
	IngressNodeID string    `json:"ingressNodeId"`
	CreatedAt     time.Time `json:"createdAt"`
	FirstRevision int64     `json:"firstRevision"`
	Revision      int64     `json:"revision"`
}
