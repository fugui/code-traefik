# Milestone 6 作战卡：分层复合架构拓扑与一致性叠加画布 (M6)

- **文档编号**：`07-m6-hierarchical-topology-canvas.md`
- **文档版本**：v1.1.0
- **更新日期**：2026-10-31
- **归属阶段**：阶段三/四：M6 TDD 任务拆解与验收标准 (`04-tasks`)
- **覆盖特性与场景**：`F-04` (分层复合拓扑视图)、`F-05` (架构一致性比对叠加层)、`SC-02` (架构拓扑推导与宏观一致性)
- **设计参考基线**：[`05-hierarchical-topology-canvas-redesign.md`](../05-adr/05-hierarchical-topology-canvas-redesign.md) (ADR-05)、[`03-architecture-elements-and-topology.md` §4~§5](../02-design/03-architecture-elements-and-topology.md)、[`04-control-plane-and-web-console.md` §4](../02-design/04-control-plane-and-web-console.md)、[`01-http-api-contract.md` §2.2](../03-api/01-http-api-contract.md)

---

## 1. 渐进式四阶段实施路线 (Phased Execution Plan)

为规避大规模图形引擎一次性堆砌导致的联调风险，M6 严格划分为 **4 个串行可独立编译测试的子阶段（`M6-P1` ~ `M6-P4`）**，前序阶段门禁 100% 通过后方可进入下一阶段：

| 阶段编号 | 阶段目标 | 核心实施动作与关键技术红线 | 阶段准出断言 |
| :---: | :--- | :--- | :--- |
| **M6-P1** | **后端 Graph API 与强 `ETag` 契约** | 1. 实现 `BuildTopologyGraph`：顶层直接从 `depth=0`（`SUBSYSTEM` / `PROCESS_GROUP` / `UNDEPLOYED_CONTAINER` / `EXTERNAL_CONTAINER`）起步，不下发冗余外层 `SYSTEM` 框；<br>2. 物理视图彻底剔除 `AGENT` 节点与 `meta.agent_id`，内部结合 `Process.AdvertiseIP` 与所属 `SHARED Agent.Endpoint` 提取完整 `EffectiveIP:ListenPort`，同时兼容 `EXTERNAL_SYSTEM` 与 `EXTERNAL_ENDPOINT`；<br>3. 对规范化 `nodes + edges` 计算 `SHA-256` 强内容指纹作为 `ModelVersion` 与 HTTP `ETag`（100% 感知 `ModuleDependency`/`ProcessModule` 物理硬删除），注册 `GET /api/arch/topology/graph` 并保留 `/api/arch/topology/static` 向后兼容。 | `TEST_DB_DSN=... go test -race ./internal/server/topology/... ./handlers/...` 全绿 |
| **M6-P2** | **前端 Vitest 基建与纯函数模型库** | 1. 先经依赖安全扫描校验后，在 `frontend/package.json` 引入 `@xyflow/react`、`elkjs`、`vitest`、`@vitest/coverage-v8`，配置 `"test": "vitest run"` 并为既有 `src/__tests__/*.test.tsx` 补齐 `describe/it` 兼容包装，同步接入 `Makefile`；<br>2. 实现 `hierarchy.ts`（树索引、祖先路径、最近公共祖先 `findLCA`）、`rollup.ts`（`rep()` 查找、同容器内部依赖/进程内调用角标折叠、动态 `VIOLATION` 叶子边合成、`UNATTRIBUTED` 双视图归属、最差状态收敛）与 `metrics.ts`（Tarjan SCC 环检测）。 | `cd frontend && npm test -- ArchCanvasRollup.test.ts --coverage` 覆盖率 `>= 90%` 且既有测试全绿 |
| **M6-P3** | **ELK Inline Worker 与 React Flow v12 画布底座** | 1. 在 `frontend/src/api/arch.ts` 实现带内存 `(view -> { etag, data })` 缓存并显式处理 `status === 304` 的 `getTopologyGraph` 请求器；<br>2. 实现 `elk.worker.ts`（`?worker&inline` 兼容 Module Federation 与 CSP `blob:`）及 `useElkLayout.ts`（支持 Node 单测同步回退、强制父节点先于子节点拓扑序、**跨复合容器 `bendPoints` 累加所属 `containingNode` 全局坐标 `(absX, absY)`**、展开/折叠视口锚点保持、手动拖拽节点后关联边自动退化为 `getSmoothStepPath`）；<br>3. 实现 `ArchCanvas`、嵌套容器/模块/外部节点与 `RollupEdge` 组件，严格限定 `.code-arch-canvas` 作用域与 100% `--color-*` Token。 | `cd frontend && npm test && npm run lint` 零警告通过 |
| **M6-P4** | **抽屉联动、一致性叠加层与全链路收尾** | 1. 将 `PacketAnatomyDrawer` 抽离提升至公共目录 `frontend/src/components/PacketAnatomyDrawer/` 并更新 `TrafficMonitorWorkbenchPage` 引用；<br>2. 实现工具栏（逻辑/物理切换、`L0~Ln` 层级滑杆、滚轮自动展开可选开关、重置布局）、全图搜索定位、节点画像抽屉、`EdgeDetailDrawer` 及 `ArchTopologyTabPane`（录制批次 `[SHARED]/[PRIVATE]` 选择器、顶部六分类摘要栏、点击采样记录唤起四段报文解剖）；<br>3. 重构 `ArchitectureWorkbenchPage`：使用 `React.lazy()` 异步分包加载 `ArchTopologyTabPane`，兼容旧书签 `tab=logical\|physical\|verify` 平滑映射，彻底删除废弃的 `StaticTopologyCanvas.tsx` 与 `ConsistencyVerifyOverlay.tsx`。 | `make test && make lint && make build && make clean` 100% 零警告通过 |

