# code-traefik: 阶段二系统架构与数据模型设计 (SDD) 提示词指令

## 角色定位与工程原则
你是一名资深 Go 全栈系统架构师与底层网络协议专家。阶段一特性清单与需求边界（[`docs/01-features/01-feature-matrix.md`](../01-features/01-feature-matrix.md) v2.5.1）与协议规范（[`docs/07-references/01-cros-rpc-protocol-spec.md`](../07-references/01-cros-rpc-protocol-spec.md)）已冻结定稿。

开发过程**严禁跳过设计堆砌业务代码**，现在正式执行：
**「阶段二：系统架构与数据模型设计 (02-design)」**。

---

## 1. 输入基线与输入约束 (Inputs as SSOT)
设计必须严格以以下已冻结文档为唯一真实源（SSOT），**无需在提示词或设计文档中重复搬运业务需求，直接聚焦技术架构与实现深度**：
1. **特性与边界**：[`docs/01-features/01-feature-matrix.md`](../01-features/01-feature-matrix.md)（严格贯彻 §2.1 关键业务假设 A-01~A-08、§3 P0 核心特性清单及 Non-Goals）。
2. **协议字节规范**：[`docs/07-references/01-cros-rpc-protocol-spec.md`](../07-references/01-cros-rpc-protocol-spec.md)（严格遵循 24B 公共头、会话头、扩展头字节偏移及字段回显规范）。

---

## 2. 阶段二交付物结构体系（双子系统拆解与五大专篇设计）
为了彻底解耦**管理面（`code-traefik-server`）**与**数据面（`code-traefik-agent`）**两大独立物理子系统，并清晰治理核心业务领域（架构元素管理、CROS RPC 协议栈、数据持久化），阶段二采用**高内聚低耦合的五篇专篇矩阵**进行输出：

| 文档编号与路径 | 篇目类型 | 归属子系统 / 核心职责与设计边界 |
| :--- | :--- | :--- |
| `docs/02-design/01-system-architecture.md` | **【总览篇】系统总体架构与主从协同** | 全局视角：微前端双模运行、主控与 Agent 物理边界、HTTP REST + WebSocket 信道协议规范、三大核心业务全链路时序图 |
| `docs/02-design/02-data-plane-and-cros-rpc-engine.md` | **【数据面篇】CROS RPC 协议栈与 Agent 引擎** | 数据面子系统 (`cmd/agent`) 与共享协议库 (`pkg/crosrpc`)：分帧状态机、eBPF 采集引擎 (`pkg/ebpf`)、纯二进制 Mock 匹配与自愈重写 (`pkg/replay`) |
| `docs/02-design/03-architecture-elements-and-topology.md` | **【业务篇】架构元素建模与拓扑一致性验证** | 业务核心资产 (SSOT)：三级架构模型树、模块接口供给/依赖契约、1 进程包含 N 模块物理映射、运行态报文穿透定位与架构一致性比对引擎 |
| `docs/02-design/04-control-plane-and-web-console.md` | **【管理面篇】Server 控制面、微前端控制台与回归断言** | 管理面子系统 (`cmd/server`) 与微前端：任务协同编排器、Protobuf 解码中心与四段解剖抽屉 DTO、双层回归 Diff 断言引擎、四大工作台组件树、RBAC 与审计 |
| `docs/02-design/05-database-schema.md` | **【数据模型篇】数据库物理模型与 GORM 建模字典** | 持久化基石：全部 11 张 `traefik_*` 表的 GORM Go struct 代码定义、字段类型、GORM Tag、联合唯一索引、外键级联与安全约束 |

---

## 3. 各专篇详细设计指令与深度要求

