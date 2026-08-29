# GUI 同步 ai-merge 端点（LLM 三版本语义合并）

日期：2026-08-28

okd 新增 `POST /api/project/sync/ai-merge`（设计文档 §11.4/§12）：冲突页"AI 合并"按钮的数据源。取冲突文件的 base/local/remote 三版本，剥离 front matter 后只把正文交给 LLM 做语义合并，结果拼回 local 版 front matter 返回 `{merged}`。纪律：front matter 不让 LLM 碰（三版本各自剥离，fm 固定取 local 本机最新）；只返回不落盘——落盘仍走 `/api/project/sync/resolve` 的 merged action，用户确认前磁盘内容不变。

可用性由项目配置 `[sync] llm_assist` 判定：`off` 或未配置全局 LLM → 409 `{error: "no_llm"}`；`server` → 409 `{error: "server_not_available"}`（服务端 LLM 未实现）；`local`/空（auto）看全局 LLM 配置。超时取 `[llm] timeout_sec`，缺省钳 60s（GUI 交互场景），ctx 再加 15s 余量；temperature 缺省不传（服务端默认值兼容性最好）。