---

## 2. 交付包与文件清单 (Deliverable Packages & Files)

| 阶段 | 操作类型 | 包 / 文件路径 | 职责与实现范围 |
| :---: | :---: | :--- | :--- |
| **P1** | **NEW** | `internal/server/topology/graph_topology.go`<br>`internal/server/topology/graph_topology_test.go` | 实现 `BuildTopologyGraph`（逻辑/去 Agent 物理双视图、`EffectiveIP` 提取、孤立模块与双类外部节点归集、`SHA-256` 强内容指纹 `ModelVersion`）及隔离数据库单测 |
| **P1** | **UPDATE** | `handlers/arch_handler.go`<br>`handlers/routes.go`<br>`handlers/arch_proto_agent_test.go` | 注册并实现 `GET /api/arch/topology/graph`，校验 `view` 参数，输出 `ETag` + `Cache-Control: private, no-cache` 并处理 `If-None-Match` -> `304`；保留 `/api/arch/topology/static` |
| **P2** | **UPDATE** | `frontend/package.json`<br>`frontend/vite.config.ts`<br>`Makefile`<br>`frontend/src/__tests__/*.test.tsx` | 安装经安全校验的 `@xyflow/react`、`elkjs`、`vitest`、`@vitest/coverage-v8`；配置 Vitest 运行脚本并兼容包装既有 3 个前端组件断言套件；将前端单测纳入工程验收脚本 |
| **P2** | **NEW** | `frontend/src/types/topology_graph.ts` | 声明 `GraphNode`、`GraphEdge`、`TopologyGraphDTO`、`RollupEdgeModel`、`ExpandStateMap` 等强类型接口（零 `any`） |
| **P2** | **NEW** | `frontend/src/components/ArchCanvas/model/hierarchy.ts`<br>`frontend/src/components/ArchCanvas/model/rollup.ts`<br>`frontend/src/components/ArchCanvas/model/metrics.ts` | 纯函数核心模型库：树构建与 `findLCA`、`rep(nodeId)` 最近可见祖先查找、静态边汇总、动态 `VIOLATION` 告警边合成（含 `VIRTUAL_UNREGISTERED_INTERFACE`）、`UNATTRIBUTED` 跨视图归属、最差状态优先级收敛与 Tarjan SCC 环路检测 |
| **P2** | **NEW** | `frontend/src/__tests__/ArchCanvasRollup.test.ts` | 针对 `hierarchy.ts`、`rollup.ts`、`metrics.ts`、动态违规边叠加及坐标转换纯函数的完整单元测试套件 |
| **P3** | **UPDATE** | `frontend/src/api/arch.ts` | 新增 `getTopologyGraph(view)`，内置按视角隔离的 `{ etag, data }` 内存缓存，携带 `If-None-Match` 并正确处理 `304 Not Modified` |
| **P3** | **NEW** | `frontend/src/components/ArchCanvas/layout/elk.worker.ts`<br>`frontend/src/components/ArchCanvas/layout/useElkLayout.ts` | 基于 `?worker&inline` 的 ELK 复合正交布局管道（含单测环境同步回退）；实现父先于子拓扑排序、跨层 `bendPoints` 全局坐标累加换算、展开/折叠视口锚点补偿与手动拖拽 `SmoothStep` 退化 |
| **P3** | **NEW** | `frontend/src/components/ArchCanvas/nodes/*`<br>`frontend/src/components/ArchCanvas/edges/RollupEdge.tsx`<br>`frontend/src/components/ArchCanvas/ArchCanvas.tsx`<br>`frontend/src/components/ArchCanvas/ArchCanvas.css` | 矩形嵌套容器节点（双击折叠/展开、内部调用角标）、模块叶子卡片（未部署虚线告警）、外部/未登记节点、正交折线汇总边（对数线宽、六分类着色、脉冲动效）、React Flow v12 画布外壳与亮/暗双模 `--color-*` 样式隔离 |
| **P4** | **MOVE / UPDATE** | `frontend/src/components/PacketAnatomyDrawer/*`<br>`frontend/src/pages/TrafficMonitorWorkbenchPage/TrafficMonitorWorkbenchPage.tsx` | 将四段二进制报文解剖抽屉提升为公共组件，供流量监控台与拓扑边明细抽屉共同复用（更新对应引用与单测导入路径） |
| **P4** | **NEW** | `frontend/src/components/ArchCanvas/panels/*`<br>`frontend/src/pages/ArchitectureWorkbenchPage/ArchTopologyTabPane.tsx` | 工具栏、全图搜索框、节点检视抽屉、`EdgeDetailDrawer`（穿透唤起公共 `PacketAnatomyDrawer`）及拓扑宿主面板（集成录制批次下拉叠加与顶部六分类状态摘要栏） |
| **P4** | **UPDATE** | `frontend/src/pages/ArchitectureWorkbenchPage/ArchitectureWorkbenchPage.tsx` | 收敛顶层 Tab 为 `[架构建模与模块契约]` (`modeling`) 与 `[静态拓扑与一致性画布]` (`topology`)，采用 `React.lazy` + `<Suspense>` 异步加载 `ArchTopologyTabPane`，兼容旧 `tab=logical\|physical\|verify` URL 参数并同步 `view`、`depth`、`session_id` |
| **P4** | **DELETE** | `frontend/src/pages/ArchitectureWorkbenchPage/StaticTopologyCanvas.tsx`<br>`frontend/src/pages/ArchitectureWorkbenchPage/ConsistencyVerifyOverlay.tsx` | 彻底移除已被 `ArchCanvas` 与一致性叠加层替代的旧版圆环 SVG 画布及独立表格验证组件 |

