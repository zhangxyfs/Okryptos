# 壳项目关联工作目录：状态正名 + 单个/一键关联 + ok init 同名补挂

日期：2026-09-01

服务器拉取/备份恢复注册的空 Paths 壳项目，hooks 按 cwd 最长前缀匹配注册表 Paths 永不命中，hook 注入/沉淀静默失效（ok.log 每回合刷「目录未注册为知识库项目」），且 GUI 服务器页对其显示「已绑定」名不副实。本期闭环：注册表新增 AddPath（锁内补挂、规范化相等冲突口径同 AddProject、ErrProjectNotFound/ErrPathConflict 哨兵）；GUI 新增 POST /api/project/attach（400 空路径或目录不存在 / 404 未知项目 / 409 路径冲突，hooks 现读注册表即时生效无需重启）；服务器页「我的项目绑定」卡对壳项目显示「未关联目录」并给行内「关联目录」按钮（uiPrompt 收绝对路径，目录名可与项目名不同），≥2 个壳项目时出「一键关联」多输入 modal 批量串行执行、单个失败不阻断、结束汇总 toast；CLI 侧 ok init 遇同名已注册项目改为幂等补挂 cwd 并输出「已关联目录」，同一 AddPath 同时救拉取壳与备份恢复壳。
