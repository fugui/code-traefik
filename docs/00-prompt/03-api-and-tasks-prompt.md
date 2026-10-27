# code-traefik: 阶段三接口契约冻结与 TDD 任务拆解 (SDD + TDD) 提示词指令

## 角色定位与工程原则
在阶段一特性矩阵（[`docs/01-features/01-feature-matrix.md`](../01-features/01-feature-matrix.md)）与阶段二系统架构设计（[`docs/02-design/01-system-architecture.md`](../02-design/01-system-architecture.md)）定稿的基础上，现在正式进入：
**「阶段三：API 契约冻结与 TDD 里程碑任务分解」**。

---

## 任务目标与交付文档矩阵

| 文档路径 | 规范类型 | 核心职责与交付要求 |
| :--- | :--- | :--- |
| `docs/03-api/01-http-api-contract.md` | **【API 契约】** | 冻结所有 RESTful 接口路径、Query/Body 参数、响应 JSON Schema、HTTP 状态码、RBAC 角色权限要求（普通登录用户 vs `traefik_admin`）及审计动作标识 |
| `docs/04-tasks/01-roadmap.md` | **【工程路线图】** | 拆解正交递进的 Milestone 1 ~ N 依赖拓扑图（DAG）、各里程碑交付物清单与强制质量门禁 |
| `docs/04-tasks/02-m1-domain-model-and-api.md` | **【M1 作战卡】** | 明确 Milestone 1 的具体编码任务、**TDD 先行单元测试用例清单**（正常/边界/异常/越权）及自动化验收命令 (Acceptance Criteria) |

> **红线提示**：每个 Milestone 作战卡必须明确列出对应的 `*_test.go` 测试用例设计，确保阶段四能够严格按 **TDD（测试先行）** 模式推进。
