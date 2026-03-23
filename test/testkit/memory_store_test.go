// memory_store_test.go verifies all in-memory store operations used by service tests.
package testkit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	apperrors "etcdisc/internal/core/errors"
	"etcdisc/internal/core/port"
)

func TestMemoryStoreCRUDCASAndLease(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, store.Status(ctx))

	rev1, err := store.Create(ctx, "/items/1", []byte("one"), 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), rev1)

	_, err = store.Create(ctx, "/items/1", []byte("dup"), 0)
	require.Equal(t, apperrors.CodeAlreadyExists, apperrors.CodeOf(err))

	record, err := store.Get(ctx, "/items/1")
	require.NoError(t, err)
	require.Equal(t, []byte("one"), record.Value)

	rev2, err := store.Put(ctx, "/items/2", []byte("two"), 0)
	require.NoError(t, err)
	require.Greater(t, rev2, rev1)

	items, err := store.List(ctx, "/items/")
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, "/items/1", items[0].Key)

	_, err = store.CAS(ctx, "/items/1", rev1+100, []byte("bad"), 0)
	require.Equal(t, apperrors.CodeConflict, apperrors.CodeOf(err))
	rev3, err := store.CAS(ctx, "/items/1", rev1, []byte("one-new"), 0)
	require.NoError(t, err)
	require.Greater(t, rev3, rev2)

	_, err = store.DeleteCAS(ctx, "/items/1", rev1)
	require.Equal(t, apperrors.CodeConflict, apperrors.CodeOf(err))
	rev4, err := store.DeleteCAS(ctx, "/items/1", rev3)
	require.NoError(t, err)
	require.Greater(t, rev4, rev3)

	_, err = store.Delete(ctx, "/items/2")
	require.NoError(t, err)
	_, err = store.Delete(ctx, "/items/missing")
	require.Equal(t, apperrors.CodeNotFound, apperrors.CodeOf(err))

	leaseID, err := store.GrantLease(ctx, 10)
	require.NoError(t, err)
	require.NoError(t, store.KeepAliveOnce(ctx, leaseID))
	require.NoError(t, store.RevokeLease(ctx, leaseID))
	err = store.KeepAliveOnce(ctx, leaseID)
	require.Equal(t, apperrors.CodeNotFound, apperrors.CodeOf(err))

	_, err = store.Get(ctx, "/items/missing")
	require.Equal(t, apperrors.CodeNotFound, apperrors.CodeOf(err))
}

func TestMemoryStoreWatch(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	ctx, cancel := context.WithCancel(context.Background())
	watchCh := store.Watch(ctx, "/watch/", 0)

	_, err := store.Create(context.Background(), "/watch/1", []byte("one"), 0)
	require.NoError(t, err)
	event := <-watchCh
	require.Equal(t, port.EventPut, event.Type)
	require.Equal(t, "/watch/1", event.Key)

	_, err = store.Delete(context.Background(), "/watch/1")
	require.NoError(t, err)
	event = <-watchCh
	require.Equal(t, port.EventDelete, event.Type)

	cancel()
	select {
	case _, ok := <-watchCh:
		require.False(t, ok)
	case <-time.After(2 * time.Second):
		t.Fatal("watch channel did not close after cancellation")
	}
}
