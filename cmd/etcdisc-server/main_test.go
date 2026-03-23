// main_test.go verifies the extracted run helper used by the CLI entrypoint.
package main

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	t.Parallel()

	original := bootstrapRun
	t.Cleanup(func() { bootstrapRun = original })

	bootstrapRun = func() error { return nil }
	require.NoError(t, run())

	bootstrapRun = func() error { return errors.New("boom") }
	require.EqualError(t, run(), "boom")
}
