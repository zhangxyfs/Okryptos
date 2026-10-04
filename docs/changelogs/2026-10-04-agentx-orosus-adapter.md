# 2026-10-04 agentx：Orosus 适配器——GUI 引导卡片 + 标记块 hooks 接入

## 背景

Orosus（自研模块化 AI 编程助手 CLI，TypeScript/Node 22）m5-hooks 批收官，hooks
协议与 Claude 生态兼容（Orosus docs/hooks.md，stdin 一行 JSON / 退出码表态 /
stdout JSON 决策）。此前已用 `ok.exe hook prompt/post-tool/stop claude` 三条手工
注册（用户层 `~/.orosus/modules.d/hooks.toml`，2026-10-04 实证知识索引注入与
touched/base_injected 落盘全通），本批把它收编为正式 agent：GUI 引导页出卡片
（logo 用 Orosus 官方 logo-icon-color.png 缩 76×76 内联 data-URI），安装/自愈/
卸载走 agentx 统一管线。

## 改动

- `internal/agentx/orosus.go`：新增 orosusAgent（Register 注册表第 11 个）。
  - `OK_OROSUS_HOME` 测试隔离口；hooks 目标 `~/.orosus/modules.d/hooks.toml`；
    技能目录 `~/.orosus/skills`（Orosus skill 模块直读该目录，独立于共享 SkillsHome）。
  - kimi 同款**标记块**模式（`UpsertHooksBlock` 家族语义、TOML 保注释幂等写）：
    `OrosusHooksBlockFor` 生成三事件表（prompt / post-tool / stop，均带
    `name`/`product = "Okryptos"` 显示元数据）；post-tool 挂 **PreToolUse**
    （matcher `^tool-fs__(write|edit)$`）而非 PostToolUse——Orosus 的
    PostToolUse 载荷不带 tool_input（协议 §3.2），ok 取不到 path；PreToolUse
    的 tool_input.path 在场，执行失败多记一笔触碰、归因语义无损。
  - `stripLegacyOKHooksOrosus`：Orosus 表格式遗留剥离（首接手写件为 TOML
    literal 串 command，与 kimi 的 `[[hooks]]`+event 键格式不同，单独实现）。
    子表按 `orosusOKCommand`（双引号 basic 串/单引号 literal 串双形态，okd
    存量同认）判定整表删；**子表归属按 TOML 位置绑定**（`[[hooks.<E>.hooks]]`
    挂最近一个同事件父表元素）——全文件搜同名子表会让第三方子表给孤儿父表
    续命，而只剩 matcher 的孤儿父表会让 Orosus 整份配置校验失败、模块激活
    炸降级窗。子表被删空的父表连带删（复合键 `事件#父表序` 防跨事件撞号）。
  - `HooksInstalled`：标记块归一比对（剥 `disabled` 键行——用户经
    /settings → 钩子 → e 的停用是意愿不是漂移，不归一会误报过期、自愈复活
    停用）；`[hooks] enabled = false` 总闸（精确正则，防 `enabled_xxx` 前缀
    误命中）视为未接入，与 zcode enabled 防御对称。
  - `EnsureHooks`：标记块丢失但遗留 ok 表在 → 备份后重建；标记块过期（exe
    迁移/超时变更）→ 重写；完全无 ok 条目不复活（kimi EnsureHooksBlock 同语义）。
- `web/app.js`：引导页 `AGENT_META.orosus`（hook 型、target、双语简介）+
  `LOGOS.orosus`（76×76 PNG data-URI，全彩图标铺满磁贴，zcode/reasonix 同规格；
  内联是既定约束——静态白名单不新增文件路由）。卡片经 `/api/status` 的
  agentx.All() 自动带出，GUI 零后端改动。
- 测试隔离：`OK_OROSUS_HOME` 补进全套 agent 遍历测试（cli/cli_test×10、
  setup_test×7、propose_test、daemon/server_test、gui/api_test、
  setupx 三件、rxext/serve_test 环境列表、hook_test TestMain）——真机
  `~/.orosus` 存在，不隔离会让 selfHeal/doctor 路径打真文件；
  gui/api_test agent 计数 10 → 11。

## 测试

`internal/agentx/orosus_test.go` 10 例：块形态（TOML 合法性）/Detect/
安装幂等（标记块恰 1、表结构、matcher、timeout int64）/手写遗留件整块替换
（含真机 2026-10-04 手写件夹具 + 第三方表保留）/卸载与二次 no-op/混装表
卸载保第三方/漂移判定（旧 exe、超时）/disabled 停用不误报不复活/总闸关
闭/自愈三分支（不凭空建、不复活、孤儿重建带备份、过期重写）/混装父表
孤儿判定（位置绑定）。全仓 `go test ./...` 全绿。
