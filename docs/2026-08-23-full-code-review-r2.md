# OpenKnowledge 全量代码审查报告（第二轮）

- **日期**：2026-08-23
- **范围**：全仓库非测试代码（internal/ 30 个包约 19.7k 行 Go、cmd/ 入口、web/ 前端 app.js 3901 行、installer/ 与 scripts/ 打包链、tests/ 覆盖面评估），含工作区未提交的 web 改动
- **方法**：6 条并行审查线（适配器层 agentx/hook/setupx ／ GUI 服务层 gui/tray/daemon ／ 检索存储层 index/retrieve/entry/store/wiki ／ 扩展与嵌入层 rxext/llmx/embed* ／ CLI 与持久化层 cli/config/registry/state/fsx/backup/打包 ／ Web 前端），逐文件阅读；高危发现由主审二次抽查核实代码证据
- **基线**：`go build ./...` 通过、`go vet ./...` 无告警、`go test ./...` 33 个包全部通过
- **与第一轮的关系**：第一轮报告见 `docs/2026-08-23-full-code-review.md`（53 项修复已随 v2.22.0 入库，commit ef47275）。本轮抽查确认第一轮关键修复仍然有效：hostGuard/Origin 校验三重防线有安全测试背书、selfHealHooks 的 CLI 入口换算未复现问题、registry.Update／updateGlobalConfig／state.Update 的锁内读改写纪律均已落地。本轮发现均为第一轮修复后剩余或新引入的问题。
- **计数口径**：同根问题合并为一条编号（如 opencode/pi 的状态误报并入 M-01、apiCaptureSet 两步落盘并入 H-01），故编号数为 4 高 / 20 中 / 30 低，另附 4 条结构性建议。

---

## 总体评价

代码质量高于同类工具平均水平：原子写（tmp+fsync+rename）与 fail-open 纪律在关键路径贯彻一致；历史坑补丁（Qoder cmd /s 包装、Codex 信任哈希与单次合并写、混合检索分通道准入、注入段序稳定、检索查询净化）经逐行核对仍然健壮且回归测试密度大；前端 XSS 防线完整（条目内容全链路转义、README 白名单净化、终端输出 textContent）。

剩余风险集中在两个主题：

1. **跨进程读-改-写缺锁的漏网处**——项目对 config.toml／state／registry 已建立 `WithFileLock` 纪律，但两处新写的入口绕开了它：GUI 层的 `setProvenanceAutoBorn` 裸写全局 config（H-01），以及五个适配器对宿主（Claude/Codex/Qoder 等）settings 文件的无锁读改写（H-02）。两者都是"复制既有函数时丢了锁包裹"的同款形状。
2. **跨模块契约不一致与静默停摆**——Reasonix manifest 与实际订阅不一致（H-03）会让压缩后 mandatory 注入整会话丢失；embedding 分批中途失败后向量写入永久停摆且用户不可见（M-07）。

---

## 修复优先级建议

| 优先级 | 发现 | 理由 |
|---|---|---|
| P0 | H-01/H-02 两处无锁读改写 | 丢用户数据（API key／第三方 hooks），且与自家锁纪律不一致，一次收口同根解决 |
| P0 | H-03 Reasonix manifest 补 `compaction.complete` | 一行改动，消除注入整会话丢失风险 |
| P1 | H-04 `ok add --file` front matter 检测 | 已沉淀过的历史坑，修复成本低 |
| P1 | M-07 embedding 停摆、M-01 HooksInstalled 校验 exe、M-13 approve 遮蔽、M-14 registry 串库 | 各为"静默失效用户不可见"类，影响信任 |
| P2 | M-02/M-15 锁纪律补齐、M-05/M-06/M-11 生命周期与超时、M-16 版本资源守门、M-18 前端白屏 | 特定条件触发，改动局部 |
| P3 | 其余 Medium 与全部 Low | 顺手修 |

---

## High（4 项）

### H-01 GUI 裸写全局 config.toml，可静默回滚并发写入

- **位置**：`internal/gui/api.go:1266`（setProvenanceAutoBorn）；同源问题 `api.go:1340`（apiCaptureSet 两步落盘无回滚）
- **证据**：
  ```go
  func setProvenanceAutoBorn(path string, autoBorn bool) error {
      ...
      data, err := os.ReadFile(path)   // 无锁读
      ...
      return fsx.WriteFile(path, ...)  // 原子写，但无 fsx.WithFileLock
  ```