### 3.1 `01-system-architecture.md`：系统总体架构与主从协同
1. **双模微前端运行拓扑**：CodeBench 门户嵌入（网关路由剥离、JWT 拦截透传）与独立开发调试双模运行拓扑；
2. **主从双子系统物理架构**：`cmd/server`（无特权控制中心）与 `cmd/agent`（业务宿主机轻量守护进程，1 Agent : N 进程，零数据库、零 `.pb`）的部署边界与职责切分；
3. **主从管控通信信道协议**：
   - HTTP REST：探活、进程探查、多进程监控元信息下发、重放任务调度接口定义；
   - WebSocket 二进制流：带进程标签信封头（`session_id`、`process_id`、`pid`、`direction`）的原始帧回传协议；
   - 健壮性保障：Agent 有界背压缓冲（默认 64MB）、丢帧计数上报、断线重连与主控失联宽限期（5分钟）探针自洁退出流程；
4. **三大端到端全链路 Mermaid 时序图**：
   - 多进程协同录制全链路时序（主控下发 -> 业务重连 -> Agent 采集推送 -> 主控解码与穿透定位）；
   - 隔离自愈重放全链路时序（重放资产编排 -> Agent 注入 -> 内核拦截 -> 本地 Mock 覆写 -> 收集结果 -> 主控双层 Diff 断言）；
   - Agent 纳管、心跳探活与探针生命周期时序。

### 3.2 `02-data-plane-and-cros-rpc-engine.md`：CROS RPC 协议栈与 Agent 引擎
1. **共享协议栈 `pkg/crosrpc`**：
   - 24B `RPC_COMM_HEAD_S` 公共头解析，`"CSE.TCP"` 魔术字流式滑动重同步状态机；
   - 多版本兼容会话头（V00~V05/V04）与扩展头（TraceKey/TimeStamp）的长度驱动解析器设计；
   - 粘包、半包碎片缓冲管理与超限大包安全丢弃设计；
2. **Agent 嵌入式 eBPF 采集引擎 (`pkg/ebpf`)**：
   - 入站被动旁路复制：基于「目标 PID + 监听端口」的内核 ring buffer 数据复制机制（不经代理、零时延损耗）；
   - 出站透明重定向：`cgroup/connect4` 拦截重定向到 Agent 本地代理机制；
   - BPF Hash Map 多 PID 注册与热更新、`monitor_pid` 自环豁免（`stat_bypassed_self`）；
   - 探针挂载、安全自洁卸载与 `CAP_BPF` / `CAP_NET_ADMIN` 权限要求；
3. **零 `.pb` 纯二进制 Mock 匹配与自愈 (`pkg/replay`)**：
   - 匹配键算法：`(ascServiceName, ascOperation)` + Payload 原始切片哈希，同接口顺序兜底；
   - 未命中处理：严禁转发至真实下游，丢弃不应答并记录 `MOCK_MISS` 明细；
   - 头部二进制自愈覆写算法：应答公共头 `ullSNO`（偏移 `8..15`）、`RPC_RSP_HEAD_S` 偏移 `8..23` 回显 `aucTraceBuff`、偏移 `32..39` 回显 `ullClientSendTime` 的内存切片改写方案；多段 `RSP_SEG` 逐片改写方案。

### 3.3 `03-architecture-elements-and-topology.md`：架构元素建模与拓扑验证
1. **三级逻辑架构体系**：子系统 -> 功能组（自嵌套树，`parent_id` + `level` + `path` 编码体系、移动节点时的递归防成环检测算法）-> 功能模块（叶子业务单元）；
2. **模块级接口供给与依赖契约**：
   - 供给接口 (`PROVIDE`)：全局唯一约束，服务名格式为 `{package}-{serviceName}`（package 原样保留点分结构，以中划线连接）；
   - 依赖接口 (`DEPEND`)：声明下游调用的服务名与方法；
