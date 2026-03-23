// clock.go provides overridable time helpers for services and tests.
package clock

import "time"

// Clock exposes the current time.
type Clock interface {
	Now() time.Time
}

// RealClock returns the actual wall clock time.
type RealClock struct{}

// Now returns the current UTC time.
func (RealClock) Now() time.Time {
	return time.Now().UTC()
}
