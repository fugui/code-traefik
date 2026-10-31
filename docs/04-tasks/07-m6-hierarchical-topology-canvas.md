# Milestone 6 作战卡：分层复合架构拓扑与一致性叠加画布 (M6)

- **文档编号**：`07-m6-hierarchical-topology-canvas.md`
- **文档版本**：v1.0.0
- **更新日期**：2026-10-31
- **归属阶段**：阶段三/四：M6 TDD 任务拆解与验收标准 (`04-tasks`)
- **覆盖特性与场景**：`F-04` (分层复合拓扑视图)、`F-05` (架构一致性比对叠加层)、`SC-02` (架构拓扑推导与宏观一致性)
- **设计参考基线**：[`05-hierarchical-topology-canvas-and-elk-layout.md`](../05-adr/05-hierarchical-topology-canvas-and-elk-layout.md) (ADR-05)、[`03-architecture-elements-and-topology.md` §4~§5](../02-design/03-architecture-elements-and-topology.md)、[`04-control-plane-and-web-console.md` §4](../02-design/04-control-plane-and-web-console.md)、[`01-http-api-contract.md` §2.2](../03-api/01-http-api-contract.md)

---

## 1. 交付包与文件清单 (Deliverable Packages & Files)

| 操作类型 | 包 / 文件路径 | 职责与实现范围（组件分层与 DTO 契约见设计文档链接） |
| :---: | :--- | :--- |
| **NEW** | `internal/server/topology/graph_topology.go`<br>`internal/server/topology/graph_topology_test.go` | 实现 `BuildTopologyGraph`，输出全系统层级树 (`nodes`) 与原子叶子边 (`edges`)，严格在物理视图下去除 `AGENT` 节点（收敛为 `PROCESS_GROUP` -> `PROCESS` -> `MODULE`）；计算 `ModelVersion` 并支持 HTTP `ETag` / `304` |
| **UPDATE** | `handlers/arch_handler.go`<br>`handlers/routes.go`<br>`handlers/arch_proto_agent_test.go` | 注册并实现 `GET /api/arch/topology/graph` 控制器，校验 `view` 参数，注入 ETag 头；保留旧 `/api/arch/topology/static` 向后兼容 |
| **NEW** | `frontend/src/types/topology_graph.ts` | 声明 `GraphNode`、`GraphEdge`、`TopologyGraphDTO`、`ExpandStateMap` 等 TypeScript 强类型（零 `any`） |
| **NEW** | `frontend/src/components/ArchCanvas/model/hierarchy.ts`<br>`frontend/src/components/ArchCanvas/model/rollup.ts`<br>`frontend/src/components/ArchCanvas/model/metrics.ts` | 纯前端核心算法库：`rep(nodeId)` 最近可见代表节点查找、叶子依赖边汇总、`INTRA_PROCESS` 进程内调用角标折叠、Tarjan 强连通分量 (SCC) 环路标红检测 |
| **NEW** | `frontend/src/components/ArchCanvas/layout/elk.worker.ts`<br>`frontend/src/components/ArchCanvas/layout/useElkLayout.ts` | 基于 `elkjs/lib/elk-worker` 的 Web Worker 异步正交分层复合排版管道；实现视口中心锚点保持算法（避免展开时全图跳动） |
| **NEW** | `frontend/src/components/ArchCanvas/nodes/*`<br>`frontend/src/components/ArchCanvas/edges/*`<br>`frontend/src/components/ArchCanvas/panels/*` | 1. 矩形嵌套容器节点组件（子系统/功能组/进程组/进程，支持双击下钻与折叠按钮）；<br>2. 模块叶子卡片（未装载虚线告警）；<br>3. 正交折线汇总边（线宽动态对数缩放、六分类状态着色、告警脉冲）；<br>4. 工具栏（逻辑/物理切换、`L0`~`Ln` 层级滑杆、语义自动展开复选开关、重置布局）、搜索框、节点检视抽屉、边明细抽屉 |
| **NEW** | `frontend/src/components/ArchCanvas/ArchCanvas.tsx`<br>`frontend/src/components/ArchCanvas/ArchCanvas.css` | React Flow 画布主体（平移、缩放、多选、小地图、LOD 视口类名）；100% 采用 `--color-*` 语义 Token 与 Flat BEM 类名 |
| **NEW** | `frontend/src/pages/ArchitectureWorkbenchPage/ArchTopologyTabPane.tsx` | 拓扑工作台宿主面板：集成逻辑/物理适配器，顶部提供录制批次下拉选择器，一键挂载 `ConsistencyReportDTO` 实施六分类一致性结果叠加与采样四段报文联动 |
| **UPDATE** | `frontend/src/pages/ArchitectureWorkbenchPage/ArchitectureWorkbenchPage.tsx` | 重构工作台二级 Tab 为 `[架构建模与模块契约]` 与 `[静态拓扑与一致性画布]`，废弃独立的割裂验证子页签，URL 状态与 `view`、`depth`、`session_id` 联动 |
| **NEW** | `frontend/src/__tests__/ArchCanvasRollup.test.ts` | 针对 `hierarchy.ts`、`rollup.ts`、`metrics.ts` 及一致性最差状态收敛算法的纯函数 100% 单元测试套件 |

