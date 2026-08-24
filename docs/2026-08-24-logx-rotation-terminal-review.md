# 未提交改动双轴 Review（日志轮转 + 终端定位/最小化）

日期：2026-08-24
固定点：`HEAD`（32f61b1，v2.22.3）
审查对象：未提交工作区改动——9 个修改文件（cmd/ok/main.go、internal/daemon/{client,run}.go、internal/embedsidecar/manager.go、internal/gui/llm.go、internal/hook/hook.go、internal/rxext/serve.go、web/app.js、web/style.css）+ 2 个新增文件（internal/logx/rotate.go、internal/logx/rotate_test.go）
Spec 源：`docs/2026-08-23-manage-search-terminal-requirements.md`（只覆盖终端面板部分；日志轮转无 spec）
方式：code-review 技能双轴并行子代理（Standards / Spec），以下为两轴原始结论汇总。

---

## Standards

### (a) 文档化标准违规（docs/ARCHITECTURE.md）

1. **包清单漂移——硬违规。** §3 写明"单 module（`openknowledge`），**15 个包**，严格单向依赖、无环"，§3/§4/§11 的包清单是穷举式。本次改动新建 `internal/logx` 并被 `cmd/ok`、`daemon`、`embedsidecar`、`gui`、`hook`、`rxext` 引用，但未更新 §3 的包数/分层图、§4 目录树、§11 依赖图。依赖关系上 logx 是纯 stdlib 叶子包，"严格单向依赖、无环"规则本身未被破坏——只是文档过期。
2. **存储布局漂移——硬违规。** §8 穷举了 `~/.openknowledge/` 的内容；轮转引入了新的 `logs/` 归档子目录（`<名>-<日期>_<时分秒>.log`，保留 7 天），§8 未提及。§16 第 4 条（"ok.log 治理……加大小滚动"）也应标记为已完成——本次改动正是*实现*了该文档化建议（属于对齐加分项）。
3. fail-open 约定（§10）、密集中文注释风格、web 前端零依赖（§4）、仅用 `testing` 的测试栈（§2）均被遵守；rotate.go 中关于 Windows 改名/句柄窗口的注释很符合仓库风格。

### (b) 基线坏味道（判断性意见）

- **Duplicated Code**——`p := filepath.Join(registry.Home(), "ok.log"); logx.RotateIfOversize(p); f, err := os.OpenFile(p, …)` 这段被原样加到四处（cmd/ok/main.go、internal/gui/llm.go:250、internal/hook/hook.go:158、internal/rxext/serve.go:200）。一个 `logx.AppendOKLog()` 式辅助函数可以收拢它。（开日志的重复在改动前就存在，本次是放大而非制造。）
- **Duplicated Code / Shotgun Surgery**——web/app.js 的 `catKeyOf(e)` 重新编码了 `groupEntries` 的分类口径，`jumpToEntry` 硬编码了 reference 子组名 `"架构总览"/"演进历程"/"其他"`，与 `refSubGroups` 的命名重复（hunk 自己的注释都承认"与 groupEntries 分组口径一致"）。将来重新分组要改两处遥远位置，且是静默失效而非编译报错。→ 应从分组代码导出键计算。
- **Speculative Generality（轻微）**——rotate.go 的包级变量 `maxLogBytes`/`archiveKeepDays` 注明"供测试调小"，可接受的 Go 测试接缝，仅记录。

### 正确性相关（判断性意见）

- **跨进程轮转竞态**：hook 以独立进程运行（§7.2），两个并发 `ok.exe hook` 可能都 stat 到超限后竞态 `os.Rename`，败者失败跳过（fail-open、有界）。非 Windows 上，在他者改名与重开之间已打开 ok.log 的进程会继续往孤儿 inode 追加。在 fail-open 契约下属良性，但值得加注释。
- **撞名循环的 TOCTOU**（rotate.go:35-40）：Lstat 再 rename，两个轮转者可能选中同一个空名，败者改名失败 → 日志继续增长直到下个窗口。fail-open、影响低。
- client.go 注释声称"旧 daemon 已不在，句柄已释放"，但 `Ensure()` 在 daemon *不健康*（可能仍活着卡死）时也会触发；Windows 上活进程的句柄会让 rename 失败。rotate.go 的 fail-open 注释已覆盖此情形——只是 client.go 的注释承诺过度。

