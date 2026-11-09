# Milestone 19 (M19) 作战卡：大规模架构拓扑渐进式下钻聚焦、多维降噪与双层混合布局持久化 (`F-04.1` ~ `F-04.3`, `ADR-15`)

- **文档编号**：`20-m19-scalable-topology-focus-and-layout-persistence.md`
- **归属阶段**：阶段三/四：TDD 任务拆解与分阶段交付 (`04-tasks`)
- **对应里程碑**：**M19 — 大规模架构拓扑渐进式下钻聚焦、多维降噪与双层混合布局持久化**
- **前置依赖**：M6（`ArchCanvas` 分层复合拓扑底座）、M10（零 Agent 物理进程蓝图拓扑）、M18（业务视角报文透视）
- **覆盖特性**：`F-04`、`F-04.1`（C4 渐进式探索、作用域下钻聚焦与大节点展开防卡死保护）、`F-04.2`（多维过滤、搜索飞行定位、1-Hop 聚光灯、Hub 匯流折叠与语义缩放 LOD）、`F-04.3`（混合增量布局引擎、镜头保持与双层布局持久化）
- **上位设计基线**：
  - [`01-feature-matrix.md`](../01-features/01-feature-matrix.md) (v2.14.0)
  - [`03-architecture-elements-and-topology.md` §6](../02-design/03-architecture-elements-and-topology.md) (v1.5.0)
  - [`05-database-schema.md` §3.7](../02-design/05-database-schema.md) (v1.5.0, 第 18 张业务表 `traefik_topology_layouts`)
  - [`01-http-api-contract.md` §2.2](../03-api/01-http-api-contract.md) (v1.5.0)
  - [`15-scalable-topology-focus-and-layout-persistence-adr.md`](../05-adr/15-scalable-topology-focus-and-layout-persistence-adr.md) (ADR-15 v1.0.0)
- **状态**：已规划待实施 (Planned — SDD+TDD 规范冻结)

---

## 1. 里程碑目标与四阶段交付拆解 (Phased Execution Plan)

针对生产级 **~25 个子系统 × 10 个功能组 × 10 个功能模块（约 2,775 个图节点 + 5,000~10,000 条底层服务契约边）** 的超大规模拓扑场景，彻底解决「全量展开巨图卡顿、蜘蛛网连线过载、拖拽后局部展开/折叠强制触发 ELK 全图重排与 `fitView()` 镜头重置、布局无法跨会话保存与团队共享」四大核心痛点。

本里程碑严格按 **4 个递进子阶段 (`M19-P1` ~ `M19-P4`)** 实施，遵循 **Red ➔ Green ➔ Refactor** 循环：

```mermaid
flowchart LR
    P1["M19-P1: 后端模型与双层布局 API<br/>(TopologyLayout 第18表 + 6个接口 + Anti-IDOR)"]
    P2["M19-P2: 纯算法层 (子图/过滤/Hub/混合布局)<br/>(subgraph.ts + layoutMerger.ts + Vitest >=90%)"]
    P3["M19-P3: 画布核心交互与渲染升级<br/>(镜头保持 + 钉住Pin + 1-Hop聚光灯 + LOD)"]
    P4["M19-P4: 顶栏工作流与双层持久化闭环<br/>(下钻面包屑 + 多维过滤 + 搜索飞跃 + 视图书签抽屉)"]

    P1 ==> P2 ==> P3 ==> P4
```

### 1.1 交付物文件清单 (Deliverables Inventory)

