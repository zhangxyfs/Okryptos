//go:build windows

package daemon

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var procFindWindowW = windows.NewLazySystemDLL("user32.dll").NewProc("FindWindowW")

// okmeterRunning 判定 OkMeter 是否在运行：FindWindowW 按窗口类名 OkMeterDock、
// 标题 OkMeter（见 okmeter/ui/app.cpp）查顶层窗口，命中即在运行。
func okmeterRunning() bool {
	class, err := windows.UTF16PtrFromString("OkMeterDock")
	if err != nil {
		return false
	}
	title, err := windows.UTF16PtrFromString("OkMeter")
	if err != nil {
		return false
	}
	h, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)))
	return h != 0
}
