// service_test.go verifies audit log recording and reverse chronological listing.
package audit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"etcdisc/internal/core/model"
	namespacesvc "etcdisc/internal/core/service/namespace"
	"etcdisc/test/testkit"
)

func TestRecordAndList(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	svc := NewService(store, namespacesvc.NewFixedClock(time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)))
	first, err := svc.Record(context.Background(), model.AuditEntry{Actor: "admin", Action: "create", Resource: "namespace", Message: "created prod-core"})
	require.NoError(t, err)
	second, err := svc.Record(context.Background(), model.AuditEntry{Actor: "admin", Action: "update", Resource: "namespace", Message: "updated prod-core"})
	require.NoError(t, err)

	entries, err := svc.List(context.Background())
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Equal(t, second.ID, entries[0].ID)
	require.Equal(t, first.ID, entries[1].ID)
}