---

## Spec

### (a) spec 要求但缺失/不完整

无可归因于本 diff 的缺失。需求 1（徽标）、2（搜索命令）、4（布局偏好）、5（树分组）未被本 diff 触及，其状态/注释（`catDefaulted // 需求 5` 等）在 HEAD 已存在——本 diff 是增量，非那些需求的实现。

### (b) 未被要求的行为（scope creep）

1. **日志轮转（Go 侧全部改动）**：`internal/logx/rotate.go`、`rotate_test.go` 及六处 `RotateIfOversize`/`CleanArchives` 接入点**无 spec 可用**。spec 文档只覆盖管理页终端工作；`docs/` 下无任何文档提及轮转/保留期。一个无关特性被打包进了 GUI 终端改动。
2. **search 命中行点击定位**（`jumpToEntry`、`pendingJump`、`.t-link`、`termJumpTip` i18n）：无 spec 覆盖。spec 第 79 行只要求"**search 命中行分数悬浮释义**……鼠标悬浮弹出浮窗说明该分数是 RRF 排名融合分"——是分数悬浮提示，不是整行点击导航。
3. **终端最小化/恢复**（`termMin` 状态、`term-min` 按钮、`.term-restore` 侧栏按钮、`ICON.term`）：无 spec 覆盖。spec 需求 4 只定义终端"**左 / 中 / 右 / 下**四档"位置，没有折叠面板的提法。

两处 GUI 新增都遵循了文档的 i18n 约定（zh+en 双份 `I18N` 条目）并复用既有状态模式，看起来是有意为之而非误入——但确实是未被要求的。

### (c) 看似实现但实现有误

1. **白名单报错改为"、"连接**（`web/app.js:2072`）：spec 第 91 行要求错误卡片列出可用命令要"**与截图的失败卡片形态一致**"，而原型（`prototype-manage-terminal.html:1009`）渲染的是 `'可用命令：\n  ' + …`——竖排、换行缩进列表。diff 的横排"、"连接偏离了原型形态（尽管按 diff 注释，它与 `internal/gui/api.go:1045` 的后端既有文案口径一致）。
2. **可点行正则验证为正确**：`ok search` 输出 `%.4f\t%s (%s)\n`（internal/cli/cli.go:408）。新的 `/ \(([^()]+?\.md)\)\s*$/` 锚定行尾且要求末括号内是 `.md`，即使标题含全角括号也能匹配。边角情况：文件名含字面 `(` `)` 不匹配；标题以 `(…​.md)` 结尾会误判。
3. **继承来的局限（记录，非新引入）**：分数浮窗与跳转链接都以 `/^0\.\d{4}\t/` 判定，只匹配 RRF 形态分数；`fusion="weighted"` 模式（internal/index/query.go:196）下分数形态不同，两个功能都不会触发。spec 第 79 行只描述了 `0.0164\t…` 形态，所以符合 spec 字面，但属潜在缺口。

---

## 小结

- **Standards 轴**：2 条硬违规 + 3 条坏味道 + 3 条正确性意见。最严重：**ARCHITECTURE.md §3/§4/§8 的包清单与存储布局文档漂移**（新 logx 包与 logs/ 归档目录未入档）。
- **Spec 轴**：3 条 scope creep + 1 条实现偏差。最严重：**日志轮转整体无 spec、与 GUI 终端改动捆绑提交**；其次是白名单报错形态偏离 spec 指定的原型卡片样式。

---
---

# OpenKnowledge 全量代码审查报告（第三轮）