---

## 2. TDD 先行测试矩阵 (Test-First Matrix)

| 目标测试文件 | 测试函数名 | 输入场景与前置条件 | 预期断言结果 (Assertions) |
| :--- | :--- | :--- | :--- |
| `internal/server/topology/graph_topology_test.go` | `TestBuildTopologyGraph_Logical` | 构造 2 个子系统、3 层功能组、4 个模块及 3 条跨子系统/同组/外部依赖契约 | 1. `nodes` 包含正确的包含树关系（`parent_id` 与 `ancestors` 精确符合物化路径）；<br>2. `edges` 正确包含 3 条原子叶子边与聚合契约信息；<br>3. 节点类型严禁包含 `AGENT`。 |
| `internal/server/topology/graph_topology_test.go` | `TestBuildTopologyGraph_Physical_NoAgent` | 构造 2 台 Agent、3 个不同 `group_name` 的进程与若干承载模块 | 1. 物理图节点树顶层严格为 `PROCESS_GROUP`，次层为 `PROCESS`，叶子为承载的 `MODULE`；<br>2. 全量节点列表中**绝对不存在 `NodeType == "AGENT"` 的节点**；<br>3. 进程节点 `meta` 中包含 `listen_port` 与 `advertise_ip`。 |
| `frontend/src/__tests__/ArchCanvasRollup.test.ts` | `testRollup_AllCollapsed` | 顶层子系统全部折叠（L0 状态），传入跨子系统底层模块依赖边 | 1. 所有边被精确上卷至所属子系统节点之间；<br>2. 同一子系统内部的依赖不生成跨容器边，子系统节点角标累加 `internalCount`；<br>3. 汇总边明细包含底层全部 Service 契约。 |
| `frontend/src/__tests__/ArchCanvasRollup.test.ts` | `testRollup_DeepExpanded` | 展开某子系统至模块叶子层，对端子系统保持折叠 | 1. 展开方的模块直接连接至对端折叠的子系统容器节点（即对端 `rep()` 命中最近可见祖先）；<br>2. 连线无脱落、无多余野指针。 |
| `frontend/src/__tests__/ArchCanvasRollup.test.ts` | `testConsistencyStatusCollapse` | 某两条底层边状态分别为 `VERIFIED` 与 `VIOLATION`，汇总至同一抽象边 | 汇总边最终状态严格收敛为 `VIOLATION`（最差状态优先级第一位）。 |
| `frontend/src/__tests__/ArchCanvasRollup.test.ts` | `testTarjanCycleDetection` | 构造 A 依赖 B，B 依赖 C，C 依赖 A 的三元循环契约 | `detectCycles` 正确检出强连通分量 `[A, B, C]`，对应的汇总边标记 `isCycle = true`。 |
| `handlers/arch_proto_agent_test.go` | `TestGetTopologyGraph_ETagAnd304` | 连续请求 `/api/arch/topology/graph`，第二次携带初次返回的 `ETag` | 1. 首次返回 `200 OK` 伴随有效 `ETag` 响应头；<br>2. 第二次请求返回 `304 Not Modified`，响应体为空，节省网络开销。 |

---

## 3. 验收门禁命令 (Acceptance Criteria)

```bash
# AC-M6-01: 纯算法与汇总纯函数单测覆盖率门禁 (>= 90%)
cd frontend && npm test -- ArchCanvasRollup.test.ts --coverage

# AC-M6-02: 物理图零 Agent 节点断言与后端接口单测
TEST_DB_DSN="host=127.0.0.1 port=5432 user=postgres password=postgres dbname=postgres sslmode=disable" \
  go test -v -race ./internal/server/topology/ -run "TestBuildTopologyGraph"

# AC-M6-03: 前端静态代码规范检查（零 any、零危险代码、样式合规）
make lint

# AC-M6-04: 全局全量单元测试与打包验证
make test && make build && make clean
```