| 层级 | 文件路径 | 变更类型 | 核心职责说明 |
| :--- | :--- | :---: | :--- |
| **后端模型** | `models/arch.go` | 修改 | 新增第 18 张业务表模型 `TopologyLayout` (`traefik_topology_layouts`) 及 JSON 载荷辅助结构 (`SavedNodePosition`, `SavedExpandState`, `SavedFilterState`, `SavedViewport`) |
| **后端迁移** | `models/db.go` | 修改 | 注册 `TopologyLayout` 自动迁移，幂等创建部分唯一索引 `uk_traefik_topo_layout_shared` 与 `uk_traefik_topo_layout_private_name` |
| **后端接口** | `handlers/arch_handler.go` | 修改 | 新增 6 个拓扑布局与个人视图书签接口（`ListTopologyLayouts`, `UpsertCanonicalTopologyLayout`, `ResetCanonicalTopologyLayout`, `CreatePersonalTopologyLayout`, `UpdatePersonalTopologyLayout`, `DeletePersonalTopologyLayout`），接入 `ScopeReadGuard`/`ScopeWriteGuard`/`AdminOnly` 与 `audit` 审计 |
| **后端路由** | `routes/routes.go` | 修改 | 在 `/api/arch/topology/layouts*` 挂载上述 6 个路由 |
| **后端测试** | `handlers/topology_layout_test.go` | **新增** | 覆盖 `SHARED` 基准布局幂等发布/重置、普通用户 `403` 拦截、`PRIVATE` 个人书签 CRUD、20 个配额上限、跨用户 Anti-IDOR 越权拦截与 `NaN/Infinity` 非法坐标拦截 |
| **前端类型与 API** | `frontend/src/types/arch.ts`<br>`frontend/src/api/arch.ts` | 修改 | 定义 `TopologyLayoutDTO`, `TopologyFocusScope`, `TopologyFilterState`, `SavedNodePosition`, `LayoutDiffNotice` 及 6 个布局持久化 API 调用封装 |
| **前端纯函数 (子图/降噪)** | `frontend/src/components/ArchCanvas/model/subgraph.ts` | **新增** | 纯函数实现：`extractScopedSubgraph`（作用域下钻 + `1-Hop` 跨边界外部上下文卡片收敛）、`applyMultiDimensionalFilters`（子系统多选、状态过滤、`1-Hop / 2-Hop Ego-Network` 邻域裁剪、隐藏孤立节点）、`foldHighDegreeHubEdges`（出入度 `>=8` 高扇出容器总线胶囊折叠）、`checkFullExpandGuard`（`>150` 可见节点防卡死预检） |
| **前端纯函数 (混合布局)** | `frontend/src/components/ArchCanvas/layout/layoutMerger.ts` | **新增** | 纯函数实现：`mergeIncrementalLayout`（`Pinned` 节点坐标保留、未钉死节点增量排布、父容器包裹尺寸自适应撑开、已删节点静默清理）、`resolveSiblingCollisionsAABB`（同父容器下未钉死节点绕开 `Pinned` 节点的 AABB 碰撞推开算法） |
| **前端布局 Hook** | `frontend/src/components/ArchCanvas/layout/useElkLayout.ts` | 修改 | 集成 `layoutMerger.ts`，支持传入 `pinnedPositions` 与 `savedLayout`；区分「首次加载/显式重置 (`shouldFitView=true`)」与「局部展开/折叠增量重算 (`shouldFitView=false`)」 |
| **前端画布组件** | `frontend/src/components/ArchCanvas/ArchCanvas.tsx`<br>`frontend/src/components/ArchCanvas/ArchCanvas.css` | 修改 | 1. 消除局部展开/折叠时的强制 `fitView()` 跳变，保持用户当前镜头 `[x, y, zoom]`；<br>2. 节点拖拽结束自动标记 `pinned: true` 并防抖写入 `localStorage` 草稿；<br>3. 单击/悬停节点激活 `1-Hop Spotlight`（无关节点/边降透明度至 `0.15`）；<br>4. 监听 `zoom` 变化切换 `.is-lod-macro` (`zoom < 0.45`) / `.is-lod-medium` / `.is-lod-detail` (`zoom >= 0.85`) 语义缩放类名 |
| **前端画布节点** | `frontend/src/components/ArchCanvas/nodes/ContainerNode.tsx`<br>`frontend/src/components/ArchCanvas/nodes/LeafNode.tsx`<br>`frontend/src/components/ArchCanvas/nodes/ExternalContextNode.tsx` (**新增**) | 修改 / 新增 | 1. `ContainerNode` 标题栏新增 `[🔍 聚焦此域]` 下钻按钮与 `[📌 已固定]` 图钉徽章（点击可解锁恢复自动布局）；<br>2. 新增 `ExternalContextNode` 渲染下钻聚焦域的 `1-Hop` 外部上下游邻居摘要卡片（支持点击 `[↗ 切换聚焦至此子系统]`）；<br>3. 新增 Hub 匯流胶囊节点/徽章展开交互 |
| **前端工具栏与抽屉** | `frontend/src/components/ArchCanvas/panels/ArchCanvasToolbar.tsx`<br>`frontend/src/components/ArchCanvas/panels/TopologyBookmarkDrawer.tsx` (**新增**) | 修改 / 新增 | 1. 顶栏新增「下钻面包屑导航栏 (`🌐 全局总览 > 📦 子系统 > 📂 功能组`)」；<br>2. 新增 `🔍 搜索模块 / 进程 / 服务...` 模糊下拉框（选中后自动展开祖先容器 + `setCenter(..., { zoom: 1.05, duration: 500 })` 平滑飞行 + `2s` 脉冲高亮）；<br>3. 新增子系统多选、状态药丸过滤（`全部 / 仅违规 / 仅活跃 / 仅未认领`）、`[🎯 邻域: 关闭 \| 1跳 \| 2跳]`、`[👁️ 隐藏孤立节点]`；<br>4. 新增 `[💾 保存个人视图]`、`[⭐ 视图书签]` 下拉/管理抽屉与管理员专属 `[🏛️ 发布为团队基准布局]` 按钮及增量变更琥珀色提示条 |
| **前端页面集成** | `frontend/src/pages/TopologyWorkbenchPage/TopologyWorkbenchPage.tsx`<br>`frontend/src/pages/ArchitectureWorkbenchPage/ArchTopologyTabPane.tsx` | 修改 | 同步 URL Search Params（`?view=LOGICAL&focus=sub_1& subsystems=...&status=...&ego=...&layoutId=...`），打通三级布局加载优先级与浏览器前进/后退导航 |
| **前端单测** | `frontend/src/__tests__/ArchCanvasSubgraphAndHybridLayout.test.ts`<br>`frontend/src/__tests__/TopologyFocusAndPersistenceWorkbench.test.tsx` | **新增** | 纯函数单测（覆盖率 `>= 90%`）+ 画布聚焦/过滤/钉住/持久化/RBAC 交互组件单测 |

