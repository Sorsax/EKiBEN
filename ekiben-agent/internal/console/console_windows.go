//go:build windows

package console

import (
	"unsafe"

	"golang.org/x/sys/windows"
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
