# P1 个人多端同步实施设计：git 单机引擎 + 团队版预留口

- 日期：2026-08-25
- 状态：待评审（未批准，未进入实施计划）
- 上游文档：`docs/superpowers/specs/2026-08-25-team-sync-design.md`（总体方案，本文档是其 §11 阶段表 P1 的细化）
- 范围声明：本文档只覆盖**个人多端数据同步**（个人层第一刀）；团队仓、PR 人审、merge driver、okserver 均不在本期，但按 §8 预留扩展口。

## 1. 目标

- 同一用户的多台设备之间同步每个项目的知识库（`knowledge/` + wiki 相关文件）；
- 本地 git 历史 + 可选远端（Gitea/任意 git 服务/NAS 裸仓）双层防丢防改；
- 无远端也可用（仅本地历史）；同步失败不影响本地任何功能（fail-open）；
- 代码与配置结构为团队版（`[sync.team]`、team 策略、双层检索）留好口子，本期不实现。

明确不做（本期）：Gitea 仓自动创建、merge driver（P2）、GUI 同步页（P5）、PR 流程、okserver、团队层目录。

## 2. 关键事实依据

- `store.Root` = 项目数据目录（`~/.openknowledge/projects/<name>/`），内含 `knowledge/`、`INDEX.md`、`kb.db`、`state/`、`config.toml`（`internal/store/store.go:9-17`）；
- 文件是真相源，kb.db 是派生索引，多写者一致性靠 mtime 增量 `db.Sync`（`docs/2026-08-21-okd-contract.md` §数据一致性模型）→ pull 引起的文件变更会被现有机制自动重建索引，本期**索引侧零新增代码**（但须测试验证闭环，见 §9）；
- okd 已有 ticker 模式（`internal/daemon/run.go:96` 自检查 ticker），同步定时器照此挂载；
- 项目级 `config.toml` 覆盖机制现成，同步配置直接放项目级。

## 3. 仓库布局

`git init` 落在项目数据目录（`store.Root`）原地成仓，文件不挪动，OK 现有读写路径零改动。

`.gitignore`（随 `git init` 生成）：

```
kb.db
kb.db-*
state/
*.log
```

进仓内容：`knowledge/`、`INDEX.md`、wiki 相关文件。一个项目一个仓——对应团队版"一个项目一个团队仓"的映射。

## 4. `internal/syncx`（新叶子包）

职责：在指定目录里跑 git。不依赖其他 internal 包（除 fsx 若需原子写状态）。

- **执行方式**：exec 参数数组调**系统 `git`**，不走 shell（防注入，借 TencentDB `git-fetcher.ts` 纪律）。启动探测 `git` 不存在 → 明确提示安装，同步禁用，本地功能不受影响。
  - 决策（默认，可翻）：**用系统 git 而非 go-git**——凭据 helper/SSH/rebase 语义现成；目标用户为开发者，git 基本必有。
- **原语**：`IsRepo / Init / Status(dirty, ahead, behind) / CommitAll(msg) / PullRebase / Push / RemoteURL`。
- **串行化**：包内 single-flight 锁——okd ticker、CLI、GUI 都可能触发同步，并发请求合并为一次执行。
- **冲突检测**：`PullRebase` 返回结构化结果（成功 / 冲突文件列表 / 错误），不就地解决。

## 5. `ok sync` 命令

一次执行 = `add -A` → 有变更则 commit（消息 `sync: <hostname> <RFC3339>`，主机名标识来源设备，排障与未来团队归因两用）→ `pull --rebase` → `push`。

输出人话状态：`推送 2 个提交 / 拉取 1 个提交 / 已是最新`。

冲突时：

1. 停止后续 push；
2. 冲突文件列表写入 `state/sync-conflict.json`；
3. 非零退出，提示去手动解决（P1 无 GUI 解决页）。

**P1 兜底原则：不丢内容、不自动猜。** add/add 改名、union 合并等智能策略属 P2 merge driver。

## 6. okd auto-sync

