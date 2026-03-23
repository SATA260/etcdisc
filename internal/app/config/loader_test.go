// loader_test.go verifies YAML loading and environment variable overrides for server config.
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadReadsYAMLAndEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "etcdisc.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(`app:
  name: etcdisc-test
  env: local
http:
  host: 127.0.0.1
  port: 18080
grpc:
  host: 127.0.0.1
  port: 19090
etcd:
  endpoints:
    - 127.0.0.1:22379
  dialMS: 5000
cluster:
  enabled: true
  nodeId: node-yaml
  advertiseHTTPAddr: 10.0.0.1:18080
  advertiseGRPCAddr: 10.0.0.1:19090
  memberTTLSeconds: 40
  memberKeepAliveSeconds: 12
  leaderTTLSeconds: 35
  leaderKeepAliveSeconds: 11
admin:
  token: from-yaml
`), 0o600))

	t.Setenv("ETCDISC_CONFIG_FILE", configPath)
	t.Setenv("ETCDISC_HTTP_PORT", "28080")
	t.Setenv("ETCDISC_ETCD_ENDPOINTS", "127.0.0.1:32379,127.0.0.1:42379")
	t.Setenv("ETCDISC_CLUSTER_NODE_ID", "node-env")
	t.Setenv("ETCDISC_CLUSTER_MEMBER_TTL_SECONDS", "50")
	t.Setenv("ETCDISC_ADMIN_TOKEN", "from-env")

	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, "etcdisc-test", cfg.App.Name)
	require.Equal(t, "127.0.0.1", cfg.HTTP.Host)
	require.Equal(t, 28080, cfg.HTTP.Port)
	require.Equal(t, 19090, cfg.GRPC.Port)
	require.Equal(t, []string{"127.0.0.1:32379", "127.0.0.1:42379"}, cfg.Etcd.Endpoints)
	require.True(t, cfg.Cluster.Enabled)
	require.Equal(t, "node-env", cfg.Cluster.NodeID)
	require.Equal(t, 50, cfg.Cluster.MemberTTLSeconds)
	require.Equal(t, "from-env", cfg.Admin.Token)
}

func TestLoadUsesDefaultsWithoutFile(t *testing.T) {
	t.Setenv("ETCDISC_CONFIG_FILE", filepath.Join(t.TempDir(), "missing.yaml"))

	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, "etcdisc", cfg.App.Name)
	require.Equal(t, 8080, cfg.HTTP.Port)
	require.Equal(t, 9090, cfg.GRPC.Port)
	require.NotEmpty(t, cfg.Etcd.Endpoints)
	require.False(t, cfg.Cluster.Enabled)
	require.Equal(t, 30, cfg.Cluster.MemberTTLSeconds)
	require.NotEmpty(t, cfg.Admin.Token)
}

func TestLoadRejectsInvalidBoolAndIntOverrides(t *testing.T) {
	t.Setenv("ETCDISC_CONFIG_FILE", filepath.Join(t.TempDir(), "missing.yaml"))
	t.Setenv("ETCDISC_CLUSTER_ENABLED", "not-bool")
	_, err := Load()
	require.Error(t, err)

	t.Setenv("ETCDISC_CLUSTER_ENABLED", "true")
	t.Setenv("ETCDISC_CLUSTER_MEMBER_TTL_SECONDS", "bad-int")
	_, err = Load()
	require.Error(t, err)
}

func TestHelperParsing(t *testing.T) {
	t.Setenv("ETCDISC_CLUSTER_ENABLED", "true")
	parsed, ok, err := parseBoolEnv("ETCDISC_CLUSTER_ENABLED")
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, parsed)

	t.Setenv("ETCDISC_CLUSTER_ENABLED", "")
	parsed, ok, err = parseBoolEnv("ETCDISC_CLUSTER_ENABLED")
	require.NoError(t, err)
	require.False(t, ok)
	require.False(t, parsed)

	require.Equal(t, []string{"a", "b", "c"}, splitAndTrim(" a, ,b, c "))
}
