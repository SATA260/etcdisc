// client_test.go verifies etcd client construction from application config.
package etcd

import (
	"testing"

	"github.com/stretchr/testify/require"

	appconfig "etcdisc/internal/app/config"
)

func TestNewClient(t *testing.T) {
	t.Parallel()

	_, err := NewClient(appconfig.Config{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "etcd endpoints are empty")

	client, err := NewClient(appconfig.Default())
	require.NoError(t, err)
	require.NotNil(t, client)
	require.NoError(t, client.Close())
}
