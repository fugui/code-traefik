# Milestone 19 (M19) 作战卡：大规模架构拓扑渐进式下钻聚焦、多维降噪与双层混合布局持久化 (`F-04.1` ~ `F-04.3`, `ADR-15`)

- **文档编号**：`20-m19-scalable-topology-focus-and-layout-persistence.md`
- **文档版本**：v1.1.0
- **归属阶段**：阶段三/四：TDD 任务拆解与分阶段交付 (`04-tasks`)
- **对应里程碑**：**M19 — 大规模架构拓扑渐进式下钻聚焦、多维降噪与双层混合布局持久化**（单里程碑整体交付，内含 7 个递进子阶段 `M19-P1` ~ `M19-P7`）
- **前置依赖**：M6（`ArchCanvas` 分层复合拓扑底座）、M10（零 Agent 物理进程蓝图拓扑）；交付顺序排在 M18 之后，与 M18 无技术依赖
- **覆盖特性**：`F-04`、`F-04.1`（C4 渐进式探索、容器下钻聚焦、有向 N-Hop 邻域与统一可见规模守卫）、`F-04.2`（多维切片过滤、聚光灯、Hub 插头折叠、搜索飞跃与语义 LOD）、`F-04.3`（位置记忆混合布局、镜头保持与位置层/视角层双层持久化）、`SC-11`
- **上位设计基线**：
  - [`01-feature-matrix.md`](../01-features/01-feature-matrix.md) (v2.14.1)
  - [`03-architecture-elements-and-topology.md` §6](../02-design/03-architecture-elements-and-topology.md) (v1.5.1)
  - [`05-database-schema.md` §3.7](../02-design/05-database-schema.md) (v1.5.1, 第 18 张业务表 `traefik_topology_layouts`)
  - [`01-http-api-contract.md` §2.2](../03-api/01-http-api-contract.md) (v1.5.1)
  - [`15-scalable-topology-focus-and-layout-persistence-adr.md`](../05-adr/15-scalable-topology-focus-and-layout-persistence-adr.md) (ADR-15 v1.1.0；**全部阈值、URL 参数与 CSS 类名以 ADR-15 §6 为唯一基准**)
- **状态**：已规划待实施 (Planned — SDD+TDD 规范冻结)

### 修订记录

| 版本 | 日期 | 修订说明 |
| :--- | :--- | :--- |
| v1.0.0 | 2026-11-09 | 初版：`M19-P1` ~ `M19-P4` 四阶段拆解 |
| v1.1.0 | 2026-11-09 | 依据 ADR-15 v1.1.0 架构深度检视加固：扩展为 `M19-P1` ~ `M19-P7` 七个子阶段；新增管线重构（`P2`）与规模验收（`P7`）阶段；交付物补齐 `structure_version`、Module `owner_id`、`gzip`、`overlay.ts` / `noiseReduction.ts` / `layoutPersistence.ts`；修正文件路径（`handlers/routes.go`、`nodes/ModuleNode.tsx`）；统一参数、URL 与 CSS 命名；补齐并发、约束、越权、守卫绕过与性能用例及可执行的覆盖率门禁命令 |

---

## 1. 里程碑目标与七阶段交付拆解 (Phased Execution Plan)

针对生产级 **~25 个子系统 × 10 个功能组 × 10 个功能模块（约 2,775 个图节点 + 5,000~10,000 条底层服务契约边）** 的超大规模拓扑场景，彻底解决「全量展开巨图卡顿、蜘蛛网连线过载、局部展开/折叠/拖拽触发全图重排与 `fitView()` 镜头重置、布局无法跨会话保存与团队共享」四大核心痛点。

M19 作为**单一里程碑整体交付**，内部严格按 7 个递进子阶段实施；每个子阶段遵循 **Red ➔ Green ➔ Refactor**，且子阶段结束时 `make test && make lint` 必须保持全绿（既有 `ArchCanvasRollup.test.ts`、`TopologyAndArchDecoupling.test.tsx` 等回归用例不得退化）：

```mermaid
flowchart LR
    P1["P1 后端<br/>structure_version + owner_id<br/>18 号表 + 7 个 API + gzip"]
    P2["P2 画布管线重构<br/>overlay 拆分 + 显式可见集<br/>拖拽不重排 + 镜头保持"]
    P3["P3 探索与降噪纯算法<br/>聚焦/邻域/切片/守卫<br/>Hub 折叠 + 聚光灯集合"]
    P4["P4 混合布局与持久化纯算法<br/>位置记忆/局部排版/碰撞消解<br/>Diff 合并/加载优先级"]
    P5["P5 画布交互与渲染<br/>邻居卡/图钉/Hub 徽章/LOD<br/>守卫弹窗 + 搜索飞跃"]
    P6["P6 工作台集成与持久化闭环<br/>topo_* URL + 面包屑 + 过滤栏<br/>书签抽屉 + 基准发布 + 草稿"]
    P7["P7 规模验收与文档收口<br/>大规模数据集 + 性能单测<br/>SC-11 + 全量门禁"]

    P1 ==> P2 ==> P3 ==> P4 ==> P5 ==> P6 ==> P7
```

