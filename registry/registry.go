// Package registry provides a registry of components typically used by testing
// to inject alternate implementations for testing purposes.
package registry

import "time"

var (
	manualTicks map[string]chan time.Time
)

func init() {
	manualTicks = map[string]chan time.Time{}
}

// CreateManualTick returns a named time.Tick-like channel that only ticks when
// a time.Time is sent to it.
func CreateManualTick(name string) chan<- time.Time {
	tick := make(chan time.Time)
	manualTicks[name] = tick
	return tick
}

// GetTick returns a named time.Tick-like channel that only ticks when its
// creator manually sends it a time.Time. If CreateManualTick hasn't already
// been called for the name, it returns a regular time.Tick(d).
func GetTick(name string, d time.Duration) <-chan time.Time {
	if tick, ok := manualTicks[name]; ok {
		return tick
	}

	return time.Tick(d)
}
