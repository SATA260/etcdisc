// wiring_test.go verifies dependency construction and key HTTP routes against local etcd.
package wiring

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	appconfig "etcdisc/internal/app/config"
	"go.etcd.io/etcd/server/v3/embed"
)

func TestBuildAndClose(t *testing.T) {
	endpoint, stopEtcd := startEmbeddedEtcd(t)
	defer stopEtcd()

	cfg := appconfig.Default()
	cfg.Etcd.Endpoints = []string{endpoint}
	cfg.Admin.Token = "secret"
	deps, err := Build(cfg)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()
	require.NotNil(t, deps.Router)
	require.NotNil(t, deps.Logger)
	require.NotNil(t, deps.GRPCServer)
	require.NotNil(t, deps.ETCDClient)
	require.NotNil(t, deps.ReadyCheck)
	require.NoError(t, deps.ReadyCheck(context.Background()))

	resp := httptest.NewRecorder()
	deps.Router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	require.Equal(t, http.StatusOK, resp.Code)

	resp = httptest.NewRecorder()
	deps.Router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/ready", nil))
	require.Equal(t, http.StatusOK, resp.Code)

	resp = httptest.NewRecorder()
	deps.Router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/admin/v1/system", nil))
	require.Equal(t, http.StatusUnauthorized, resp.Code)

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/system", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp = httptest.NewRecorder()
	deps.Router.ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code)

	require.NoError(t, deps.Close())
}

func TestBuildWithClusterEnabled(t *testing.T) {
	endpoint, stopEtcd := startEmbeddedEtcd(t)
	defer stopEtcd()

	cfg := appconfig.Default()
	cfg.Etcd.Endpoints = []string{endpoint}
	cfg.Admin.Token = "secret"
	cfg.Cluster.Enabled = true
	cfg.Cluster.NodeID = "node-test"
	deps, err := Build(cfg)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()
	require.NotNil(t, deps.Cluster)
	require.NotNil(t, deps.Heartbeat)
	require.NotNil(t, deps.Probe)
	require.Eventually(t, func() bool {
		members := deps.Cluster.Members()
		return len(members) == 1 && members[0].NodeID == "node-test"
	}, 10*time.Second, 100*time.Millisecond)
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/system", nil)
	req.Header.Set("Authorization", "Bearer secret")
	deps.Router.ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code)
}

func TestWiringHelpers(t *testing.T) {
	t.Parallel()
	require.Equal(t, "a", firstNonEmpty("", "a", "b"))
	require.Equal(t, "", firstNonEmpty())
	require.Equal(t, "127.0.0.1:8080", normalizeAdvertiseAddr("0.0.0.0", 8080))
	require.Equal(t, "127.0.0.1:8080", normalizeAdvertiseAddr("", 8080))
	require.Equal(t, "127.0.0.1:8080", normalizeAdvertiseAddr("127.0.0.1", 8080))
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
	return cfg.ListenClientUrls[0].String(), func() { e.Close() }
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
