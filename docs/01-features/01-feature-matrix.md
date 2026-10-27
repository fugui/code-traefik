# 业务流量监控与重放 (`code-traefik`) 特性清单与需求范围界定规范 (Feature Matrix & Scope)

- **文档编号**：`01-feature-matrix.md`
- **文档版本**：v0.1.0
- **编制日期**：2026-10-27
- **当前阶段**：阶段一：特性清单与边界明确
- **状态**：初始基线（请结合具体业务需求演进完善）

---

## 1. 系统愿景与产品定位 (Vision & Positioning)

### 1.1 项目愿景
**业务流量监控与重放 (`code-traefik`)** 是挂载于 CodeBench 开发者综合工作台的核心业务子系统，旨在提供高内聚、可观测、权限严密且完全符合双模主题体验的 业务流量监控与重放 能力闭环。

### 1.2 核心价值主张
1. **即插即用微前端架构**：既可作为远程联邦模块（`/traefik`）无缝嵌入 CodeBench 门户共享单点登录与导航，亦可独立启动（`:8091` / Vite `:5181`）进行全功能研发调试。
2. **共享库安全隔离与全量审计**：与平台共享核心用户与部门底座，模块自有表统一通过 `traefik_` 前缀物理隔离，所有业务变更自动记录字段级 Diff 审计日志。
3. **SDD + TDD 质量内建**：所有特性具备明确的接口契约与基于 `testdb.SetupIsolatedDB` 的自动化回归测试保护。

---

## 2. 需求功能矩阵 (Feature Matrix)

特性优先级定义：
- **P0 (Must-Have)**：核心基石特性，首期必须完整落地并具备 100% 自动化测试覆盖。
- **P1 (Should-Have)**：重要业务增强与体验优化特性。
- **P2 (Nice-to-Have)**：远期演进与高级分析扩展特性。

| 编号 | 特性名称 | 涉及层级 | 优先级 | 功能描述与技术规范 | TDD 验收指标 / 衡量标准 |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **F-01** | **核心业务看板与健康观测** | `Frontend`<br>`Backend` | **P0** | 提供 `/api/health` 免密探针与 `/api/items/stats` 业务核心指标聚合看板，实时展示各状态条目分布与运行状态。 | `GET /api/health` 返回 `200 OK`；看板卡片在亮/暗双模主题下无硬编码色值且数据准确。 |
| **F-02** | **业务实体全生命周期 CRUD 与 URL 分页** | `Frontend`<br>`Backend`<br>`Models` | **P0** | 支持业务核心实体的分页列表查询（关键词搜索、状态/分类过滤）、新建、编辑与二次确认删除；前端采用 5 页滑动窗口且与 URL Search Params 双向同步（默认 25 条/页）。 | 1. 隔离库单测验证创建、唯一性冲突(`409`)、修改、删除及分页筛选全部通过。<br>2. 浏览器前进/后退保持分页与筛选状态。 |
| **F-03** | **多级 RBAC 权限守卫与负责人关联** | `Backend`<br>`Frontend` | **P0** | 集成 `commonAuth.AuthMiddleware` 与 `RequireAdmin("traefik_admin")`；普通成员仅可管理本人创建或负责的实体，模块管理员（`traefik_admin`）与超管拥有全量治理权限。 | 未携带 Token 返回 `401`；普通用户调用 `/api/admin/*` 或修改他人条目返回 `403 Forbidden`。 |
| **F-04** | **字段级 Diff 操作审计追踪** | `Backend` | **P0** | 所有写操作（新建/更新/删除/配置变更）接入 `commonAudit.RecordDiff`，自动比对并记录变更前后的字段差异至平台审计中心。 | 执行更新接口后，审计表准确落盘操作人、目标对象及字段级 Old/New Diff。 |
| **F-05** | **模块动态参数治理中心** | `Frontend`<br>`Backend` | **P1** | 为 `traefik_admin` 提供专属模块参数配置页（`/admin/settings`），支持运行时调整模块级开关与阈值。 | 仅管理员可见侧边栏入口并可成功保存配置；普通用户访问被拦截。 |

> 💡 **待扩展业务特性**：请在此处继续追加 `业务流量监控与重放` 专属的领域特性（如 `F-06`、`F-07`...）。

---

## 3. 核心验收场景矩阵 (Verification Scenarios)

| 场景编号 | 场景名称 | 触发条件与输入特征 | 预期行为与 TDD 断言 |
| :--- | :--- | :--- | :--- |
| **SC-01** | **免密探针与未认证拦截** | 1. 请求 `GET /api/health`<br>2. 无 Token 请求 `GET /api/me` | 1. 返回 `200 OK` 且包含 `service: "Code-Traefik"`<br>2. 返回 `401 Unauthorized` |
| **SC-02** | **业务实体生命周期与唯一性约束** | 认证用户依次执行 `POST /api/items` -> 重复编码 `POST` -> `PUT` 更新 -> `GET` 分页查询 -> `DELETE` | 首次创建 `201`；重复编码拦截返回 `409`；更新与删除返回 `200` 且数据一致 |
| **SC-03** | **RBAC 管理员越权防护** | 普通角色用户（无 `traefik_admin`）请求 `GET /api/admin/settings` 或 `PUT /api/admin/settings` | 鉴权中间件直接短路拦截，返回 `403 Forbidden` |

---

## 4. 非目标边界界定 (Non-Goals)

- **Non-Goal 1**：不重复自建独立的用户管理、部门树维护或登录认证中心，统一复用 CodeBench 与 `code-common` 底座。
- **Non-Goal 2**：不在共享 PostgreSQL 数据库中创建任何不带 `traefik_` 前缀的业务表。
- **Non-Goal 3**：不引入 Ant Design、Material UI 等重型第三方 UI 组件库，保持 `@code/common` + 原生 Vanilla CSS 的轻量一致性。