| 子阶段 | 目标 | 阶段准出 |
| :--- | :--- | :--- |
| **M19-P1** | 后端数据与接口底座：Graph DTO 增加 `structure_version` 与 Module `owner_id`；`traefik_topology_layouts` 模型、约束与 7 个布局 API；拓扑 Graph 接口 `gzip` 协商压缩 | §3.1 后端用例全绿 |
| **M19-P2** | 现有画布管线重构（不新增可见功能）：拆出 `synthesizeOverlay`（按模块对建索引）、`useElkLayout` 改为接收显式 `visibleIds`、拖拽不再触发 ELK、`fitView` 收紧为显式导航 | §3.2 管线用例 + 既有回归用例全绿 |
| **M19-P3** | 探索与降噪纯算法：`scopeAndFilter`（聚焦/透明祖先/外部邻居代表/有向邻域/切片过滤）、`estimateVisibleNodeCount`、`pruneIsolated`、`foldHubEdges`、`computeSpotlightSet` | §3.3 用例全绿，相关文件行覆盖率 `≥ 90%` |
| **M19-P4** | 混合布局与持久化纯算法：`planLayout`、`resolveOverlapsBottomUp`、`markDegradedEdges`、`placeNewNodes`、`diffLayoutAgainstGraph`、`computeViewportCompensation`、`resolveInitialLayout`、`composeEffectivePositions`、`extractPersistablePositions`、`diffOverrides` | §3.4 用例全绿，相关文件行覆盖率 `≥ 90%` |
| **M19-P5** | 画布交互与渲染：外部邻居卡、`[🎯 聚焦]` 按钮、`📌` 图钉、`✨` 新增徽标、Hub 插头徽章、聚光灯与 LOD 类名、统一守卫弹窗、搜索候选飞跃（移除自动全展） | §3.5 前 4 项用例全绿 |
| **M19-P6** | 工作台集成与持久化闭环：`topo_*` URL 双向同步、面包屑、多维过滤栏、书签抽屉（复用 `@code/common` `Drawer`）、团队基准发布（合并 + 乐观锁冲突提示）、草稿与 Diff 提示条 | §3.5 全部用例全绿 |
| **M19-P7** | 规模验收与文档收口：大规模数据集生成器与种子脚本、管线性能单测、`SC-11` 浏览器实测记录、运维手册更新、全量门禁 | §4 全部 AC 通过 |

### 1.1 交付物文件清单 (Deliverables Inventory)

