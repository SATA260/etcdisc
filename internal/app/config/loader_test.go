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
admin:
  token: from-yaml
`), 0o600))

	t.Setenv("ETCDISC_CONFIG_FILE", configPath)
	t.Setenv("ETCDISC_HTTP_PORT", "28080")
	t.Setenv("ETCDISC_ETCD_ENDPOINTS", "127.0.0.1:32379,127.0.0.1:42379")
	t.Setenv("ETCDISC_ADMIN_TOKEN", "from-env")

	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, "etcdisc-test", cfg.App.Name)
	require.Equal(t, "127.0.0.1", cfg.HTTP.Host)
	require.Equal(t, 28080, cfg.HTTP.Port)
	require.Equal(t, 19090, cfg.GRPC.Port)
	require.Equal(t, []string{"127.0.0.1:32379", "127.0.0.1:42379"}, cfg.Etcd.Endpoints)
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
	require.NotEmpty(t, cfg.Admin.Token)
}