---

## 2. 各阶段技术实现规范与契约细则

### 2.1 `M19-P1`：后端 `TopologyLayout` 模型与双层布局 REST API

1. **GORM 模型与部分唯一索引 (`models/arch.go` & `models/db.go`)**：
   - 严格按 [`05-database-schema.md` §3.7](../02-design/05-database-schema.md) 定义 `TopologyLayout`，表名强制为 `config.TablePrefix + "topology_layouts"`（即 `traefik_topology_layouts`）；
   - 在 `models/db.go` 的 `ensureCustomIndexes` 中幂等执行：
     ```sql
     CREATE UNIQUE INDEX IF NOT EXISTS uk_traefik_topo_layout_shared
         ON traefik_topology_layouts (view, focus_scope)
         WHERE scope = 'SHARED';
     CREATE UNIQUE INDEX IF NOT EXISTS uk_traefik_topo_layout_private_name
         ON traefik_topology_layouts (owner_id, view, focus_scope, name)
         WHERE scope = 'PRIVATE';
     ```
2. **输入校验与安全防腐 (`handlers/arch_handler.go`)**：
   - `view` 仅允许 `"LOGICAL"` 或 `"PHYSICAL"`；
   - `focus_scope` 为空时默认规范化为 `"GLOBAL"`，长度 `<= 64`，仅允许 `^[A-Za-z0-9_:-]{1,64}$`；
   - `node_positions` 映射条目数上限为 `5000`，每个坐标 `x, y, w, h` 必须为有限浮点数（拦截 `NaN` / `±Inf`），超出或格式非法返 `400 Bad Request`；
   - **配额守卫**：单个用户在 `traefik_topology_layouts` 中 `scope = 'PRIVATE'` 的总记录数上限为 **`20` 条**，超出返 `400 Bad Request` (`code: "PERSONAL_VIEW_QUOTA_EXCEEDED"`)；
   - **默认视图互斥**：当创建或更新个人视图设置 `is_default = true` 时，在同一数据库事务内先执行 `UPDATE traefik_topology_layouts SET is_default = false WHERE scope = 'PRIVATE' AND owner_id = ? AND view = ? AND focus_scope = ? AND id != ?`。

### 2.2 `M19-P2`：前端子图裁剪、多维降噪与混合增量布局纯算法层

