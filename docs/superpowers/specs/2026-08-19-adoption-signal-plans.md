# 采纳信号接通：两套方案（A+B 组合 / C MCP server）

日期：2026-08-19
状态：待评审

## 背景与问题

`[retrieve.feedback]` 的"持续注入零采纳自动降权"代码闭环完整（`internal/index/feedback.go`、`internal/hook/core.go` 的 `TrackTouched`/`EventAdopted` 入账），但默认关闭（`internal/config/config.go:171`）：采纳 = 注入过的条目文件被读工具读取，而十个宿主的 post-tool 订阅**全部只订了写工具**，读事件根本不进 ok 的钩子，采纳信号恒零，开降权会全员误伤。

关键利好（已核实）：`TrackTouched` **不按 toolName 过滤**，采纳判定只看路径是否落在 KnowledgeDir 且 basename 命中本会话 `InjectedKnowledge`（`core.go:296-308`）。所以接通读信号**只需改各宿主的 matcher/过滤代码，`internal/hook` 输入层零改动**；前提是宿主事件带含 `path`/`file_path` 的 `tool_input`。

## 逐宿主能力矩阵（2026-08-19 代码核实）

| 宿主 | 适配器 | post-tool 订阅现状 | 读工具可订阅性 |
|---|---|---|---|
| kimi | `kimi.go` TOML `[[hooks]]` | `matcher = "Write\|Edit"`（kimi.go:41-43） | **未知，需实测**。读工具名疑似 `read_file`；matcher 匹配语义无实测记录 |
| pi | `pi_extension.ts` | 代码过滤 `write`/`edit`（:47-56） | **可订阅**：`tool_result` 对所有工具派发，加 `read` 即可 |
| zcode | `zcode.go` config.json | `"Write\|Edit"`（:34） | 机制上可（Claude 同款正则），无实测 |
| opencode | `opencode_plugin.ts` | 代码过滤 `write`/`edit`/`apply_patch`（:80-93） | **可订阅**：`tool.execute.after` 全派发，读工具 `read`（`args.filePath`） |
| claude | `claude.go` settings.json | `"Write\|Edit"`（:50） | 机制上可：`"Write\|Edit\|Read"`，无实测 |
| codex | `codex.go` hooks.json+信任哈希 | `"apply_patch"`（:51） | **未知**：Codex 是否向 hooks 暴露读工具无记录；改 matcher 需重算信任哈希（codex.go:502-514） |
| qoder | `qoder.go` settings.json | `"Write\|Edit"`（:47） | 契约逐字兼容 Claude，读工具名无实测 |
| qoder-ide | `qoderide.go` | `"Write\|Edit"`（:46） | 工具名双套映射，读工具原生名未知 |
| dsh | `dsh_plugin.js` | 代码过滤 `write`/`edit`（:87-103） | **可订阅**：`tools/post-execute` 全派发，参数键 `file_path` |
| reasonix | sidecar `rxext/serve.go` | 白名单 `write_file`/`edit_file`/...（:180-184） | **完全可订阅**：`read_file` 加进 :181 的 switch 即可 |

transcript_path 现状：仅 claude/codex 的契约确认带此字段，ok 未解析；kimi 载荷实测无此字段（0.28.1 附录A 校准）。

## 方案一：A+B 组合（逐宿主接通 read 订阅 + transcript 归因兜底）

### A：逐宿主把读工具加进 post-tool 订阅

改动量小、逐宿主独立，按"已确认可订阅"优先排序：

1. **reasonix**：`serve.go:181` switch 加 `read_file`。一行，sidecar 协议已带完整参数。
2. **pi**：`pi_extension.ts:47-56` 过滤数组加 `read`。
3. **opencode**：`opencode_plugin.ts:80-93` 过滤加 `read`（取 `args.filePath`，模板已有绝对化逻辑）。
4. **dsh**：`dsh_plugin.js:87-103` 过滤加读工具名（参数键 `file_path` 已兼容）。
5. **kimi**（实测先行，见下）：matcher 改 `"Write|Edit|read_file"`，工具名以实测为准。
6. **claude**：`"Write|Edit|Read"`。
7. **zcode / qoder / qoder-ide**：机制上同 claude，读工具名逐个实测后补。
8. **codex**：最后做。需先确认 Codex 是否向 hooks 暴露读工具；改 matcher 触发信任哈希过期，需走自愈迁移并实测不静默跳过。

配套改动：

- **早退优化**：post-tool 链路在解析 JSON 后、加载会话状态前，先做"路径是否含 KnowledgeDir 前缀"的廉价判断，不命中立即退出——把每次读文件的 hook 开销压到进程启动 + JSON 解析（Windows 上几十毫秒），避免订阅 read 后状态文件读锁竞争放大（state 文件有多进程竞态前科，锁内重放+原子落盘已是约定）。
- **适配器测试**：各 `*_test.go` 的 wantMatcher 同步更新；kimi 侧确认 `EnsureHooksBlock` 只看 MarkerBegin 不动块内容，改 matcher 不会触发自愈误判（已核实安全）。
- **能力矩阵落档**：每个宿主实测结果（读工具名、是否派发、字段名）记回知识库/适配器注释，终结"未知"状态。

### B：transcript 归因兜底（A 不通的宿主）