| 子阶段 | 层级 | 文件路径 | 变更类型 | 核心职责说明 |
| :---: | :--- | :--- | :---: | :--- |
| P1 | 拓扑构建 | `internal/server/topology/graph_topology.go` | 修改 | `TopologyGraphDTO` 新增 `structure_version`（仅对排序后的 `(id, parent_id)` 计算 SHA-256，格式 `s1-<view>-<hex16>`）；逻辑与物理视图的 `MODULE` 节点 `meta` 补充 `owner_id` |
| P1 | 拓扑测试 | `internal/server/topology/graph_topology_test.go` | 修改 | 新增 `structure_version` 稳定性与 Module `owner_id` 用例 |
| P1 | 后端模型 | `models/arch.go` | 修改 | 新增第 18 张业务表模型 `TopologyLayout` 及 JSON 载荷结构 `SavedNodePosition`、`SavedExpandState`、`SavedFilterState`、`SavedViewport` |
| P1 | 后端迁移 | `models/db.go` | 修改 | `AutoMigrate` 注册 `TopologyLayout`（注释同步为 18 张表）；幂等创建 3 个 `CHECK` 约束与 3 个部分唯一索引（见 §2.1） |
| P1 | 后端接口 | `handlers/topology_layout_handler.go` | **新增** | 7 个布局接口（`ListTopologyLayouts`、`UpsertCanonicalTopologyLayout`、`ResetCanonicalTopologyLayout`、`GetPersonalTopologyLayout`、`CreatePersonalTopologyLayout`、`UpdatePersonalTopologyLayout`、`DeletePersonalTopologyLayout`）；专用请求 DTO 绑定、`http.MaxBytesReader`、输入校验、乐观锁、配额咨询锁与摘要式审计 |
| P1 | 后端压缩 | `handlers/gzip.go` | **新增** | 基于标准库 `compress/gzip` 的轻量协商压缩中间件（仅对 `GET /api/arch/topology/graph` 挂载，按 `Accept-Encoding` 协商，不引入第三方依赖） |
| P1 | 后端路由 | `handlers/routes.go` | 修改 | 挂载 `/api/arch/topology/layouts*` 7 个路由（管理员接口挂 `requireAdmin`）并为 Graph 接口挂载 `gzip` |
| P1 | 后端测试 | `handlers/topology_layout_test.go` | **新增** | 覆盖 §3.1 全部后端用例（`testdb.SetupIsolatedDB(t)` 物理隔离） |
| P2 | 前端叠加合成 | `frontend/src/components/ArchCanvas/model/overlay.ts` | **新增** | `synthesizeOverlay`：从 `rollup.ts` 拆出动态 `VIOLATION / UNATTRIBUTED` 边与虚拟节点合成、静态边状态映射（以 `(src_module_id, dst_module_id)` 建索引替代 O(报告条数 × 边数) 嵌套扫描） |
| P2 | 前端边上卷 | `frontend/src/components/ArchCanvas/model/rollup.ts` | 修改 | 新增 `aggregateRollup(nodeMap, overlay, effectiveExpandState)`；保留 `rollupEdges(...)` 为兼容门面（内部组合 `synthesizeOverlay + aggregateRollup`），既有用例零修改通过 |
| P2 | 前端布局 Hook | `frontend/src/components/ArchCanvas/layout/useElkLayout.ts` | 修改 | 接收 `visibleIds` / `effectiveExpandState` / 位置记忆输入；移除 `draggedNodeIds` 依赖（拖拽不再触发 ELK）；P4 起集成 `layoutMerger.ts` 的局部/全局/快轨三模式 |
| P2 | 前端画布 | `frontend/src/components/ArchCanvas/ArchCanvas.tsx` | 修改 | 删除监听 `layoutNodes.length` 自动 `fitView` 的副作用；`fitView` 仅由显式导航触发；改用 `onNodeDragStop` 记录位置 |
| P3 | 前端纯函数 | `frontend/src/components/ArchCanvas/model/subgraph.ts` | **新增** | `scopeAndFilter`（聚焦 + 透明祖先 + `resolveExternalRep` + 有向 `extractEgoNetwork` + `applySliceFilters`）、`estimateVisibleNodeCount`、`pruneIsolated` |
| P3 | 前端纯函数 | `frontend/src/components/ArchCanvas/model/noiseReduction.ts` | **新增** | `foldHubEdges`（不同顶层调用方 `≥ 8`、违规边豁免、徽章最差状态）、`computeSpotlightSet`（1-Hop + 祖先容器保亮） |
| P4 | 前端纯函数 | `frontend/src/components/ArchCanvas/layout/layoutMerger.ts` | **新增** | `planLayout`、`mergeLocalLayout`、`resolveOverlapsBottomUp`、`markDegradedEdges`、`placeNewNodes`、`diffLayoutAgainstGraph`、`computeViewportCompensation` |
| P4 | 前端纯函数 | `frontend/src/components/ArchCanvas/layout/layoutPersistence.ts` | **新增** | `resolveInitialLayout`、`composeEffectivePositions`、`extractPersistablePositions`、`diffOverrides`、草稿读写（键 `traefik_topo_draft_v2_${userId}_${view}`） |
| P2/P4 | 前端坐标后处理 | `frontend/src/components/ArchCanvas/layout/layoutProcessor.ts` | 修改 | 边退化标记改由 `markDegradedEdges` 结果驱动（覆盖位移祖先与后代）；输出 `data.isPinned` / `data.isNewInLayout` |
| P5 | 前端画布节点 | `frontend/src/components/ArchCanvas/nodes/ContainerNode.tsx`<br>`frontend/src/components/ArchCanvas/nodes/ModuleNode.tsx`<br>`frontend/src/components/ArchCanvas/nodes/ExternalContextNode.tsx` (**新增**) | 修改 / 新增 | 1. `ContainerNode` 标题栏新增 `[🎯 聚焦]` 按钮（**双击仍为展开/折叠**）、`📌` 图钉徽章（点击解除固定）与 Hub 插头徽章区；<br>2. `ModuleNode` 支持图钉与 `✨ 新增` 徽标；<br>3. `ExternalContextNode` 渲染外部邻居卡（方向修饰类 + `[↗ 切换聚焦]`） |
| P5 | 前端画布样式 | `frontend/src/components/ArchCanvas/ArchCanvas.css` | 修改 | ADR-15 §6.3 全部 BEM 类名；淡化透明度变量；颜色 100% 采用 `--color-*` 语义 Token，亮/暗主题双模适配 |
| P5/P6 | 前端工具栏与抽屉 | `frontend/src/components/ArchCanvas/panels/ArchCanvasToolbar.tsx`<br>`frontend/src/components/ArchCanvas/panels/TopologyBookmarkDrawer.tsx` (**新增**)<br>`frontend/src/components/ArchCanvas/panels/VisibilityGuardDialog.tsx` (**新增**) | 修改 / 新增 | 1. 面包屑导航（`🌐 全系统全景 > 📦 子系统 > 📂 功能组`）；<br>2. 搜索候选下拉（最多 20 项、按类型分组）与飞跃定位；<br>3. 多维过滤栏：顶层容器多选、`我负责的`、状态快筛（`全部 / 仅异常 / 仅违规`，未挂载批次禁用）、`[🔍 邻域: 关闭 \| 1 跳 \| 2 跳]`、`隐藏孤立节点`、`Hub 折叠`；<br>4. `[📑 另存为个人视图]`、`[⭐ 视图书签]`（基于 `@code/common` `Drawer`）、管理员 `[🏛️ 发布为团队基准布局]` / `[重置团队基准]`、`[↺ 恢复基线]`、`[重新自动排版]`、`● 未保存本地调整` 胶囊与琥珀色 Diff 提示条；<br>5. 统一守卫弹窗 |
| P6 | 前端类型与 API | `frontend/src/types/topology_graph.ts`<br>`frontend/src/types/arch.ts`<br>`frontend/src/api/arch.ts` | 修改 | `TopologyGraphDTO.structure_version`；`TopologyLayoutDetail` / `TopologyLayoutSummary` / `SavedNodePosition` / `SavedExpandState` / `SavedFilterState` / `SavedViewport` / `TopologyFilterState` / `LayoutDiffNotice` 严格类型（零 `any`）；7 个布局 API 封装 |
| P6 | 前端页面集成 | `frontend/src/pages/ArchitectureWorkbenchPage/ArchTopologyTabPane.tsx`<br>`frontend/src/pages/TopologyWorkbenchPage/TopologyWorkbenchPage.tsx` | 修改 | 1. **删除**“输入关键字自动展开全部命中父链”的副作用；<br>2. 统一可见性出口 `applyVisibilityChange()`（滑杆、全展、单点展开、滚轮语义展开、搜索定位、邻域、过滤、聚焦、书签应用全部经过）；<br>3. `topo_*` URL 双向同步（与 `view` / `session_id` / `env_agent_id` 共存）与浏览器前进/后退；<br>4. 打通加载优先级与草稿 |
| P3~P6 | 前端单测 | `frontend/src/__tests__/ArchCanvasScopeAndFilter.test.ts` (**新增**)<br>`frontend/src/__tests__/ArchCanvasHybridLayout.test.ts` (**新增**)<br>`frontend/src/__tests__/TopologyFocusAndPersistenceWorkbench.test.tsx` (**新增**) | 新增 | 见 §3.2 ~ §3.5 |
| P7 | 规模数据集 | `frontend/src/__tests__/fixtures/largeTopology.ts` (**新增**)<br>`frontend/src/__tests__/ArchCanvasScale.perf.test.ts` (**新增**) | 新增 | 确定性生成 25 × 10 × 10 拓扑（含 1 个被 12 子系统依赖的公共底座、外部系统与一致性报告）；管线性能 P95 断言 |
| P7 | 种子脚本 | `scripts/seed-large-topology/main.go` (**新增**) | 新增 | 通过 GORM 向数据库写入同构大规模架构数据供 `SC-11` 浏览器实测（参数 `-dsn`、`-subsystems`、`-groups`、`-modules`、`-hub-callers`、`-with-violation-session`）；内置熔断：仅允许写入库名以 `_test` 或 `_dev` 结尾的数据库 |
| P7 | 运维手册 | `docs/06-ops/01-verification-and-deployment-guide.md` | 修改 | 按已冻结的 §4 `SC-11` 联调步骤（使用物理 IP `192.168.56.18` 访问）执行验收，并填写 §4.3 性能实测记录 |