1. **作用域下钻子图裁剪 (`extractScopedSubgraph` in `subgraph.ts`)**：
   - 当 `focusScope === 'GLOBAL'` 时返回原图；
   - 当 `focusScope` 指定某个容器节点 `C`（如 `sub_1`、`grp_12`、`pgrp_3`）时：
     - 收集 `C` 及其所有后代节点作为**主画布内部节点集 $V_{\text{in}}$**；
     - 扫描所有底层契约边/实测边，凡是一端在 $V_{\text{in}}$、另一端在 $V_{\text{in}}$ 之外（外部节点 $u \notin V_{\text{in}}$）的跨边界边，将外部节点按其顶层容器（外部 `Subsystem`、`ExternalSystem` 或外部 `ArchProcessGroup`）聚合为 **`1-Hop` 外部上下文摘要节点 (`ExternalContextNode`)**；
     - 严禁把外部子系统的内部功能组与模块展开拉入当前聚焦画布，确保聚焦单子系统时画布总节点数恒定控制在 `20 ~ 120` 个以内。
2. **多维正交过滤与 Ego-Network (`applyMultiDimensionalFilters` in `subgraph.ts`)**：
   - 支持按 `subsystemIds: string[]`、`statusFilter: 'ALL' | 'VIOLATION_ONLY' | 'ACTIVE_ONLY' | 'UNCLAIMED_ONLY'`、`egoCenterId?: string` + `egoHops: 1 | 2`、`hideIsolated: boolean` 组合裁剪；
   - **祖先容器自动保留铁律**：任意保留的叶子节点或子容器，其祖先容器链（直至当前 `focusScope` 根）必须自动保留并重新计算折叠徽章计数，严禁产生无父容器的悬空内部节点。
3. **高扇出 Hub 匯流折叠 (`foldHighDegreeHubEdges` in `subgraph.ts`)**：
   - 在 `L0` 或 `L1` 视图下，若某个容器节点关联的可见跨容器聚合边总数 `>= 8` 且该节点未被放入 `expandedHubNodeIds` 集合，则将其跨容器边折叠为 1 个虚拟总线胶囊说明项（标注 `⚡ 公共底座匯流 (N 个上下游契约)`），点击胶囊可单独展开该 Hub 的放射连线。
4. **混合增量布局与同层碰撞推开 (`mergeIncrementalLayout` & `resolveSiblingCollisionsAABB` in `layoutMerger.ts`)**：
   - 对拥有持久化或会话内 `pinned: true` 坐标的存量节点，100% 锁定其相对父容器的 `(x, y)` 坐标；
   - 对新增节点或刚展开的容器，先由 ELK 计算基准布局，再通过 `resolveSiblingCollisionsAABB` 检测同一 `parentId` 下未钉死节点与 `Pinned` 节点的矩形包围盒（含 `24px` 安全间距）是否重叠；若重叠则沿水平/垂直最小逃逸向量平移未钉死节点；
   - 若 `savedLayout.base_model_version !== graph.model_version`，返回 `{ addedCount, removedCount }` 供顶栏渲染琥珀色温和提示条。

### 2.3 `M19-P3`：画布相机视口保持、图钉固定、1-Hop 聚光灯与语义缩放 LOD

1. **彻底根治局部展开/折叠时的镜头跳变 (`ArchCanvas.tsx`)**：
   - 将 `fitView` 触发条件收紧为仅在以下 4 种显式导航动作时执行：
     1. 首次进入拓扑页初始化加载（且加载的布局未包含有效 `viewport`）；
     2. 切换 `LOGICAL / PHYSICAL` 架构视图；
     3. 切换 `focusScope` 下钻域（进入子系统或点击面包屑返回）；
     4. 用户点击顶栏 `[重置布局]` 或切换个人书签视图。
   - 当用户在画布上点击单个容器的展开/折叠箭头，或拖拽节点后，**严禁调用 `fitView()`**，严格保留用户当前的 `[x, y, zoom]`。
2. **大节点量全展开防卡死守卫 (`> 150` 可见节点拦截)**：
   - 在 `focusScope === 'GLOBAL'` 下点击 `L2 全展`，若 `checkFullExpandGuard` 预估可见节点数 `> 150`，弹出确认提示框引导用户先下钻子系统或使用过滤，同時提供 `[仍然全量展开]` 强制逃生按钮。

### 2.4 `M19-P4`：顶栏面包屑、搜索飞跃、多维过滤栏与双层布局书签管理

