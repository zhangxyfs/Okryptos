# 内置托管本地 LLM（chat sidecar）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给 [llm] 增加 kind=builtin 档——ok 托管下载 chat 模型（GGUF）并用 llama-server sidecar 提供本地 OpenAI 兼容 chat 端点，与 embedding builtin 档体验完全对齐（GUI 下拉+一键下载+进度条，装完即用、无需 ollama）。

**Architecture:** 新增两个包：`internal/chatx`（chat 模型清单 + 下载薄封装，复用 `embed.Download` 经结构体映射）与 `internal/chatsc`（chat sidecar 进程托管，从 `internal/embedsidecar` 适配——独立状态文件族 `chat-sidecar.*`、独立 llama-server 实例、参数去 `--embeddings/--pooling` 加 `-c 8192 --chat-template-kwargs '{"enable_thinking":false}'`）。**embedsidecar 一行不改**（已发布功能零回归风险；进程管理核的有界重复由终审分诊）。daemon 加第二个 janitor 看护 chat sidecar；llmx 加 kind=builtin（状态文件发现端口，未就绪写 want 返回"启动中"错误——**New 保持永不返回 nil 的既有契约**，调用方零改动）；GUI 后端/前端镜像 embedding 的 builtin 交互。

**Tech Stack:** Go（stdlib only）、llama.cpp llama-server（b10405，安装包已分发 dist/runtime）、HF hf-mirror 下载、原生 JS GUI。

**关键已核实事实（2026-09-12，hf-mirror 一手）：**
- Qwen 官方 GGUF 仓 0.6B/1.7B **只有 Q8_0**；无官方 4B-Instruct-2507 GGUF（用 unsloth 仓， provenance 已记录）。`X-Linked-Etag` 响应头 = lfs sha256（与 embedding 清单存量条目交叉验证一致）。
- `dist/runtime/llama-server.exe --help` 实测支持 `--chat-template-kwargs`（可压 qwen3 混合思考模型的 thinking，防吃光过滤 max_tokens 预算）与 `--jinja`（默认开）。
- embedsidecar 机制（agent 探索报告）：Manager{RuntimeDir/ModelsDir/HealthTimeout 90s/IdleTimeout 10min}、Ensure/Reconcile/Stop、状态文件 `<OK_HOME>/embed-sidecar.json`（{pid,port,model_id,started_at,last_used}）+ `.want` + `.log`、`freePort`、`State.BaseURL()/Healthy()/Touch()`、daemon `sidecarJanitor` 10s 调和（internal/daemon/sidecar.go）、`embed.Download`（.part 断点续传+sha256 校验+原子改名）、GUI dlJob 进度模式（internal/gui/embedding.go:23-31）。**测试电池存在**（manager_test.go + sidecar_test.go），chatsc 适配时同款翻译。

## Global Constraints

- Go 版本见 `go.mod`；**禁止新增第三方依赖**（stdlib + 现有 internal 包）。
- **embedsidecar 与 embedding 链路一行不改**；embedding 现有测试必须原样全绿（不修改、不跳过）。
- 行为变化类新测试必须先验证对旧代码变红（判别性测试）。
- hook 链路 fail-open：chat sidecar 未就绪/启动失败时 filterx 静默降级（保留全部候选），绝不阻断注入。
- llmx.New 保持**永不返回 nil** 的契约（全仓 4 个调用点无 nil 检查）；builtin 未就绪在 Chat/Test 返回描述性错误。
- 全局 config.toml 写盘只走 `setupx.updateGlobalConfig`（fsx.WithFileLock 锁内）。
- GUI 开关两段式（draft + 确定统一落盘）；api_key 掩码/防外传语义不破坏（builtin 无 key 概念）。
- 提交信息格式：`type(scope): 中文描述`；changelog 不硬折行。
- 模型文件**不进安装包**（运行时下载，与 embedding 同款）；llama-server 二进制已随包（不动构建脚本）。
- Windows 上 git-bash 跑 go 命令；go test 不接管道。

---

### Task 1: chatx 包——chat 模型清单与下载薄封装

**Files:**
- Create: `internal/chatx/chatx.go`
- Test: `internal/chatx/chatx_test.go`

