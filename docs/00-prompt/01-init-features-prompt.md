# code-traefik: 阶段一特性清单与需求边界 (SDD) 提示词指令

## 角色定位与工程原则
你是一名资深 Go 全栈系统架构师与产品领域专家。我们将采用 **SDD (规范驱动开发) + TDD (测试驱动开发)** 的工程方法，在 CodeBench 微前端体系下开发 **code-traefik（业务流量监控与重放）** 业务子系统。

开发过程**严禁直接跳过设计堆砌业务代码**，必须严格遵循：
**「阶段一：特性与边界明确 (`01-features`) -> 阶段二：架构与模型定稿 (`02-design`) -> 阶段三：契约冻结与 TDD 任务拆解 (`03-api` & `04-tasks`) -> 阶段四：TDD 测试先行与分阶段实施」**。

---

## 1. 模块基础元数据契约
- **模块唯一标识 (`ModuleKey`)**：`traefik`（网关与前端路由前缀 `/traefik`）
- **模块中文名称**：`业务流量监控与重放`（服务名 `Code-Traefik`）
- **二进制**：同仓双入口 `cmd/server`（`code-traefik-server` 控制面）与 `cmd/agent`（`code-traefik-agent` 数据面）
- **端口分配**：主控 Go 后端 `:8091`，前端 Vite 独立调试端口 `5181`
- **数据库隔离策略**：主控与 CodeBench 共享 PostgreSQL 实例（复用 `users`、`departments`、`audit_logs`），本模块业务表强制使用前缀 `traefik_`；Agent 不连接数据库
- **RBAC 权限角色**：模块管理员角色为 `traefik_admin`（`super_admin` 拥有同等最高权限）

---

## 2. 核心业务背景与架构聚焦点
1. **只支持 DOPHI CROS RPC 协议**：详见项目内置参考规范 [`docs/07-references/01-cros-rpc-protocol-spec.md`](../07-references/01-cros-rpc-protocol-spec.md)。`24B 公共头 RPC_COMM_HEAD_S`（`"CSE.TCP"` 魔术字、大端 `ullSNO`/`uiLength`/`usHeadLen`）+ 多版本会话头（REQ/RSP/MNG）+ `0..N` 段扩展头（Trace 信息）+ Protobuf Payload；首期只覆盖 IPv4 TCP 明文；服务名映射为 package 前缀全名 `{package}-{serviceName}`（package 保持原有点分结构，以中划线 `-` 连接 Service 名称），方法名为 `ascOperation`。
2. **控制面 / 数据面分离**：主控负责架构元素 SSOT、Agent 纳管、录制编排、`.pb` 解码、架构一致性验证、重放编排与双层 Diff；Agent（每台业务机 1 个，同时监控 N 个进程）零 `.pb` 依赖，负责采集与本地重放。信道为 HTTP（主控下发）+ WebSocket 二进制帧（Agent 回传）。
3. **嵌入式 `cilium/ebpf`**：入站按「PID + 监听端口」被动旁路复制；出站经 `cgroup/connect4` 透明重定向到 Agent；Agent 自身 PID 豁免防自环；长连接场景需在下发监控后重启或重连目标进程。
4. **架构元素建模**：子系统 → 功能组（自嵌套树）→ 功能模块；接口 `PROVIDE` / `DEPEND` 在模块层面声明，`PROVIDE` 全局唯一；1 个进程（单监听端口）承载 N 个模块，每个模块只部署 1 份。
5. **零 `.pb` 二进制重放**：Agent 以 `(ascServiceName, ascOperation)` + Payload 字节哈希匹配下游 Mock，自愈覆写公共头偏移 `8..15` 的 `ullSNO` 及应答头 `aucTraceBuff`、`ullClientSendTime`；重放只在测试环境执行。

---

## 3. 阶段一执行指令（生成或细化 `docs/01-features/01-feature-matrix.md`）
请结合具体业务诉求，在 `docs/01-features/01-feature-matrix.md` 中输出详尽、结构化、可直接指导后续架构设计与 TDD 验收的**特性清单矩阵与范围界定规范**：

1. **系统愿景与核心价值主张**：明确解决的核心业务痛点与目标用户角色。
2. **关键业务假设**：列出已与业务方确认的前提假设，并标注受影响的特性。
3. **结构化功能特性矩阵表格**：按业务子域划分（列包含：特性编号 `F-XX`、特性名称、涉及组件、优先级 `P0/P1/P2`、功能描述与技术规范、TDD 验收指标）；验收指标必须可断言，并包含负向用例。
4. **核心业务验收场景矩阵 (Verification Scenarios)**：设计 `SC-XX` 端到端场景（含正常主链路、参数校验拦截、RBAC 越权拦截、并发/边界与异常场景）。
5. **明确定义 Non-Goals（非目标边界）**：界定当前版本明确不做的功能，防止需求蔓延。
6. **非功能性与安全红线 (NFR & Guardrails)**：明确性能基线、数据保留、数据隔离、字段级审计、测试策略与亮/暗双模主题适配要求。
7. **术语表**：集中解释协议与领域术语。
8. **在本阶段定稿确认前，请勿修改或新增任何 `.go` / `.tsx` 业务代码。**
