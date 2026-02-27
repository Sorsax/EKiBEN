
package main


import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)


// Minimal WebView2 launcher using Edge WebView2Loader.dll via syscall
func launchWebView2(htmlPath string) {
	// 1. Initialize COM
	ole32 := syscall.NewLazyDLL("ole32.dll")
	coInitializeEx := ole32.NewProc("CoInitializeEx")
	coUninitialize := ole32.NewProc("CoUninitialize")
	hr, _, _ := coInitializeEx.Call(0, 2) // COINIT_APARTMENTTHREADED
	if hr != 0 {
		fmt.Fprintf(os.Stderr, "CoInitializeEx failed: 0x%X\n", hr)
		return
	}
	defer coUninitialize.Call()

	// 2. Load WebView2Loader.dll
	dll, err := syscall.LoadDLL("WebView2Loader.dll")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load WebView2Loader.dll: %v\n", err)
		return
	}
	defer dll.Release()

	// 3. Get CreateCoreWebView2EnvironmentWithOptions proc

	_, err = dll.FindProc("CreateCoreWebView2EnvironmentWithOptions")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to find CreateCoreWebView2EnvironmentWithOptions: %v\n", err)
		return
	}

	// 4. Create a fullscreen window (stub)
	hwnd := createFullscreenWindow()
	if hwnd == 0 {
		fmt.Fprintf(os.Stderr, "Failed to create fullscreen window\n")
		return
	}

	// 5. Call CreateCoreWebView2EnvironmentWithOptions (stub)
	// TODO: Implement COM callback and environment creation
	fmt.Println("Would call CreateCoreWebView2EnvironmentWithOptions and embed WebView2 in window", hwnd)

	// 6. Message loop (stub)
	runMessageLoop()
}

// createFullscreenWindow creates a borderless fullscreen window and returns its HWND
func createFullscreenWindow() uintptr {
	user32 := syscall.NewLazyDLL("user32.dll")
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getModuleHandleW := kernel32.NewProc("GetModuleHandleW")
	registerClassExW := user32.NewProc("RegisterClassExW")
	createWindowExW := user32.NewProc("CreateWindowExW")
	showWindow := user32.NewProc("ShowWindow")
	getSystemMetrics := user32.NewProc("GetSystemMetrics")

	type WNDCLASSEXW struct {
		CbSize        uint32
		Style         uint32
		LpfnWndProc   uintptr
		CbClsExtra    int32
		CbWndExtra    int32
		HInstance     uintptr
		HIcon         uintptr
		HCursor       uintptr
		HbrBackground uintptr
		LpszMenuName  *uint16
		LpszClassName *uint16
		HIconSm       uintptr
	}

	wndProc := syscall.NewCallback(func(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
		switch msg {
		case 2: // WM_DESTROY
			postQuitMessage := user32.NewProc("PostQuitMessage")
			postQuitMessage.Call(0)
			return 0
		}
		defWindowProcW := user32.NewProc("DefWindowProcW")
		ret, _, _ := defWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
		return ret
	})

	className := syscall.StringToUTF16Ptr("EKiBEN_WebView2Win")
	hInstance, _, _ := getModuleHandleW.Call(0)
	wndClass := WNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		Style:         0,
		LpfnWndProc:   wndProc,
		CbClsExtra:    0,
		CbWndExtra:    0,
		HInstance:     hInstance,
		HIcon:         0,
		HCursor:       0,
		HbrBackground: 0,
		LpszMenuName:  nil,
		LpszClassName: className,
		HIconSm:       0,
	}
	registerClassExW.Call(uintptr(unsafe.Pointer(&wndClass)))

	SM_CXSCREEN := 0
	SM_CYSCREEN := 1
	screenW, _, _ := getSystemMetrics.Call(uintptr(SM_CXSCREEN))
	screenH, _, _ := getSystemMetrics.Call(uintptr(SM_CYSCREEN))

	WS_POPUP := uintptr(0x80000000)
	WS_VISIBLE := uintptr(0x10000000)
	hwnd, _, _ := createWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("EKiBEN Shell WebView2"))),
		WS_POPUP|WS_VISIBLE,
		0, 0, screenW, screenH,
		0, 0, hInstance, 0,
	)
	SW_SHOWMAXIMIZED := uintptr(3)
	showWindow.Call(hwnd, SW_SHOWMAXIMIZED)
	return hwnd
}

// runMessageLoop runs the Windows message loop
func runMessageLoop() {
	user32 := syscall.NewLazyDLL("user32.dll")
	getMessage := user32.NewProc("GetMessageW")
	translateMessage := user32.NewProc("TranslateMessage")
	dispatchMessage := user32.NewProc("DispatchMessageW")
	type MSG struct {
		hwnd    uintptr
		message uint32
		wParam  uintptr
		lParam  uintptr
		time    uint32
		pt      struct{ x, y int32 }
	}
	var msg MSG
	for {
		ret, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) == 0 {
			break
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		dispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
}
