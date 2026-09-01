# 统一 git 凭证为「每台机器一条」设计

日期：2026-09-01
状态：已定稿（待实施）

## 背景与问题

当前 git 凭证有三条发放路径，命名语义互相打架：

1. 建用户（管理员代建）：发固定名 `ok-sync`（`internal/oksrv/http.go:429`），随初始密码交给管理员。
2. 首次建仓：发 `ok-sync-<project>`（`internal/oksrv/http.go:266`），幂等重入返回空串。
3. 自助重发：按机器分名 `ok-sync-r-<hostname>`（`internal/oksrv/http.go:283`），v2.24.1 终审裁决的产物。

问题：Gitea token 是**账号级**的，不按仓授权——`ok-sync-<project>` 的"按项目吊销粒度"是假象（同机任意一条 token 都能推所有仓），唯一真实效果是凭证列表噪音。用户裁决：**全部统一为按机器分名，每台机器恰好一条**，凭证列表即"我的机器列表"。

## 关键事实（实施前已验证）

- 客户端绑定管线 `serveBindRepo`（`internal/gui/api_server.go:268-287`）已具备空 token 容错：provision 返回空 → 查本机已存凭据 → 没有则按 hostname 自助重发。服务端建仓停发 token 后该路径自然成为主线，客户端零新增逻辑。
- 重置密码（`apiUserResetPassword`，http.go:457-489）**不会**重发 git token——早期文档"丢失走 reset-password 联动重发"未落地，本设计不依赖它。
- Gitea token 值只在创建时可见一次，列表接口只有名字。**客户端无法把本机已存 token 对应到服务器上的凭证名**——"判断本机用的是否旧凭证"不可行，迁移触发只能用本地版本标记。
- `sanitizeTokenName`（http.go:294）：hostname 只留 `[a-zA-Z0-9_-]`，其余折 `-`，全非法落 `unknown`，截 32。已知边角：同 hostname 两机互顶；纯非 ASCII 主机名落 `ok-sync-r-unknown` 互顶。沿用现状，不治理。
- tokens 列表/删除端点已存在（`GET /api/v1/tokens`、`DELETE /api/v1/tokens/{name}`），清理能力无需新增服务端接口。

## 决策记录（与用户逐项确认）

1. **范围：三条发放路径全部统一**——建用户、建仓均不再发 token，`apiGitToken` 成为唯一发放口。
2. **存量旧凭证：提供一键清理**（凭证页按钮），不做启动自动迁移清理。
3. **旧客户端（<v2.25，无自助重发）连新服务端：不兼容**，依赖版本配对纪律，报错文案引导升级。客户端自动/强制升级另行立项，**不在本次范围**。
4. **凭证自愈拆两半**（用户提出"同步时检测旧凭证自动换新+清理"，受上方关键事实约束调整为）：
   - 后台同步路径只做**本机自动迁移**（拿新凭证、覆盖本机存储），**绝不后台删凭证**——会静默吊销其他未升级机器（"多机互不吊销"裁决的延伸）。
   - 删旧凭证永远走**一键清理按钮**，且清理前先保本机（无本机新凭证则先生成）。

## 改动设计

### oksrv

- `apiUserCreate`：删除 `CreateUserToken("ok-sync")` 步骤，响应去掉 `git_token` 字段。顺序简化为 Gitea 用户 → 本地用户，少一步回滚分支。
- `apiPersonalRepo`：删除建仓后的 `CreateUserToken("ok-sync-"+project)`；**`git_token` 字段保留但恒为空串**（客户端按字符串读，空串即走重发路径，JSON 形状不变）。
- `apiGitToken`：不动。唯一发放口。

### 客户端迁移（internal/gui 或 syncx，实施时按归属定）

- 新增机器级迁移：服务器已配置且本地无迁移标记时（同步周期入口与绑定管线前的公共检查点）：
  1. 调 `GitToken(hostname)` 拿新凭证；
  2. 对本机已绑定项目的 remote URL 覆盖 `StoreCredential`；
  3. 写迁移标记文件（`registry.Home()` 下，内容含 username/hostname/时间）。
- 标记写失败 fail-open：不阻断同步，下次重试（重发幂等：删同名再建，无副作用堆积）。
- `serveBindRepo` 中 `gitToken := pr.GitToken` 非空分支成为死代码，删除；404 分支文案补"或客户端版本过旧，请升级"。

### web/app.js（GUI 服务器页凭证卡）

- 新增「清理旧版凭证」按钮，点击后：
  1. 拉 tokens 列表，检查是否存在 `ok-sync-r-<本机hostname>`；**没有则先调重发生成并存入本机 helper**（覆盖"升级后尚未同步就来点清理"的场景）；
  2. 弹确认框，列出所有将被删除的旧命名凭证（不匹配 `ok-sync-r-*` 者），文案明示"其他未升级的机器将失效，需升级后重新绑定自愈"；
  3. 确认后逐条 DELETE，结束汇总 toast。遵循项目 GUI 两段式约定（确认前零副作用）。
- 凭证卡说明文案改为"每台拉取过的机器一条"。
- 管理面建用户成功展示去掉 git_token，只给初始密码，文案说明"用户首次绑定项目时其机器自动获取 git 凭证"。
- i18n 中英字典同步加 key。

### 统一后的绑定数据流

```
绑定/拉取项目 → provision（建仓，永返空 token）
  → 本机已存凭据？── 有 → 直接用
                 └─ 无 → git-token 重发（name_hint=hostname）
                        → StoreCredential（无 helper 回退 URL 内嵌，沿用现有 credNote 警告）
```

## 兼容与风险

- 新服务端 + 旧客户端（<v2.25）：首次绑定报错，文案引导升级（决策 3）。已绑定机器不受影响（凭据已在本机）。
- 一键清理波及未升级的他机：无法避免，靠确认框文案明示（决策 4）。
- 迁移标记与凭证状态不一致（如标记在但 helper 被清）：现有"无凭据则重发"分支兜底，收敛正确。

## 测试

- 行为变化类新测试先验证对旧代码变红（项目 TDD 约定）。
- `internal/oksrv/http_test.go`：建用户不再断言 `git_token`；建仓断言 `git_token` 恒空且 backend 不再收到 `CreateUserToken`；tokens 列表用例去掉默认 `ok-sync`。
- `internal/gui/api_server_test.go`：provision 空 token 成主线，调整 fake；新增迁移标记逻辑与清理按钮过滤逻辑测试。
- `tests/e2e/okserver_test.go:112`：建用户响应不再含 `git_token`。

## 发布纪律

- 版本 bump 必跑 `scripts/sync-version.sh`（README/官网徽标漂移已有 pre-push 兜底）。
- Release 正文与 changelog 不硬折行。
- okserver 与 server/ 目录不进任何客户端安装包（发布边界）。

## 范围外

- 客户端自动升级/强制升级机制（决策 3，另行立项）。
- hostname 互顶边角治理（关键事实节，沿用现状）。
