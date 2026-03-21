// bootstrap_test.go verifies bootstrap error paths for config loading and listener startup.
package bootstrap

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunFailsOnInvalidEnvOverride(t *testing.T) {
	t.Setenv("ETCDISC_HTTP_PORT", "bad-port")
	require.Error(t, Run())
}

func TestRunFailsWhenGRPCPortIsOccupied(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "etcdisc.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(fmt.Sprintf(`app:
  name: etcdisc
  env: test
http:
  host: 127.0.0.1
  port: 18080
grpc:
  host: 127.0.0.1
  port: %s
etcd:
  endpoints:
    - 127.0.0.1:2379
  dialMS: 3000
admin:
  token: secret
`, portText)), 0o600))
	t.Setenv("ETCDISC_CONFIG_FILE", configPath)
	t.Setenv("ETCDISC_HTTP_PORT", "")
	t.Setenv("ETCDISC_GRPC_PORT", "")

	err = Run()
	require.Error(t, err)
	require.Contains(t, err.Error(), "bind")
}