---

## 2. 各阶段技术实现规范与契约细则

### 2.1 `M19-P1`：后端 Graph 增强、`TopologyLayout` 模型与布局 REST API

1. **Graph DTO 增强 (`graph_topology.go`)**：
   - `structure_version`：对按 `id` 排序后的 `id + "|" + parent_id` 行序列计算 SHA-256，取前 8 字节十六进制，格式 `s1-<view>-<hex16>`；**不受边、名称、描述、元数据与 `env_agent_id` 端点叠加影响**；
   - `MODULE` 节点 `meta.owner_id`：逻辑视图与物理视图均输出（`nil` 时省略）；
   - `gzip`：请求头含 `Accept-Encoding: gzip` 时压缩响应体并设置 `Content-Encoding: gzip` 与 `Vary: Accept-Encoding`；`304` 响应不压缩。
2. **GORM 模型与约束 (`models/arch.go` & `models/db.go`)**：严格按 [`05-database-schema.md` §3.7](../02-design/05-database-schema.md) 定义 `TopologyLayout`，表名 `config.TablePrefix + "topology_layouts"`；在迁移阶段幂等执行：
   ```sql
   -- CHECK 约束（先 DROP IF EXISTS 再 ADD，保持幂等）
   ALTER TABLE traefik_topology_layouts ADD CONSTRAINT chk_traefik_topo_layout_scope
       CHECK (scope IN ('SHARED','PRIVATE'));
   ALTER TABLE traefik_topology_layouts ADD CONSTRAINT chk_traefik_topo_layout_view
       CHECK (view IN ('LOGICAL','PHYSICAL'));
   ALTER TABLE traefik_topology_layouts ADD CONSTRAINT chk_traefik_topo_layout_owner
       CHECK ((scope = 'SHARED'  AND owner_id IS NULL AND focus_scope = 'GLOBAL' AND is_default = false)
           OR (scope = 'PRIVATE' AND owner_id IS NOT NULL));
   -- 部分唯一索引
   CREATE UNIQUE INDEX IF NOT EXISTS uk_traefik_topo_layout_shared
       ON traefik_topology_layouts (view) WHERE scope = 'SHARED';
   CREATE UNIQUE INDEX IF NOT EXISTS uk_traefik_topo_layout_private_name
       ON traefik_topology_layouts (owner_id, view, name) WHERE scope = 'PRIVATE';
   CREATE UNIQUE INDEX IF NOT EXISTS uk_traefik_topo_layout_private_default
       ON traefik_topology_layouts (owner_id, view, focus_scope) WHERE scope = 'PRIVATE' AND is_default;
   ```
3. **输入校验与安全防腐 (`handlers/topology_layout_handler.go`)**：
   - **专用 DTO 绑定**：严禁直接绑定 GORM 模型；`scope`、`owner_id`、`revision`、`created_by`、`updated_by` 一律由服务端决定（防批量赋值）；
   - **请求体上限**：`http.MaxBytesReader(1MB)`，超限 `413 PAYLOAD_TOO_LARGE`；
   - `view` 仅允许 `LOGICAL` / `PHYSICAL`；`focus_scope` 规范化缺省为 `GLOBAL`，且必须匹配 `^(GLOBAL|(sub|grp|pgrp|proc)_[0-9]+|ext_container|undeployed_container)$`；
   - `node_positions` 条目数 `≤ 5000`、键非空且长度 `≤ 128`、`x / y` 为有限实数（拦截 `NaN / ±Inf`）；`expand_state.level ∈ [0, 16]`、`overrides` 条目数 `≤ 5000`；`viewport.zoom ∈ [0.15, 2.0]`；`filter_state.subsystem_ids` 条目数 `≤ 100`、`ego_hops ∈ {1, 2}`、`status ∈ {ALL, ABNORMAL, VIOLATION_ONLY}`；
   - **团队基准乐观锁**：事务内 `SELECT ... FOR UPDATE` 锁定 `(scope='SHARED', view)` 行；`expected_revision` 与当前 `revision` 不一致（含首次创建时 `expected_revision ≠ 0`）返回 `409 LAYOUT_REVISION_CONFLICT`；并发首次创建触发唯一索引冲突同样映射为 `409`；成功后 `revision + 1`；
   - **个人书签配额**：事务内先执行 `SELECT pg_advisory_xact_lock(hashtext('traefik_topo_layout_quota'), <owner_id>)` 再计数，超过 `20` 条返回 `400 PERSONAL_VIEW_QUOTA_EXCEEDED`；
   - **默认书签互斥**：设置 `is_default = true` 时同事务先将同 `(owner_id, view, focus_scope)` 其余书签置 `false`，并由 `uk_traefik_topo_layout_private_default` 兜底；
   - **越权**：个人书签的读取/修改仅限本人，删除限本人或 `traefik_admin`，越权统一 `403`（与 `F-16` / `SC-01` 公私域约定一致）；个人书签接口作用于 `SHARED` 行返回 `404`；
   - **审计**：全部写操作接入 `commonAudit`，**仅记录摘要**（`view`、`focus_scope`、`name`、坐标条目数、`structure_version`、`revision`），禁止将 `node_positions` 等大 JSON 写入字段级 Diff。

### 2.2 `M19-P2`：现有画布管线重构

