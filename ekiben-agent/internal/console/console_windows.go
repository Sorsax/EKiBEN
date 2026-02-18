//go:build windows

package console

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var shutdownHandler func()
var consoleCtrlHandler uintptr
var instanceHandle windows.Handle

type ShutdownReason int

const (
	ShutdownCtrlC ShutdownReason = iota
	ShutdownClose
)

// EnableANSI enables ANSI escape processing for the current console.
func EnableANSI() bool {
	h := windows.Handle(windows.Stdout)
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return false
	}
	mode |= windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	if err := windows.SetConsoleMode(h, mode); err != nil {
		return false
	}
	return true
}

// SetTitle sets the console window title.
func SetTitle(title string) {
	ptr, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	proc := kernel32.NewProc("SetConsoleTitleW")
	_, _, _ = proc.Call(uintptr(unsafe.Pointer(ptr)))
}

// RegisterShutdown registers a handler for console close events.
func RegisterShutdown(handler func(ShutdownReason)) {
	shutdownHandler = nil
	wrapped := func(reason ShutdownReason) {
		handler(reason)
	}
	_ = wrapped
	// Keep handler for callback
	shutdownHandler = func() {
		// default to close unless overridden in callback
		wrapped(ShutdownClose)
	}
	if consoleCtrlHandler == 0 {
		consoleCtrlHandler = windows.NewCallback(func(ctrlType uint32) uintptr {
			switch ctrlType {
			case windows.CTRL_C_EVENT:
				if handler != nil {
					handler(ShutdownCtrlC)
				}
				return 1
			case windows.CTRL_CLOSE_EVENT, windows.CTRL_LOGOFF_EVENT, windows.CTRL_SHUTDOWN_EVENT:
				if handler != nil {
					handler(ShutdownClose)
				}
				return 1
			default:
				return 0
			}
		})
	}
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	proc := kernel32.NewProc("SetConsoleCtrlHandler")
	_, _, _ = proc.Call(consoleCtrlHandler, 1)
}

// EnsureSingleInstance returns true if this is the first instance.
func EnsureSingleInstance(name string) (bool, error) {
	ptr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return false, err
	}
	h, err := windows.CreateMutex(nil, false, ptr)
	if err != nil {
		if err == windows.ERROR_ALREADY_EXISTS {
			return false, nil
		}
		return false, err
	}
	if windows.GetLastError() == windows.ERROR_ALREADY_EXISTS {
		_ = windows.CloseHandle(h)
		return false, nil
	}
	instanceHandle = h
	return true, nil
}
