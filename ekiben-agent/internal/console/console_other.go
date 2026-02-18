//go:build !windows

package console

// EnableANSI is a no-op on non-Windows builds.
func EnableANSI() bool {
	return false
}

// SetTitle is a no-op on non-Windows builds.
func SetTitle(title string) {}

// RegisterShutdown is a no-op on non-Windows builds.
func RegisterShutdown(handler func()) {}

// EnsureSingleInstance always returns true on non-Windows builds.
func EnsureSingleInstance(name string) (bool, error) { return true, nil }