1. **拆出叠加合成**：`synthesizeOverlay(nodes, edges, report, view)` 返回 `{ edges: GraphEdge[] /* 静态 + 动态 */, dynamicNodes, edgeStatus, edgeCalls, edgeSample, unattributedPendingList }`；静态边匹配以 `Map<"srcModuleId->dstModuleId", GraphEdge[]>` 索引，消除 O(报告条数 × 边数) 扫描；
2. **兼容门面**：`rollupEdges(nodes, edges, expandState, report, view)` 签名与输出保持不变，内部改为 `aggregateRollup(nodeMap, synthesizeOverlay(...), expandState)`；
3. **显式可见集**：`useElkLayout` 改为接收 `visibleIds`（P2 阶段由“祖先全部展开”规则计算，行为与现状一致；P3 起由 `scopeAndFilter` 提供）；
4. **拖拽不重排**：移除 `draggedNodeIds` 作为 ELK 重排依赖；`onNodeDragStop` 仅更新会话位置记忆（P4 接入碰撞消解与草稿）；
5. **镜头保持**：删除监听 `layoutNodes.length` 的自动 `fitView`；`fitView` 仅在 ①首次加载完成（布局无有效视口）②切换视图 ③切换聚焦域 ④点击 `[居中]` / `[重新自动排版]` ⑤应用书签时执行。

### 2.3 `M19-P3`：探索与降噪纯算法

1. **`scopeAndFilter(input): ScopedGraph`**：输入为全量节点（含动态节点）、叠加后边集、用户展开态、过滤状态与 `currentUserId`；输出 `{ visibleIds, effectiveExpandState, relativeDepth, neighborMeta, breadcrumbs, estimatedVisibleCount }`；
2. **聚焦**：内部集 = 聚焦根 + 后代；聚焦根祖先为透明展开；聚焦根默认展开 1 层；外部邻居代表按 `resolveExternalRep`：取 `LCA(u, focus)` 的直接子节点，无公共祖先取顶层祖先；结果为 `ext_container` 时外部系统/端点叶子单独成卡；虚拟未登记接口节点单独成卡；邻居强制折叠并标注方向；
3. **有向邻域 `extractEgoNetwork`**：上游沿反向边、下游沿正向边各扩展 N 跳后取并集，严禁混合方向；容器中心以其叶子后代为种子；
4. **切片过滤 `applySliceFilters`**（各维度取交集，命中节点祖先链自动保留）：
   - `subsystemIds`：仅保留所选顶层容器子树及其互调边；
   - `onlyMyScope`：命中 `meta.owner_id === currentUserId` 的子系统（整棵子树）与模块，并保留命中模块的叶子级 1-Hop 上下游；
   - `status`：`ABNORMAL` 保留 `VIOLATION / UNATTRIBUTED / UNCOVERED` 边端点，`VIOLATION_ONLY` 仅保留 `VIOLATION` 边端点；未挂载一致性报告时忽略；
5. **`pruneIsolated`**：在边上卷之后执行，隐藏无任何关联汇总边的可见叶子/折叠容器，并级联隐藏被清空的容器；
6. **`estimateVisibleNodeCount`**：对候选状态执行同一裁剪逻辑仅计数，供统一守卫使用；
7. **`foldHubEdges`**：统计每个可见节点的跨顶层入边中**不同调用方顶层容器**数量，`≥ 8` 且未在 `expandedHubIds` 中时移除其跨顶层汇总边（`VIOLATION / UNATTRIBUTED` 豁免），为每个调用方生成 `HubPillSummary { hubId, hubName, contractCount, worstStatus }`；
8. **`computeSpotlightSet(activeId, rollupEdges, nodeMap)`**：返回 1-Hop 关联节点、关联边，以及命中节点的祖先容器（容器不淡化）。

### 2.4 `M19-P4`：混合布局与持久化纯算法

1. **`planLayout`**：根据触发类型与位置记忆返回 `GLOBAL_ELK`（首次无坐标 / 重新自动排版）、`LOCAL_ELK`（返回需局部排版的容器列表）或 `FAST_PATH`（可见节点全部有坐标）；
2. **`resolveOverlapsBottomUp(tree, anchorId, gap = 32)`**：锚点不动；同层兄弟按距锚点由近及远单调平移；未固定节点优先移动，`pinned` 节点仅在确实重叠时被推开并保持相对方位；父容器尺寸逐层重算并向上传播；保证终止；
3. **`markDegradedEdges`**：端点或其任一祖先相对最近一次 ELK 输出有位移、或在最近一次 ELK 输出中无拐点数据的边标记退化；快轨模式全部退化；
4. **`placeNewNodes` / `diffLayoutAgainstGraph`**：按 ADR-15 §4.3 计算 `addedCount / removedCount`，新增节点落位于父容器已排布包围盒右侧/底部网格空位并标记 `isNewInLayout`；`structure_version` 相同则直接跳过；
5. **`computeViewportCompensation`**：`viewport -= Δ × zoom`；
6. **`resolveInitialLayout`**：基线 = URL 书签 ≻ 当前 `(view, focus_scope)` 默认书签 ≻ 团队基准 ≻ 无；草稿仅当 `draft.base` 与基线一致时叠加，否则丢弃并返回提示标记；
7. **`composeEffectivePositions` / `diffOverrides`**：生效坐标 = 基准 ⊕ 书签差量 ⊕ 草稿差量；保存书签时仅提交相对团队基准变化或被固定的节点；
8. **`extractPersistablePositions`**：聚焦态仅返回聚焦根严格后代的坐标。

### 2.5 `M19-P5`：画布交互与渲染

