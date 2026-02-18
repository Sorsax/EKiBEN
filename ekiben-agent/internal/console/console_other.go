//go:build !windows

package console

// EnableANSI is a no-op on non-Windows builds.
func EnableANSI() bool {
	return false
}

// SetTitle is a no-op on non-Windows builds.
func SetTitle(title string) {}

type ShutdownReason int

const (
	ShutdownCtrlC ShutdownReason = iota
	ShutdownClose
)

// RegisterShutdown is a no-op on non-Windows builds.
func RegisterShutdown(handler func(ShutdownReason)) {}

// EnsureSingleInstance always returns true on non-Windows builds.
func EnsureSingleInstance(name string) (bool, error) { return true, nil }