- **日期**：2026-08-24
- **范围**：全仓库（internal/ 30 个包约 39.7k 行 Go、cmd/ 三入口、web/ 前端 app.js 4066 行 + style.css），含工作区未提交改动（logx 轮替 + 终端定位/最小化，即本文件上半部分的双轴审查对象）
- **方法**：6 条并行审查线（agentx 适配器层 ／ hook+setupx+state+enforce ／ gui+daemon+tray 服务层 ／ index+retrieve+wiki+embed 检索存储层 ／ cli+config+registry+fsx+backup+llmx+logx 持久化层 ／ rxext+web 前端），逐文件完整阅读 + 调用链交叉验证；Medium 级发现由主审二次核实代码证据（7/7 成立）
- **基线**：`go build ./...` 通过、`go vet ./...` 无告警、`go test ./...` 30 个测试包全部通过
- **与前两轮的关系**：第一轮（`docs/2026-08-23-full-code-review.md`，53 项修复随 v2.22.0 入库）、第二轮（`docs/2026-08-23-full-code-review-r2.md`，4 高 / 20 中 / 30 低）。本轮逐条核验：**r2 的 4 条 High（setProvenanceAutoBorn 裸写、宿主 settings 无锁读改写、Reasonix manifest、add --file front matter）全部已修且未回退**，抽查的 M/L 项亦全部维持修复（明细见文末核验表）。
- **计数口径**：同根问题合并为一条编号；编号前缀 A–F 对应六条审查线。本轮 **0 高 / 7 中 / 16 低**——前两轮的系统性问题（鉴权链、锁纪律、写双轨）已基本清剿，剩余为修复漏网分支与同款形状的残余实例。

---

## 总体评价

两轮治理后的代码面明显收敛：r1 的远程攻击链（hostGuard 三重防线 + fragment 发 token + key 回查钉死 base_url）、r2 的锁纪律收口（updateGlobalConfig／registry.Update／state.Update 单一写路径）经逐行核验全部健壮，且都有判别性回归测试背书。fail-open 铁律（hook 入口 panic→0、rxext 拦截器 err 折叠 Continue、技能/自愈 stat 失败 no-op）贯彻一致；原子写（tmp+fsync+rename）除两处残留（C-02、A-04）外无例外；前端 XSS 管线在本次新增代码（jumpToEntry、termMin）中未破坏。

剩余风险收敛为两个主题：

1. **"复制既有函数时丢了关键包裹/分支"的同款形状仍在产生新实例**——kimi/reasonix/dsh 的宿主文件读改写没进 r2 H-02 修过的锁包裹（A-01）；`ok propose --file` 漏掉 r2 H-04 给 `ok add --file` 补的 front matter 剥离（E-01）；rxext 的 tool.after 只认 `path` 字段，漏掉 hook 层记载过坑的 `file_path` 兼容（F-02）；技能烘焙 exe 路径没跟上 r2 M-01 给 hooks 补的迁移自愈（B-01）。四条全是"修了 A 处、同形 B 处漏网"。
2. **静默停摆/降级的残余路径**——embedding 末批 meta 写失败不清回滚，落入自家注释里写明的"永久停摆"分支（D-01）；前端 pendingJump 重试绕过全部重渲守卫，会突然弹 confirm/清终端输入（F-01）。

---

## 修复优先级建议

| 优先级 | 发现 | 理由 |
|---|---|---|
| P1 | A-01 三处宿主文件 RMW 补 WithFileLock | 与 r2 H-02 同根，一次收口同形解决，settings_lock_test 现成模板 |
| P1 | D-01 meta 写失败回滚 written 向量行 | 一处补齐消灭"永久静默停摆"残余路径（failEmbed 已有，缺的只是两行） |
| P1 | E-01 propose --file 复用 Add 的 StripFrontmatter | 已沉淀历史坑的同族命令漏网，修复成本一行 |
| P1 | F-02 rxext tool.after 补 file_path 双字段 | 已记载坑"派发层字段名断链零派发"的漏网实例，enforce 对 Reasonix 整体失效且无观测 |
| P1 | F-01 pendingJump 重试块移到守卫后 | 用户可感知（突然 confirm 丢草稿/清输入），edBusy 时清挂起即弃 |
| P2 | B-01 技能烘焙路径自愈、E-02 backup parts[1] 校验 | 各为 r2 M-01／"GUI 加固 CLI 裸奔"模式的代表残余 |
| P3 | 其余 Low | 顺手修；E-04（四份 ok.log 追加助手收拢 logx）建议与 E-01 同批做 |

---

## Medium（7 项，均已主审核实）

### A-01 kimi/dsh/reasonix 宿主文件读改写无 WithFileLock（r2 H-02 同形漏网）

