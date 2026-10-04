# 2026-10-04 agentx：Orosus 适配器两处修正（PostToolUse 挂载 + 标记块换行）

## 现象与根因

Orosus 真机报 hook 异常，两个独立缺陷：

1. **标记块末行与结束标记粘连**：`OrosusHooksBlockFor` 各事件块末行 `timeout = N`
   不带换行，upsert 直接拼 `MarkerEnd`，产出
   `timeout = 20# <<< okryptos hooks <<<` 连行——TOML 行尾注释前无空白，Orosus
   用的 smol-toml 解析失败、hooks 模块激活炸降级窗。测试当时用 BurntSushi 验
   TOML 合法性，而 BurntSushi 容忍该形态，没拦住——回归钉改为直接断言换行。
2. **post-tool 挂载位过时**：适配器按旧文档（PostToolUse 载荷不带 tool_input）
   把 post-tool 挂在 PreToolUse。Orosus 文档已补齐——PostToolUse 载荷带
   `tool_input`（工具实收参数、改参后为最终值，cc 口径同款），§12 示例四明确
   事前挂 PreToolUse 会记「未遂的写」（工具可能被审批拒绝或执行失败）。

## 改动

- `orosusHookEvents`：post-tool 改挂 **PostToolUse**（matcher `^tool-fs__(write|edit)$`
  不变），与 ZCode 的 Write|Edit 同位。
- `OrosusHooksBlockFor`：每行（含末行 timeout）`\n` 收尾，`Join(blocks, "\n")`
  块间恰一空行；结束标记恒独立成行。
- 存量治理：PreToolUse 旧挂载表由 `stripLegacyOKHooksOrosus` 照常剥离（判定只看
  command 行、与事件名无关）——真机旧标记块自愈时自动重写为 PostToolUse 形态。
- 测试：块形态/幂等/换行回归钉（末行 `\n` 收尾、MarkerEnd 独立成行且前行恰为
  timeout 行、块内行尾无注释）；手写件夹具刻意保留 PreToolUse 旧形态验证存量
  替换为 PostToolUse。
- 真机 `~/.orosus/modules.d/hooks.toml` 已同步（标记块表头 PreToolUse →
  PostToolUse，Orosus /reload 生效）；PostToolUse 载荷实测 post-tool 记账
  `touched` 正确落盘。

## 测试

`go test ./internal/agentx/ -run Orosus` 10 例全绿。**注意：已发布的 v2.26.5
安装包携带本修正前的代码**（新装用户经引导页安装会写出粘连形态）——需补丁版
收编。
