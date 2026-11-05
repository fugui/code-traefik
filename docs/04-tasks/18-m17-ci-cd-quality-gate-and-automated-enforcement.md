# M17 作战卡：CI/CD 自动化强阻断质量门禁、实测边回传与排他锁有界排队

- **里程碑编号**：`M17`
- **状态**：✅ 已交付 (Completed)
- **前置依赖**：`M16` 已交付
- **关联特性**：`F-27`、`SC-10`
- **关联设计规范**：[`14-governance-dashboard-golden-suite-and-ci-gate-adr.md`](../05-adr/14-governance-dashboard-golden-suite-and-ci-gate-adr.md) (ADR-14 v1.1.0)、[`05-database-schema.md`](../02-design/05-database-schema.md) (v1.4.0)、[`01-http-api-contract.md`](../03-api/01-http-api-contract.md) (v1.4.0)

---

## 1. 里程碑目标与 TDD 实施要点

1. **数据面零 `.pb` 重放实测出站调用汇总回传 (`pkg/agentproto`, `pkg/replay`, `cmd/agent`)**：
   - 在 `pkg/agentproto/protocol.go` 的 `ReplayTaskResultResp` 中新增 `ObservedEgressCalls []ObservedEgressCall{ServiceName, Operation, DstAddr, CallCount, MatchStatus}`（严格保持 `cmd/agent` 零 DB、零 `.pb` 依赖，仅复用 `pkg/crosrpc` 头解析提取 `ascServiceName` 与 `ascOperation`）；
   - `ReplayScheduler` 将回传的 `ObservedEgressCalls` 持久化至 `ReplayTask.ObservedEgressCalls`；
   - 在 `internal/server/topology/consistency.go` 新增 `VerifyProcessObservedCalls(ctx, db, archProcessID, observedCalls)`：比对目标 `ArchProcess` 所装载模块的静态 `DEPEND` 契约，识别所有未报备的违规出站调用并写入 `ReplayTask.ViolationCount`。
2. **高安全 `CIOrAdminGuard` 机器 Token 鉴权 (`handlers/ci_auth.go` & `handlers/settings.go`)**：
   - 实现 `POST /api/admin/ci-token/rotate`（`crypto/rand` 生成 32B 随机凭据 `ctk_...`，仅在 `traefik_settings.ci_gate_token_sha256` 保存 SHA-256 摘要，响应强制 `Cache-Control: no-store`，通用 `GET /api/admin/settings` 自动屏蔽该内部键）与 `DELETE /api/admin/ci-token`；
   - 实现 `CIOrAdminGuard` 中间件：支持管理员 JWT 或 `X-Traefik-CI-Token` 请求头（`crypto/subtle.ConstantTimeCompare` 常量时间比对，严禁 URL Query 传参、严禁源码硬编码 fallback、严禁日志打印明文）并内置滑动窗口限流；前端 `/admin/settings` 增加 CI 门禁凭据轮换卡片。
3. **排他锁有界排队与双重强阻断门禁评估 (`POST /api/gates/evaluate`, `handlers/gate_handler.go`)**：
   - 支持 `wait_lease_seconds`（默认 `60s`，上限 `300s`）：当共享测试环境 `env_agent_id` 处于短期 `REPLAY` 租约占用时，按退避策略轮询等待获锁，消除多 MR 并发跑流水线导致的 `409` 误挂；
   - **正交 HTTP 状态码契约**：
     - `503 Service Unavailable`：Agent 离线、等锁超时（`GATED_ENV_BUSY`）或目标进程 `WAITING_PROC / NEED_RECONNECT`（`GATED_ENV_NOT_READY`）；
     - `422 Unprocessable Entity`：第一关检出未声明调用返回 `GATED_VIOLATION`，第二关检出套件非 `PASS`（状态码异常、`MOCK_MISS`、字段级 `FAILED Diff`）返回 `GATED_BREAKING_DIFF`；
     - `200 OK`：`ViolationCount == 0` 且套件全量 `PASS`，返回 `PASSED`；
   - 自动渲染结构化 `markdown_report`（含违规边/字段 Diff 表格及 `/traefik/debug?replay_task_id=<id>` 直达链接）。