- **位置**：`internal/agentx/kimi.go:146-193`（UpsertHooksBlock：os.ReadFile→改→fsx.WriteFile，裸 RMW）、`kimi.go:260-286`（RemoveHooks）、`kimi.go:200-221`（EnsureHooksBlock）；`deepharness.go:139-143`（cordis.patch.yml upsert）；`reasonix.go:236-245/248-273/277-293`（plugin-packages.json upsert/remove）
- **证据**：`settings_lock_test.go:23-27` 的锁回归测试只覆盖五家 JSON 适配器（claude/codex/qoder/qoderide/zcode）
- **影响**：GUI 安装与 selfHealHooks（每 5min）并发时互相覆盖丢更新；reasonix 的 plugin-packages.json 含第三方插件登记，丢失即静默失效
- **建议**：三处 RMW 补 `fsx.WithFileLock` 并入 settings_lock_test。opencode/pi 为自家独占整文件写（marker 门控、内容幂等），不另计

### B-01 技能烘焙 exe 路径无自愈、状态检测只查存在性（r2 M-01 同款缺口）

- **位置**：`internal/setupx/setupx.go:78-79`（`{{EXE}}` 烘焙绝对路径）+ `internal/hook/hook.go:209-213`（selfHealHooks 只调 `a.EnsureHooks(exe)`）+ `internal/gui/api.go:437-444`（skillsInstalled 仅 `os.Stat`，doctor 不查技能）
- **影响**：exe 移动/换路径后（gui-split 部署迁移是真实场景）技能指令指向死路径，agent 执行报错；GUI 状态页与 doctor 均误报正常
- **建议**：selfHealHooks 同窗口比对待写内容不等即重写（InstallSkills 本身幂等），状态检测比对烘焙路径

### D-01 embedding 末批 SetMeta 失败不清回滚，向量写入永久静默停摆（r2 M-07 残余路径）

- **位置**：`internal/index/sync.go:311-316`——向量批全部提交后 `SetMeta(embedding_model/dim)` 失败直接 `return err`
- **证据**：`sync.go:264-268` 的 failEmbed 注释明写"否则会留下 meta 未写 + vectors 有部分行……向量写入永久静默停摆"，但该回滚只护住批失败；meta 写失败（磁盘满/busy 超时）时本轮向量已落库而 meta 未写，下轮 Sync 命中 `sync.go:165-167`"meta 空+HasVectors→embedBlocked"
- **建议**：meta 写失败同样走 failEmbed 回滚，或并入末批小事务同提交

### E-01 `ok propose --file` 不剥 front matter（r2 H-04 同族漏网）

- **位置**：`internal/cli/cli.go:718-724`——`content = string(data)` 直接落库；对照 Add 在 `cli.go:175-178` 已剥离+警告
- **影响**：带 front matter 的文件提议草稿仍层层嵌套、内层元数据静默变正文；propose_test.go 无 --file 用例
- **建议**：复用 Add 同款 `entry.StripFrontmatter` 剥离

### E-02 backup 导入条目路径的 `parts[1]` 未校验（"GUI 加固 CLI 裸奔"残余）

- **位置**：`internal/backup/backup.go:139-150, 224`——registry.toml 内项目名已过 `ValidProjectName`（:170-174），但 `projects/<name>/knowledge/x.md` 的 `<name>` 不校验
- **影响**：包内含 `projects/con/knowledge/x.md` 且 registry 不含 "con" 时：Windows 上 MkdirAll 即挂，"重新导入可续传"指引对该包永久失效；Linux 上写入未注册孤儿目录并重建索引
- **建议**：收集条目前对 parts[1] 同款校验

### F-01 pendingJump 重试绕过全部重渲守卫，可弹意外 confirm/丢输入

- **位置**：`web/app.js:1056-1060`——重试块位于 `:1063-1065` 的 menu/edBusy/term-in/search 焦点守卫**之前**（未提交新改动）
- **影响**：点击缓存外 search 命中后用户进入编辑态打字或正在终端输入 → refreshManage 完成即调 `jumpToEntry(...,true)`：`exitEditGuarded()` 突然弹"丢弃未保存修改"confirm，成功路径无条件 `render()` 清掉终端输入框未发送字符；重试落空时 `return` 还导致本轮刷新数据不落画
- **建议**：重试块移到守卫后，edBusy/termBusy 时直接清 pendingJump 放弃

### F-02 rxext tool.after 只认 `path` 字段，未复用 hook 层双字段兼容

