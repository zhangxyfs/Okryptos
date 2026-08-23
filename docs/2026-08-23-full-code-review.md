# OpenKnowledge 全量代码审查报告

- **日期**：2026-08-23
- **范围**：全仓库（193 个 Go 文件、web/ 前端、scripts/ 构建发布脚本、installer/、tests/e2e、CI workflow），约 43K 行
- **方法**：5 条并行审查线（HTTP 攻击面 / hooks 与 agentx 适配层 / 数据层与并发 / CLI-配置-注入链 / 前端与构建发布），逐文件完整阅读 + 调用链交叉验证；High 级发现由主审二次核实代码证据
- **基线**：`go build ./...` 通过、`go vet ./...` 无告警、`go test ./...` 33 个包全部通过

---

## 总体评价

代码质量**明显高于同类本机工具的平均水准**：原子写（tmp+fsync+rename）全链路贯彻、SQL 全参数化且 FTS MATCH 双重转义、路径穿越有专项安全测试、模型下载带 sha256 强校验、zip 导入防 zip-slip/zip-bomb、前端 44 处 innerHTML 无一遗漏走转义管线、构建链的历史坑修复大多带防回归断言、大量注释直接绑定上游源码证据。fail-open 铁律（stdin 解析失败恒 exit 0、panic 折叠放行）贯彻得很一致。

结构性弱点集中在两处：

1. **鉴权模型建立在"浏览器同源策略会替我保住 token"的假设上**——首页无鉴权发 token + 全局无 Host/Origin 校验，使该假设同时被"同机其他用户直连 loopback"与"远程页面 DNS rebinding"两类对手击穿；token 一旦泄露，`/api/llm/test` 的"任意 BaseURL + 回查已存 key"设计把密钥外传变成一个 HTTP 请求的事（H1–H3 构成完整攻击链）。
2. **"GUI 加固、CLI 裸奔"与"写路径双轨制"**——validProjectName/空 slug 校验只在 GUI 层存在，`ok init`/`ok add`/`backup Import` 直接接受任意输入；config 写入一半走锁内整档重编码、一半走无锁行级 upsert，违反项目自己声明的收口不变量。

---

## 修复优先级建议

| 优先级 | 发现 | 理由 |
|---|---|---|
| P0 | M-01 加 Host/Origin 校验中间件 | 一处改动掐断整条远程链（rebinding/CSRF），性价比最高 |
| P0 | H-04 selfHealHooks 补 CLI 入口换算 | 已记载教训的漏网分支，影响全部适配器，用户可感知（doctor 误报、配置反复改写） |
| P0 | H-05 embedding 移出写事务 | 全量重建期间全系统 hook 注入静默为空数分钟，架构级锁粒度错误 |
| P1 | H-01/H-03 token 发放方式与 key 回查条件 | 与 P0 中间件配合才完整 |
| P1 | M-02 行级配置写收口加锁 / M-13 ok init 形状校验 | 各为两个系统性模式（写双轨、CLI 裸奔）的代表修复 |
| P2 | 其余 Medium | 小改动大收益居多（大小写折叠、kimi 复活、vectors.json 隔离、iss 目录定位） |
| P3 | Low | 顺手修 |

---

## High（5 项）

### H-01 GUI 首页无鉴权直接回吐 API token，击穿 daemon.json 的 0600 隔离

- **位置**：`internal/gui/api.go:56-57`（路由未包 withAuth）、`api.go:156-165`（serveIndex）
- **证据**：
  ```go
  mux.HandleFunc("GET /{$}", h.serveIndex)        // 未包 withAuth
  ...
  _, _ = w.Write([]byte(strings.ReplaceAll(string(data), "{{TOKEN}}", h.token)))
  ```
  配合 `web/index.html:9` 的 `<script>window.OK_TOKEN = "{{TOKEN}}";</script>`。
