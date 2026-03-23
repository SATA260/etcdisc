// logger.go creates the structured logger used across the etcdisc server skeleton.
package logging

import (
	"log/slog"
	"os"
)

// New returns a JSON structured logger.
func New() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, nil))
}