- 照 `run.go:96` ticker 模式加同步 ticker：默认 5 分钟（`auto_interval_min` 可配，0 = 关闭），到点对每个已启用同步的项目跑 §5 同逻辑；
- **写入后尽快推**：条目写入动作（approve、GUI 编辑等）触发一次 30s 防抖的延迟 sync，把未同步窗口压到分钟级（未同步窗口是本方案唯一的残余丢失面）；
- 失败仅记日志 + 更新状态文件，绝不影响 hook 注入等本地链路。

状态文件 `state/sync-status.json`，**从第一天就按层建模**（§8 口子 3）：

```json
{"layers": {"personal": {"last_sync": "...", "ahead": 0, "behind": 0, "conflict": false, "last_error": ""}}}
```

## 7. 配置面

项目级 `config.toml`（复用现有覆盖机制）：

```toml
[sync]
enabled = true
remote = "git@gitea.local:you/ok-notes.git"   # 留空 = 仅本地 commit，不推远端
auto_interval_min = 5
```

- `remote` 留空也可用：本地历史是防丢/防改第一层，远端是第二层 → **P1 不依赖 Gitea 即可交付**；
- 解析实现为团队版按层预留（§8 口子 1）：内部结构即按 `personal` 层建模，序列化为 `[sync]` 是 P1 的简写形态。

## 8. 团队版扩展口（本期必须留，但不实现）

1. **配置按层**：P1 的 `[sync]` 在团队版演进为 `[sync.personal]` + `[sync.team]`；解析代码现在就把字段挂在层结构下，不摊平到顶层；
2. **策略参数化**：`syncx.Sync(dir, policy)`；P1 仅 `personal` 策略（直推 main）；团队版加 `team` 策略（推分支 + 建 PR），pull/rebase/冲突处理共用；
3. **状态/GUI 按层**：`sync-status.json` 的 `layers` 结构（§6）；GUI 同步页（P5）按层渲染；
4. **不新增"单知识源"假设**：团队层将来是项目下独立第二目录/仓（总体方案 §4）；本期不改检索合并路径，但新代码不得假设项目只有一个知识源，此类判断收敛到 store 层。

## 9. 设备初始化流（`ok sync init [remote-url]`）

| 情形 | 行为 |
|---|---|
| 目录非仓、无知识内容 | clone remote 到该目录 |
| 目录非仓、已有内容 | `git init` + 首个 commit + 关联 remote + push（首台设备路径） |
| 两边各自初始化过、均有内容 | **不自动合并**，报错并指引手动 `--allow-unrelated-histories` 合并一次（低频一次性场景） |

## 10. 测试策略

- 不依赖 Gitea：测试内 `git init --bare` 临时裸仓，`file://` 协议当 remote；
- 核心 E2E：两个临时 `OK_HOME` 模拟双设备——"设备 A 写条目 → sync → 设备 B sync → B 能检索到该条目"闭环（专门验证 §2 的 mtime 重建链路，防"单元测试绿、链路断"的已知坑模式）；
- 手动跑 E2E 必须导出全套 agent home 隔离变量（既有知识条目教训）；
- 冲突路径用例：双设备改同一文件 → 第二台 sync 报冲突、写 conflict 状态、内容不丢。

## 11. 工作量与交付边界

新增：`internal/syncx` 包、`ok sync` / `ok sync init` 命令、okd 同步 ticker + 写入防抖触发、状态/冲突两个状态文件、配置段、E2E。无新第三方依赖（系统 git 除外）。P1 交付仅 CLI 形态，GUI 展示属 P5。

## 12. 已决与待决

已决（默认值，评审时可翻）：

- 用系统 git，不用 go-git（§4）；
- P1 不含 Gitea 仓自动创建，remote 手动配置或留空（§7）；
- 冲突只检测不自动解决，智能合并归 P2 merge driver（§5）。

待决：

- `auto_interval_min` 默认值（建议 5）与写入防抖时长（建议 30s）；
- commit 消息格式是否需含项目名（多项目并行排障场景）。