---

## 3. TDD 先行测试矩阵 (Test-First Matrix)

| 阶段 | 目标测试文件 | 测试函数名 | 输入场景与前置条件 | 预期断言结果 (Assertions) |
| :---: | :--- | :--- | :--- | :--- |
| **P1** | `internal/server/topology/graph_topology_test.go` | `TestBuildTopologyGraph_Logical` | 构造 2 个子系统、3 层功能组、4 个模块及跨子系统/同组/外部依赖契约 | 1. 顶层节点 `depth == 0` 直接为 `SUBSYSTEM` 与 `EXTERNAL_CONTAINER`（无外层 `SYSTEM` 框）；<br>2. `parent_id` 与 `ancestors` 严格精确；<br>3. `edges` 完整聚合 `services[]`，零 `AGENT` 节点。 |
| **P1** | `internal/server/topology/graph_topology_test.go` | `TestBuildTopologyGraph_Physical_NoAgentAndEffectiveIP` | 构造 `SHARED` Agent (`192.168.56.20:8090`)、留空 `AdvertiseIP` 的进程、显式 `AdvertiseIP` 的进程、孤立模块及 `ExtraDownstreams` | 1. 物理图节点仅含 `PROCESS_GROUP`、`PROCESS`、`MODULE`、`UNDEPLOYED_CONTAINER`、`EXTERNAL_*`，**绝对不存在 `AGENT` 节点且 `meta` 不含 `agent_id`**；<br>2. 留空 `AdvertiseIP` 的进程正确回退推导为 `"192.168.56.20:<port>"`；<br>3. 未绑定公共进程的孤立模块归入 `UNDEPLOYED_CONTAINER`。 |
| **P1** | `handlers/arch_proto_agent_test.go` | `TestGetTopologyGraph_ETag304AndHardDeleteInvalidation` | 1. 连续两次请求 `/api/arch/topology/graph`（携带 `If-None-Match`）；<br>2. 硬删除一条 `ModuleDependency` 后再次携带旧 `ETag` 请求 | 1. 首次返回 `200 OK` 及非空 `ETag`；未变更时第二次返回 `304 Not Modified`；<br>2. 硬删除依赖边后携带旧 `ETag` 请求**必须返回 `200 OK` 与新 `ETag`**（严禁误判为 `304`），响应边数精确减 1。 |
| **P2** | `frontend/src/__tests__/ArchCanvasRollup.test.ts` | `testRollup_AllCollapsedAndInternalBadge` | 顶层容器全部折叠（`L0` 状态），传入跨子系统边、同子系统内部边与同进程 `INTRA_PROCESS` 边 | 1. 跨容器边精确上卷至顶层子系统/进程组之间；<br>2. 同容器边不产生自环连线，精确累加至容器 `internalEdgeCount` / `intraProcessCallCount` 角标；<br>3. 汇总边完整保留底层全部 `services` 明细。 |
| **P2** | `frontend/src/__tests__/ArchCanvasRollup.test.ts` | `testRollup_AsymmetricDeepExpanded` | 展开子系统 A 至 `Module` 叶子层，对端子系统 B 保持 `L0` 折叠 | 展开侧模块叶子直接连向对端折叠的子系统容器（`rep()` 命中最近可见折叠祖先），无断线或悬空节点引用。 |
| **P2** | `frontend/src/__tests__/ArchCanvasRollup.test.ts` | `testOverlay_WorstStatusAndDynamicViolationUnattributed` | 叠加含静态 `VERIFIED`、未在静态图声明的 `VIOLATION`（含指向未登记接口）及 `src_module_id=null` 的 `UNATTRIBUTED` 报告 | 1. 同一汇总边下 `VERIFIED + VIOLATION` 严格收敛为 `VIOLATION`；<br>2. 未声明 `VIOLATION` 动态生成告警边并按需注入 `VIRTUAL_UNREGISTERED_INTERFACE`；<br>3. `UNATTRIBUTED` 在物理视图挂接 `proc_<id>`，在逻辑视图准确挂载至 `candidate_module_ids` 的 `LCA` 祖先节点（跨子系统时归入顶栏待定列表），零空指针异常。 |
| **P2** | `frontend/src/__tests__/ArchCanvasRollup.test.ts` | `testTarjanCycleDetection` | 构造 `A -> B -> C -> A` 三元循环依赖与单向无环分支 `A -> D` | `detectCycles` 精确标记环内 3 条有向边 `isCycle = true`，无环分支 `A -> D` 保持 `isCycle = false`。 |
| **P3** | `frontend/src/__tests__/ArchCanvasRollup.test.ts` | `testLayout_ParentFirstOrderAndCompoundBendPoints` | 构造展开的两级嵌套容器（含偏移坐标）及一条容器内跨组折线边，执行布局坐标后处理与节点拖拽标记 | 1. 输出 `nodes` 数组严格满足每个 `parentId` 节点索引小于其所有子节点索引；<br>2. 跨组边的 `bendPoints` 精确加上所属 `containingNode` 的全局绝对偏移 `(absX, absY)`；<br>3. 标记关联节点被手动拖拽后，边渲染模式自动切换为动态 `smoothstep`。 |

---

## 4. 验收门禁命令 (Acceptance Criteria)

```bash
# AC-M6-01: 前端纯算法、动态叠加与复合坐标换算单测覆盖率门禁 (>= 90%) 及全量前端单测
cd frontend && npm test -- --coverage

# AC-M6-02: 物理图零 Agent 泄露、EffectiveIP 回退与硬删除强 ETag 失效后端单测
TEST_DB_DSN="host=127.0.0.1 port=5432 user=postgres password=postgres dbname=postgres sslmode=disable" \
  go test -v -race ./internal/server/topology/... ./handlers/... -run "TestBuildTopologyGraph|TestGetTopologyGraph"

# AC-M6-03: 前端 TypeScript 严格类型与 ESLint 检查（零 any、零废弃死代码、100% --color-* Token 合规）
make lint

# AC-M6-04: 全局全量隔离单元测试、双二进制内嵌构建与产物清理门禁
TEST_DB_DSN="host=127.0.0.1 port=5432 user=postgres password=postgres dbname=postgres sslmode=disable" make test && make build && make clean
```
