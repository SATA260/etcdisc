// hash.go implements rendezvous hashing used by the assignment leader to choose service owners.
package cluster

import (
	"hash/fnv"
	"sort"
)

// ChooseOwner selects one node from the provided member IDs using rendezvous hashing.
func ChooseOwner(serviceKey string, memberIDs []string) string {
	if len(memberIDs) == 0 {
		return ""
	}
	bestNode := memberIDs[0]
	bestScore := score(serviceKey, bestNode)
	for _, memberID := range memberIDs[1:] {
		currentScore := score(serviceKey, memberID)
		if currentScore > bestScore || currentScore == bestScore && memberID < bestNode {
			bestNode = memberID
			bestScore = currentScore
		}
	}
	return bestNode
}

// SortedMemberIDs returns a deterministic member ID list from the current member view.
func SortedMemberIDs(members map[string]struct{}) []string {
	ids := make([]string, 0, len(members))
	for memberID := range members {
		ids = append(ids, memberID)
	}
	sort.Strings(ids)
	return ids
}

func score(left, right string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(left))
	_, _ = h.Write([]byte("#"))
	_, _ = h.Write([]byte(right))
	return h.Sum64()
}