1. **URL Search Params 双向同步**：
   - 将 `view` (`LOGICAL|PHYSICAL`)、`focus` (`GLOBAL|sub_1|...`)、`subsystems` (逗号分隔)、`status`、`ego` (`nodeId:hops`)、`hideIsolated`、`layoutId` 同步至 URL Search Params，支持深链分享与浏览器前进/后退。
2. **三级布局加载优先级**：
   - 优先级 1：URL 指定 `layoutId` 或用户在当前 `(view, focus_scope)` 设有 `is_default=true` 的个人书签；
   - 优先级 2：`localStorage` 中匹配的未保存会话草稿（`traefik_topo_draft_v1_${userId}_${view}_${focusScope}`）；
   - 优先级 3：服务端 `canonical_layout`（团队官方 `SHARED` 基准布局）；
   - 优先级 4：ELK `L0` 默认折叠自动布局。

---

## 3. TDD 先行测试用例矩阵 (Test-First Specification)

实施代码前，必须先编写以下单元测试与组件测试（确认 Red 失败后再编写业务实现至 Green 全绿）：

### 3.1 后端 Go 单元测试 (`handlers/topology_layout_test.go`)

| 测试函数名 | 覆盖特性 | 核心断言内容 |
| :--- | :---: | :--- |
| `TestTopologyLayout_CanonicalUpsertAndReset_RBAC` | `F-04.3`, `F-16` | 1. 普通用户调用 `PUT /api/arch/topology/layouts/canonical` 返 `403`；<br>2. `traefik_admin` 调用 `PUT` 成功创建 `(LOGICAL, GLOBAL)` 官方 `SHARED` 布局，再次调用幂等更新同一行（守护 `uk_traefik_topo_layout_shared`）并写入审计日志；<br>3. 管理员按不同 `focus_scope="sub_1"` 可独立保存子系统级官方基准布局；<br>4. `DELETE /api/arch/topology/layouts/canonical` 成功重置。 |
| `TestTopologyLayout_PersonalCRUD_DefaultMutex_AndAntiIDOR` | `F-04.3`, `F-16` | 1. 用户 A 创建 2 个同 `(LOGICAL, GLOBAL)` 个人视图书签，将第 2 个设为 `is_default=true` 时，第 1 个自动变为 `is_default=false`；<br>2. 同名书签重复创建返 `409 Conflict`；<br>3. 用户 B 调用 `GET /api/arch/topology/layouts` 仅能看到官方 `canonical_layout` 与用户 B 自己的书签，绝不泄露用户 A 的 `PRIVATE` 书签；<br>4. 用户 B 尝试 `PUT` 或 `DELETE` 用户 A 的个人书签返 `403 Forbidden`（Anti-IDOR）。 |
| `TestTopologyLayout_Quota20Limit_AndInvalidCoordinatesGuard` | `F-04.3` | 1. 单用户连续创建 20 个个人视图书签成功，第 21 个返 `400 PERSONAL_VIEW_QUOTA_EXCEEDED`；<br>2. 传入非法的 `view="UNKNOWN"` 或超长 `focus_scope` 返 `400`。 |

### 3.2 前端纯算法 Vitest 测试 (`frontend/src/__tests__/ArchCanvasSubgraphAndHybridLayout.test.ts`)