- **问题**：该函数是 `config.SetCapture` 的复制版但丢了 `fsx.WithFileLock` 包裹（config 包注释明言裸跑会把对方刚写入的内容静默回滚）。与 GUI `SaveLLMProfile`（锁内整档重编码）并发交错时，后写者静默回滚对方刚写入的 profile／API key——丢配置数据。直接违反"config 保存必须锁内原子写"约定。
- **附带**：`apiCaptureSet` 先 `config.SetCapture` 再 `setProvenanceAutoBorn` 两次独立落盘，第二步失败返回 500 时 capture 已改而 auto_born 未改，无回滚。
- **修法**：把 `[provenance] auto_born` 的写入下沉到 config 包（锁内），gui 层删掉复制版；两步落盘合并为一次锁内写或加回滚。

### H-02 五个适配器改宿主 settings 全程无跨进程锁，可丢用户第三方 hooks

- **位置**：`internal/agentx/claude.go:310-322`（InstallHooks）；codex.go:746-770、qoder.go:359-379、qoderide.go:319-337、zcode.go:246-258 同形
- **证据**：
  ```go
  cfg, err := loadClaudeSettings()   // 无锁读
  ... // 内存合并
  return writeClaudeSettings(cfg)    // 仅原子写，无 WithFileLock
  ```
- **问题**：`hook.go:195` 每个 prompt 都跑 `selfHealHooks`（多会话/多 agent 并行是常态），与 GUI `/api/setup/hooks` 或另一 hook 进程并发时，两进程同时 load→改→rename，后写者覆盖前写者——用户自装的第三方 hooks 条目被静默丢弃。项目对 config.toml/state 都有锁纪律，宿主 settings 这层漏了同款保护。
- **修法**：每个宿主 settings 路径的 load→改→write 全程包 `fsx.WithFileLock`（锁文件可与 settings 同目录）。

### H-03 Reasonix manifest 与实际订阅不一致，压缩后 mandatory 注入整会话丢失

- **位置**：`internal/agentx/reasonix.go:73`（manifest）↔ `internal/rxext/serve.go:52`（Initialize 订阅）
- **证据**：
  ```go
  "intercepts": []any{"input.receive", "tool.after"},          // manifest
  Subscriptions: []string{"input.receive", "tool.after", "compaction.complete"},  // 实际
  ```
- **问题**：SDK 契约明言宿主拒绝超出 manifest 的订阅。轻则 `compaction.complete` 永不派发——`onCompaction` 不触发、BaseInjected 压缩后不重置、mandatory 全文整会话丢失；重则 Initialize 被拒、Reasonix 注入整体失效。且 `HooksInstalled` 不校验 intercepts 字段，自愈也不会修。
- **修法**：manifest 补 `compaction.complete`；`HooksInstalled` 增加 intercepts 一致性校验。

### H-04 `ok add --file` 不检测来源文件 front matter，层层嵌套（已知坑仍在）

- **位置**：`internal/cli/cli.go:167-174`
- **证据**：`body = string(data)` 直接落 `e.Body`，`Serialize` 再包一层 `---`；`entry.Parse` 只找第一个结束分隔符，内层元数据静默变正文。
- **问题**：知识库已沉淀的坑（"ok add --file 只收纯正文：传带 front matter 的文件会层层嵌套"），至今未修。
- **修法**：读入后检测开头 `---` 分隔的 front matter 块，存在则剥离（或报错提示用 `ok add` 正文参数）。

---

## Medium（20 项）

### 适配器层（agentx/hook/setupx）

**M-01 HooksInstalled 不校验当前 exe，迁移/改名后状态误报且不自愈**
`internal/agentx/kimi.go:234`、`opencode.go:72`、`pi.go:58`：只查 marker/fingerprint 存在，不比对当前 exe 路径（claude 系与 deeparness.go:104 的全量比对是正确样板）。exe 迁移后 hook 指向死路径，doctor/GUI 仍报"已接入"；且自愈入口本身依赖旧 exe 判定，永远不触发。→ 三处补 exe 比对。

**M-02 uninstall.RemoveSection 写 config.toml 不走锁**
`internal/setupx/uninstall.go:57-58`：与 `setupx.go:93` updateGlobalConfig 的锁内读改写纪律不一致，卸载与 GUI Save* 并发时互相覆盖（丢 profile）。→ 收进锁内写入口。

