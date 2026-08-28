# 个人多端同步：syncx 包落地 git 执行器（错误三分类）

日期：2026-08-28

个人多端同步功能的第一块落地：新建 `internal/syncx` 包（叶子包，仅依赖 procx），提供后续所有同步任务共用的 git 执行入口 `execGit(dir, timeout, args...)`——参数数组直调系统 git 不走 shell，统一注入 `GIT_TERMINAL_PROMPT=0` 防凭据提示挂起，Windows 下经 `procx.HideWindow` 隐藏控制台窗口。

错误三分类，调用方可分别决策：`ErrGitNotFound`（系统未装 git 或不在 PATH——同步禁用，本地功能不受影响）、`ErrTimeout`（执行超时，本地操作默认上限 10s、clone/pull/push 等网络操作 60s，均为包级 var 便于测试调小）、`*ExitError`（git 非零退出，含退出码与 stdout+stderr 合并输出）。判定顺序先超时后退出码——超时被 kill 的进程同样表现为 ExitError，必须先判 `ctx.Err()`。

当前仅基础设施，无用户可见行为变化；后续任务（仓库初始化、提交快照、远端推拉）均经此入口执行 git。

测试：4 个用例覆盖三分类与正常路径——`--version` 探测（无 git 机器自动 SKIP）、PATH 置空测 NotFound、非仓目录测 ExitError（含退出码非零与输出断言）、1ns 超时测 ErrTimeout。`go test ./internal/syncx/ -v` 全绿，`go vet` 干净。