- **位置**：`internal/rxext/serve.go:258-263` 仅解 `json:"path"`；对照 `internal/hook/hook.go:68-80` `Event.FilePath()` 特意做 path/file_path 双字段
- **影响**：宿主字段名漂移 → TrackTouched 静默零派发 → changelog enforce 对 Reasonix 整体失效且无观测（已记载坑"派发层字段名断链零派发"的漏网实例）
- **建议**：同款双字段解析

---

## Low（16 项，摘要）

| 编号 | 位置 | 问题 | 建议 |
|---|---|---|---|
| A-02 | opencode_plugin.ts:7、pi_extension.ts:38、dsh_plugin.js:7 | timeout_sec 对插件型宿主不生效（硬编码 10s/5s），GUI 设置后静默忽略 | 文档声明或模板参数化 |
| A-03 | ARCHITECTURE.md:550 | dsh patch 行文档写绝对路径，代码实为 `file:///` URL | 修文档 |
| A-04 | reasonix.go:113-117 | writeReasonixState 手写 tmp+rename（固定名、无 fsync）偏离 fsx.WriteFile | 改用 fsx.WriteFile |
| B-02 | enforce.go:19 | 多条同类 changelog_required 规则共享防重键，注释称"每规则"实为"每 type" | 键改规则指纹或文档声明 |
| C-01 | api.go:1812-1818、:340 | apiHeartbeat/listProjects 把注册表项目名拼路径未过 validProjectName，与 apiProjectDelete 既有防线不一致 | 补同款校验 |
| C-02 | changelog.go:151 | apiChangelogSeen 用 os.WriteFile 裸写 gui.json，违反原子写纪律 | 改 fsx.WriteFile |
| C-03 | daemon/run.go:159、daemonx.go:132 | Stop/StopDaemon 无条件 Remove 凭据，无 Run 退出 defer 的 PID 复核，并发窗口可误删新 daemon 凭据 | Stop 同样 PID 复核 |
| C-04 | gui/llm.go:216-228 | apiLLMTest 的 BaseURL 未 trim（ProfileSave 有 trim），带空格复测误报不一致 400 | 比对前 TrimSpace |
| C-05 | embedsidecar/manager.go:147 | 模型切换路径轮替大概率失效：stopLocked 只 Kill 不 Wait（注释明示回收归看护 goroutine），紧接的 rename 因旧句柄未释放而跳过 | Kill 后等 waitCh 或轮替延后 |
| D-02 | embedsidecar/sidecar.go:88-95 | Touch() 跨进程无锁读改写状态文件，与 Ensure/Stop 交错可复活已删 State（约 20s 自愈窗口） | 容忍写失败即弃或改心跳文件 |
| D-03 | sync.go:183-193、db.go:150-151 | draft/mandatory 条目也计算 embedding 但检索恒排除（纯浪费）；db.go 注释与 archived 可检索的设计矛盾 | 跳过二者；修注释 |
| E-03 | logx/rotate.go:41-44 | rename 保留原 mtime：ok.log 超 8MB 且最后写入早于 7 天时，归档随即被 CleanArchives 判过期删除（归档即焚） | 归档后 Chtimes 或按文件名判期 |
| E-04 | hook.go:158 等四份 | ok.log 追加助手（rotate+append+前缀）四份逐字复制；gate.go:39-67 又一份 replaceSection 内联复制 | 抽 logx 共享 helper（与上半部分 Duplicated Code 结论一致） |
| F-03 | app.js:2072、api.go:1045 | 白名单文案前后端口径已一致（11 条同集、同"、"横排）但 en 语言仍用中文顿号、展示顺序两端不同 | 按语言选分隔符、顺序对齐 |
| F-04 | app.js:1934-1938 vs 1468-1480 | catKeyOf 逐字复制 groupEntries 归组优先级，双实现漂移会让定位展开错误类目 | 导出共享实现 |
| F-05 | app.js:1571-1573 | jumpToEntry reference 分支依赖 BRANCH 惰拉，首次调用分支信息未到位时定位落空（低概率） | branch 就绪后再重跳一次 |

---

## r1/r2 发现核验结论（本轮全量核验）

**r2 四条 High 全部已修且未回退**：

