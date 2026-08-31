# GUI 强制改密前端：不可取消改密弹窗 + 顶行自助改密入口 + api() 全局 403 钩子

日期：2026-08-31

强制改密链路在 GUI 前端闭环，全部改动集中在 `web/app.js`。`api()` fetch 封装新增全局 403 钩子：任何页面收到 okserver 下发的 `{"error":"must_change_password"}` 都就地弹强制改密框，错误文案换成可读的"必须设置新密码"，调用方 catch 里不再露出 `must_change_password` 原文。新增 `srvChangePwdModal(force)` 弹窗：force=true 时无取消按钮、点遮罩不关，并带 `SRV.pwdModalOpen` 防重入（全局钩子可能并发触发多次）；force=false 为顶行"修改我的密码"入口打开的自助改密框，可取消。弹窗本地先校验三项（新密码不足 8 位 / 两次输入不一致 / 新旧相同）再调 `POST /api/server/change-password`，成功后清 `SRV.user.must_change_password`、关框、toast"密码已修改"。

三个触发点分工：`loadServer` 补拉 me 时若带标记立即弹框（刷新页面场景）；`srvLogin` 成功路径在 toast"登录成功"前检查标记弹框（新登录场景）；全局钩子兜底"管理员中途重置密码、旧会话再操作"的场景。`srvForcePwdModal()` 作为共用入口，仅在已登录（`SRV.user` 非空）时弹框，避免登录页上误弹。i18n 新增 9 个键 zh/en 双语同步（srvChangePwd / srvOldPwd / srvConfirmPwd / srvForcePwdTitle / srvForcePwdHint / srvPwdMismatch / srvPwdSame / srvPwdChanged / srvChangePwdFail），复用既有 srvNewPwd / srvPwdTooShort / fCancel。

前端无测试框架，验证靠 `node --check web/app.js` 语法检查与 `go build ./...` / `go vet ./...` / `go test ./...` 确认 Go 侧无恙；真机走查（初始密码登录弹强制框、本地校验分支、改密后恢复、重置密码后旧会话全局钩子触发）按简报清单人工执行。
