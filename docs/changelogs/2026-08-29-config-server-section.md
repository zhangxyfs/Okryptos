# 全局 [server] 段（okserver 管理面连接）

日期：2026-08-29

P1-C2（本地接入 okserver）配置底座：全局 config.toml 新增 `[server]` 段（url/username/token），记录 okserver 管理面连接（设计文档 §12）。该段仅存全局文件，LoadMerged 合并时项目文件不写 [server] 即继承全局。

`config.SetServer` 照 SetSync 的行级小节读-改-写形态（fsx 锁内完成），只重写 [server] 段，文件其余内容（含注释）原样保留。token 传空串 = 保留旧 token 行——GUI 回写脱敏值（不回显真实 token）时不致把已存 token 静默清掉；url/username 空串则整行省略。
