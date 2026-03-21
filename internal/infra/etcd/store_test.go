// store_test.go exercises the etcd-backed store against a real local etcd instance.
package etcd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/server/v3/embed"

	appconfig "etcdisc/internal/app/config"
	apperrors "etcdisc/internal/core/errors"
	"etcdisc/internal/core/port"
)

func TestStoreRoundTripAndWatch(t *testing.T) {
	client, cleanup := mustEtcdClient(t)
	defer cleanup()
	store := NewStore(client)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	prefix := fmt.Sprintf("/coverage/etcd/%d/", time.Now().UnixNano())
	_, _ = client.Delete(ctx, prefix, clientv3.WithPrefix())

	require.NoError(t, store.Status(ctx))

	watchCtx, watchCancel := context.WithCancel(ctx)
	watchCh := store.Watch(watchCtx, prefix, 0)

	leaseID, err := store.GrantLease(ctx, 5)
	require.NoError(t, err)
	require.NoError(t, store.KeepAliveOnce(ctx, leaseID))

	key1 := prefix + "a"
	key2 := prefix + "b"
	rev1, err := store.Create(ctx, key1, []byte("one"), leaseID)
	require.NoError(t, err)
	_, err = store.Create(ctx, key1, []byte("dup"), 0)
	require.Equal(t, apperrors.CodeAlreadyExists, apperrors.CodeOf(err))

	event := <-watchCh
	require.Equal(t, port.EventPut, event.Type)
	require.Equal(t, key1, event.Key)

	rev2, err := store.Put(ctx, key2, []byte("two"), 0)
	require.NoError(t, err)
	require.Greater(t, rev2, rev1)

	record, err := store.Get(ctx, key1)
	require.NoError(t, err)
	require.Equal(t, []byte("one"), record.Value)
	require.Equal(t, leaseID, record.LeaseID)

	records, err := store.List(ctx, prefix)
	require.NoError(t, err)
	require.Len(t, records, 2)

	_, err = store.CAS(ctx, key1, rev1+100, []byte("bad"), 0)
	require.Equal(t, apperrors.CodeConflict, apperrors.CodeOf(err))
	rev3, err := store.CAS(ctx, key1, rev1, []byte("one-new"), 0)
	require.NoError(t, err)
	require.Greater(t, rev3, rev2)

	_, err = store.DeleteCAS(ctx, key1, rev1)
	require.Equal(t, apperrors.CodeConflict, apperrors.CodeOf(err))
	rev4, err := store.DeleteCAS(ctx, key1, rev3)
	require.NoError(t, err)
	require.Greater(t, rev4, rev3)

	seenDelete := false
	for range 4 {
		event = <-watchCh
		if event.Type == port.EventDelete && event.Key == key1 {
			seenDelete = true
			break
		}
	}
	require.True(t, seenDelete)

	_, err = store.Delete(ctx, key2)
	require.NoError(t, err)
	_, err = store.Delete(ctx, key2)
	require.Equal(t, apperrors.CodeNotFound, apperrors.CodeOf(err))

	_, err = store.Get(ctx, prefix+"missing")
	require.Equal(t, apperrors.CodeNotFound, apperrors.CodeOf(err))
	require.NoError(t, store.RevokeLease(ctx, leaseID))

	watchCancel()
	for range watchCh {
	}
}

func TestStoreHelpersAndErrorMapping(t *testing.T) {
	t.Parallel()

	require.Nil(t, withLease(0))
	require.Len(t, withLease(3), 1)
	require.NoError(t, mapEtcdError(nil, "ok"))
	require.Equal(t, apperrors.CodeNotFound, apperrors.CodeOf(mapEtcdError(rpctypes.ErrKeyNotFound, "missing")))
	require.Equal(t, apperrors.CodeFailedPrecondition, apperrors.CodeOf(mapEtcdError(rpctypes.ErrCompacted, "compacted")))
	require.Equal(t, apperrors.CodeUnavailable, apperrors.CodeOf(mapEtcdError(errors.New("boom"), "boom")))
}

func mustEtcdClient(t *testing.T) (*clientv3.Client, func()) {
	t.Helper()
	endpoint, stopEtcd := startEmbeddedEtcd(t)
	cfg := appconfig.Default()
	cfg.Etcd.Endpoints = []string{endpoint}
	client, err := NewClient(cfg)
	if err != nil {
		stopEtcd()
		t.Fatalf("etcd client unavailable: %v", err)
	}
	return client, func() {
		_ = client.Close()
		stopEtcd()
	}
}

func startEmbeddedEtcd(t *testing.T) (string, func()) {
	t.Helper()
	cfg := embed.NewConfig()
	cfg.Dir = filepath.Join(t.TempDir(), "etcd")
	cfg.Logger = "zap"
	cfg.LogLevel = "error"
	clientURL := mustURL(t, reserveAddress(t))
	peerURL := mustURL(t, reserveAddress(t))
	cfg.ListenClientUrls = []url.URL{clientURL}
	cfg.AdvertiseClientUrls = []url.URL{clientURL}
	cfg.ListenPeerUrls = []url.URL{peerURL}
	cfg.AdvertisePeerUrls = []url.URL{peerURL}
	cfg.InitialCluster = cfg.InitialClusterFromName(cfg.Name)
	e, err := embed.StartEtcd(cfg)
	require.NoError(t, err)
	select {
	case <-e.Server.ReadyNotify():
	case <-time.After(10 * time.Second):
		e.Server.Stop()
		t.Fatal("embedded etcd did not become ready in time")
	}
	return cfg.ListenClientUrls[0].String(), func() {
		e.Close()
	}
}

func reserveAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return listener.Addr().String()
}

func mustURL(t *testing.T, address string) url.URL {
	t.Helper()
	parsed, err := url.Parse("http://" + address)
	require.NoError(t, err)
	return *parsed
}
