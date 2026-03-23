// clock_test.go verifies the real clock returns UTC timestamps near now.
package clock

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRealClockNow(t *testing.T) {
	t.Parallel()

	now := RealClock{}.Now()
	require.Equal(t, time.UTC, now.Location())
	require.WithinDuration(t, time.Now().UTC(), now, 2*time.Second)
}
