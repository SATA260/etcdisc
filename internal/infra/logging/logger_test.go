// logger_test.go verifies the logger factory returns a usable structured logger.
package logging

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Parallel()

	logger := New()
	require.NotNil(t, logger)
	logger.Info("logger smoke test")
}
