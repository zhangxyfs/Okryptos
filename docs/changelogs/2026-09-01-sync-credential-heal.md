# 同步遇 git 认证失败自愈（token 重发 + 凭据刷新 + 重试）

日期：2026-09-01

修复服务端 git token 失效后客户端永久同步失败的漏洞（Linux 实证：`Authentication failed` 反复报错，重拉取无效）。根因：绑定管线用 `HasStoredCredential` 判断是否需重发 token——它只认"本机 helper 有记录"不认"凭据仍有效"；token 在服务端被删除/改密码/同名重发顶掉后，重新拉取因"有记录"跳过重发，git 持死凭据推送永久失败，GUI 无自愈路径。

现在 `POST /api/project/sync` 与绑定管线首次同步遇 git 认证失败（`Authentication failed` / `could not read Username`）时自动兜底：经 `POST /api/v1/git-token` 自助重发 token → 刷新凭据（有 credential helper 写 helper，无 helper 重新内嵌 remote URL 并落盘 config.toml）→ 重试一次。非认证类失败（网络不可达等）不触发重发。

新增 `syncx.StripURLAuth`（剥 URL 内嵌 userinfo，供凭据刷新取净 remote）。

测试：`TestIsGitAuthFailure`（4 条文案判定 + nil）、`TestApiProjectSyncCredentialHeal`（恒 401 假 Gitea + git-token 计数 + 断言新 token 内嵌落盘、重试携带新凭据）、`TestApiProjectSyncNoHealOnNetError`（网络错误不重发）、`TestStripURLAuth`（4 条形态）。