下一轮 prompt 时回放上一轮 transcript，assistant 输出引用了本会话注入条目的标题/文件名即记采纳。覆盖 A 路走不通的宿主（codex 读工具不暴露、kimi 不派发读事件等场景）。

- 输入来源：hook 载荷的 `transcript_path`——claude/codex 契约已确认有；**kimi 实测无此字段**，kimi 若 A 路不通需另找会话日志路径（实测时顺手 dump 完整载荷确认）。
- 归因口径：从"读取了条目文件"变为"回复里提到了条目"，语义略宽但更贴近"采纳"本意；与 A 路信号取并集入账。
- 实现位置：`core.go` prompt 轮入账段（`EventAdopted` 入账处，core.go:64-75 附近），按宿主能力选择信号源。

### 测试计划（Kimi 先行，不用 Claude）

1. 导出 `KIMI_CODE_HOME` 到隔离目录（agent home 隔离变量全套，E2E 约定），`ok init` 注册测试库，装 hooks。
2. **探针先行**：matcher 先写 `*`（或尽量宽），在 post-tool 链路加临时 debug 落盘完整 stdin JSON——一次会话同时实测：①Kimi 是否对读工具派发 PostToolUse；②读工具的真实工具名；③载荷里有没有 transcript_path 类隐藏字段。
3. 确认后 matcher 收敛为 `Write|Edit|<读工具名>`，重跑会话：prompt 注入条目 → kimi 读条目文件 → 下一轮 prompt 后查 `entry_events` 表/`ok.log` 出现 `adopted`。
4. 性能：秒表对比订阅 read 前后一轮会话的读文件延迟，确认早退优化后开销可接受。
5. 反馈开关验证：造一个"注入 ≥4 次零采纳"条目 + 一个有采纳条目，开 `feedback.enabled`，确认前者 ×0.8 后者不变。

### 发布策略

- `feedback.enabled` 默认仍 false；按宿主逐个实测接通后，在 Release Note 公布"已接通宿主清单"，用户手动开启。
- 可选：GUI 设置页加 feedback 开关（勾选+保存两段式，遵循既有约定）。

### 风险

- kimi/zcode/qoder/codex 读工具派发性全部未实测，最坏情况 A 路只通 4 个宿主（pi/opencode/dsh/reasonix）+ claude。
- Windows 进程创建偏贵，订阅 read 后 hook 频率显著上升，早退优化是硬需求。
- codex 改 matcher 牵动信任哈希，有静默跳过前科，必须实测。

## 方案二：C —— MCP server（结构性解法）

`ok mcp-serve`：以 MCP server（stdio JSON-RPC）暴露知识库——`search` 工具、`read` 条目资源/工具、INDEX 资源。读取请求经过 ok 自己，采纳信号天然可靠，不依赖任何宿主的 hook 派发能力；同时是 hook-less agent 的预案（知识库已有此 reference 条目）。

### 组成

1. **MCP 协议层**：stdio JSON-RPC，实现 initialize/tools/list/tools/call/resources 等核心方法。可复用 reasonix sidecar 的 NDJSON stdio 服务骨架（`rxext/serve.go` 已是同类形态）。
2. **检索复用**：`search` 直接调 `internal/index.Query` 现有混合检索（按通道准入、recency、feedback 全套生效），返回条目摘要+路径；`read` 返回条目全文并**原地记采纳事件**（信号源的一手数据）。
3. **逐宿主 MCP 接入**：agentx 新增各宿主的 MCP 配置写盘（kimi/claude/codex/opencode 等支持 MCP 的宿主各一段配置；不支持的宿主此路不通，仍需 hook 注入）。复用适配器注册表形态，一个宿主一段配置函数。
4. **与 hook 注入并存**：push（UserPromptSubmit 注入）保留——MCP 是 pull，模型不一定主动调；注入段底部附"可用 ok-mcp 检索更多"提示引导模型调用，读取行为即采纳。
5. **采纳归因**：MCP `read` 调用带 session 上下文（MCP 无 session 概念，需在工具参数或 initialize 元数据里透传 project/cwd），直接写 `entry_events`，可靠性不依赖宿主。

### 工作量与风险

- 量级是一整个子系统：协议层 + 检索封装 + 十个宿主的 MCP 配置适配 + 信任门（参考 codex hooks 信任哈希、reasonix plugin-packages 的坑，MCP server 注册在多数宿主也要用户确认）。
- 检索质量依赖模型主动调用行为，不可控——push 注入不能下。
- 宿主 MCP 支持度不齐，覆盖率低於 hook 方案。
- 收益上限最高：信号可靠性结构性解决，且打开 hook-less agent 的未来形态。

## 对比与建议

| | 方案一 A+B | 方案二 C |
|---|---|---|
| 改动量 | 小（逐宿主几行 + 归因逻辑） | 大（子系统级） |
| 信号可靠性 | 受宿主派发能力约束，需逐宿主实测 | 一手信号，结构性可靠 |
| 宿主覆盖 | 十个宿主都可走 A 或 B | 仅支持 MCP 的宿主 |
| 风险 | 最坏只通部分宿主；hook 开销 | 模型不主动调则信号仍稀；工程量大 |
| 附带收益 | 终结能力矩阵的"未知" | hook-less 预案落地 |

**建议：先方案一，Kimi 实测探针开路；方案二作为中期路线单独立项。** 两者不冲突——方案二落地后方案一的 transcript 归因仍可作并集信号源。
