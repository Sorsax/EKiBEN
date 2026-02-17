//go:build !windows

package console

// EnableANSI is a no-op on non-Windows builds.
func EnableANSI() bool {
	return false
}

// SetTitle is a no-op on non-Windows builds.
func SetTitle(title string) {}