**M-03 五适配器约 180 行/个同形复制 + 三套 wrapper 三件套逐字复制**
`stripOKXxxHooks`/`hasOKXxxHook`/`loadXxx`/`writeXxx` 在 claude/codex/qoder/qoderide/zcode 五份近乎逐字相同；`ensureQoderWrappers`（qoder.go:83）/`ensureCodexWrappers`（codex.go:88）/`ensureLingmaWrappers`（qoderide.go:77）仅目录不同。Qoder cmd /s、Codex 信任门这类历史坑被迫各修三份，正是漏改根源。→ 抽取"命令生成器 + 事件表"参数化的共享引擎，建议在下一个适配器加入前完成。

### GUI/daemon/tray

**M-04 embedding name-only 复测对无 key profile 必误报**
`internal/gui/embedding.go:258-269`：base_url 回填只在 `ResolvedAPIKey()!=""` 分支执行，ollama 等无 key profile 的 `p.BaseURL` 留空 → `ClientForProfile` 判 nil 报"profile 不可用"，与注释"等价于复测已存 profile"矛盾。→ 回填逻辑移出 key 分支。

**M-05 托盘双击同步拉起浏览器卡死消息线程，daemon 2s 超时跳过 cleanup → 幽灵图标**
`internal/tray/tray_windows.go:262` + `internal/gui/browser_windows.go:30`（maximizeWindowByTitle 轮询最长 10s×2）+ `internal/daemon/run.go:108-113`（对 trayDone 只等 2s 即返回）。双击托盘同时点网页退出可复现，恰是 run.go 注释宣称要避免的"幽灵图标"场景。→ OpenBrowser 异步化或缩短轮询；daemon 退出等待与浏览器轮询时序对齐。

**M-06 终端执行 10s 一刀切超时，wiki 长任务必被 kill**
`internal/gui/api.go:975` `terminalExecTimeout = 10 * time.Second`：白名单内 `wiki`（多轮 git+LLM）必超时返回 124，违反"超时按场景区分"约定。→ 按命令类别区分超时。

### 检索/存储层

**M-07 embedding 分批中途失败后向量写入永久静默停摆**
`internal/index/sync.go:256-294`（分批失败返回，meta 未写但 vectors 已有部分行）+ `sync.go:161-165`（下轮命中"meta 空 + HasVectors → embedBlocked"分支）。首次建库遇网络抖动即可触发，仅 ok.log 一行，须手动 `ok index` 触发 ClearVectors 重建。→ 失败时清理本次已写向量，或 embedBlocked 状态向用户可见（GUI/doctor 提示）。

**M-08 语义通道每查询全量加载全部条目正文**
`internal/index/query.go:232-235`：准入判定只需 filename+向量，却 `SELECT … e.body … FROM vectors JOIN entries` 全量进内存；万级条目每次注入数十 MB IO/内存。→ 两阶段查询（先算余弦，准入后按名回查 body）。

**M-09 recency 排序比较器缺 Filename 决胜键**
`internal/index/recency.go:58-64`：输入来自 map 遍历（随机序）+ `sort.Slice` 不稳定，同名同分条目（query.go:326 自述 wiki 多分支差异条目同名是常态）在两次排序中相对顺序不定，RecencyShifted 观测漂移。同款比较器已在 query.go:319、feedback.go:33 复制三份，仅此份缺决胜。→ 补 Filename 决胜并抽共享比较器。

### 扩展/嵌入层

**M-10 sidecar 崩溃计数不重置，"连续两轮"退化为"一轮"**
`internal/embedsidecar/manager.go:225-233`：`unhealthyStreak` 仅健康分支归零，Stop 与重新 Ensure 都不重置——崩溃判定 Stop 后计数停在 2，新 sidecar 首次 800ms 探测瞬时失败即被杀。→ Stop/Ensure 重置计数。

**M-11 daemon 退出可被 Ensure 持锁阻塞最长 90s**
`internal/daemon/run.go:137`（defer sidecarMgr.Stop()）+ `manager.go:82-162`（janitor 的 Ensure 持 mu 横跨就绪等待）：shutdown 的 Stop() 抢不到锁，daemon 收到退出信号后最多阻塞 90s。→ 就绪等待移出锁外或 Stop 支持取消。

**M-12 selfHealHooks 在锁内且每条输入执行，放大 input.receive 延迟**
`internal/rxext/serve.go:62-65`：遍历全部 detected agent 的检测/重写 I/O 在 `h.mu` 锁内执行（它不触碰会话状态，无需锁），串行化后逼近 manifest timeoutMillis。→ 移出锁外并降频（如每 N 条输入一次）。

