# M14 作战卡：平台级契约治理全景大盘、三维封顶健康指数与信息架构升维

- **里程碑编号**：`M14`
- **状态**：✅ 已交付 (Completed)
- **前置依赖**：`M1 ~ M13` 已交付
- **关联特性**：`F-15`、`F-24`
- **关联设计规范**：[`14-governance-dashboard-golden-suite-and-ci-gate-adr.md`](../05-adr/14-governance-dashboard-golden-suite-and-ci-gate-adr.md) (ADR-14 v1.1.0)、[`01-feature-matrix.md`](../01-features/01-feature-matrix.md) (v2.12.1)、[`01-http-api-contract.md`](../03-api/01-http-api-contract.md) (v1.4.0)

---

## 1. 里程碑目标与 TDD 实施要点

1. **分步无死链信息架构升维 (`menu.ts` & `App.tsx` & `StandaloneShell.tsx`)**：
   - 将侧边栏菜单升级为 **4 组 9 项**（第 10 项 `/golden-suites` 留待 M15 随黄金套件功能同步挂载，严禁在 M14 暴露空壳页）：
     - **全局概览**：`治理大盘` (`/dashboard`，设为 `DEFAULT_ENTRY_PATH = '/traefik/dashboard'` 与 `DEFAULT_STANDALONE_PATH = '/dashboard'`)；
     - **契约核验与回归**：`契约采集` (`/record`)、`事务分析` (`/debug`)、`防劣化回归` (`/replay`)；
     - **架构与契约**：`架构拓扑` (`/topology`)、`架构元素` (`/architecture`)、`Proto 契约` (`/schemas`)；
     - **环境与治理**：`Agent 与进程` (`/agents`)、`模块参数配置` (`/admin/settings`, `adminOnly`)；
   - 保持 `/traffic`、`/architecture?tab=*` 等历史深链 100% 无损带参重定向。
2. **公私域严格隔离与三维封顶健康分引擎 (`internal/server/dashboard/` & `handlers/dashboard_handler.go`)**：
   - 实现 `GET /api/dashboard/overview?subsystem_id=` 聚合接口（零 N+1 参数化查询，不扫描 `traefik_traffic_records` `bytea` 大字段）；
   - **统计防污染铁律**：仅统计 `scope = 'SHARED'` 公共录制核验报告与权威回归任务（`trigger_source IN ('GOLDEN_SUITE', 'CI_GATE')`，M14 阶段若尚无套件任务则回归子分默认满分 `30`），严禁将研发个人 `PRIVATE` 素材或 `/debug` 单包临时重放混入全局扣分；
   - **三支柱子分封顶算法**：
     - 架构防漂移子分 $S_{\text{arch}} = \max(0, 40 - \text{ActiveViolationEdges} \times 10)$（取各标准进程最近一次有效公共核验的去重违规边 `(src_module_id, service_name)`）；
     - 契约资产认领子分 $S_{\text{asset}} = \text{round}(30 \times \text{ClaimedActiveServices} / \text{TotalActiveServices})$（排除 `0` Service `BASE_LIB`，分母为 `0` 时计 `30` 分）；
     - 防劣化回归子分 $S_{\text{reg}} = \max(0, 30 - \text{UnhealthySuiteProcesses}_{7\text{d}} \times 10)$；
     - 总分 $S = S_{\text{arch}} + S_{\text{asset}} + S_{\text{reg}} \in [0, 100]$，评级 `EXCELLENT (90~100)` / `GOOD (75~89)` / `SUBHEALTH (60~74)` / `CRITICAL (<60)`。
3. **全景治理大盘前端工作台 (`frontend/src/pages/DashboardWorkbenchPage/`)**：
   - 100% 使用 `--color-*` 语义 Design Tokens（深浅双模无缝适配）与扁平化 BEM 类名；
   - 渲染子系统筛选器及四大核心视窗（健康指数与整改建议卡、架构漂移雷达 TOP 5 穿透 `/topology`、孤儿契约清零看板直达 `/schemas?tab=catalog&claimed_status=UNCLAIMED`、防劣化回归防线）。

---

## 2. 交付包与先行测试清单 (TDD Red -> Green)

| 分层 | 目标代码文件 | 先行测试文件与核心用例 |
| :--- | :--- | :--- |
| **后端领域与 API** | `handlers/dashboard_handler.go`<br>`handlers/routes.go` | `handlers/dashboard_handler_test.go` (`testdb.SetupIsolatedDB`)：<br>- `TestGetDashboardOverview_PerfectScore100_AndEmptyState`<br>- `TestGetDashboardOverview_SubScoreCaps_30OrphansOnlyCapsAssetSubScore`<br>- `TestGetDashboardOverview_ExcludesPrivateSessionsAndSingleDebugReplays`<br>- `TestGetDashboardOverview_DeduplicatesViolationEdges_AndSubsystemFilter` |
| **前端菜单与大盘** | `frontend/src/menu.ts`<br>`frontend/src/App.tsx`<br>`frontend/src/types/dashboard.ts`<br>`frontend/src/api/dashboard.ts`<br>`frontend/src/pages/DashboardWorkbenchPage/` | `frontend/src/__tests__/MenuAndLegacyRoutes.test.tsx`（更新 4 组 9 项与 `/dashboard` 默认落地断言）<br>`frontend/src/pages/DashboardWorkbenchPage/__tests__/DashboardWorkbench.test.tsx`：<br>- `testDashboard_RendersHealthSubScores_DriftRadar_AndOrphanDeepLinks` |

---

## 3. 准出质量门禁命令 (Exit Gate)

```bash
TEST_DB_DSN="host=127.0.0.1 port=5432 user=postgres password=PWD dbname=postgres sslmode=disable" make test
make lint && make build && make clean
```