1. **外部邻居卡与聚焦按钮**：`ExternalContextNode` 按方向渲染修饰类；`ContainerNode` 标题栏 `[🎯 聚焦]`，双击保持展开/折叠；
2. **图钉与新增徽标**：`📌` 点击解除固定（不立即重排，下次「重新自动排版」时参与 ELK）；`✨ 新增` 在保存布局后消失；
3. **聚光灯与 LOD**：悬停 `80ms` 防抖、单击锁定、点击空白或 Esc 清除；三档 LOD 类名挂载于 `.code-arch-canvas` 根节点；
4. **统一守卫弹窗**：超阈值时展示预估节点数与「先聚焦 / 先过滤 / 仍要展开」选项；滚轮语义展开超限时改为非模态提示并放弃本次展开；
5. **搜索飞跃**：移除关键字自动全展；候选选中后仅展开目标单一祖先链（经守卫）；目标不在当前聚焦/过滤可见域时，自动将聚焦切换至目标顶层容器后再飞跃；执行 `fitView({ nodes: [{ id }], maxZoom: 1.15, duration: 360 })` 与 `2s` `.code-arch-node--pulse`。

### 2.6 `M19-P6`：工作台集成与持久化闭环

1. **URL 双向同步**：按 ADR-15 §6.2 同步全部 `topo_*` 参数，支持深链分享与浏览器前进/后退；
2. **书签抽屉**：列表仅展示摘要，选中后按需拉取详情；支持重命名、设为默认、删除；
3. **团队基准发布**：拉取最新基准 ➔ 合并 `extractPersistablePositions` 结果 ➔ 携带 `expected_revision` 提交；`409` 时提示“基准已被他人更新，请刷新后重试”；
4. **草稿**：拖拽结束 `500ms` 防抖写入；「↺ 恢复基线」清除草稿；
5. **降级**：书签 `403 / 404` 降级至团队基准并 toast；书签 `focus_scope` 容器不存在时降级至全局。

### 2.7 `M19-P7`：规模验收与文档收口

1. `largeTopology.ts` 生成确定性数据集；`ArchCanvasScale.perf.test.ts` 对「叠加合成 + 裁剪过滤 + 边上卷」连续执行 20 次取 P95，CI 阈值 `≤ 60ms`；
2. 使用种子脚本写入 `_dev` / `_test` 库，按 `SC-11` 步骤在浏览器（`http://192.168.56.18:<port>`）实测局部 ELK、全局 ELK、快轨直出与 Graph 压缩体积，并将结果记录到运维手册；
3. 完成全部文档同步并执行 §4 全量门禁。

---

## 3. TDD 先行测试用例矩阵 (Test-First Specification)

实施代码前，必须先编写以下单元测试与组件测试（确认 Red 失败后再编写业务实现至 Green 全绿）：

### 3.1 后端 Go 单元测试（`M19-P1`）

| 测试函数名 | 所在文件 | 核心断言内容 |
| :--- | :--- | :--- |
| `TestBuildTopologyGraph_StructureVersion_IgnoresEdgesMetaAndEnv` | `internal/server/topology/graph_topology_test.go` | 新增依赖边、修改模块名称/描述、PHYSICAL 叠加不同 `env_agent_id` 时 `structure_version` 不变而 `model_version` 改变；新增/删除/移动模块时 `structure_version` 改变 |
| `TestBuildTopologyGraph_ModuleOwnerIDInMeta_LogicalAndPhysical` | 同上 | 逻辑与物理视图 `MODULE` 节点 `meta.owner_id` 与库内一致，未设置负责人时省略 |
| `TestGetTopologyGraph_GzipNegotiation` | `handlers/arch_proto_agent_test.go` | 携带 `Accept-Encoding: gzip` 返回压缩体与 `Vary` 头且可解压还原；未携带时返回明文；`304` 不压缩 |
| `TestTopologyLayout_SchemaConstraints_ScopeOwnerAndPartialUniques` | `handlers/topology_layout_test.go` | 直接插入 `SHARED + owner_id` 或 `PRIVATE + owner_id IS NULL` 违反 `CHECK`；同 `view` 第二条 `SHARED`、同人同视图同名书签、同 `(owner, view, focus_scope)` 第二个默认书签均触发唯一约束 |
| `TestTopologyLayout_CanonicalUpsert_RevisionConflictAndReset_RBAC` | 同上 | 普通用户 `PUT/DELETE canonical` 返 `403`；管理员 `expected_revision=0` 创建成功（`revision=1`）、携带正确 revision 更新成功（`revision=2`）、携带过期 revision 返 `409 LAYOUT_REVISION_CONFLICT`；`DELETE` 成功后再次 `DELETE` 返 `404`；审计日志仅含摘要字段 |
| `TestTopologyLayout_PersonalCRUD_DefaultMutex_QuotaAndAntiIDOR` | 同上 | 用户 A 两个同 `(LOGICAL, GLOBAL)` 书签切换默认时互斥；同名返 `409`；第 21 个返 `400 PERSONAL_VIEW_QUOTA_EXCEEDED`；用户 B 列表不含 A 的书签，B 读取/修改/删除 A 的书签返 `403`；管理员可删除 A 的书签；个人接口作用于 `SHARED` 行返 `404` |
| `TestTopologyLayout_ConcurrentCreate_QuotaAndDefaultNeverViolated` | 同上 | 用户已有 19 个书签时并发 10 个创建请求，最终总数恰为 20；并发设置默认后同域默认书签恰为 1 个 |
| `TestTopologyLayout_InputGuards_PayloadLimitMassAssignmentAndNaN` | 同上 | 请求体 `> 1MB` 返 `413`；Body 夹带 `scope="SHARED"` / `owner_id` / `revision` 被忽略；`view="UNKNOWN"`、非法 `focus_scope`、坐标 `NaN/Inf`、`> 5000` 条目、`zoom` 越界均返 `400` |
| `TestTopologyLayout_ListOmitsPayload_DetailOwnerOnly` | 同上 | `GET /layouts` 中 `personal_views` 为摘要（无 `node_positions` 等 JSON 字段），`canonical_layout` 为完整详情；`GET /layouts/personal/:id` 本人返回完整详情 |