### CLI/注册表/打包

**M-13 `ok approve` 被 cwd 同名文件遮蔽，写穿到知识库外**
`internal/cli/cli.go:779-782`：裸文件名若恰在 cwd 存在（如导出编辑的草稿副本），会批准并 Serialize 重写 cwd 那份，真草稿仍未转正——静默失效且写穿库外。对比 Archive（cli.go:520）固定 `filepath.Base` 收敛，同源命令口径不一。→ approve 同样收敛到库内路径。

**M-14 同目录重复注册静默串库 + 路径小写折叠遮蔽**
`internal/registry/registry.go:95-108`（AddProject 不查路径冲突）+ `:77-93`（FindByCwd 取先注册者）+ `:44-48`（NormalizePath 全小写，Linux 大小写敏感 FS 上仅大小写不同的目录互相遮蔽）。`ok init 新名` 后 `ok add` 全写进旧项目知识库，无告警。→ AddProject 查同路径已注册则告警/拒绝；小写折叠仅 Windows 生效。

**M-15 文件锁 2s 即抢占，慢临界区下互斥失效**
`internal/fsx/lock.go:50-57`：临界区含 fsx.WriteFile 的 fsync+rename，杀软实时扫描下超 2s 并不罕见；原持有者仍在执行 fn 时被抢锁并行进入——恰好复现锁要防的丢注册/丢更新，strict 模式同样中招，且抢占路径无日志。→ 抬高阈值或抢占时记日志告警。

**M-16 exe 版本资源链路无守门**
`scripts/build-dist.sh:6-8`、`scripts/build.py:77-84`：仓库内 `cmd/*/rsrc_windows_amd64.syso` 只含图标无 VS_VERSION_INFO；build 链不跑 go-winres，缺工具仅打印跳过照常出包；pre-push 漂移检测清单不含 `cmd/*/winres.json`。经 build-installer.sh 发布的 exe 文件版本资源缺失/漂移。→ build 链补 go-winres 步骤与失败即断；pre-push 清单补 winres.json。

**M-17 `ok list` 吞掉整项目条目**
`internal/cli/cli.go:570-573`：严格 `entry.Load` 单个坏文件即报错，`continue` 后该项目条目在列表整体消失且无提示，与 entry 包"错误要暴露给用户"的注释相反。→ 改 LoadTolerant 或打印警告。

### Web 前端

**M-18 【未提交改动】loadTermHist 防御只做一半，可致整页白屏且每次启动复现**
`web/app.js:610`：只校验 `cmd` 为字符串，`out/err/code` 不校验；localStorage 存了非字符串 `out`（旧版 schema/手改）时 `outBlock` 的 `(text||"").split("\n")` 抛 TypeError，`render()`（3792）无 try 兜底且 3808 行已先清空 innerHTML——只能手动清 localStorage 自愈。→ 校验补全 `out/err` 为字符串、`code` 为数字。

**M-19 Reasonix 强制模式 radio onchange 即存，违反两段式约定**
`web/app.js:551`：全文件唯一例外（沉淀模式 radio 2762 走草稿+保存），代码注释以"sidecar 即时生效"辩护，但同类设置交互不一致。→ 统一为勾选+保存两段式。

**M-20 编辑态误触侧栏/树上条目即静默弃稿**
`web/app.js:1454、1607`：长正文编辑中一次误触 `exitEdit()` 无确认丢全部输入。注释声明是既定决策，但确属丢用户输入的真实风险。→ 加脏检查确认。

---

## Low（30 项）

### 适配器层

- **L-01** RemoveHooks 半卸载：Windows 先删 wrapper 后 `loadCodexHooks` 解析失败即 return，hooks.json 留下指向已删文件的死命令（codex.go:776、qoder.go:385、qoderide.go:341 同形）
- **L-02** HandleStop/HandleCompact 吞错无日志（`hook/hook.go:333-336`、287-290），与 HandlePrompt 不一致，Stop 链路故障不可诊断
- **L-03** Codex 信任 key 含组下标，第三方组插到 ok 组之前时旧 key 永久残留（codex.go:548-564）
- **L-04** dshPatchBlock YAML 单引号未转义，用户名含 `'` 时 file URL 断裂、挂载失效（deepharness.go:69-71）
- **L-05** selfHealHooks 每 prompt 全量读各 agent 配置，固定 IO 开销（hook.go:169-187）
- **L-06** kimi okHookCommand 正则可误删用户自装的同名 `ok` 工具 command 行（kimi.go:65）

