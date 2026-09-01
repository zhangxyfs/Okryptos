# 壳项目关联工作目录 设计

日期：2026-09-01
前置问题记录：`docs/2026-09-01-pull-shell-empty-paths-issue.md`（拉取/恢复壳项目空 Paths → hooks 按 cwd 匹配失效）

## 背景与目标

`POST /api/server/pull` 与备份恢复注册的是空 Paths 壳项目：知识库数据在 `~/.openknowledge/projects/<name>`，但用户工作目录未挂进注册表，hooks（`project.FromCwd` → `FindByCwd` 最长前缀匹配 Paths）永不命中，hook 注入/沉淀静默失效，ok.log 每回合刷「目录未注册为知识库项目」。同时 GUI 服务器页对壳项目显示「已绑定」，名不副实。

目标：

1. 壳项目状态正名（不再叫「已绑定」）；
2. 单项目「关联目录」按钮：显式登记工作目录绝对路径（目录名与项目名**不要求同名**——关联是显式登记，不靠名字匹配）；
3. 「一键关联」批量入口；
4. CLI 侧 `ok init` 同名幂等补挂（同一注册表方法，顺带修 issue 文档记录的 CLI 断链）。

非目标：目录名↔项目名自动猜测匹配；递归扫描；团队仓关联。

## 后端

### registry.AddPath

`internal/registry/registry.go` 新增：

```go
// AddPath 把 path 补挂到已存在项目的 Paths（拉取/备份恢复的空壳项目关联工作目录）。
// 锁由调用方（registry.Update）持有。同路径已挂到别的项目 → 冲突错误；本已挂载 → 幂等 nil。
func (r *Registry) AddPath(name, path string) error
```

- NormalizePath 规范化；未知项目名 → 错误；
- 冲突口径沿用 AddProject 的同路径检查（registry.go:134 段，规范化后**相等**判断）：path 已挂在**其他**项目 Paths 下 → 拒绝；
- 已在**本**项目 Paths 中 → 幂等成功不重复追加；其余情况（含本项目的父/子目录 widening）正常追加。

### POST /api/project/attach

`internal/gui/api.go` 注册 `api("POST /api/project/attach", h.apiProjectAttach)`：

- 请求 `{project, path}`；项目不存在 → 404；path 为空/不存在/非目录 → 400；AddPath 冲突 → 409，其他（锁/IO）→ 500；成功 200 返回 `{name, paths}`。
- 走 `registry.Update` 锁内读-改-写（注册表裸读改写丢项目的既有坑）。
- hooks 每次现读注册表，关联后即时生效，无需重启 daemon。

### CLI：ok init 同名补挂

`internal/cli/cli.go` Init：`registry.Update` 闭包内改为——项目已存在（按名）→ `AddPath(name, cwd)`，输出「已关联目录 …」；不存在 → 维持 `AddProject`。同名补挂仅限空 Paths 壳项目或幂等重入（cwd 已在 Paths）；Paths 非空且不含 cwd 时保留旧的「项目已存在」报错（防敲错项目名串库）。后续 EnsureDirs/默认 config/hooks 写入均为幂等，流程不变。

## 前端（web/app.js 服务器页成员视图 bindCard）

### 状态正名

行内状态逻辑（现 `bound = p.sync && p.sync.is_repo`）：

- bound 且 `p.paths` 为空 → chip「未关联目录」（off 样式）+ 行内「关联目录」按钮；
- bound 且有 paths → 「已绑定」（不变）；
- 未 bound → 「建仓」按钮（不变）。

`SRV.projects` 来自 `/api/projects`，已含 `paths`，无新增数据请求。

### 单个关联

「关联目录」→ modal：项目名 + 绝对路径文本输入框（粘贴任意存在目录）→ 确定调 `POST /api/project/attach` → toast 结果 → `SRV.projects = null; loadServerRoleData()` 局部刷新。确定按钮点击即禁用防连点。

### 一键关联

壳项目（bound 且空 paths）≥ 2 个时卡底部出现「一键关联」按钮 → modal 列出全部壳项目、每行一个路径输入框（可留空跳过）→ 一次提交逐个串行调 attach，单个失败不阻断，结束汇总 toast「已关联 n 个，失败：…」（与「全部拉取」同款交互）。

### 解除关联（2026-09-01 增补，挂错目录的撤销路径）

- `registry.RemovePath(name, path)`：AddPath 对偶——锁内、规范化相等匹配、路径不在项目下幂等 nil、未知项目 ErrProjectNotFound；最后一条解除后项目回到空 Paths 壳（项目与知识库数据保留）。
- `POST /api/project/detach {project, path}`：**不做目录存在性校验**（目录已删是常见解绑场景）；400 空路径 / 404 未知项目；paths 锁内取出回包。
- 已绑定行新增「目录」按钮 → modal 列出 paths 逐条「解除」（确认 + busy + toast）；全部解除后回到「未关联目录」。

### i18n

zh/en 词条补齐：`srvUnlinked`（未关联目录）、`srvAttach`（关联目录）、`srvAttachAll`（一键关联）、`srvAttachPathPlaceholder`、`srvAttachDone`（已关联 {n} 个）、`srvAttachFail`（，失败：）等。遵守「假功能按钮必须有可见反馈」约定：所有按钮有 busy 态与结果 toast。

## 错误处理汇总

| 场景 | 码 | 前端表现 |
|---|---|---|
| path 为空/不存在/非目录 | 400 | toast 错误文案 |
| 项目不存在 | 404 | toast |
| 路径已挂别的项目 | 409 | toast 冲突文案 |
| 锁/IO | 500 | toast |

## 测试

- `registry.AddPath` 单测：补挂成功、同路径幂等不重复追加、跨项目路径冲突、未知项目。
- `apiProjectAttach` 端点测试（沿用 api_server_test 的 fake 模式）：200/400/404/409。
- `ok init` 同名补挂测试：先注册空壳，再在工作目录 init 同名，Paths 含 cwd。
- 行为变化类新测试先验证对旧代码变红（既有约定）。
- 前端手工走查清单：壳项目显示「未关联目录」；单个关联（含异名目录）；一键关联（含留空跳过、单个失败）；关联后 ok.log 不再刷「目录未注册」且 hooks 恢复注入。

## 兼容与影响面

- 纯增量端点，旧客户端不调用无影响；无 okserver 改动，无发布边界问题。
- 改动文件：`internal/registry/registry.go`(+test)、`internal/gui/api.go`(+test)、`internal/cli/cli.go`(+test)、`web/app.js`。
