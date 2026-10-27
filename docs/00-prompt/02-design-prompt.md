# code-traefik: 阶段二系统架构与数据模型设计 (SDD) 提示词指令

## 角色定位与工程原则
在阶段一特性清单与边界界定规范（[`docs/01-features/01-feature-matrix.md`](../01-features/01-feature-matrix.md)）已定稿的基础上，现在正式进入：
**「阶段二：系统架构与数据模型设计文档编制」**。

---

## 任务目标与设计产出 (`docs/02-design/`)

请在 `docs/02-design/` 下编制或细化系统架构设计文档（默认主文档为 `01-system-architecture.md`，复杂业务可按双位编号拆分为多篇专篇）：

| 文档路径 | 规范类型 | 核心职责与设计边界 |
| :--- | :--- | :--- |
| `docs/02-design/01-system-architecture.md` | **【总览篇】系统总体架构与数据模型** | 微前端嵌入/独立双模拓扑、端到端请求序列图、GORM 实体关系图 (ER Diagram) 与 `traefik_*` 表结构字典、前端页面与公共组件复用设计 |
| `docs/02-design/02-core-domain-engine.md` *(按需新增)* | **【业务篇】核心领域引擎与状态机** | 核心业务状态流转图、关键算法/规则编排、异步任务或外部系统交互容错设计 |

## 核心设计约束
1. **表名前缀强制隔离**：所有新增 GORM 模型必须显式定义 `TableName() string` 返回 `"traefik_<name>"`，严禁污染公共表空间。
2. **公共能力最大化复用**：
   - 后端复用 `code-common/backend/auth`（JWT + `traefik_admin` 角色守卫）与 `code-common/backend/audit`（字段级 Diff 操作审计）；
   - 前端复用 `@code/common`（`Pagination` + `usePagination`、`Drawer`、`ConfirmProvider`、`ToastProvider`、`MemberSearchSelect`、`StatusTag`），样式 100% 采用 `--color-*` 语义 Token 与 `.code-traefik-*` Flat BEM 命名。
3. **红线提示**：本阶段只产出架构与模型设计文档，**暂不编写具体业务实现代码**。