### GUI/daemon

- **L-07** 长期令牌经 URL query 下发（`/api/project/readme-asset?token=`），进浏览器历史/书签（api.go:145-153）；可改一次性短时票据
- **L-08** daemon hookHandler 读 body 失败静默置 nil 仍 200（daemon/server.go:112-115），客户端不知已坏
- **L-09** apiApprove/apiEntryArchive 把一切 ReadFile 错误伪装"条目不存在"400（api.go:1166、1219）
- **L-10** dlSnapshot 遍历 map 随机序挑任务，并发下载进度条乱跳（embedding.go:39-44）
- **L-11** `config.LoadMerged(registry.Home()+config.toml)` 路径拼接重复 ≥8 处、registry.Load+线性找项目循环重复 7+ 处，既有 `globalConfigPath()`/`findProject` 收口失守（api.go:289/819/944/1457/1556、llm.go:384 等）

### 检索/存储

- **L-12** `db.FeedbackStats(30)` 硬编码 30 天，与查询侧 `cfg.Feedback.WindowDays` 口径不一致（sync.go:389 vs feedback.go:39）
- **L-13** wiki 标签匹配双实现：query.go:474 SQL LIKE 四分支 vs branch.go:32 hasWikiTag
- **L-14** SemanticRejected 诊断块重算 max/median/RelGap，与 SemanticFloor 公式重复（query.go:286-299 vs 47-56）
- **L-15** `entry.Serialize` 对 yaml.Marshal 失败直接 panic（entry.go:66-68），库代码 panic 坏味道
- **L-16** index 包内文件职责混杂：sync.go 后半（380-533）是 INDEX.md 渲染层、query.go 尾部（404-484）是 wiki 查询群，可各自成文件

### 扩展/嵌入

- **L-17** SDK vendor 快照读循环退出即 close(notifyQueue)，与 notify() 先查后发非原子，in-flight 发送会 panic（rxext/sdk/wire.go:204/552；当前未注册 Provider 不可达）
- **L-18** serve.go:69 unmarshal 失败静默吞且无日志，宿主载荷变化时注入静默失联（hook 包同类分支均 logErr）
- **L-19** freePort 先关 listener 再由子进程 bind，TOCTOU 窗口可被抢占（manager.go:239-246；可自愈影响小）
- **L-20** enforce 吞 malformed glob 错误，规则静默失效（enforce.go:24、29；ARCHITECTURE §16.5 已列建议）
- **L-21** 每条 input.receive 调 CheckStop 使 StopCount++，auto 模式"回合"在 Reasonix 下变"输入次数"，提醒口径漂移（serve.go:76 + hook/core.go:417）

### CLI/注册表/打包

- **L-22** Archive/WikiCmd 用法错误返回 2（cli.go:508、511、896、1086），违反"CLI 错误=1、exit 2 仅留 hook stop 阻断"退出码约定
- **L-23** 备份导入校验不对称：项目名过 ValidProjectName，条目文件名 parts[3] 不做同款校验，`con.md` 类文件在 Windows rename 失败致导入中途挂（backup.go:139-150）
- **L-24** okd/ok 两份 `findWebDir`/`isDir` 整段复制，仅靠注释约定手工同步（cmd/okd/main.go:31-51 vs cmd/ok/main.go:159-179）
- **L-25** registry.Home() 双重失败回退相对路径 `".openknowledge"`，数据根随 cwd 漂移且两次调用可能不一致（registry.go:34-38）
- **L-26** ARCHITECTURE.md:666 称 `ok index` 无 key 时"退出码 1"，实际 return 0（cli.go:459，代码注释称刻意对齐）——文档漂移

### Web 前端

- **L-27** 【未提交改动】RRF 分浮窗仅 hover 驱动，键盘/触屏不可达；滚动时浮窗滞留与锚点脱节（app.js:1841）
- **L-28** `pswitch` 设了 `role="switch"` 但从不设 `aria-checked`（app.js:706）
- **L-29** `'<span class="ls ls-'+l.src+'">'` 未转义直拼 class（app.js:3372；当前后端枚举安全，属防御缺口）
- **L-30** 重复形状：四份"惰性缓存+refresh+menu 守卫"页面模板可抽 `lazyPage()`（app.js:412/966/2533/3439）；复合键 `project+"\n"+file` 手工拼 4 处应收敛 `keyOf()`（1009/1013/2231/2166）；两套下拉键盘导航近乎逐行重复（1319-1350 vs 1797-1829）；双击计时检测同款 4 份（1477/1534/1561/1611）