**Interfaces:**
- Produces（Task 2/4/5 消费）:
  - `type Model struct { ID, Label, Repo, File string; Size int64; SHA256 string; Thinking bool }`
  - `var Models = []Model{...}`（三条，精确值见下，**逐字钉死**）
  - `func FindModel(id string) (Model, bool)`
  - `func (m Model) InstalledPath(modelsDir string) string`（`<modelsDir>/<id>.gguf`，与 embedding 同惯例）
  - `func (m Model) Installed(modelsDir string) bool`
  - `func Download(ctx context.Context, hc *http.Client, m Model, mirror, modelsDir string, progress func(done, total int64)) error`（薄封装：映射为 `embed.BuiltinModel` 调 `embed.Download`）

**模型清单（2026-09-12 hf-mirror 实测，逐字）：**

| ID | Label 建议 | Repo | File | Size | SHA256 | Thinking |
|---|---|---|---|---|---|---|
| `qwen3-1.7b-q8` | Qwen3-1.7B · Q8_0（首选 · 1.8GB · 中文强） | `Qwen/Qwen3-1.7B-GGUF` | `Qwen3-1.7B-Q8_0.gguf` | 1834426016 | `061b54daade076b5d3362dac252678d17da8c68f07560be70818cace6590cb1a` | true |
| `qwen3-0.6b-q8` | Qwen3-0.6B · Q8_0（最省 · 640MB · 老机器可跑） | `Qwen/Qwen3-0.6B-GGUF` | `Qwen3-0.6B-Q8_0.gguf` | 639446688 | `9465e63a22add5354d9bb4b99e90117043c7124007664907259bd16d043bb031` | true |
| `qwen3-4b-instruct-2507-q4km` | Qwen3-4B-Instruct-2507 · Q4_K_M（最稳 · 2.5GB · 非思考版） | `unsloth/Qwen3-4B-Instruct-2507-GGUF` | `Qwen3-4B-Instruct-2507-Q4_K_M.gguf` | 2497281120 | `3605803b982cb64aead44f6c1b2ae36e3acdb41d8e46c8a94c6533bc4c67e597` | false |

（清单首条 = 默认推荐，与 embedding 清单"默认第一条"惯例一致。4B 用 unsloth 仓的原因写进注释：官方无 4B-Instruct-2507 GGUF。）

- [ ] **Step 1: 写失败测试** `internal/chatx/chatx_test.go`：
  - `TestManifestWellformed`：三条模型 ID 唯一；Size>0；SHA256 为 64 位小写 hex；首条 ID == "qwen3-1.7b-q8"（默认推荐钉死）；`FindModel` 命中/未命中。
  - `TestInstalledPathAndInstalled`：TempDir 下放 `<id>.gguf`（按 Size 截断写入无效——`Installed` 语义与 embedding 同款：存在且 size 一致；写恰好 Size 字节的假文件断言 true，少 1 字节断言 false）。
  - `TestDownloadMapsToEmbed`：httptest 模拟下载源（serve 一个小文件 + Range 支持），Model 的 Size/SHA256 改成该小文件实测值调 `Download`，断言落盘 `<modelsDir>/<id>.gguf` 内容一致且 progress 回调被调。
- [ ] **Step 2: RED**——`go test ./internal/chatx/`（包不存在编译失败）
- [ ] **Step 3: 实现** `internal/chatx/chatx.go`：Model 结构体（注释标 Thinking 语义：true=混合思考模型，sidecar 启动参数需压 thinking，见 Task 2）、Models 清单（上表逐字 + 每条注释来源日期）、FindModel、InstalledPath、Installed、Download 薄封装：
```go
// Download 复用 embed.Download（.part 断点续传 + sha256 校验 + 原子改名）：
// chat 模型映射为 embed.BuiltinModel 仅取 ID/Repo/File/Size/SHA256 五字段。
func Download(ctx context.Context, hc *http.Client, m Model, mirror, modelsDir string, progress func(done, total int64)) error {
	return embed.Download(ctx, hc, embed.BuiltinModel{
		ID: m.ID, Repo: m.Repo, File: m.File, Size: m.Size, SHA256: m.SHA256,
	}, mirror, modelsDir, progress)
}
```
（`InstalledPath`/`Installed` 若 `embed.BuiltinModel` 的同名方法可直接复用映射，则同样薄封装；否则按 embedding 同款语义自写：存在且 size 一致。）
- [ ] **Step 4: GREEN**——`go test ./internal/chatx/ ./internal/embed/ -v`（embed 包零回归）
- [ ] **Step 5: Commit** `feat(chatx): chat 模型清单（1.7B 首选/0.6B/4B-instruct）与下载薄封装`

### Task 2: chatsc 包——chat sidecar 进程托管

