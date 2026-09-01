# oksrv 建仓不再下发 git token（凭证统一，唯一通道=自助重发）

日期：2026-09-01

git 凭证统一（2026-09-01 设计）服务端第一步：`POST /api/v1/repos/personal` 建仓后不再调用 `CreateUserToken`，响应 `git_token` 恒为空串——字段保留、JSON 形状不变，旧客户端解析不破。原 v1.1 语义"token 只在首次建仓下发、幂等重试返空、丢失走 reset-password 联动重发"就此废除：首次与幂等路径行为拉齐，凭证发放收敛到唯一通道 `apiGitToken` 自助重发（按机器分名 `ok-sync-r-<hostname>`，删同名再建、多机互不吊销）。审计口径不变，`create-personal-repo` 仍随建仓事实落库。

配套契约留给后续任务消费：客户端拿到空 `git_token` 时走已存在的自助重发路径（gui 绑定管线改造另任务落地）；建用户随附的 `ok-sync` token 停发是紧随的下一任务，本次不动 `apiUserCreate`。`TestFullManagementFlow` 建仓段断言随之反转（首次与幂等建仓 `git_token` 均须空串），并新增"两次建仓前后 alice 的 token 数不变"断言钉住"建仓全程零发放"——用增量而非恒零口径，是因为同一测试里建用户流程在下一任务落地前仍会发一条 `ok-sync`，与建仓行为无关。

TDD 开发：断言先改、对旧实现变红（首次建仓返回非空 token），删 `CreateUserToken` 调用及其错误分支后转绿；`internal/oksrv` 全包测试一次通过，`go build ./...` 干净。
