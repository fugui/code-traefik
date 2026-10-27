# code-traefik: 阶段一特性清单与需求边界 (SDD) 提示词指令

## 角色定位与工程原则
你是一名资深 Go 全栈系统架构师与产品领域专家。我们将采用 **SDD (规范驱动开发) + TDD (测试驱动开发)** 的工程方法，在 CodeBench 微前端体系下开发 **code-traefik（业务流量监控与重放）** 业务子系统。

开发过程**严禁直接跳过设计堆砌业务代码**，必须严格遵循：
**「阶段一：特性与边界明确 (`01-features`) -> 阶段二：架构与模型定稿 (`02-design`) -> 阶段三：契约冻结与 TDD 任务拆解 (`03-api` & `04-tasks`) -> 阶段四：TDD 测试先行与分阶段实施」**。

---

## 1. 模块基础元数据契约
- **模块唯一标识 (`ModuleKey`)**：`traefik`（网关与前端路由前缀 `/traefik`）
- **模块中文名称**：`业务流量监控与重放`（服务名 `Code-Traefik`）
- **端口分配**：Go 后端监听 `:8091`，前端 Vite 独立调试端口 `5181`
- **数据库隔离策略**：与 CodeBench 共享 PostgreSQL 实例（复用 `users`, `departments`, `audit_logs`），本模块所有业务表强制使用前缀 `traefik_`
- **RBAC 权限角色**：模块管理员角色为 `traefik_admin`（`super_admin` 拥有同等最高权限）

---

## 2. 核心业务背景与三大架构聚焦点
1. **嵌入式原生 eBPF（摒弃 Keploy）**：不再使用外部 Keploy 工具链，直接在 `code-traefik-server` 中内嵌 `github.com/cilium/ebpf` 内核探针，实现内核态透明流量拦截、BPF Map 实时遥测、`monitor_pid` 防自环死锁豁免及探针自动卸载，并支持无特权环境平滑降级。
2. **基于系统 PID 的目标进程监控（非固定 Demo 进程）**：监控对象不再是硬编码的演示进程，而是通过宿主机系统 **PID**（结合 `/proc/<pid>` 进程探查与端口规则）动态指定与纳管的真实业务进程，在内核 eBPF 层按目标 `PID` 精准过滤入站（Ingress）与出站（Egress）流量。
3. **私有网络协议 + Protobuf 字节码通信协议**：聚焦原生 TCP 之上 **24 字节定长大端二进制帧头（Magic/Version/OpCode/Flags/Status/RequestID/PayloadLen/CRC32）+ Protobuf 序列化字节码载荷** 的私有协议栈；支持动态注册 `.pb`（`FileDescriptorSet`）字节码描述集与 `OpCode` 映射字典，实现粘包/半包流式拆帧、Protobuf 明文 JSON 解码、动态 `RequestID` 重写、`CRC32` 自愈离线回放与字段级 Diff 断言。

---

## 3. 阶段一执行指令（生成或细化 `docs/01-features/01-feature-matrix.md`）
请结合具体业务诉求，在 `docs/01-features/01-feature-matrix.md` 中输出详尽、结构化、可直接指导后续架构设计与 TDD 验收的**特性清单矩阵与范围界定规范**：

1. **系统愿景与核心价值主张**：明确 `业务流量监控与重放` 解决的核心业务痛点与目标用户角色。
2. **结构化功能特性矩阵表格**：
   - 按核心业务子域划分（列包含：特性编号 `F-XX`、特性名称、优先级 `P0/P1/P2`、功能描述与技术规范、TDD 验收衡量标准）。
3. **核心业务验收场景矩阵 (Verification Scenarios)**：设计 `SC-01` ~ `SC-05` 端到端核心业务场景（含正常主链路、参数校验拦截、RBAC 越权拦截、并发/边界场景）。
4. **明确定义 Non-Goals（非目标边界）**：界定当前版本明确不做的功能，防止需求蔓延。
5. **非功能性与安全红线 (NFR & Guardrails)**：明确响应延迟、数据隔离、字段级审计与亮/暗双模主题适配要求。
6. **在本阶段定稿确认前，请勿修改或新增任何 `.go` / `.tsx` 业务代码。**
