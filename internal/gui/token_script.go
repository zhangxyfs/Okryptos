package gui

import "strconv"

// TokenInitScript 生成注入 WebView2 AddScriptToExecuteOnDocumentCreated 的脚本：
// 每次导航（含刷新）在页面脚本执行前写入 window.__okToken，替代 URL fragment。
// 仅在 okd origin 下注入——脚本对每个文档生效，导航离开本机地址后不得带出 token。
func TokenInitScript(origin, token string) string {
	return "if(location.origin===" + strconv.Quote(origin) + "){window.__okToken = " + strconv.Quote(token) + ";}"
}