- **影响**：daemon.json 以 0600 保存 token，本意只允许属主使用；但 Windows loopback 无用户级隔离，同机任意低权限用户 `curl http://127.0.0.1:17888/` 即可取得 token，进而调用全部 /api/*（读写知识库、执行白名单命令、装卸 hooks、改配置）。它也是 H-02 远程链的第一步。
- **建议**：token 不应无差别内嵌。可选：首屏引导改用 `#token=`（fragment 不发服务器、不进 Referer）一次性注入；或引入会话 cookie。至少先配合 H-02 的 Host 校验收紧。

### H-02 全 HTTP 面无 Host/Origin 校验，DNS rebinding 可远程接管 daemon

- **位置**：`internal/daemon/server.go:26-58`、`internal/gui/api.go:133-141`（withAuth 仅查共享秘密头）
- **证据**：全仓无任何 `r.Host` 校验、无 Origin 检查、无 CORS 头；daemon 固定监听 `127.0.0.1:17888`——固定端口正是 rebinding 的理想目标。
- **影响**：攻击者控制 `evil.com` DNS，先以公网 IP 服务页面、TTL 过期后解析改 `127.0.0.1`，页面内 `fetch("/api/...")` 对浏览器仍是同源，可读任意响应：GET / 拿 token → 带头调用全部 API → 执行 terminal/exec、重写各 agent hooks、经 /api/llm/test 外传 key、/api/uninstall 破坏安装。Chrome 的 Private Network Access 对同源 rebinding 不设防，Firefox/Safari 亦无。
- **建议**：mux 最外层加中间件：`r.Host` ∈ {`127.0.0.1:17888`, `localhost:17888`} 否则拒绝；/api/* 同时校验 Origin/Referer（存在则必须为本 origin 或空）；永不响应 `Access-Control-Allow-Origin`。

### H-03 /api/llm/test 与 embedding/test 可把已存真实 API key 发送到任意 URL（密钥外传 + SSRF）

- **位置**：`internal/gui/llm.go:184-221`、`internal/gui/embedding.go:221-260、406-417`
- **证据**：
  ```go
  if req.APIKey == "" || req.APIKey == llmKeyMask {      // key 留空/掩码
      ... req.APIKey = p.APIKey                          // 回查已存 profile 的真实 key
  }
  ...
  if err := llmx.New(p, 0).Test(ctx); err != nil {       // BaseURL 完全来自请求体，无校验
  ```
- **影响**：拿到 token 的攻击方（H-01/H-02 链）POST `/api/llm/test`，`base_url` 填自己的服务器、`name` 填已存 profile 名——config.toml（0600）里的真实 key 就以 Bearer 头送出，全程无需文件读取权限。apiOllamaModels 另可用作内网/云元数据（169.254.169.254）GET 探测并回读响应。
- **建议**：回查已存 key 时限制 base_url 与已保存值一致（改了 URL 就必须显式重填 key）；base_url 校验 scheme；apiOllamaModels 仅放行 loopback 形态目标。

### H-04 selfHealHooks 在 daemon 进程内执行时未换算 CLI 入口，把各 agent hooks 改写为指向 okd.exe

- **位置**：`internal/hook/hook.go:164-177`（同款问题 `internal/rxext/serve.go:144-155`）
- **证据**：
  ```go
  func selfHealHooks() {
      exe, err := os.Executable()   // daemon 进程内 = okd.exe，未做 CLI 换算
      ...
      for _, a := range agentx.Detected() { a.EnsureHooks(exe) }
  ```
- **影响**：hook 请求经 daemon 转发后在 okd 进程内执行 HandlePrompt → selfHealHooks，此时 `os.Executable()`=okd.exe，claude/zcode/opencode/pi/dsh/reasonix 的 settings 会被重写为 okd.exe 形态，codex/qoder/qoderide 的 wrapper 同理。后果：`ok setup` 后第一条 prompt 即触发改写（配置+`.bak-openknowledge` 反复振荡）；`ok doctor`（ok.exe 视角）持续误报"未安装"而 GUI 报"已安装"，视图分裂；宿主执行多一跳转发。这正是已记载教训"daemon 内注册命令必须换算 CLI 入口"的漏网分支——GUI 注册路径已用 `cliExePath()`→`daemonx.CliTargetFor` 换算（gui/api.go:311-324），hook 自愈路径漏掉了。
- **建议**：selfHealHooks（两处）在 EvalSymlinks 后增加 `daemonx.CliTargetFor(exe)` 换算，换算失败直接 return。

### H-05 Sync 在写事务内执行 embedding 网络调用，持 SQLite 写锁可达分钟级

- **位置**：`internal/index/sync.go:131-260`（tx.Begin → INSERT entries → 批次 EmbedDocuments 全在锁内）
- **证据**：`tx.Exec("INSERT INTO entries...")` 升级为写锁后，`for ... client.EmbedDocuments(context.Background(), texts)`（32 条/批）循环在事务内执行；`ok index` 路径 embedding 超时下限 120s。
- **影响**：万级条目全量重建（模型切换后 ClearVectors）= 300+ 批网络调用，写锁连续持有 5–15 分钟；DSN 的 `busy_timeout(3000)` 完全撑不住——期间 GUI 保存条目、hook 进程的 Sync/RecordEvents 全部 SQLITE_BUSY 失败，hook 降级也失败后 `return ""`，**重建期间所有 prompt 注入为空**、事件统计静默丢失。
- **建议**：embedding 移出写事务。方案 a：先无事务 diff、锁外算完向量、再开短事务统一写；方案 b（最小改动）：entries/fts 先提交一个事务，vectors 每批一个小事务。Sync 以 mtime 幂等，部分失败由"未变化缺向量补齐"路径重试。

---

## Medium（18 项）

### M-01 条目 AI 优化的"事实检索"路径穿越：条目内 `../` 引用可读项目根外文件并外发给 LLM

- **位置**：`internal/gui/llm.go:270、304-336、535-549`
- **证据**：`entryPathRef` 正则字符集含 `.` 与 `/`，`filepath.Join(root, file)` 后无包含性校验，`../../../../Users/x/secret.md` 直接逃逸，随后拼进 prompt 发往外部 LLM。
- **影响**：条目内容可经 propose/导入被污染，用户点"AI 优化"时成为本机文件外发的隐蔽通道。
- **建议**：对 full 做 Abs+前缀包含性复核（同 apiProjectReadmeAsset），越界跳过；或拒绝 `..` 段。

### M-02 全局 config.toml 行级写路径绕过 updateGlobalConfig 锁收口，并发丢更新（可丢刚保存的 API key）

- **位置**：`internal/gui/api.go:1299-1342`（apiCaptureSet 等五组端点）、`internal/config/capture.go:15`、`gate.go:17`、`toml_upsert.go:15`、`internal/cli/cli.go:799`
- **证据**：`SetCapture/SetGate/SetRetrieveDedupTurns/SetInjectMandatoryMax/SetEnforceRules` 为裸 Read→改→Write；而项目不变量（setupx.go:93-115）是 updateGlobalConfig 用 `fsx.WithFileLock` 包住 LoadMerged→改→落盘。
- **影响**：行级写者与锁内整档写者（保存 LLM/embedding profile）交错时，行级写者基于旧快照写回，把刚保存的 profile（含明文 api_key）静默回滚；交替写还使文件在"保留注释/重编码丢注释"两形态间漂移。
- **建议**：五个行级写函数内部套 `fsx.WithFileLock(path, ...)`（或统一改走 updateGlobalConfig）。

### M-03 注入链无转义：条目 title/summary 原样进 INDEX.md 与注入文本，可伪造结构

- **位置**：`internal/index/sync.go:407-421、438`、`internal/hook/core.go:127、150-155、214-216`
- **证据**：`fmt.Fprintf(&b, "- **%s** (%s) [%s] — %s\n", title, ...)` 原样拼接；title 含换行（CLI 可传、GUI「AI 优化」直接落盘 LLM 输出）即可插入伪造行，如 `## 分支差异（x）` 假标题——`TrimIndexBranchSections` 按行匹配会真的把它当小节头裁剪。
- **影响**：被污染条目可伪造知识注入结构、伪 `[OpenKnowledge]` 系统指令行、破坏 INDEX。claude 协议路径经 json.Marshal 转义是安全的；纯文本路径（kimi/pi）与 INDEX.md 无防护。
- **建议**：渲染层统一去控制字符/换行，转义 markdown 元字符；TrimIndexBranchSections 同时校验行格式。

### M-04 ok init 项目名无形状校验：路径穿越、非法 Windows 名称、大小写冲突（"GUI 加固、CLI 裸奔"模式）

- **位置**：`internal/cli/cli.go:72-84`（对照 gui/api.go:254-262 已有防御且注释点名 CLI 缺口）
- **影响**：`ok init "..\..\x"` 把 knowledge/state 目录建到知识库根之外；注册表先写、EnsureDirs 失败后条目残留；Windows 大小写不敏感使 `Foo`/`foo` 共享同一 store 串数据。
- **建议**：Init 复用 GUI 同款形状校验，写注册表之前报错。

### M-05 backup.Import 接受包内 registry.toml 任意项目名，无形状校验（同 M-04 模式）

- **位置**：`internal/backup/backup.go:179-191`
- **影响**：恶意/畸形备份包可把 `../evil` 写进注册表（zip 路径本身有 validName 防穿越，不逃逸），之后 GUI resolveProject/FromCwd 解析到任意目录，可浏览/注入该目录 `*.md` 内容。
- **建议**：Import 循环内对 p.Name 复用 validProjectName 同款校验，非法拒绝整包或跳过并告警。

### M-06 entry.Slug 不处理空结果/控制字符/保留设备名；CLI 缺 GUI 已有的空 slug 校验

- **位置**：`internal/entry/entry.go:127-137`、`internal/cli/cli.go:178、694`（GUI 在 api.go:842-845 有校验）
- **影响**：纯符号标题生成空 slug → `.md` 幽灵条目；换行进文件名在 Explorer/git 下极难处理；`con`/`nul` 等保留名创建失败且报错难懂。
- **建议**：CLI Add/Propose 补 `Slug(title)==""` 校验；Slug 剔除控制字符、映射保留设备名、限长。

### M-07 fsx 文件锁 stale 抢占存在 stat→remove 竞态，可删新持有者的锁，互斥失效

- **位置**：`internal/fsx/lock.go:38-40`
- **证据**：两等待者同时判 stale：A remove 旧锁并 O_EXCL 建新锁，B 的 remove 落在 A 创建之后，删掉 A 的新锁 → A、B 同时进临界区。
- **影响**：这把锁要保护的 registry 读改写丢项目场景（并发 init/GUI 删项目/导入）重新打开窗口。另 `lockStaleAge=2s` 假设临界区毫秒级，慢盘/杀软扫描下合法持锁也会被误抢。
- **建议**：抢占改"先 rename 再删"（只有一个等待者能成功），或直接换 `LockFileEx`/`flock` 内核原语。

### M-08 WithFileLock 超时后 fail-open 无锁执行，registry.Update 互斥保证在争用下失守

- **位置**：`internal/fsx/lock.go:42-44`
- **证据**：等锁 5s 后 `return fn()` 无锁裸跑读-改-写。
- **影响**：registry.Update 注释承诺跨进程锁内 RMW 防丢更新，但争用超 5s 即放弃互斥——两个进程同时 fail-open 就是"丢项目注册"的复现路径。fail-open 对 hook 的 state 会话锁是合理权衡，对注册表这类强一致数据语义就错了。
- **建议**：拆出严格模式（超时返回错误）供 registry.Update 使用；state 路径保留 fail-open。

### M-09 Sync 以秒级 mtime 判变化，外部编辑器同秒双写漏同步

- **位置**：`internal/index/sync.go:90、168`
- **影响**：自家写路径有 BumpMtime 兜底，但用户外部编辑器连续两次保存间隔 <1s（或 FAT 2 秒粒度）时第二次内容不同的写入被判未变化——FTS/向量/INDEX 停留旧内容，静默不一致。
- **建议**：diff 判据改"mtime 相等且 size 相等"，或 mtime 刻度升到 UnixMilli（存量一次性全量重读，幂等无害）。

### M-10 损坏的 vectors.json 使 Open 永久失败，整个知识库不可用

- **位置**：`internal/index/db.go:237-244`
- **影响**：半截 JSON（旧版 O_TRUNC 直写时代可能产生）导致该项目 index.Open 每次失败：注入全空、CLI 全部 exit 1、GUI 打不开，直到用户手工删文件。
- **建议**：unmarshal 失败时 rename 为 `vectors.json.bad` 并继续（向量可重建），记告警。

### M-11 kimi 自愈无法区分"宿主清了标记行"与"用户显式卸载"，已移除集成会被复活

- **位置**：`internal/agentx/kimi.go:163-173`
- **影响**：其它适配器都有"无 ok 条目=不复活"保护，kimi 只看标记块存在与否——用户移除 kimi hooks（保留其它 agent）后，任何一次 prompt 的 selfHealHooks 无条件重装，"卸载不掉"。
- **建议**：标记块缺失时先判断是否残留 ok 的 `[[hooks]]` 表（`okHookCommand` 命中）才修复，全无则 no-op。

### M-12 zcode 适配器不校验 hooks.enabled，自愈重写时把用户显式关闭的总开关强行翻回 true

- **位置**：`internal/agentx/zcode.go:110-123、185-220、273-292`
- **影响**：与 qoder 的对称防御不对称——用户关闭 hooks 后 doctor/GUI 仍报"已安装"（实际零派发）；EnsureHooks 走到重写路径会翻回 true，违背"显式关闭不复活"原则。
- **建议**：zcodeHooksCurrent 增加 enabled 检查并入 HooksInstalled；重写路径尊重现状 enabled。

### M-13 EvalChangelog 路径大小写折叠使大写 glob 永不匹配，changelog_required 可长期误阻断

- **位置**：`internal/enforce/enforce.go:17-29` + `internal/registry/registry.go:44-48`
- **证据**：`Touched` 全小写入库，而 changelog_glob 按用户原样大小写匹配（doublestar 区分大小写）。
- **影响**：`CHANGELOG.md`、`CHANGELOG/**` 等业界惯例写法永远匹配不到 → 每次会话白挨一次 Stop 阻断（有 MarkBlocked 防死循环，但每会话必触发一次）。
- **建议**：匹配前对 glob 与路径统一折叠大小写，或配置校验强制小写并配测试。

### M-14 卸载路径存在非原子写盘，违背项目自身原子落盘纪律

- **位置**：`internal/agentx/kimi.go:225`、`internal/setupx/uninstall.go:121`
- **证据**：两处 `os.WriteFile` 裸写（fsx.WriteFile 注释明确记载"各宿主 settings 均曾被 O_TRUNC 写坏"）；另 RemoveSection 对全文每行 TrimRight 会改掉用户其它小节多行字符串的行尾空白。
- **建议**：改用 fsx.WriteFile；RemoveSection 去掉全文行尾裁剪。

### M-15 wiki 状态检测的 git 子进程无超时、无进程树收割

- **位置**：`internal/wiki/status.go:23-31、47-51`
- **影响**：注入链每次 spawn 多个 git；git 挂起（网络盘/凭据提示）会拖满宿主超时，Windows 上 kill 不收割 git 孙进程。这是全链超时体系（宿主→daemon 9s→插件 execFile→embed 5s）里唯一裸奔的 spawn 点。
- **建议**：exec.CommandContext 包 3–5s 超时；Windows 可用 Job Object 收割子进程树。

### M-16 llmx anthropic 与 openai 的 base_url 约定不对称，/v1 拼接导致 404

- **位置**：`internal/llmx/llmx.go:75-77、152、189`
- **影响**：openai 要求 base 含 `/v1`，anthropic 要求不含（代码拼 `/v1/messages`）；GUI 两种 kind 共用同一 base_url 表单无区分。用户按 openai 习惯填 → `/v1/v1/messages` 404，报错难定位。embedx 已为 ollama 修过同款（TrimSuffix "/v1"），llmx 没做。
- **建议**：anthropic 分支对 BaseURL 做 TrimSuffix "/v1" 归一，或 GUI 按类型显示不同 placeholder。

### M-17 iss 卸载数据目录用 `{userdocs}\..` 定位，Documents 重定向下指错目录甚至误删无关数据

- **位置**：`installer/openknowledge.iss:155-165`
- **影响**：OneDrive 已知文件夹迁移/企业重定向（个人机极常见）下，`DataDir` 指向错误位置：轻则"删除数据"选项永远找不到真实目录；重则该错误路径下恰好存在同名 `.openknowledge` 时，用户确认后 DelTree 递归删除无关目录。
- **建议**：改 `ExpandConstant('{%USERPROFILE}\.openknowledge')`，或由 okd 暴露 data-dir 子命令回传真实路径。

### M-18 runtime 目录双路径漂移：bash 构建链不产 runtime 而 iss 无条件打包；build.py 对已存在 runtime 永不刷新

- **位置**：`scripts/build-dist.sh:6-11`、`scripts/build.py:48-50`、`installer/openknowledge.iss:41`
- **影响**：与已修的"changelogs 双路径"同型残留——bash 全链路从不下载 llama runtime，全新环境 ISCC 直接编译失败；跑过 build.py 的机器则把旧 LLAMA_TAG 的 llama-server 静默打进新安装包（LLAMA_TAG 升级无刷新机制）。
- **建议**：下载后写 tag 标记文件，版本不符重下；或 build-dist.sh 调用同一 runtime 准备逻辑。

### （另有两项前端 Medium）

- **设置页"✓已保存"反馈定时器触发全局重渲，静默丢弃用户未提交的输入**（`web/app.js:634-637、2429-2432、3360-3363`）：保存后 1.5s 内在 ptext 输入框打字，定时器 render() 全量重建 DOM，已敲入未失焦的字符丢失——整个设置页"oninput 直写不重渲"保焦点设计的漏网之鱼。建议定时器只摘除对应反馈节点，或 render 前检测 activeElement。

---

## Low（20 项）

| # | 发现 | 位置 | 摘要 |
|---|---|---|---|
| L-01 | /api/export 缺 validProjectName 校验（与同文件其它端点不一致） | gui/api.go:569-598 | 注册表被毒化时可越出 projects/ 读文件打包；一行修复 |
| L-02 | 除 /api/import 外全部 JSON 端点无请求体大小上限 | gui/api.go:407-413 | 唯 decodeJSON 处统一包 MaxBytesReader(4MB) |
| L-03 | Windows 浏览器兜底 `cmd /c start <url>` 对 cmd 元字符（`&` 等）防御不完整（当前不可达） | gui/browser_windows.go:33 | safeAppURL 补拒 `& ^ \| % , ; =` 或改 explorer.exe |
| L-04 | token 经 `/?token=` 明文流转且无人消费（死参数） | daemon/run.go:113、151 | 留在地址栏/历史；去掉拼接即可 |
| L-05 | UpsertHooksBlock 只处理第一个标记块，多标记块时旧块残留双派发 | agentx/kimi.go:127-153 | upsert 前循环剥离全部旧块 |
| L-06 | InjectForPrompt 决策快照锁外读取，窗口内可能重复 nudge 一次 | hook/core.go:82 | 毫秒级窗口、后果仅多一条提示行；可并入 state.Update 闭包 |
| L-07 | qoderide PostToolUse 注释称"双套工具名都认"，matcher 实际只有兼容名 | agentx/qoderide.go:38-48 | 补 `create_file\|search_replace` 或修正注释 |
| L-08 | claude Windows 下维持带引号 shell 串，与 codex/qoder wrapper 防御不对称（观察项） | agentx/claude.go:56-58 | 注释补空格路径实测结论，或统一 wrapper |
| L-09 | upsertTomlKey 键名前缀匹配可误替换用户自定义键（如 `dedup_turns_v2`） | config/toml_upsert.go:49-56 | 命中条件加键名边界判定 |
| L-10 | `ok index` embedding 未配置时退出码 1，与 add/approve 同场景的 0 不一致 | cli/cli.go:437-441 | 降级路径返回 0；统一 usage 错误码 |
| L-11 | embedding 成功响应体读取无大小上限（llmx 有 1MB cap，embed 没有） | embed/embed.go:108-113 | 包 io.LimitReader(4MB) |
| L-12 | 模型下载无 HTTP 客户端超时，setup 路径 ctx 为 Background | embed/download.go:17-20 | 连接挂起只能 Ctrl+C；加 Timeout |
| L-13 | BackfillBorn 写回后不调 BumpMtime，同秒回填被 mtime diff 漏掉 | cli/cli.go:259-264 | Add/Approve/Archive 都有，此处漏配 |
| L-14 | embedsidecar stopLocked 与看护 goroutine 双重 Wait 竞态 | embedsidecar/manager.go:172-176 | 日志误报 `no child processes`；删一处 Wait |
| L-15 | daemon 无信号处理，前台 Ctrl+C 跳过所有 defer | daemon/run.go:31、66-70、125 | signal.NotifyContext + srv.Shutdown；写盘安全无虞（全链原子写） |
| L-16 | hook payload 1MB 静默截断，超限事件整包丢弃且零观测 | cmd/ok/main.go:103 | 大文件 Write 的 PostToolUse 会触发；至少 logErr 一行 |
| L-17 | `ok wiki mark` 吞 Sync 错误且 count 归零 | cli/cli.go:930-934 | 失败时 stderr 提示 |
| L-18 | Import 多阶段非原子：注册/条目/config/Sync 部分失败无回滚（幂等可重试但无提示） | backup/backup.go:175-258 | 错误信息注明"重新导入即可续传" |
| L-19 | 旧库迁移 ALTER TABLE 无并发防护，多进程首开同一旧库偶发 duplicate column | index/db.go:117、149 | ALTER 失败后重探列存在性收尾 |
| L-20 | 关键词准入 floor 按全库 Count()（含 draft/archived）缩放，与"可检索条目数"注释意图不符；结果排序 title 平局无 filename 决胜 | index/query.go:176-179、312-323 | Count 改 WHERE 口径；比较器补 filename 决胜（feedback.go 已有同款） |

**前端/脚本/安装器 Low**：条目"批准"按钮无 in-flight 防重，双击后展示误导性错误（app.js:2089-2098）；termHist 无上限增长 + 每次全量重建 DOM（app.js:1792-1824，建议环形截断 200 条）；pollManage 与 refreshManage 交错可丢一轮更新（4s 自愈，app.js:1017-1029）；sync-version.sh 徽标行不存在时 guard 失效、sed -i 照跑造成 CRLF 假漂移（scripts/sync-version.sh:15-22）；iss 卸载确认框 MB_YESNO 默认按钮为"是"，连续回车即删全量数据（iss:160-164）；RemoveFromUserPath 边缘 case 残留尾部空 PATH 条目（iss:113-121）；e2e `ok doctor` 忽略退出码（tests/e2e/integration_test.go:203）。

---

## 已验证无问题（正面确认）

- **SQL/FTS 注入**：全部 SQL 参数化；`buildMatch` 对词元双引号包裹转义，且词元产自 `retrieve.Terms`（仅字母数字/CJK）天然不含语法字符——双保险。
- **terminal/exec 端点**：参数数组直传 exec 不经 shell，首参白名单 11 条，10s 超时 + 64KB 截断，前端另有白名单预拦截。
- **XSS**：app.js 全部 44 处 innerHTML 逐点核对无一遗漏走 esc/renderMd/sanitizeHtml 管线；README 富渲染"先转义、白名单还原、DOMParser 重建"双层防御正确；`javascript:` 协议、on* 属性、路径穿越均拦截。
- **路径穿越**：validProjectName/validEntryFile 拒绝 `..`/分隔符/盘符/UNC 且有专项 security_test；readme-asset 三重复核 + 10MB 上限 + SVG CSP；backup.Import 的 validName 防 zip-slip + 256MB 解压预算。
- **供应链**：模型下载强制整文件 sha256 比对 + 大小核对；发布凭据经 git credential fill、token 只进 Authorization 头不落日志；发布幂等（422 复用 + 同名跳过）。
- **Windows rename**：Go os.Rename 走 MoveFileEx(REPLACE_EXISTING)，fsx.WriteFile 可正确覆盖；所有落盘路径确认均走原子写。
- **CI**：仅 push/pull_request 触发、permissions 最小化、无 github.event 插值进 run、不使用 secrets，无命令注入面。
- **退出/超时分层**：宿主 timeout→daemon 转发 9s→TS 插件 execFile→embed 5s，分层清晰（除 M-15 git 一处裸奔）。
- **门控计数**：先推进时钟再取冷却集，无 off-by-one；enforce 有 HasBlocked 防阻断死循环。
- **rxext SDK（stdio）**：帧上限、并发上限、通知队列溢出断链、panic 全 recover——防御性编程范本。

## 历史坑守卫现状核查

| 已知教训 | 现状 |
|---|---|
| 派发层 matcher 断链零派发 | ✅ 已守住（FilePath 兼容 path/file_path、PatchPaths 解析补丁头，有回归测试） |
| Windows cmd /s 剥引号 | ✅ codex/qoder/qoderide 已 wrapper 化；claude 维持 quoted（注释称实测 OK，观察项 L-08） |
| Codex 信任过期静默跳过 | ✅ 已守住（trusted_hash 同步重算 + HooksInstalled 验证） |
| Qoder hooksConfig.enabled 默认关 | ✅ 已守住；但 zcode 对等开关漏了（M-12） |
| 会话状态多进程竞态 | ✅ state.Update 锁内重放+原子写；卸载路径两处例外（M-14） |
| Reasonix 拦截器 err 吞 Continue | ✅ 已守住（panic 折叠 Continue） |
| opencode 写盘按模型互斥 | ✅ 已守住（插件认 write/edit/apply_patch） |
| daemon 内注册命令换算 CLI 入口 | ⚠️ 部分守住：拉起与 okd 转发在；**hook/rxext 的 selfHealHooks 漏换算（H-04）** |
| 注册表裸读改写丢项目 | ⚠️ 写路径已走 registry.Update；但锁本体在争用下失守（M-07/M-08） |
| 全局配置锁内原子写收口 | ⚠️ profile 类已收口；五个行级写函数未收口（M-02） |
| 字符 vs 字节数（中文差3倍） | ✅ EstimateTokens 按 rune 分类计 |
| LLM 超时分场景 + temperature 缺省不传 | ✅ 已守住 |
| 混合检索按通道独立判定阈值 | ✅ 已守住；但 floor 的 Count 口径含草稿（L-20） |
| iss→dist 暂存区 + changelogs 双路径 | ✅ 已修；runtime 目录存在同型残留（M-18） |
| chmod 静默无效 | ✅ 已修且三重钉扎（tar --mode + nfpm + verify-deb 断言） |
| sync-version.sh sed 分隔符冲突 | ✅ 已改 `~` 分隔符；徽标行缺失分支残留（Low） |

---

## 统计

- Critical：0
- High：5（H-01…H-05）
- Medium：19（M-01…M-18 + 前端定时器丢输入）
- Low：27

测试全绿与上述发现并不矛盾：多数发现落在"集成测试直接调核心函数拦不住传输/进程视角分裂"（H-04）、"并发窗口"（M-02/M-07/M-08）、"安全假设"（H-01…H-03）、"跨工具链边界"（M-17/M-18）这几类现有测试结构天然覆盖不到的位置。

---

## 增量复审（同日第二轮：wiki 结构治理改动）

**改动内容**：① `ok add --force` 覆盖写继承盘上原条目的 `created/draft/archived`（cli.go:183-194，修复归档/草稿条目被静默转正——正是新 wiki 纪律"版本段子条目同名 `--force` 重写"所依赖的前提），新增回归测试；② 前端知识树：类目行改双热区（箭头/图标单击展开、点名选中、双击展开）、演进历程升级为"索引+版本段子条目"双身份节点（tags `wiki+历史` 路由、版本号数值升序 v2.9<v2.10）、老库无版本段时详情页出迁移提示；③ SKILL.md/需求文档同步改写；④ iss postinstall 去掉 `unchecked`（装完默认勾选打开配置中心）。

**验证结论**：`go build`/`go vet`/全量 `go test` 通过（含新测试 TestAddForceInheritsLifecycle）。cli.go 修复位置正确（在"条目已存在"检查之后、仅 `--force` 可达），不继承 `mandatory` 与 GUI writeEntry（api.go:770-772）口径一致；Propose 无覆盖写路径不受影响。SKILL.md 显式声明了该继承行为。

**新发现（1 项 Medium）**：

- **[Medium] 类目行与双身份节点的原生 `ondblclick` 在全量重渲下不可靠**（web/app.js:1437-1446、1483-1484）：`renderBody` 每次 `app.innerHTML=""` 整页重建（app.js:3721），单击选中即换节点——本文件 914 行注释已自证"单击即重渲换节点，原生 dblclick 不可靠"，项目行因此用 `nmLastClick` 时间戳手工检测（1562-1564）。新加的 `crow.ondblclick`/`leaf.ondblclick` 大概率永不触发：两次 click 分别落在被换掉的旧节点和新节点上，浏览器合成的 dblclick 派发到公共祖先而非按钮。后果不止"双击功能死了"——类目行旧行为是整行单击展开，现在整行单击只选中，展开只剩小箭头/图标热区，可用展开目标实际上缩水了。**建议**：catSel 行与 renderDualLeaf 复用 `nmLastClick` 时间戳检测模式（400ms 窗口内二次点击=toggle）。

**轻微观察（不阻塞）**：`renderDualLeaf` 在 kids 为空时仍渲染箭头且可切换空节点（cnt=0，配合迁移提示属过渡态，可接受）；双身份宿主条目按标题精确匹配"架构总览"/"演进历程"，改名即失配（SKILL.md 已钉死命名，可接受）；快速双击箭头热区=toggle 两次回到原状（无害）。

---

## 修复核验（同日第三轮：对照本报告的整改）

**基线**：`go build` / `go vet` / 全量 `go test ./...` 通过；新增 4 个测试文件（host_guard / http_hardening / db_corrupt / sync_tx），断言均为内容级（Host 各形态 403/放行、改 URL 发 key 必须 400、损坏 vectors.json 隔离后 Open 正常、embedding 中途故障后条目保留+向量下轮补齐），非空洞断言。

**High 5 项全部修复**：

- **H-01** ✅ serveIndex 不再内嵌 token；token 经 URL fragment（`#token=`）注入 index.html inline script → sessionStorage → `history.replaceState` 抹除地址栏。okmanager 是薄启动器（全走 okd），无第二服务面。
- **H-02** ✅ `hostGuard` 中间件包住整个 daemon mux：Host 必须回环（127.0.0.1/localhost/[::1]，端口不限）；`/api/*` 的 Origin/Referer（存在时）必须同源自回环；不输出 CORS 头。`null` Origin 因 scheme 解析为空被拒。hook 客户端不带这两个头不受影响。
- **H-03** ✅ `/api/llm/test`、`/api/setup/embedding/test` 回查已存 key 时强制 base_url 与已存值一致（改地址必须重填 key）+ scheme 校验；`apiOllamaModels` 仅放行回环目标。
- **H-04** ✅ `selfHealHooks`（hook.go 与 rxext/serve.go 两处）经 `daemonx.CliTargetFor` 换算，换算失败放弃本次自愈。
- **H-05** ✅ Sync 改两阶段：entries/fts/删除先在一个事务提交，embedding 网络调用在事务外、每批 32 条开毫秒级小事务写 vectors；批失败幂等重试由"未变化缺向量补齐"路径承接。

**Medium 19 项修了 17 项**：M-02（五个行级写函数全部 `fsx.WithFileLock` 收口）、M-03（`SanitizeInline`/`StripControls` 消毒全部渲染点 + `branchSectionName` 严格格式校验）、M-04/M-05（`registry.ValidProjectName` 三端共用 + Windows 大小写冲突拒绝）、M-06（Slug 剔控制字符/保留名前缀 `_`/限长 80 rune + CLI 空 slug 校验）、M-07（锁抢占 rename-then-delete）、M-08（`WithFileLockStrict` 供 registry）、M-09（mtime+size 双判，存量库首轮幂等全量重读）、M-10（损坏 vectors.json 隔离 `.bad`）、M-11（kimi 仅在残留 ok hook 表时复活）、M-12（zcode hooks.enabled 入 HooksInstalled + 重写保留现状值，nil-map 写分支经核验安全）、M-14（kimi/uninstall 原子写 + 不裁用户行尾空白）、M-15（git 5s CommandContext 超时）、M-16（anthropic base_url /v1 去重）、M-17（iss `%USERPROFILE%` 定位 + MB_DEFBUTTON2 默认"否" + PATH 尾分号剥离）、M-18（build.py runtime tag 标记文件 + build-dist.sh 缺失明确报错）、上轮增量的 dblclick（`catLastClick` 时间戳模式）与前端定时器丢输入（`clearFb` 定向摘除不整页重渲）。

**Low 27 项修了 25 项**（含：decodeJSON 统一 4MB 上限、apiExport 校验、explorer.exe 替代 cmd /c start、`#token=` 替代 `?token=`、kimi 多标记块剥离、nudge 锁内重查、qoderide matcher 补原生工具名、upsert 键名边界、ok index 退出码 0、embed 响应 4MB 限额、下载 ResponseHeaderTimeout 30s（不设整体超时避免误杀大文件，取舍合理）、BackfillBorn BumpMtime、sidecar 单点 Wait、daemon 信号处理、payload 截断记 ok.log、wiki mark 不吞错、导入失败带续传指引、ALTER 并发收尾、检索计数口径、排序 filename 决胜、批准按钮防双击、termHist 环形 200、refreshManage 重入补一轮、sync-version 徽标缺失 guard、e2e doctor 退出码、iss PATH 尾分号）。

**遗留 2 项已在本轮补修（第四轮核验通过）**：

- **M-01** ✅ 新增 `resolveRefPath`（gui/llm.go）：Abs + `absRoot+分隔符` 前缀包含性复核，`../` 逃逸引用一律跳过；`llm_test.go` 覆盖 `../../secret.md`、`../sibling/x.go`、`sub/../../../secret.md` 三种逃逸形态。
- **M-13** ✅ EvalChangelog 匹配前对 glob 与路径双侧折叠小写（doublestar 区分大小写），`CHANGELOG.md` 等大写惯例写法恢复命中；测试覆盖正反两个方向。
- L-08（claude Windows quoted 形态）为观察项未动，注释已声明实测依据，可接受。

**新引入代码检查**：逐处核对本轮改动实现，未发现新缺陷（zcode nil-map 写分支仅在 hadEnabled 时可达、stripMarkerBlocks 必然终止、Sync 两阶段幂等语义成立、build.py shutil 已导入）。一个已知 UX 取舍：token 走 sessionStorage 后，daemon 重启（token 轮换）会令旧页 401，需从托盘/快捷方式重开 GUI——安全模型内的接受成本。

**终态**：审查发现的全部 High/Medium 已修复并配回归测试，Low 除 L-08 观察项外全部修复；`go build` / `go vet` / 全量 `go test ./...` 通过。