3. **物理部署映射**：1 个操作系统进程承载多个功能模块、单监听端口互斥、`cmd_pattern` 反查实时 PID；
4. **静态拓扑自动推导**：模块间依赖边匹配推导算法、未登记外部系统占位边、同进程内模块互调（标记为「进程内不可观测」）；
5. **动态报文穿透与架构一致性比对引擎**：
   - 目的模块定位：通过报文会话头 `ascServiceName:ascOperation` 穿透物理进程定位唯一提供模块；
   - 来源模块判定：三级判定策略（出站 Trace 关联父 Span 模块 -> 进程内唯一 DEPEND 声明模块 -> 只能归属至进程级）；
   - 结果分类算法：`Verified`（合规）、`Violation`（越权/未声明调用，架构漂移告警）、`Uncovered`（未触发）、`Not Observable`（进程内互调）、`Unattributed`（来源只能归到进程级）。

### 3.4 `04-control-plane-and-web-console.md`：Server 控制面、微前端控制台与回归断言
1. **Server 核心服务与调度**：多进程协同录制批次编排器、Agent 连接池管理、重放任务调度器；
2. **IDL 描述集引擎与动态编解码**：
   - `.pb` 描述集文件注册、合法性校验、多版本管理与切换；
   - 基于 `dynamicpb` 按 `{package}-{serviceName}` 映射执行 Payload JSON 动态反序列化与序列化；
3. **双层回归 Diff 断言引擎**：
   - 第一层协议头级比对：状态码 `uiStatusCode` / `ullStatusCode`、消息类型、Payload 字节比对；
   - 第二层 Protobuf 语义字段级 Diff 算法：服务端时间戳固定豁免规则、忽略字段配置解析、map/repeated 顺序语义处理；
4. **微前端架构与组件树**：
   - 模块联邦路由表与导出契约（`./App`、`./menu`）；
   - 四大核心工作台（架构拓扑台、接口文档台、流量解剖台、重放断言台）的 React 组件分层树；
   - 流量解剖右侧抽屉 DTO：分段呈现 ① 24B公共头位图、② 会话头明细、③ 扩展头 Trace、④ Protobuf JSON 对比 Hex 的前后端数据契约；
5. **权限与审计治理**：`commonAuth`（普通认证用户 vs `traefik_admin` 管理员权限矩阵）与 `commonAudit` 字段级 Diff 审计接入点。

### 3.5 `05-database-schema.md`：数据库物理模型与 GORM 建模字典
1. **表前缀与隔离规范**：全量表强制以 `traefik_` 开头，模型显式实现 `TableName() string`；
2. **11 张核心数据表 GORM Struct 代码定义与 DDL 字典**：
   - 架构元素：`traefik_subsystems`、`traefik_functional_groups`、`traefik_modules`；
   - 接口契约：`traefik_module_interfaces`；
   - 实体部署：`traefik_agents`、`traefik_processes`；
   - IDL 描述：`traefik_proto_schemas`、`traefik_proto_operations`；
   - 录制与流量：`traefik_recording_sessions`、`traefik_traffic_records`；
   - 回放断言：`traefik_replay_tasks`；
3. **字段与索引规范**：提供包含完整 GORM Tag、字段注释、主键、联合唯一索引（如 `UK(agent_id, listen_port)`、`UK(service_name, operation)` 等）、外键或逻辑外键关联的 Go 结构体代码及安全级联保护逻辑。

---

## 4. 阶段二工程质量门禁与实施红线
1. **SSOT 一致性**：严禁引入与阶段一特性矩阵（`01-feature-matrix.md`）或 CROS RPC 协议规范冲突的自定义设计；
2. **代码纯粹性**：本阶段**严禁新增或修改任何工程落地实现代码（`.go` 或 `.tsx`）**，所有技术细节仅在 `docs/02-design/` 设计文档中以标准 Markdown、Go Struct 声明、算法伪代码或 Mermaid 图呈现；
3. **文档与编号规范**：严格遵守 `01-` ~ `05-` 的双位编号治理规则；设计完成后必须同步更新 `docs/README.md` 中的知识库索引目录。
