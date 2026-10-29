# code-traefik: 阶段二系统架构与数据模型设计 (SDD) 提示词指令

## 角色定位与工程原则
你是一名资深 Go 全栈系统架构师与网络协议专家。阶段一特性清单与需求边界（[`docs/01-features/01-feature-matrix.md`](../01-features/01-feature-matrix.md) v2.5.1）与协议规范（[`docs/07-references/01-cros-rpc-protocol-spec.md`](../07-references/01-cros-rpc-protocol-spec.md)）已冻结定稿。

开发过程**严禁跳过设计堆砌业务代码**，现在正式执行：
**「阶段二：系统架构与数据模型设计 (02-design)」**。

---

## 1. 输入基线与输入约束 (Inputs as SSOT)
设计必须严格以以下已冻结文档为唯一真实源（SSOT），**无需在提示词或设计文档中重复搬运业务需求，直接聚焦技术架构与实现深度**：
1. **特性与边界**：[`docs/01-features/01-feature-matrix.md`](../01-features/01-feature-matrix.md)（严格贯彻 §2.1 关键业务假设 A-01~A-08、§3 P0 核心特性清单及 Non-Goals）。
2. **协议字节规范**：[`docs/07-references/01-cros-rpc-protocol-spec.md`](../07-references/01-cros-rpc-protocol-spec.md)（严格遵循 24B 公共头、会话头、扩展头字节偏移及字段回显规范）。

---

## 2. 阶段二交付物规范 (`docs/02-design/01-system-architecture.md`)
请全面重构并细化主设计文档 `docs/02-design/01-system-architecture.md`，输出包含以下六大维度的生产级架构设计：

### 2.1 系统总体拓扑与双二进制边界 (System Topology & Dual-Binary)
- **部署架构**：微前端嵌入/独立双模运行架构图，以及主控 `code-traefik-server` 与业务机轻量代理 `code-traefik-agent` 的物理拓扑；
- **管控通信信道**：设计主控与 Agent 之间的通信协议规范（HTTP REST 指令接口定义 + WebSocket 二进制报文流契约），明确连接鉴权（预留）、心跳保活、有界背压缓冲与失联宽限期（5分钟）探针自洁退出流程。

### 2.2 数据库物理模型与 GORM 建模 (Physical Database Schema)
在阶段一概念模型基础上，输出生产级 GORM Go struct 代码定义与 DDL 字典（强制 `traefik_` 表前缀）：
- **架构元素模型**：`traefik_subsystems`、`traefik_functional_groups`（自嵌套树，带 `path`/`level` 级联与防成环设计）、`traefik_modules`；
- **接口契约模型**：`traefik_module_interfaces`（`PROVIDE` 全局唯一约束，服务名格式 `{package}-{serviceName}` 点分结构保持）；
- **物理与实体模型**：`traefik_agents`（实例纳管与能力）、`traefik_processes`（1 进程多模块，监听端口互斥）；
- **IDL 与流量模型**：`traefik_proto_schemas`、`traefik_proto_operations`、`traefik_recording_sessions`、`traefik_traffic_records`、`traefik_replay_tasks`；
- 明确所有字段的类型、长度、联合唯一索引（`UniqueIndex`）、外键约束及级联删除安全保护逻辑。

### 2.3 CROS RPC 协议栈与分帧解码架构 (Protocol Engine & Codec)
- **共享库 `pkg/crosrpc`**：分帧状态机设计（`"CSE.TCP"` 滑动重同步、大端解析、半包/粘包缓冲管理）、会话头（V00~V05/V04）与扩展头长度驱动解析器设计；
- **主控动态解码**：基于 `dynamicpb` 按 `{package}-{serviceName}` 和 `ascOperation` 反射解码设计；
- **解剖抽屉 DTO**：定义微前端右侧抽屉展示四段报文（24B公共头位图、会话头、扩展头、明文/Hex对比）的前后端数据契约。

### 2.4 Agent 本地采集与零 `.pb` 重放自愈引擎 (Agent eBPF & Replay Engine)
- **eBPF 采集引擎 (`pkg/ebpf`)**：入站被动旁路复制（监听端口过滤）与出站 `cgroup/connect4` 透明重定向代理机制；BPF Hash Map 多 PID 热更新与 Agent PID 自环豁免（`monitor_pid`）；探针安全卸载与生命周期；
- **零 `.pb` 二进制 Mock 与自愈 (`pkg/replay`)**：Mock 匹配键哈希算法、顺序兜底与未命中丢弃机制；应答头二进制自愈覆写（`ullSNO`、`aucTraceBuff`、`ullClientSendTime`）在内存字节切片上的具体实施方案。

### 2.5 核心业务交互时序图 (Core Sequence Diagrams)
必须绘制标准的 Mermaid 时序图，刻画全链路交互：
1. **多进程协同录制时序**：主控下发元信息 -> 业务进程重启/重连 -> Agent 采集打标与 WebSocket 推送 -> 主控解码与模块穿透定位；
2. **隔离重放与自愈 Mock 时序**：重放资产下发 -> Agent 请求注入 -> 出站调用内核拦截 -> 本地 Mock 覆写自愈应答 -> 收集结果 -> 主控双层 Diff 断言；
3. **Agent 纳管与探针自洁时序**：注册、心跳、网络中断重连及主控失联超时自洁卸载。

### 2.6 微前端视图与组件设计 (Frontend Architecture)
- 路由表划分与微前端导出契约（`./App`、`./menu`）；
- 四大核心操作台（架构拓扑、接口文档、流量解剖、重放 Diff）的组件分层；
- 遵循工作区规章：`theme.css` 的 `--color-*` 语义 Token、标准 5 页 URL 滑动窗口分页控件。

---

## 3. 设计阶段质量门禁与红线
1. **SSOT 一致性**：严禁引入与阶段一特性矩阵或 CROS RPC 协议规范相冲突的自定义设计；
2. **代码纯粹性**：本阶段**严禁新增或修改任何 `.go` / `.tsx` 业务实现代码**，所有技术细节仅在 Markdown 设计文档中以伪代码、Go Struct 声明或架构图呈现；
3. **文档与目录规范**：主设计文档统一采用 `01-system-architecture.md`，若需补充关键决策记录，请在 `docs/05-adr/` 下按双位编号追加。