---

## 2. 交付包与先行测试清单 (TDD Red -> Green)

| 分层 | 目标代码文件 | 先行测试文件与核心用例 |
| :--- | :--- | :--- |
| **Agent 实测边回传** | `pkg/agentproto/protocol.go`<br>`pkg/replay/` / `internal/agent/` | `pkg/replay/replay_test.go`：<br>- `TestReplayExecutor_CollectsObservedEgressCalls_HitAndMiss`（附 `cmd/agent` 零 DB/零 `.pb` 依赖门禁断言） |
| **CI 鉴权、排队与门禁 API** | `internal/server/topology/consistency.go`<br>`handlers/ci_auth.go`<br>`handlers/gate_handler.go`<br>`frontend/src/pages/AdminSettingsPage.tsx` | `handlers/gate_evaluate_test.go`：<br>- `TestCITokenRotate_ConstantTimeAuth_RejectQueryToken_AndRevoke`<br>- `TestGateEvaluate_ObservedEgressCallsBlocksViolationWith422`<br>- `TestGateEvaluate_BlocksBreakingDiffWith422`<br>- `TestGateEvaluate_ConcurrentPipelinesQueueSmoothly_AndTimeoutReturns503`<br>- `TestSC10_CICDQualityGateHardEvaluationAndReport` |

---

## 3. 准出质量门禁命令 (Exit Gate)

```bash
go list -deps ./cmd/agent | grep -E "gorm|dynamicpb|protoregistry" && exit 1 || true
TEST_DB_DSN="host=127.0.0.1 port=5432 user=postgres password=PWD dbname=postgres sslmode=disable" make test
make lint && make build && make clean
```

---

## 4. 交付验证结果 (Verification Summary)

- ✅ **Agent 零 DB / 零 .pb 依赖隔离**：`go list -deps ./cmd/agent` 校验通过，无 `gorm`、`dynamicpb`、`protoregistry` 依赖；重放出站调用汇总仅依赖 `pkg/crosrpc` 二进制头解析。
- ✅ **CI 机器 Token 常量时间鉴权**：通过 `TestCITokenRotate_ConstantTimeAuth_RejectQueryToken_AndRevoke` 验证高熵凭据生成（`ctk_...`）、DB 仅存 SHA-256 哈希、常量时间防时序攻击比对、严格拒绝 URL Query 传参、以及注销后 401 拦截。
- ✅ **实测出站边第一关强阻断 (422 GATED_VIOLATION)**：通过 `TestGateEvaluate_ObservedEgressCallsBlocksViolationWith422` 验证未报备调用即刻阻断。
- ✅ **契约劣化第二关强阻断 (422 GATED_BREAKING_DIFF)**：通过 `TestGateEvaluate_BlocksBreakingDiffWith422` 验证状态码异常与字段级 Diff 阻断。
- ✅ **排他锁有界排队与正交 503**：通过 `TestGateEvaluate_ConcurrentPipelinesQueueSmoothly_AndTimeoutReturns503` 验证并发等待退避顺利获锁（零 409 误伤）以及超时返回 503 `GATED_ENV_BUSY` 与未就绪返回 503 `GATED_ENV_NOT_READY`。
- ✅ **SC-10 端到端全通与绿牌准出 (200 OK PASSED)**：通过 `TestSC10_CICDQualityGateHardEvaluationAndReport` 验证零违规、100% 通过、结构化 Markdown 报告生成及 `/traefik/debug?replay_task_id=` 直达链接。
- ✅ **前端治理卡片与工作台单测**：`/admin/settings` CI 凭据治理卡片、轮换与吊销模态框就绪，`AdminSettingsPage.test.tsx` 校验通过，`make lint` 零警告零错误。
- ✅ **单二进制打包验证**：`make build && make clean` 100% 成功。

