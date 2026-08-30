// Package webui 内嵌 okdeploy 的前端静态资源。
package webui

import (
	"embed"
	"io/fs"
)

//go:embed web
var content embed.FS

// WebFS 返回 web 子目录的文件系统视图。
func WebFS() fs.FS {
	sub, err := fs.Sub(content, "web")
	if err != nil {
		panic(err)
	}
	return sub
}
