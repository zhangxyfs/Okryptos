//go:build !windows

package daemon

// okmeterRunning 非 Windows 平台无 OkMeter，恒 false；OkMeter.exe 不存在时
// 保活自然空转（decideLaunchOKMeter 的 exeExists 分支收口），不报错。
func okmeterRunning() bool { return false }
