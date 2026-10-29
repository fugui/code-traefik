# code-traefik: 阶段三 API 契约冻结与 TDD 任务拆解提示词指令

## 1. 角色与输入基线 (SSOT)
基于已定稿的以下文档，执行 **「阶段三：API 契约冻结 (`03-api`) 与 TDD 任务分解 (`04-tasks`)」**：
- **特性基线**：[`docs/01-features/01-feature-matrix.md`](../01-features/01-feature-matrix.md)（`F-01`~`F-16`、`SC-01`~`SC-08`）
- **架构与模型基线**：[`docs/02-design/01~05`](../02-design/) 及 [`docs/05-adr/01~04`](../05-adr/)

> **核心原则**：特性文档与设计文档已明确的业务规则、协议字节布局、算法伪代码、状态机与 GORM 结构体**严禁在本文档或阶段三产出物中重复抄写**，一律通过 Markdown 链接按章节引用。现有脚手架模板代码（`traefik_items` 等）在阶段四直接删除重写，无需兼容。

---

## 2. 阶段三交付文档矩阵

| 文档路径 | 核心交付内容（仅写增量契约与任务分解，不重复阶段一/二内容） |
| :--- | :--- |
| `docs/03-api/01-http-api-contract.md` | **HTTP / SSE 接口契约字典**：按领域归类列出主控对外 REST/SSE 接口及主从交互接口的方法、路径、Query/Body 参数、响应 JSON 结构、状态码（`200/201/400/401/403/404/409`）、RBAC 守卫要求与 `AuditAction` 标识。 |
| `docs/04-tasks/01-roadmap.md` | **TDD 工程路线图**：清理模板代码策略、M1 ~ M5 里程碑依赖拓扑（DAG）、各里程碑职责边界、与 `F-01`~`F-16` / `SC-01`~`SC-08` 的覆盖映射表及全局质量门禁。 |
| `docs/04-tasks/02-m1-crosrpc-and-domain-models.md` | **M1 作战卡（协议栈与数据模型）**：清理模板代码、`pkg/crosrpc` 分帧/头解析/切片改写、`models/` 13 张表与分区管理器的实现任务、先行单测/Fuzz 清单与验收命令。 |
| `docs/04-tasks/03-m2-arch-proto-and-agent-control.md` | **M2 作战卡（架构元素、`.pb` 与 Agent 管控）**：三级架构元素、模块接口、进程关联、静态拓扑、`.pb` 动态编解码、Agent 公私域纳管/排他租约/心跳对账 API、RBAC 与审计的 TDD 用例清单。 |
| `docs/04-tasks/04-m3-agent-dataplane-ebpf-and-replay.md` | **M3 作战卡（数据面 Agent 引擎）**：`cmd/agent` 受控信道、`pkg/ebpf` 采集与重定向（含 `:ebpf` 特权隔离单测）、`pkg/replay` 零 `.pb` 作用域 Mock 匹配与头部自愈的 TDD 用例清单。 |
| `docs/04-tasks/05-m4-recording-topology-and-diff-engine.md` | **M4 作战卡（协同录制、一致性验证与回归 Diff）**：录制编排与双轨落盘管道、离线架构一致性比对引擎、跨 Agent 同构重放调度与双层回归 Diff 引擎的 TDD 用例清单。 |
| `docs/04-tasks/06-m5-microfrontend-and-e2e-delivery.md` | **M5 作战卡（微前端工作台与全链路交付）**：四大业务工作台组件、四段报文解剖抽屉、双模主题与 URL 分页、`SC-01`~`SC-08` 端到端集成回归及单二进制打包验证。 |

---

## 3. 编写要求与质量红线
1. **高信息密度与零冗余**：API 文档聚焦路径、参数表、状态码与权限矩阵；每张作战卡仅包含 **① 交付包与文件清单**、**② TDD 先行测试矩阵（测试函数名、输入场景、预期断言）**、**③ 验收门禁命令（AC）**。
2. **TDD 可执行性**：每个特性（`F-01`~`F-16`）与验收场景（`SC-01`~`SC-08`）必须落位到具体 Milestone 的具体 `*_test.go` / 前端测试中，数据库单测强制标注 `testdb.SetupIsolatedDB(t)`，eBPF 内核测试强制标注 `//go:build linux && ebpf` 隔离。
3. **阶段边界**：本阶段仅输出/更新 `docs/03-api/`、`docs/04-tasks/` 与 `docs/README.md`，**不修改任何 `.go` 或 `.tsx` 代码**。
