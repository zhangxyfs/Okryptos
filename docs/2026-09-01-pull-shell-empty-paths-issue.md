# 拉取壳项目空 Paths 导致 hooks 全失效（2026-09-01 记录，待修）

## 现象

Linux 机器上用 GUI「拉取」把服务器仓拉到本机后，ok.log 反复出现：

```
stop project (cwd="/home/zhangxyfs/桌面/develop/OpenKnowledge"): 目录未注册为知识库项目: ...
```

而 GUI 服务器页该项目显示「已绑定」，数据也确实拉下来了——两个现象同时成立，不矛盾。

## 根因

- `POST /api/server/pull`（`internal/gui/api_server.go` apiServerPull）注册的是**空 Paths 壳项目**：
  `reg.Projects = append(reg.Projects, registry.Project{Name: req.Project})`（备份恢复同款先例）。
  知识条目 clone 到 `~/.openknowledge/projects/<name>`，但用户的工作目录从未挂进注册表。
- hooks（stop 等）按 cwd 走 `project.FromCwd` → `registry.FindByCwd`（`internal/registry/registry.go:103`）最长前缀匹配 Paths；
  空 Paths 永不命中 → 「目录未注册为知识库项目」，该项目在该机器上 **hook 注入/沉淀全部静默失效**（每回合 stop 刷一条日志）。
- GUI「已绑定」看的是项目 config 的 `sync.is_repo`（git 远端绑定状态），与注册表 Paths 是两套独立状态。

同源隐患：备份恢复也注册空 Paths 壳，同样有此问题。

## 修法方向（最小改动）

1. **`ok init` 同名幂等化**：`registry.AddProject` 目前同名直接报「项目已存在」（registry.go:124）。
   改为：同名项目已存在时，把 cwd 规范化后**补挂进 Paths**（锁内去重；已是成员则幂等成功）。
   用户在项目目录里跑一次 `ok init` 即完成关联——同时救 pull 壳与备份恢复壳。
   注意同路径冲突检查（registry.go:132 段）语义保持：路径已挂在**别的**项目下仍须拒绝。
2. 可选（GUI 层）：管理页/服务器页对空 Paths 项目提示「未关联工作目录，请在项目目录执行 ok init」。

## 验证口径

- 新增测试：拉取/恢复出的空壳项目，在其工作目录 `ok init` 后 Paths 含该目录，`FindByCwd` 命中。
- 行为变化类测试先验证对旧代码变红（既有约定）。