**Files:**
- Create: `internal/chatsc/manager.go`、`internal/chatsc/sidecar.go`、`internal/chatsc/spawn_windows.go`、`internal/chatsc/spawn_other.go`
- Test: `internal/chatsc/manager_test.go`、`internal/chatsc/sidecar_test.go`（从 embedsidecar 测试电池翻译）

**Interfaces:**
- Consumes: `chatx.Model`（Task 1）
- Produces（Task 3/4/5 消费）:
  - `type Manager struct { RuntimeDir, ModelsDir string; HealthTimeout, IdleTimeout time.Duration; ... }`（与 embedsidecar.Manager 同字段）
  - `func (m *Manager) Ensure(model chatx.Model) (*State, error)`
  - `func (m *Manager) Reconcile(desired *chatx.Model, now time.Time)`
  - `func (m *Manager) Stop()`
  - `func LoadState() *State`、`func RequestStart()` / `ClearWant()` / `WantPending()`、`func (s *State) BaseURL() string` / `Healthy() bool` / `Touch()`
  - `func DefaultRuntimeDir() string`、`func DefaultModelsDir() string`、`func RuntimeServerPath(runtimeDir string) string`（复用同一 llama-server 二进制，与 embedsidecar 同路径发现逻辑）

**适配规约（从 internal/embedsidecar 逐文件适配，行为差异仅限以下四点，其余语义逐字保持）：**
1. **状态文件族**：`embed-sidecar.json/.want/.log` → `chat-sidecar.json/.want/.log`（`statePath()` 等全部换前缀；`State` 结构同构）。
2. **进程参数**（spawnLocked 内命令行构造）：`llama-server -m <modelPath> --port <port> --host 127.0.0.1 -c 8192 --chat-template-kwargs '{"enable_thinking":false}'`——去 `--embeddings/--pooling`；`-c 8192`（过滤 prompt ~1-2K token 留余量，内存可控）；`--chat-template-kwargs` 压 qwen3 混合思考模型的 thinking（4B-instruct 模板不用该键，无害——llama.cpp 只把模板消费的键传入）。Thinking=false 的模型（4B）同样带该参数（统一无脑压，免分支）。
3. **模型类型**：`embed.BuiltinModel` → `chatx.Model`（无 Pooling/Dim/前缀概念）。
4. **看护字段语义**：`lastDesired` 比对、failCount 3 次冷却、unhealthyStreak 2 轮判死、IdleTimeout 回收、freePort TOCTOU 靠 Reconcile 自愈——全部逐字保持（复制时锁纪律（mu）一行不丢，知识库"复制函数丢锁包裹"教训：Manager 每个导出方法逐一核对锁包裹）。

- [ ] **Step 1: 翻译测试电池**：embedsidecar 的 manager_test.go/sidecar_test.go 逐用例翻译为 chatsc 版本（状态文件名、Model 类型、args 断言三处适配）；新增 `TestSpawnArgsChat`：构造 Manager 调参数构造函数（若不可达则经 ServerCommand 接缝断言），断言命令行含 `-c 8192` 与 `--chat-template-kwargs`、**不含** `--embeddings`。
- [ ] **Step 2: RED**（包不存在编译失败）
- [ ] **Step 3: 适配实现**（逐文件复制后按四点规约改；spawn_windows.go 的 hideWindow 原样需要）
- [ ] **Step 4: GREEN**——`go test ./internal/chatsc/ ./internal/embedsidecar/ -v`（两包都绿，embedding 零回归实证）
- [ ] **Step 5: Commit** `feat(chatsc): chat sidecar 进程托管（独立状态文件族，压 thinking 参数）`

### Task 3: daemon——chat sidecar 看护接线

**Files:**
- Modify: `internal/daemon/sidecar.go`（加 chatJanitor 或泛化 janitor 双实例）、`internal/daemon/run.go:143-150`（建第二个 Manager）
- Test: `internal/daemon/sidecar_test.go`（追加 desiredChatModel 用例）

**Interfaces:**
- Consumes: `chatsc.Manager`（Task 2）、`config.LLM`（ActiveProfile，kind=builtin 时 Model=chatx 清单 id）
- Produces: `func desiredChatModel(cfg config.Config) *chatx.Model`（active profile 非 builtin/未知 id 返 nil，与 desiredBuiltinModel 同构）