---

## 结构性建议

1. **锁纪律收口**（对应 H-01/H-02/M-02/M-15）：项目已证明 `WithFileLock` 模式可行，剩余问题是四处新入口绕开它。建议按"宿主 settings 写入"与"全局 config 写入"各建一个收口点，所有写入方走收口。
2. **适配器共享引擎**（对应 M-03）：五份同形 load/strip/write + 三套 wrapper 建议在下一个适配器加入前参数化抽取；Qoder cmd /s、Codex 信任门历史坑已各修三份，是漏改的根源。
3. **gui/api.go 拆分**：1926 行 60+ 端点混装条目 CRUD/终端执行/导入导出/README 渲染，llm.go 内嵌完整 prompt 工程，browser/window/changelog 同包，已超出 ARCHITECTURE.md §5.11 自述的"gui 只出 HTTP API 与静态页"。建议按 changelog/llm/window 拆子包，共享校验下沉（H-01 正是膨胀的产物）。
4. **app.js 渐进拆分**（无需引入构建工具）：三个天然边界——markdown 渲染管线（721-901，纯函数零 state，应最先拆、可独立测试）→ 设置页（2505-3343，约 840 行自内聚）→ 终端面板（1700-1940，状态与 DOM 引用自足）。

---

## 附：抽查核实记录

主审对以下高危发现做了二次代码核实，均属实：

- H-01：api.go:1266 起 `os.ReadFile` → 改写 → 写盘，函数内无 WithFileLock ✅
- H-02：claude.go:310-322 InstallHooks 全程 loadClaudeSettings → writeClaudeSettings 无锁 ✅
- H-03：reasonix.go:73 intercepts 两项 vs serve.go:52 订阅三项 ✅
- H-04：cli.go:167-174 `body = string(data)` 无 front matter 检测 ✅
- M-07：sync.go:161-165 embedBlocked 分支与 256-294 分批失败路径吻合 ✅

---

## 修复核验（2026-08-23 第二批，主审逐项对照代码验证）

基线：`go build ./...`、`go vet ./...`、`go test ./...` 33 包全部通过。修复批次共 37 文件 +1151/−391，新增 4 个测试文件（settings_lock_test / provenance_test / tray_windows_test 及多项既有测试扩充）。

### 已修复并核验（16 项）

| 编号 | 核验要点 |
|---|---|
| H-01 | `config.SetCaptureAndAutoBorn` 一次锁内重写 [capture]+[provenance] 单次落盘；gui 层 `setProvenanceAutoBorn` 复制版已删除，两步中间态一并消除 |
| H-02 | claude/codex/qoder/qoderide/zcode 五适配器的 InstallHooks/EnsureHooks/RemoveHooks 读-改-写全程 `fsx.WithFileLock`（每文件 6 处），新增 settings_lock_test.go |
| H-03 | manifest 补全三项 intercepts（`reasonixIntercepts`）；`reasonixCurrent` 校验 intercepts 全集，旧 manifest 判过期触发自愈重写 |
| H-04 | `entry.StripFrontmatter`（BOM/CRLF 同口径）+ stderr 警告，元数据以命令行参数为准 |
| M-01 | kimi（HooksBlockFor 全量比对含超时）/opencode/pi（渲染全量比对，dsh 同款）三处 HooksInstalled 补 exe 校验 |
| M-02 | `RemoveSection` 锁内实现（removeSectionLocked），与 updateGlobalConfig 同纪律 |
| M-05 | 托盘 openOrFocus 异步化 + opening 防抖 + guiHwnd 锁保护；消息线程不再被 20s 轮询卡死 |
| M-06 | `terminalTimeoutFor`：wiki 走 10min 长超时，其余保持 10s；超时提示带实际时长 |
| M-07 | `failEmbed` 回滚本轮已提交向量行（DELETE IN），清理失败不遮蔽原始错误 |
| M-11 | Ensure 拆出 spawnLocked，就绪等待移出 mu 锁外；新增 TestStopNotBlockedByEnsure |
| M-13 | approve 固定收敛库内 knowledge 目录（与 Archive 同口径），消除 cwd 遮蔽写穿 |
| M-14 | NormalizePath 小写折叠仅 Windows；AddProject 同路径冲突拒绝并报已注册项目名 |
| M-15 | lockStaleAge 2s→15s（注释说明杀软场景）；抢占路径补日志 |
| M-16 | build-dist.sh 缺 go-winres 即断；build.py 失败即退（--skip-winres 须显式） |
| M-18 | loadTermHist 补校验 out/err 字符串、code 数字，白屏路径关闭 |

