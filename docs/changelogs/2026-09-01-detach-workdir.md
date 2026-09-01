# 解除目录关联：RemovePath + detach 端点 + GUI 目录管理入口

日期：2026-09-01

关联工作目录（attach）上线后补齐对偶的撤销路径：挂错目录（存在但挂错项目）此前只能手编 registry.toml。注册表新增 `RemovePath`（AddPath 对偶：锁内、规范化相等匹配、路径不在项目下幂等、未知项目 ErrProjectNotFound；最后一条解除后项目回到空 Paths 壳状态，项目与知识库数据保留）；GUI 新增 `POST /api/project/detach`——**故意不做目录存在性校验**（目录已删正是常见解绑场景），400 空路径 / 404 未知项目，paths 在 Update 锁内取出回包（同 attach 终审裁决口径）；服务器页「我的项目绑定」卡已绑定行新增「目录」按钮，弹窗列出已关联目录逐条「解除」（uiConfirm 确认、busy 态、toast 反馈），全部解除后项目自动回到「未关联目录」可重新关联。hooks 现读注册表，解除即时生效无需重启。i18n zh/en 同步 5 键（srvPathsBtn/srvPathsTitle/srvDetach/srvDetachConfirm/srvDetachDone）。