- [ ] **Step 1: 失败测试**：`TestDesiredChatModel`——active 为 builtin 且 id 在清单 → 返回该模型指针；kind=openai → nil；id 未知 → nil；无 active → nil。
- [ ] **Step 2: RED**
- [ ] **Step 3: 实现**：`desiredChatModel`（镜像 desiredBuiltinModel，sidecar.go:17）；run.go 在 sidecarMgr 旁建 `chatMgr := &chatsc.Manager{RuntimeDir: chatsc.DefaultRuntimeDir(), ModelsDir: chatsc.DefaultModelsDir(), HealthTimeout: 90*time.Second, IdleTimeout: 10*time.Minute}`，`defer chatMgr.Stop()`，janitor 循环内每 10s 同样 `LoadMerged` → `chatMgr.ModelsDir = embedsidecar.ModelsDir(cfg)`（**与 embedding 共用同一模型目录**，GGUF 按 id 文件名天然隔离）→ `chatMgr.Reconcile(desiredChatModel(cfg), time.Now())`。两 sidecar 共存内存预算写注释（0.6B emb + 1.7B chat Q8 ≈ 3GB）。
- [ ] **Step 4: GREEN**——`go test ./internal/daemon/ -v`
- [ ] **Step 5: Commit** `feat(daemon): chat sidecar 看护（第二 janitor，与 embedding 共存）`

### Task 4: llmx——kind=builtin（状态文件发现端口）

**Files:**
- Modify: `internal/llmx/llmx.go`（New 加 builtin 分支 + not-ready 语义）
- Modify: `internal/config/config.go:79-83`（LLMProfile.Kind 注释加 builtin）
- Test: `internal/llmx/llmx_test.go`（追加）

**Interfaces:**
- Consumes: `chatsc.LoadState()` / `RequestStart()` / `State.BaseURL()` / `Healthy()`（Task 2）
- Produces: `LLMProfile.Kind` 接受 `"builtin"`（Model=chatx 清单 id）；**New 契约不变（永不 nil）**——builtin 未就绪时返回的 client 在 Chat/Test 立即返回错误 `内置模型启动中（已请求拉起 sidecar），请稍后重试`

- [ ] **Step 1: 失败测试**（OK_HOME 隔离到 TempDir）：
  - `TestChatBuiltinNotReady`：无状态文件 → Chat 返回"启动中"错误，且 `<OK_HOME>/chat-sidecar.want` 被写入（RequestStart 生效）；
  - `TestChatBuiltinReady`：httptest 起 /health + /v1/chat/completions，手写 `chat-sidecar.json` 指向其端口 → Chat 正常返回（复用既有 openai 断言模式）。
- [ ] **Step 2: RED**（kind=builtin 走 default 报"未知 llm 类型"；want 未写）
- [ ] **Step 3: 实现**：`New` 加分支——`p.Kind == "builtin"` 时：`st := chatsc.LoadState()`；`st != nil && st.Healthy()` → `p.BaseURL = st.BaseURL()`、`p.Kind = "openai"`（内部按 openai 协议走，state.Touch() 由成功调用方路径……Touch 语义：embedx 是 sidecarClient 成功调用后 Touch——此处简化为 New 内 healthy 时 Touch 一次，注释说明）；否则 `chatsc.RequestStart()` 并把 client 标记 notReady（加私有字段 `notReady error`）。`Chat`/`Test` 入口：`if c.notReady != nil { return Reply{}, c.notReady }`。
- [ ] **Step 4: GREEN**——`go test ./internal/llmx/ ./internal/filterx/ -v`（filterx 零回归：llmx.New 契约未变）
- [ ] **Step 5: Commit** `feat(llmx): kind=builtin 本地托管档（sidecar 状态发现，未就绪"启动中"错误）`

### Task 5: GUI 后端——builtin 校验与下载端点

**Files:**
- Modify: `internal/gui/llm.go`（save/active/test 加 builtin 分支；apiLLMGet 响应加 builtin_models + chat 下载快照）
- Modify: `internal/gui/api.go`（注册 `/api/llm/download`、`/api/llm/download/cancel` 路由，镜像 embedding.go:120-128 区）
- Test: `internal/gui/llm_test.go`（追加）

**Interfaces:**
- Consumes: `chatx.Models`/`FindModel`/`Download`（Task 1）、`chatsc.RequestStart`（Task 2）
- Produces（Task 6 前端消费的契约）：
  - `GET /api/llm` 响应加 `builtin_models: [{id,label,size,downloaded}]` 与 `chat_download: {model_id,state,done,total,error}`
  - `POST /api/llm/profile`：kind=builtin 时 Model 必须在 chatx 清单、base_url/api_key 忽略（置空）
  - `POST /api/llm/active`：激活 builtin profile 要求模型已下载（未下载 409，镜像 apiEmbeddingActive :169 语义），成功后 `chatsc.RequestStart()` 预热
  - `POST /api/llm/download` `{model_id, mirror}` / `POST /api/llm/download/cancel` `{model_id}`（dlJob 模式，与 embedding 下载 job 用 key 前缀 `chat:` 隔离或独立 map，实现者读 embedding.go:23-31/290-355 镜像）