| r2 编号 | 结论 | 证据 |
|---|---|---|
| H-01 setProvenanceAutoBorn 裸写 | 已修 | config/provenance.go:17-32 `SetCaptureAndAutoBorn` 锁内一次写；gui 复制版已删，api.go:1321 唯一调用 |
| H-02 宿主 settings 无锁读改写 | 已修（五适配器） | claude.go:87/115/183、codex.go:629/664/713、qoder.go:177/206/241、qoderide.go:139/164/198、zcode.go:169/190/221 全程锁内 + settings_lock_test；残余见 R3 A-01 |
| H-03 Reasonix manifest 不一致 | 已修 | reasonix.go:61 intercepts 三项 = rxext/serve.go:60 订阅；:216 校验覆盖，旧 manifest 判过期自愈 |
| H-04 add --file front matter | 已修 | cli.go:175-178 + cli_test.go:1282；同族漏网见 R3 E-01 |

**其余抽查项均维持修复**（逐条证据见各审查线，摘要）：M-01 HooksInstalled 全量比对（kimi.go:245-249、opencode.go:82-87、pi.go:68-73）；M-02 RemoveSection 锁内（uninstall.go:81）；M-05/M-06 tray 异步防抖、wiki 长超时；M-07 failEmbed 批失败回滚（残余路径见 R3 D-01）；M-08/M-09 语义两阶段、hitLess 三处共用；M-10/M-11 stopLocked 计数重置、锁外就绪等待；M-13 approve 收敛 Base；M-14 registry 串库拒绝；M-15 锁 15s 抢占；M-16 build-dist 缺 winres 即断；M-17 LoadTolerant；M-18 loadTermHist 校验；M-19 草稿+保存两段式；M-20 exitEditGuarded；L 系（L-01~L-06、L-12~L-23、L-25、L-27~L-30）全部已修。r1 H-05（embedding 移出写事务）未回退（sync.go:252 批 32 小事务）；r1 M-02（配置写双轨）已收口（五个行级写全部锁内）。

**r1 防线正面确认**：hostGuard 覆盖全部路由、全部 /api/* 走 withAuth（唯二例外刻意且有测试背书）；token 仅经 fragment 下发；llm/embedding test 回查 key 钉死 base_url 相等；终端执行白名单精确匹配、参数数组直传无 shell、cwd 过校验、输出 64KB 截断。

**正面结论**（各线确认健壮）：

1. **fail-open 铁律全链路成立**：cmd/ok/main.go:88-127（panic→0、未知事件→0）、rxext 三拦截器全路径 Continue + continueOnPanic 显式置 err=nil（SDK 吞 result 坑有注释钉死）、EnsureHooks stat 失败一律 no-op 不复活。
2. **锁纪律成体系**：registry.Update 唯一写路径（TestUpdateConcurrentWritersKeepAll 背书）；state 全部写路径走 state.Update 锁内重放+原子落盘（TestUpdateMergesConcurrentFields/TestUpdatePreemptsStaleLock）；config 五个行级写全部锁内。
3. **检索层双保险**：FTS MATCH 双重转义 + SQL 全参数化；分通道独立准入 + 模型无关 SemanticFloor；RRF 与 weighted 形态一致。
4. **前端 XSS 无回归**：新增代码全走 textContent/属性赋值，60+ 处 innerHTML 逐点复核均静态或经转义管线；termMin 会话级不落盘，与布局持久化互不干扰。
5. **新改动（logx 轮替）接入核验**：六处接入点窗口选择一致（拉起前/append 前/daemon 启动）、fail-open 正确、四测试断言可观测终态（判别性成立）；已知竞态（跨进程 rename 败者跳过）属明示接受，见本文件上半部分。

---

## 小结（R3）

第三轮全量审查：**0 High / 7 Medium / 16 Low**。前两轮的 4+5 条 High 全部确认修复且未回退，系统性问题（鉴权链、锁纪律、写双轨）已清剿完毕；剩余 Medium 全部是"修复的同款形状漏网分支"（A-01/B-01/E-01/F-02）与"静默停摆残余路径"（D-01/F-01），单条修复成本均为一行到一函数级。最值得先做的一条：**A-01（三处宿主文件 RMW 补锁）**——同根一次收口，回归测试模板现成。