### 3.2 前端管线重构 Vitest 测试（`M19-P2`）

| 测试用例名 | 所在文件 | 核心断言内容 |
| :--- | :--- | :--- |
| `testSynthesizeOverlay_IndexedStaticMatch_AndDynamicViolationEdges` | `ArchCanvasScopeAndFilter.test.ts` | 静态边状态映射结果与旧实现逐条一致；动态 `VIOLATION / UNATTRIBUTED` 边与虚拟未登记接口节点正确生成 |
| `testRollupFacade_BackwardCompatible` | `ArchCanvasRollup.test.ts`（既有文件追加） | `rollupEdges` 兼容门面输出与 `aggregateRollup(synthesizeOverlay(...))` 完全一致；既有用例零修改全绿 |
| `testDragStopDoesNotTriggerElkRelayout` | `TopologyFocusAndPersistenceWorkbench.test.tsx` | 拖拽结束后 ELK 排版函数调用次数不增加，节点停留在释放位置 |
| `testLocalExpandKeepsViewport_NoGlobalFitView` | 同上 | 单点展开/折叠与拖拽不调用 `fitView`；切换视图、切换聚焦与点击 `[居中]` 时调用 |

### 3.3 探索与降噪纯算法 Vitest 测试（`M19-P3`，`ArchCanvasScopeAndFilter.test.ts`）

| 测试用例名 | 核心断言内容 |
| :--- | :--- |
| `testFocusScope_TransparentAncestors_RelativeDepth_DefaultExpandOneLevel` | 聚焦 `grp_10` 时其祖先 `sub_1` 不可见但视为展开，`grp_10` 成为画布根，`relativeDepth(grp_10)=0`；默认仅展开 1 层 |
| `testExternalRep_LcaChild_SiblingGroup_ExternalSystemStandalone` | 聚焦 `grp_10` 时同子系统兄弟组内模块收敛为其 LCA 直接子组；跨子系统模块收敛为对方子系统；外部系统 `UNIONPAY` 单独成卡而非整个 `ext_container`；方向标注正确 |
| `testFocusScope_ViolationEdgeProducesNeighborCard_NoEdgeDropped` | 仅存在于一致性报告的 `VIOLATION` 跨界调用同样产生外部邻居卡，且汇总边端点均在可见集内（零丢边） |
| `testEgoNetwork_DirectionalHops_NoSiblingCallerLeak` | `mod_101` 2 跳：包含调用方的调用方与被调方的被调方，不包含“调用方的其他被调方”；祖先外壳保留 |
| `testSliceFilters_SubsystemsMyScopeAbnormal_IntersectAndAncestorsKept` | 子系统多选、`onlyMyScope`（子系统负责人命中整棵子树、模块负责人命中模块及 1-Hop）、`ABNORMAL` / `VIOLATION_ONLY` 组合取交集，祖先链完整；未挂载报告时状态过滤被忽略 |
| `testPruneIsolated_AfterRollup_CascadeEmptyContainers` | 无汇总边的叶子被隐藏，被清空的容器级联隐藏 |
| `testVisibilityGuard_GlobalL2Blocked_FocusRelativeL2Allowed` | 2,775 节点数据集：全局 `L2` 预估 `2775` 被拦截；聚焦 `sub_1` 后相对 `L2` 预估 `≤ 150` 放行；Hub 模块 2 跳邻域超限被拦截 |
| `testHubFolding_DistinctCallerThreshold_ViolationExempt_PillWorstStatus` | 公共底座被 12 个子系统依赖：12 条跨顶层边被收起并生成 12 个调用方徽章；其中 1 条 `VIOLATION` 边保留显示且对应徽章为违规色；加入 `expandedHubIds` 后恢复；7 个调用方时不折叠 |
| `testSpotlightSet_OneHopWithAncestorContainers` | 选中节点的 1-Hop 节点与边命中，命中节点的祖先容器不淡化，其余节点与边进入淡化集合 |

### 3.4 混合布局与持久化纯算法 Vitest 测试（`M19-P4`，`ArchCanvasHybridLayout.test.ts`）

| 测试用例名 | 核心断言内容 |
| :--- | :--- |
| `testPlanLayout_GlobalOnFirstLoad_LocalOnExpand_FastPathWhenAllPositioned` | 三种触发分别返回 `GLOBAL_ELK` / `LOCAL_ELK(容器列表)` / `FAST_PATH` |
| `testResolveOverlapsBottomUp_AnchorFixed_ParentGrows_Terminates` | 展开 C 后锚点不动、重叠兄弟沿最小穿透轴推开且间距 `≥ 32px`；父容器尺寸撑开并向上传播；未重叠节点坐标严格不变；100 次随机布局均终止且零重叠 |
| `testPinnedNodePushedOnlyWhenOverlapping_RelativeOrderPreserved` | 已固定 A、B 未与 C 重叠时坐标不变；A 与 C 重叠时 A 被推开且 A/B 相对方位保持，A 的固定坐标同步更新 |
| `testMarkDegradedEdges_DisplacedAncestorsAndDescendants` | 被推开容器内部子孙节点的关联边、跨层边全部标记退化；未位移区域的边保留 ELK 拐点；快轨模式全部退化 |
| `testDiffLayoutAgainstGraph_EdgeOnlyChangeNoBanner_AddedRemovedCounted` | 仅新增依赖边（`structure_version` 不变）不提示；新增 2 个模块、删除 1 个模块返回 `{ addedCount: 2, removedCount: 1 }`；保存时未展开容器内的节点不计为新增 |
| `testPlaceNewNodes_GridSlotNoOverlap_FlaggedIsNewInLayout` | 新节点落位于父容器已排布包围盒右侧/底部且零重叠，`isNewInLayout = true` |
| `testResolveInitialLayout_PriorityMatrix_DraftBaseMismatchDiscarded` | URL 书签 ≻ 默认书签 ≻ 团队基准 ≻ 无；草稿 `base` 一致时叠加、不一致时丢弃并返回提示标记 |
| `testComposeAndDiffOverrides_BookmarkStoresDeltaOnly` | 生效坐标叠加顺序正确；保存书签仅包含相对基准变化或被固定的节点；基准更新后书签未覆写节点自动继承新坐标 |
| `testExtractPersistablePositions_FocusModeExcludesRootAndNeighbors` | 聚焦态仅输出聚焦根严格后代坐标 |
| `testViewportCompensation_AnchorDelta` | 锚点位移 `(Δx, Δy)` 时视口按 `Δ × zoom` 反向补偿；位移为 0 时视口不变 |