- [ ] **Step 1: 失败测试**：save builtin（清单内 id → 200；未知 id → 400）；active 未下载 → 409；下载端点已下载 → `{state:"done"}`。
- [ ] **Step 2: RED**
- [ ] **Step 3: 实现**（逐段镜像 embedding.go 对应 handler，模型源换 chatx）
- [ ] **Step 4: GREEN**——`go test ./internal/gui/ -v`
- [ ] **Step 5: Commit** `feat(gui): LLM builtin 档端点（清单/下载/激活门控）`

### Task 6: GUI 前端——LLM 弹窗 builtin 档

**Files:**
- Modify: `web/app.js`（LLM 弹窗：kind 下拉加 builtin、builtin 表单=模型下拉+mirror+下载进度、i18n 键）

**Interfaces:**
- Consumes: Task 5 契约

- [ ] **Step 1: i18n**：加 `kindBuiltin:"内置本地模型（ok 托管 · 无需联网）"`（与 embedding 同款文案键区分命名如 `lKindBuiltin` 防撞 `typeBuiltin`）。
- [ ] **Step 2: 表单**：`llmForm.kind==="builtin"` 时——模型字段变下拉（`PREFS.llm.builtin_models` 驱动，含 size/downloaded 标注）+ mirror 选择 + 下载按钮/进度条（翻译 embedding 弹窗 paintEmbDl 模式为 paintLlmDl，轮询 GET /api/llm 的 `chat_download`）；base_url/key/temperature 行隐藏；识别意图勾选与说明照常（builtin 是识别意图的主力场景，默认提示文案可复用）。
- [ ] **Step 3: 列表行/设置卡徽标**：kind=builtin 显示 `内置`（照 embedding 的 builtin 徽标配色）。
- [ ] **Step 4: 静态检查** `node --check web/app.js` + Grep 无遗漏 kind 两/三档硬编码。
- [ ] **Step 5: Commit** `feat(gui): LLM 弹窗 builtin 托管档（模型下拉+一键下载+进度条）`

### Task 7: 全量回归 + changelog + 手工验证

- [ ] **Step 1: 全量**：`go build ./...`、`go test ./internal/...`（不接管道）全绿，embedding 链路零回归重点确认。
- [ ] **Step 2: changelog**：`docs/changelogs/2.26.0.md` 追加（或按当时惯例）——[llm] builtin 托管档：三模型清单（1.7B 首选/0.6B 最省/4B 非思考最稳）、ok 托管下载、chat sidecar（压 thinking）、GUI 一键下载。
- [ ] **Step 3: 手工验证**（控制器/用户）：重建 dist → 部署 → GUI 新增 builtin profile → 下载 qwen3-1.7b-q8（真实 1.8GB 走一遍，含进度条/取消/续传）→ 激活 → 测试连接 → 勾识别意图设为使用中 → 发 prompt 看 ok.log `prompt filter:` 行走本地 sidecar（任务管理器可见 llama-server chat 实例）；断网场景验证 fail-open（sidecar 杀掉后发 prompt 注入不阻断、ok.log 有"启动中/调用失败保留全部"行）。

---

## Self-Review 记录

- **Spec 覆盖**：builtin 托管档全链路——清单/下载（T1）→ sidecar 托管（T2）→ daemon 看护（T3）→ 客户端发现（T4）→ 后端端点（T5）→ 前端交互（T6）→ 回归验证（T7）。模型选型继承上一计划的调研结论并已全部钉死精确值。
- **关键设计裁决**：① 克隆适配而非泛化 embedsidecar（已发布链路零风险，进程管理核重复有界，终审分诊）；② llmx.New 永不 nil 契约保持（免全仓调用点扫荡）；③ chat 与 embedding 共用模型目录与 llama-server 二进制；④ thinking 统一由 sidecar 启动参数压制（不污染 llmx 请求体，对 4B 无害）。
- **已知残留**：unsloth 仓 4B 的 provenance 弱于官方仓（无官方可选，注释已记）；chat 与 embedding sidecar 并存时内存 ≈3GB（注释提示）；`New 内 Touch` 是对 embedx"成功调用后 Touch"语义的简化（差异：未用也续命——Reconcile 的 IdleTimeout 回收因此偏保守，方向安全）。