### 未修（本轮未处理，9 项 + Low 系列大体未动）

- **M-10**：`unhealthyStreak` 仍只在健康分支归零（manager.go:249），Stop/Ensure 不重置——"连续两轮"判定在重拉后仍可能退化为"一轮"。
- **M-17**：`ok list` 仍 `entry.Load` 出错即 `continue`（cli.go:578-580），坏文件吞掉整项目条目。
- **M-19/M-20**：前端 radio 即存与编辑态误触弃稿未改。
- **M-04/M-08/M-09/M-12**：embedding 复测误报、语义通道全量 body、recency 决胜键、rxext 锁内 selfHealHooks，文件未动。
- **M-03**：适配器共享引擎（结构性重构）未做，三套 wrapper 仍逐字复制——与本轮 H-02 修复无冲突，可后续单独做。
- **L-27** 部分：浮窗重绘时已关闭（paintTermHist 调 hideScoreTip），但键盘/触屏仍不可达。

### 修复批次引入的一个行为变化（提示，非缺陷）

`apiCaptureSet` 现在无论是否传 `auto_born` 都走 `SetCaptureAndAutoBorn` 单次写：显式 project 模式下，若请求未带 `auto_born`，会把**从全局继承的 auto_born 当前值固化写进项目 config.toml**（此前只在显式传入时才写 [provenance]）。此后再改全局 auto_born，该项目不再跟随。若期望"项目缺省跟随全局"语义，需在未显式传入时跳过 [provenance] 重写。

---

## 修复核验（2026-08-23 第三批，主审逐项对照代码验证）

基线：`go build ./...`、`go vet ./...`、`go test ./...` 33 包全部通过。本批覆盖第二批剩余全部中危与绝大部分 Low，多项 Low 的修复质量超出预期（如 L-07 做成短时票据机制、L-03 做成索引无关哈希 + 陈旧节扫描）。

### 已修复并核验（中危 8 项 + 低危 23 项）

| 编号 | 核验要点 |
|---|---|
| M-04 | BaseURL 回填移出 key 分支（name-only 复测 ollama 生效）；密钥保护语义保持：显式改地址仍须重填 key |
| M-08 | 语义通道两阶段查询：先只读 filename+向量算余弦分布并准入，准入后按名回查 body |
| M-09 | 统一比较器 `hitLess`（分数/标题/文件名三级决胜），recency/feedback/query 三处共用 |
| M-10 | `stopLocked` 重置 unhealthyStreak（实例生灭归零） |
| M-12 | `maybeSelfHeal`：独立 healMu + `hook.SelfHealMinInterval`（5min）节流，移出会话锁；与 hook 包 HandlePrompt 同口径 |
| M-17 | `ok list` 改 `entry.LoadTolerant`，坏文件告警 stderr、好条目照常列出 |
| M-19 | Reasonix 模式 radio 改草稿+保存两段式（onchange 只判脏，保存按钮实时禁用） |
| M-20 | `exitEditGuarded` 脏检查确认（edDirty 对比基线，cfmDiscard 双语），侧栏/树误触不再静默弃稿 |
| L-01 | RemoveHooks 解析失败即停且保留 wrapper（无死命令残留）；hooks.json 不存在时 Windows 才删 wrapper |
| L-02 | HandleStop/HandleCompact 全路径 logErr |
| L-03 | 信任哈希输入去掉组索引（索引无关）+ `codexStaleTrustKeys` 扫描清除陈旧 hooks.state 节 |
| L-04 | YAML 单引号转义（`'` → `''`） |
| L-05 | selfHealHooks 进程内 5min 节流（同 M-12） |
| L-06 | kimi 正则收紧：引号感知 + 限定 `ok[.exe] hook prompt|post-tool|stop` 子命令 |
| L-07 | readme 资产改短时票据：前端 POST 申领 ticket（走 withAuth）再拼 `?ticket=`，长期 token 不进 URL |
| L-08 | daemon hook 读 body 失败记日志并 400，不再伪装空事件 200 |
| L-09 | apiApprove/apiEntryArchive 区分 ErrNotExist（400）与其余 IO 错误（记日志 500） |
| L-10 | dlSnapshot 按 key 排序遍历，进度条稳定 |
| L-11 | `resolveConfigTarget`/`globalConfigPath`/`findProject` 收口，api.go/llm.go/embedding.go 的路径拼接与线性查找重复全部消除 |
| L-12 | Sync 传 `FeedbackWindowDays`（config 口径贯通，backup 同步传） |
| L-13 | wiki 标签判定收敛为 Go 侧唯一实现 `hasWikiTag`，SQL LIKE 仅作粗筛 |
| L-14 | `cosStats` 共享 max/median/relGap 计算，SemanticFloor 与诊断块不再两处维护 |
| L-15 | `Entry.Serialize` 改返回错误，不再 panic |
| L-18 | rxext unmarshal 失败 logErr，注入失联可诊断 |
| L-20 | enforce malformed glob 上抛错误 |
| L-21 | `CheckStopTurn` 合成回合（goal 自动续跑）不推进 auto 回合计数 |
| L-22 | cli.go 不再有 `return 2`，退出码约定恢复 |
| L-23 | 备份导入条目文件名过 ValidProjectName，非法名跳过计数不中断 |
| L-24 | `webdir.Find()` 下沉共享，ok/okd 各自的 findWebDir 复制删除 |
| L-25 | Home() 双重失败走 `fallbackHome()`（sync.Once 固定选择，不再随 cwd 漂移） |
| L-26 | ARCHITECTURE.md 同步更新 |
| L-28 | pswitch 同步设置 aria-checked |
| L-29 | `esc(String(l.src))` 转义后拼 class |