### 3.5 工作台交互 Vitest 测试（`M19-P5` / `M19-P6`，`TopologyFocusAndPersistenceWorkbench.test.tsx`）

| 测试用例名 | 核心断言内容 |
| :--- | :--- |
| `testFocusDrillDown_BreadcrumbNeighborCard_AndTopoUrlParams` | 点击 `[🎯 聚焦]` 写入 `topo_focus=sub_1`，渲染面包屑与邻居卡；点击邻居卡切换聚焦；双击容器仍为展开/折叠；浏览器后退恢复全景 |
| `testSearchDropdown_SingleAncestorChain_FlyTo_NoAutoExpandAll` | 输入关键字不改变展开态；选中候选后仅展开目标祖先链并调用 `fitView({ nodes: [{ id }], maxZoom: 1.15, duration: 360 })`；目标在聚焦域外时自动切换聚焦 |
| `testGuardDialog_AllEntryPointsRoutedThroughChokePoint` | 层级滑杆、全展、滚轮语义展开、邻域 2 跳与多选过滤超阈值时均被拦截（滚轮为非模态提示）；「仍要展开」生效 |
| `testSpotlightLodPinAndNewBadges_BemClasses` | 聚光灯 `.code-arch-node--dimmed` / `.code-arch-edge--dimmed`、LOD `.code-arch-canvas--lod-*`、`📌` 解除固定、`✨` 新增徽标类名正确 |
| `testDualTierPersistence_BookmarkSave_AdminPublishMerge_RevisionConflict` | 普通用户可见 `[📑 另存为个人视图]` 与 `[⭐ 视图书签]`、不可见管理员按钮；管理员发布时提交合并后的坐标与 `expected_revision`，`409` 时展示冲突提示 |
| `testDraftAndDiffBanner_OnlyWhenNodeSetChanges` | 拖拽后出现 `● 未保存本地调整` 并写入草稿；`↺ 恢复基线` 清除；仅节点集合变化时显示琥珀色提示条 |
| `testDeepLinkFallback_ForbiddenBookmarkAndMissingFocus` | `topo_layout_id` 返回 `403/404` 时降级至团队基准并 toast；书签聚焦容器不存在时降级至全局 |

### 3.6 规模性能测试（`M19-P7`，`ArchCanvasScale.perf.test.ts`）

| 测试用例名 | 核心断言内容 |
| :--- | :--- |
| `testScalePipeline_2775Nodes_10kEdges_P95Budget` | 基于 `largeTopology.ts`（含一致性报告）连续执行 20 次「叠加合成 + 裁剪过滤 + 边上卷」，P95 `≤ 60ms`（CI 阈值） |

---

## 4. 里程碑准出质量门禁与自动化验收命令 (Exit Gate & AC)

在完成 M19 全部 7 个子阶段后，必须依次执行以下自动化门禁命令并确保 **100% 零错误、零警告通过**：

```bash
# AC-M19-01 (P1): Graph 结构指纹 / owner_id / gzip 与 18 表迁移、双层布局 API、并发与 Anti-IDOR 单测
TEST_DB_DSN="postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable" \
  go test -v -race ./internal/server/topology/... ./models/... ./handlers/... \
  -run "TestTopologyLayout|TestBuildTopologyGraph|TestGetTopologyGraph_Gzip"

# AC-M19-02 (P2~P4): 管线重构、探索降噪与混合布局纯算法单测（新增纯函数文件行覆盖率 >= 90%）
cd frontend && npx vitest run \
  src/__tests__/ArchCanvasRollup.test.ts \
  src/__tests__/ArchCanvasScopeAndFilter.test.ts \
  src/__tests__/ArchCanvasHybridLayout.test.ts \
  --coverage \
  --coverage.include='src/components/ArchCanvas/model/{overlay,subgraph,noiseReduction}.ts' \
  --coverage.include='src/components/ArchCanvas/layout/{layoutMerger,layoutPersistence}.ts' \
  --coverage.thresholds.lines=90

# AC-M19-03 (P5~P6): 聚焦、守卫、搜索飞跃、聚光灯、镜头保持与双层持久化工作台组件单测
cd frontend && npx vitest run src/__tests__/TopologyFocusAndPersistenceWorkbench.test.tsx src/__tests__/TopologyAndArchDecoupling.test.tsx

# AC-M19-04 (P7): 大规模管线性能预算
cd frontend && npx vitest run src/__tests__/ArchCanvasScale.perf.test.ts

# AC-M19-05 (P7): 全量后端测试、前端类型检查、ESLint 门禁与单二进制生产构建验证
make test && make lint && make build && make clean
```

**人工验收（`SC-11`）**：按 [`06-ops/01-verification-and-deployment-guide.md`](../06-ops/01-verification-and-deployment-guide.md) 使用种子脚本写入大规模数据集，在 `http://192.168.56.18:<port>` 实测并记录 ADR-15 §5 性能预算各项，结果达标后方可将 Roadmap 中 M19 标记为已交付。
