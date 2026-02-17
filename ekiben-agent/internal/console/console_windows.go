//go:build windows

package console

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var shutdownHandler func()
var consoleCtrlHandler uintptr

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
func RegisterShutdown(handler func()) {
	shutdownHandler = handler
	if consoleCtrlHandler == 0 {
		consoleCtrlHandler = windows.NewCallback(func(ctrlType uint32) uintptr {
			switch ctrlType {
			case windows.CTRL_C_EVENT, windows.CTRL_CLOSE_EVENT, windows.CTRL_LOGOFF_EVENT, windows.CTRL_SHUTDOWN_EVENT:
				if shutdownHandler != nil {
					shutdownHandler()
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