### 剩余未修（汇总两批后）

- **M-03** 适配器共享引擎：三套 wrapper（codex/qoder/lingma）仍逐字复制——结构性重构，明确推迟，建议下一个适配器加入前做。
- **auto_born 钉死行为**（见上节提示）：apiCaptureSet 仍无条件重写 [provenance]，显式 project 且未传 auto_born 时会把继承值固化进项目配置——保持现状则建议在文档/帮助中声明该语义。
- **L-16** index 包内 sync.go/query.go 的职责拆分（渲染层/wiki 查询群各自成文件）——未做。
- **L-17** rxext SDK vendor 快照的 close-channel 竞态——未动（当前不可达，属上游快照）。
- **L-19** freePort TOCTOU 窗口——未动（可自愈，影响小）。
- **L-27** RRF 分浮窗键盘/触屏可达性——部分（重绘时已关闭浮窗，仍 hover-only）。
- **L-30** 前端重复形状（lazyPage 模板/keyOf 复合键/双击计时/两套下拉导航）——未抽。

---

## 修复核验（2026-08-23 第四批，主审逐项对照代码验证）

基线：`go build ./...`、`go vet ./...`、`go test ./...` 33 包全部通过。本批清掉第三批剩余的结构项。

| 编号 | 核验要点 |
|---|---|
| M-03 | 新 `agentx/wrappers.go` 共享引擎：`ensureHookWrappers`/`removeHookWrappers` 参数化（home/事件表/错误前缀），codex/qoder/qoderide 三处改为薄委托；背景注释完整保留（cmd /s 剥引号、与 exe 位置解耦、信任哈希不过期），并如实登记"用户名含空格仍会被上游 bug 截断"的已知限制 |
| L-16 | sync.go 的 INDEX.md 渲染层拆出 `indexmd.go`，query.go 的 wiki 查询群拆出 `wiki_query.go` |
| L-17 | vendor SDK 竞态修复：notifyMu/notifyClosed 使 close 与先查后发互斥；按快照纪律在 sdk/doc.go 登记"本地偏离"，同步上游时不丢 |
| L-27 | 分浮窗 tabIndex=0 + focus/blur + click（触屏兜底），键盘与触屏可达；锚点滚动/重绘即关 |
| L-30 | 抽出 `lazyPage`（414）/`keyOf`（1020）/`ddNav`（1227），页面模板、复合键、下拉键盘导航去重 |

### 接受现状（2 项）

- **L-19** freePort TOCTOU：保持原实现（bind 失败可自愈，影响小）。
- **auto_born 钉死行为**：apiCaptureSet 保持一次锁内双小节写（避免中间态优先），代码注释以"缺省表示保持不变"描述；"显式 project 未传时继承值被固化、此后不再跟随全局"的语义未在帮助文档显式声明——已知悉并接受，后续如收到用户反馈再议。

### 四批累计

4 高 / 20 中全部修复；30 低中修复 29 项、接受现状 1 项（L-19）。R2 报告全部发现清零（除上述两项明示接受）。
