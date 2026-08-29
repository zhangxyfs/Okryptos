# okserver SQLite 存储层（internal/oksrv）

日期：2026-08-29

新增 okserver（NAS/Docker 部署的管理面）存储层 `internal/oksrv`：单文件 SQLite 库 `<dataDir>/okserver.db`，七表——users（root 全库唯一、可禁用但 root 拒绝禁用）、sessions（认证层用，仅建表）、orgs、org_members（INSERT OR REPLACE 幂等加成员）、repos（layer/owner/project 三元组主键，重复登记只更新 clone_url）、audit（倒序分页，写入失败只记 stderr 不中断业务操作）、meta KV（存 schema 版本等）。时间一律 UTC RFC3339 存 TEXT；dsn pragma 串（busy_timeout/WAL/synchronous=NORMAL）照 internal/index 先例。读路径出错与空结果同态返回 nil/空切片，写路径错误原样上抛。

TDD 开发：生命周期测试（建 root→拒绝第二个 root→重名拒绝→禁用/改密→组织成员→仓库 upsert→审计→meta→关库重开验证持久化）先行，实现后一次通过。
