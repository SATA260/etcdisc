// service_test.go verifies namespace lifecycle, CAS updates, and access-mode enforcement.
package namespace

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	apperrors "etcdisc/internal/core/errors"
	"etcdisc/internal/core/model"
	"etcdisc/test/testkit"
)

func TestNamespaceLifecycle(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	svc := NewService(store, NewFixedClock(time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)))
	ctx := context.Background()

	created, err := svc.Create(ctx, CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	require.Equal(t, model.NamespaceAccessReadWrite, created.AccessMode)
	require.NotZero(t, created.Revision)

	listed, err := svc.List(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, created.Name, listed[0].Name)

	updated, err := svc.UpdateAccessMode(ctx, UpdateAccessModeInput{
		Name:             created.Name,
		AccessMode:       model.NamespaceAccessReadOnly,
		ExpectedRevision: created.Revision,
	})
	require.NoError(t, err)
	require.Equal(t, model.NamespaceAccessReadOnly, updated.AccessMode)
	require.Greater(t, updated.Revision, created.Revision)
}

func TestNamespaceUpdateRequiresMatchingRevision(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	svc := NewService(store, NewFixedClock(time.Now()))
	ctx := context.Background()

	created, err := svc.Create(ctx, CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)

	_, err = svc.UpdateAccessMode(ctx, UpdateAccessModeInput{
		Name:             created.Name,
		AccessMode:       model.NamespaceAccessReadOnly,
		ExpectedRevision: created.Revision + 100,
	})
	require.Error(t, err)
	require.Equal(t, apperrors.CodeConflict, apperrors.CodeOf(err))
}

func TestNamespaceAccessChecks(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	svc := NewService(store, NewFixedClock(time.Now()))
	ctx := context.Background()

	created, err := svc.Create(ctx, CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)

	_, err = svc.UpdateAccessMode(ctx, UpdateAccessModeInput{
		Name:             created.Name,
		AccessMode:       model.NamespaceAccessWriteOnly,
		ExpectedRevision: created.Revision,
	})
	require.NoError(t, err)

	require.Error(t, svc.CheckRead(ctx, created.Name, false))
	require.NoError(t, svc.CheckWrite(ctx, created.Name, false))
	require.NoError(t, svc.CheckRead(ctx, created.Name, true))
	require.NoError(t, svc.CheckWrite(ctx, created.Name, true))
}
