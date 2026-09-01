# CLI `ok init` 同名补挂：空壳项目关联工作目录

日期：2026-09-01

服务器拉取与备份恢复会在注册表留下空 Paths 的壳项目，此前在对应工作目录运行 `ok init <name>` 直接报「项目已存在」退出，壳项目永远挂不上真实目录，hooks 的 `FindByCwd` 不命中、注入静默失效。本次把 `ok init` 的同名分支从报错改为补挂：注册表闭包内按名预检，命中同名项目即调用 Task 1 落地的 `registry.AddPath(name, cwd)` 追加当前目录（幂等/冲突哨兵沿用注册表口径），未命中才走原有 `AddProject` 新建。

行为口径：补挂成功后输出「项目 %q 已注册，已关联目录 %s」（含「已关联目录」字样）以区别于首次注册；`AddPath` 报 `ErrPathConflict`（cwd 已挂其他项目）时仍按错误退出，语义与旧报错路径一致。读-改-写仍在 `registry.Update` 跨进程文件锁内，与并发 GUI 删除/备份恢复互不覆盖。

TDD 开发：`TestInitAttachesWorkdirToShellProject` 预置同名空壳项目、在工作目录（目录名与项目名刻意不同）跑 `Init`，断言退出码 0、输出含「已关联目录」、`FindByCwd` 命中。先跑确认对旧代码变红（`init attach code=1 err="项目 \"demo\" 已存在"`），实现后 `internal/cli` 全包一次通过，`go build ./... && go vet ./...` 干净。测试沿用 `TestInitAddSearchList` 同款 Setenv 隔离组，Windows/Linux 双平台可跑。