| 测试用例名 | 覆盖特性 | 核心断言内容 |
| :--- | :---: | :--- |
| `testExtractScopedSubgraph_SubsystemFocusAnd1HopExternalContext` | `F-04.1` | 聚焦 `sub_1` 时，仅保留 `sub_1` 内部功能组/模块；跨子系统调用 `sub_2.mod_a` 与外部系统 `UNIONPAY` 的边被自动收敛为 `1-Hop` 外部上下文摘要节点，且不展开 `sub_2` 内部树。 |
| `testFullExpandGuard_BlocksOver150NodesInGlobalScope` | `F-04.1` | 模拟 25 个子系统 × 10 组 × 10 模块（`2,775` 节点）：在 `GLOBAL` 域请求 `L2` 全展时 `checkFullExpandGuard` 返回 `{ blocked: true, estimatedVisibleNodes: 2775 }`；而在聚焦单子系统 `sub_1`（`111` 节点）时返回 `{ blocked: false }`。 |
| `testMultiDimensionalFilters_StatusEgoNetworkAndIsolatedPruning` | `F-04.2` | 1. `VIOLATION_ONLY` 过滤仅保留含违规边的节点及其祖先容器链；<br>2. `Ego-Network (1-Hop / 2-Hop)` 精确保留中心节点 N 跳邻域及父容器；<br>3. `hideIsolated=true` 隐藏度数 `0` 的容器但保留有连线的容器。 |
| `testFoldHighDegreeHubEdges_CollapsesOver8CrossEdges` | `F-04.2` | 当公共底座子系统被 12 个其他子系统依赖时，在 `L0` 下默认将其 12 条跨容器边折叠为 1 个 Hub 总线摘要节点；加入 `expandedHubNodeIds` 后恢复 12 条独立边。 |
| `testMergeIncrementalLayout_PreservesPinnedNodes_ResolvesAABBCollision_AndDiffsModelVersion` | `F-04.3` | 1. `Pinned` 节点在架构新增模块（`model_version` 变化）后坐标 `(x, y)` 100% 不变；<br>2. 新模块的 ELK 初始位置若与 `Pinned` 节点重叠，`resolveSiblingCollisionsAABB` 将其推开且保持 `>=24px` 间距；<br>3. 已删除节点从 `node_positions` 中清理并返回准确的 `{ addedCount: 2, removedCount: 1 }`。 |

### 3.3 前端工作台交互 Vitest 测试 (`frontend/src/__tests__/TopologyFocusAndPersistenceWorkbench.test.tsx`)

| 测试用例名 | 覆盖特性 | 核心断言内容 |
| :--- | :---: | :--- |
| `testProgressiveDrillDown_BreadcrumbNavigation_AndSearchFlyTo` | `F-04.1`, `F-04.2` | 1. 默认 `GLOBAL` 域以 `L0` 加载；点击子系统 `[🔍 聚焦此域]` 切换至 `focus=sub_1` 并渲染面包屑 `🌐 全局总览 > 📦 电商交易系统` 与外部 `1-Hop` 邻居卡；<br>2. 搜索框输入模块编码选中后，自动展开父容器并触发平滑聚焦定位。 |
| `testSpotlightDimming_PinBadgeToggle_AndCameraRetentionOnLocalExpand` | `F-04.2`, `F-04.3` | 1. 单击节点激活 `1-Hop` 聚光灯，无关节点添加 `.is-dimmed`；<br>2. 局部点击容器展开/折叠时，不触发全图 `fitView()` 重置用户当前镜头；<br>3. 固定的节点渲染 `📌` 徽章，点击徽章可解锁。 |
| `testDualTierLayoutPersistence_SavePersonalView_AdminPublishCanonical_AndDiffBanner` | `F-04.3`, `F-16` | 1. 普通用户可见 `[💾 保存个人视图]` 与 `[⭐ 视图书签]`，不可见 `[🏛️ 发布为团队基准布局]`；<br>2. 管理员点击 `[🏛️ 发布为团队基准布局]` 调用 `PUT /api/arch/topology/layouts/canonical`；<br>3. 当 `base_model_version` 与当前图 `model_version` 不一致时，顶部展示琥珀色增量合并提示条。 |

---

## 4. 里程碑准出质量门禁与自动化验收命令 (Exit Gate & AC)

在完成 M19 全部 4 个子阶段后，必须依次执行以下自动化门禁命令并确保 **100% 零错误、零警告通过**：

```bash
# AC-M19-01: 后端 GORM 18 表迁移与双层拓扑布局 API / Anti-IDOR 隔离单测
TEST_DB_DSN="postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable" \
  go test -v -race ./models/... ./handlers/... -run "TestTopologyLayout|TestBuildTopologyGraph"

# AC-M19-02: 前端子图裁剪、多维降噪、Hub 折叠与混合增量布局纯算法单测 (覆盖率 >= 90%)
cd frontend && npx vitest run src/__tests__/ArchCanvasRollup.test.ts src/__tests__/ArchCanvasSubgraphAndHybridLayout.test.ts

# AC-M19-03: 前端拓扑聚焦、聚光灯、相机保持与双层布局书签工作台组件单测
cd frontend && npx vitest run src/__tests__/TopologyFocusAndPersistenceWorkbench.test.tsx src/__tests__/TopologyAndArchDecoupling.test.tsx

# AC-M19-04: 全量后端测试、前端类型检查、ESLint 门禁与单二进制生产构建验证
make test && make lint && make build && make clean
```
