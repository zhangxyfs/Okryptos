//go:build !windows

package gui

// OpenPreferred 非 Windows 平台：内嵌窗口不可用，直接浏览器路径。
func OpenPreferred(url string) uintptr { return OpenBrowser(url) }
