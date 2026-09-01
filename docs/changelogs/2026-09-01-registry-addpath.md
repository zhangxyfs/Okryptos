# registry.AddPath：空壳项目补挂工作目录（幂等/冲突哨兵）

日期：2026-09-01

服务器拉取与备份恢复会注册空 Paths 的壳项目，而 hooks 的 `FindByCwd` 只按 Paths 前缀匹配 cwd——壳项目永不命中，ok.log 持续刷「目录未注册」，hook 注入静默失效。注册表新增 `AddPath(name, path) error` 补挂方法：把真实工作目录追加进已存在项目的 Paths，hooks 每次现读注册表，关联后即时生效，无需重启 daemon。

配套两个分类哨兵 `ErrProjectNotFound` / `ErrPathConflict`（`%w` 包装），调用方用 `errors.Is` 分类——GUI 端点据此映射 404/409，CLI 补挂据此给可读报错（均在后续任务接入，本次只落注册表方法）。行为口径：规范化后同路径幂等（尾分隔符/大小写差异不重复追加）；路径已挂其他项目拒绝，冲突判断与 `AddProject` 一致（规范化后相等，不引入前缀级冲突）；锁与持久化不收进方法内，由调用方 `registry.Update`（跨进程文件锁）与 `Save` 负责，与 `RemoveProject` 同款约定。

实现采用两阶段扫描而非"名匹配即追加"：先全量扫描非目标项目判冲突并记录目标下标，再对目标项目做幂等检查后追加——单循环短路写法在目标项目排在冲突项目之前时会漏掉冲突检查，路径被静默追加（计划稿原实现即踩此坑，测试实跑变红后修正）。

TDD 开发：`TestAddPath`（补挂成功 / 尾分隔符幂等 / 跨项目冲突 / 未知项目四条路径）先跑确认变红（编译失败，API 与哨兵尚不存在），实现后 `internal/registry` 全包 14 个测试一次通过，`go build ./... && go vet ./...` 干净。测试路径用反斜杠 Windows 形式，靠 `NormalizePath` 的分隔符统一与 Windows 限定小写折叠保证 Linux 下同样成立。
