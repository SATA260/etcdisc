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
