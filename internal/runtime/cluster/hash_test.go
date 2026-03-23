// hash_test.go verifies rendezvous hash owner selection stability.
package cluster

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChooseOwnerIsStable(t *testing.T) {
	t.Parallel()

	members := []string{"node-a", "node-b", "node-c"}
	owner1 := ChooseOwner("prod/payment-api", members)
	owner2 := ChooseOwner("prod/payment-api", members)
	require.NotEmpty(t, owner1)
	require.Equal(t, owner1, owner2)
}

func TestSortedMemberIDs(t *testing.T) {
	t.Parallel()

	ids := SortedMemberIDs(map[string]struct{}{"node-c": {}, "node-a": {}, "node-b": {}})
	require.Equal(t, []string{"node-a", "node-b", "node-c"}, ids)
}
