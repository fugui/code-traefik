# M15 作战卡：黄金契约套件自包含快照库、双源重放解耦与事务固化工作台

- **里程碑编号**：`M15`
- **状态**：✅ 已交付 (Delivered)
- **前置依赖**：`M14` 已交付
- **关联特性**：`F-15`（补齐第 10 项菜单）、`F-25`
- **关联设计规范**：[`14-governance-dashboard-golden-suite-and-ci-gate-adr.md`](../05-adr/14-governance-dashboard-golden-suite-and-ci-gate-adr.md) (ADR-14 v1.1.0)、[`05-database-schema.md`](../02-design/05-database-schema.md) (v1.4.0)、[`01-http-api-contract.md`](../03-api/01-http-api-contract.md) (v1.4.0)

---

## 1. 里程碑目标与 TDD 实施要点

1. **17 表物理模型升级与分区清理彻底解耦 (`models/golden_suite.go`, `models/replay.go`, `models/db.go`)**：
   - 新增表 16 `GoldenSuite` (`traefik_golden_suites`) 与表 17 `GoldenSuiteItem` (`traefik_golden_suite_items`)；
   - **完全自包含快照铁律**：沉淀用例时完整深拷贝固化 `raw_req_packet` (`bytea`)、`raw_rsp_packet` (`bytea`)、`rsp_seg_count`、`service_name`、`operation`、`baseline_status`、`scoped_mocks` (`[]agentproto.MockRuleDownstream` 成对请求+应答闭包)、`egress_endpoints` 与 `mask_rules_json`；`source_session_id` / `source_record_id` 设为可空弱引用，**运行时绝不回查 `traefik_traffic_records`**；
   - **双源 `ReplayTask` 解耦**：将 `ReplayTask.SessionID` 改为可空 `*uint64`（`ON DELETE SET NULL`），新增 `GoldenSuiteID *uint64` 与 `TriggerSource`（`MANUAL | SINGLE_DEBUG | GOLDEN_SUITE | CI_GATE`）；`ReplayItem` 新增 `SuiteItemID *uint64` 并以 `(task_id, seq_no)` 为唯一约束；`DeleteArchProcess` 增加 `GoldenSuite` 引用 `409` 守卫。
2. **事务一键沉淀与套件脱机回归调度 (`internal/server/diff/scheduler.go` & `handlers/golden_suite_handler.go`)**：
   - 实现 `POST /api/golden-suites/curate`（支持从 `/debug` 勾选单笔或连带前置依赖事务链，校验 Anti-IDOR 与单套件同属同一 `ArchProcess`）；
   - 实现套件台账 CRUD（`GET/PUT/DELETE /api/golden-suites`、用例排序与 `mask_rules_json` 更新）；
   - 升级 `ReplayScheduler` 支持 `ExecuteGoldenSuite(ctx, suiteID, envAgentID, triggerSource)`：直接从 `GoldenSuiteItem` 快照构建 `IngressItems` 与 `ScopedEgressMocks`，合并套件 `ExtraIgnorePaths` 与用例 `MaskRulesJSON` 执行双层 Diff，并联动刷新 `F-24` 大盘回归子分。
3. **前端固化抽屉与 `/golden-suites` 工作台 (`frontend/src/`)**：
   - `menu.ts` 正式挂载第 10 项 `黄金用例库` (`/golden-suites`)，完成 4 组 10 项完整拼图；
   - 在 `/debug` 事务调用树操作栏新增 `[🌟 沉淀为黄金契约]` 680px 抽屉（支持加入既有套件或新建套件）；
   - 新建 `GoldenSuitesWorkbenchPage`（标准 5 页滑动窗口 URL 分页 `15/25/50/100` 默认 `25`、套件详情与掩码配置抽屉、选定目标环境 Agent 一键回归与报告跳转）。

---

## 2. 交付包与先行测试清单 (TDD Red -> Green)

| 分层 | 目标代码文件 | 先行测试文件与核心用例 |
| :--- | :--- | :--- |
| **数据模型与双源调度** | `models/golden_suite.go`<br>`models/replay.go`<br>`models/db.go`<br>`internal/server/diff/scheduler.go`<br>`handlers/golden_suite_handler.go` | `models/golden_suite_test.go` & `handlers/golden_suite_handler_test.go`：<br>- `TestGoldenSuiteSelfContainedSnapshot_SurvivesSourceSessionAndPartitionDrop`（物理删除源 Session 与分区表后，套件回放依然 100% `PASS`）<br>- `TestCurateTransaction_AntiIDOR_AndMaskRulesDiffIgnore`<br>- `TestDeleteArchProcess_BlockedWhenGoldenSuiteExists409` |
| **前端工作台与抽屉** | `frontend/src/menu.ts`<br>`frontend/src/pages/GoldenSuitesWorkbenchPage/`<br>`frontend/src/pages/TrafficDebugWorkbenchPage/GoldenCurateDrawer.tsx` | `frontend/src/__tests__/GoldenSuiteWorkbench.test.tsx`：<br>- `testCurateFromDebugTree_AndExecuteGoldenSuiteReplay` |

---

## 3. 准出质量门禁命令 (Exit Gate)

```bash
TEST_DB_DSN="host=127.0.0.1 port=5432 user=postgres password=PWD dbname=postgres sslmode=disable" make test
make lint && make build && make clean
```

