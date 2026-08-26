package gui

import "strconv"

// TokenInitScript 生成注入 WebView2 AddScriptToExecuteOnDocumentCreated 的脚本：
// 每次导航（含刷新）在页面脚本执行前写入 window.__okToken，替代 URL fragment。
func TokenInitScript(token string) string {
	return "window.__okToken = " + strconv.Quote(token) + ";"
}
