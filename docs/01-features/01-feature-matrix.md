# 业务流量监控与重放 (`code-traefik`) 特性清单与需求范围界定规范 (Feature Matrix & Scope)

- **文档编号**：`01-feature-matrix.md`
- **文档版本**：v2.5.0
- **更新日期**：2026-10-28
- **当前阶段**：阶段一：特性清单与边界明确
- **状态**：定稿待审 (Ready for Review)
- **协议依据**：《DOPHI CROS RPC 协议规范》（文档编号 / 版本：`TODO` 待补充；本文所有字段名、偏移与取值均以该规范原文为准）

### 修订记录

| 版本 | 日期 | 修订要点 |
| :--- | :--- | :--- |
| v2.0.0 | 2026-10-27 | 重构为主控 + Agent 分离、架构拓扑驱动模型 |
| v2.1.0 | 2026-10-27 | 按 CROS RPC 协议规范收敛协议边界 |
| v2.2.0 | 2026-10-28 | 剥离 Agent 侧 `.pb` 依赖，收敛为纯二进制收集与重放 |
| v2.3.0 | 2026-10-28 | 支持单 Agent 多进程并发监控与任务级协同录制 |
| v2.4.0 | 2026-10-28 | 明确架构元素 SSOT、三级模型、模块级接口契约与多模块进程映射 |
| v2.5.0 | 2026-10-28 | 依据阶段一出口检视修订：新增「关键业务假设」；补充入站旁路采集与长连接前置条件；新增 Agent 实例纳管；PROVIDE 全局唯一与来源模块判定规则；重放结果状态与 Mock 未命中策略；`.pb` 版本管理；信道收敛为 HTTP + WebSocket；重排 P0/P1/P2；补充性能、存储保留与 eBPF 测试策略 NFR；补充术语表 |

---

## 1. 系统愿景与产品定位 (Vision & Positioning)

### 1.1 项目愿景
**业务流量监控与重放 (`code-traefik`)** 是挂载于 CodeBench 开发者综合工作台的业务子系统，**只服务于业务实际使用的 DOPHI `CROS RPC` 协议**（`24B 公共消息头 RPC_COMM_HEAD_S` + `会话头 RPC_REQ/RSP/MNG_HEAD_S` + `0..N 段扩展头` + `序列化 Payload`），不做任何其他协议扩展。

目标用户与痛点：

| 角色 | 痛点 | 本系统提供的能力 |
| :--- | :--- | :--- |
| 业务开发 / 测试工程师 | 联调与回归依赖完整下游环境，问题难以复现 | 录制一次真实业务任务，下游关闭后在测试环境离线重放并做字段级 Diff |
| 架构师 / 模块负责人 | 架构设计中的模块依赖与实际调用逐渐漂移，无从核对 | 维护三级架构元素与模块接口契约，用实测流量验证架构一致性 |
| 平台管理员 | 抓包工具需要业务进程改造或全机特权，风险大 | 每台机器 1 个零 Schema 依赖的 Agent，只作用于指定进程 |

系统采用 **「控制面主框架 (`code-traefik-server`) + 独立数据面代理 (`code-traefik-agent`)」同仓双二进制**架构：

1. **控制面 `code-traefik-server`**（保持在 `code-bench` 微前端家族内）：作为架构元素的单一真实源（SSOT，选型依据见 §2.2 与 Non-Goal 7），负责三级架构元素与接口契约建模、Agent 纳管、多进程协同录制编排、`.pb` 解码、架构一致性验证、重放编排与回归 Diff 报告。
2. **数据面 `code-traefik-agent`**（同仓 `cmd/agent`）：零前端、零数据库、零 `.pb` 依赖的守护进程，每台业务机器部署 1 个，同时监控本机多个目标进程（`1 Agent : N Processes`）；负责报文采集、原始二进制回传、本地重放注入、下游 Mock 与 `ullSNO` 自愈。

### 1.2 核心价值主张 (Core Value Propositions)
1. **单机多进程、零 Schema 的轻量 Agent**：同仓构建主控与 Agent；Agent 不感知 `.pb`，免除跨机同步 Schema 的负担。
2. **架构元素闭环与模块级一致性验证**：自主维护「子系统 → 功能组（树）→ 功能模块」与「1 进程 : N 模块」映射，以功能模块为接口契约主体；用实测报文的 `(ascServiceName, ascOperation)` 定位功能模块，还原跨进程调用关系并与声明的依赖比对。
3. **原生契合 CROS RPC**：原生解析 `"CSE.TCP" + ucProtocol` 魔术字、大端 `ullSNO`/`uiLength`/`usHeadLen` 分帧、多版本会话头与扩展头（Trace 信息、时间戳）。
4. **嵌入式 `cilium/ebpf` 引擎**：Agent 内挂载单套探针，入站被动旁路复制、出站透明重定向，基于 BPF Hash Map 做多 PID `O(1)` 过滤，Agent 自身 PID 内核态豁免防自环。
5. **零 `.pb` 的二进制重放与 `ullSNO` 自愈**：利用「每次运行都会变化的协议字段全部位于 `24 + usHeadLen` 之前」这一结构特征，Agent 以 `(ascServiceName, ascOperation)` + Payload 字节摘要匹配 Mock 并覆写 `ullSNO`；字段级回归 Diff 由主控完成。

---

## 2. 关键业务假设、系统拓扑与数据模型

### 2.1 关键业务假设 (Key Assumptions，已与业务方确认)

以下假设是本版需求边界的前提，任一假设被推翻都需要重新评审相关特性：

| 编号 | 假设 | 影响的特性 |
| :--- | :--- | :--- |
| **A-01** | 业务进程之间普遍使用 **CROS RPC 长连接**；录制 / 重放前**可以接受重启目标进程或触发重连** | F-08、F-09、F-11、F-12 |
| **A-02** | 每个操作系统进程**只监听 1 个** CROS RPC 端口 | F-02、F-08 |
| **A-03** | 每个功能模块**只部署 1 份**（只属于 1 个进程，不存在多副本 / 多机部署） | F-02、F-05 |
| **A-04** | 同一进程内的功能模块之间走**进程内函数调用**，不经过网络 | F-02、F-05 |
| **A-05** | 同一 `(ascServiceName, ascOperation)` **只由 1 个功能模块供给**（全局唯一） | F-02、F-03、F-05 |
| **A-06** | 重放**只在测试环境**执行；目标进程对 DB / MQ 等非 CROS RPC 依赖的副作用首期不处理（DB 中主要是很少变化的元数据） | F-12、Non-Goal 8 |
| **A-07** | `ascServiceName` / `ascOperation` 与 `.pb` 中的 Service / Method 名称**字符串直接相等**，不做任何转换 | F-03、F-10 |
| **A-08** | 首期聚焦 REQ / RSP 业务调用；MNG 管理消息（`ucMsgType=5/6/7`）只做解析展示与原样回放，不做握手语义仿真 | F-12、F-13、Non-Goal 9 |

### 2.2 主从运行架构与网络交互拓扑

```mermaid
flowchart TB
    subgraph Portal["CodeBench 微前端控制台 (/traefik | :5181 -> :8091)"]
        UI_Arch["架构元素管理与拓扑画布"]
        UI_Monitor["流量监控台与 CROS RPC 四段报文解剖抽屉"]
        UI_Replay["重放编排台与双层 Diff 报告"]
    end

    subgraph ControlPlane["code-traefik-server 控制面 (cmd/server :8091)"]
        Arch_Mgr["架构元素与模块接口契约 (SSOT)"]
        Agent_Hub["Agent 纳管与信道编排 (HTTP 下发 + WebSocket 接收)"]
        Decoder["pkg/crosrpc 分帧 + Protobuf 动态解码"]
        Topo_Eng["拓扑还原与架构一致性验证"]
        Diff_Eng["双层回归 Diff 断言"]
        Store[("PostgreSQL 共享库 (traefik_* 前缀)")]
    end

    subgraph DataPlane["业务机器: code-traefik-agent (1 Agent : N 进程 | 零 .pb)"]
        Agent_Srv["受控接入服务 (HTTP + WebSocket | IP 白名单)"]
        EBPF_Mgr["cilium/ebpf 探针管理 (多 PID Map / 自身 PID 豁免)"]
        Proxy["出站透明代理 / 重放注入 / 二进制 Mock (pkg/crosrpc 头解析)"]
    end

    subgraph BizHost["业务机器内核与目标进程"]
        Ingress_Hook["eBPF 入站旁路复制 (按 PID + 监听端口过滤)"]
        Connect_Hook["eBPF cgroup/connect4 出站重定向"]
        TargetProc["目标进程 1..N (各含 N 个功能模块, 单监听端口)"]
        Client["上游调用方"]
        Downstream["下游 CROS RPC 服务 (重放时关闭)"]
    end

    Portal == "REST API (JWT + RBAC) / SSE 实时推送" ==> ControlPlane
    Arch_Mgr --> Agent_Hub
    Agent_Hub --> Decoder --> Topo_Eng
    Decoder --> Diff_Eng
    ControlPlane --> Store

    Agent_Hub == "1. HTTP 下发监控元信息 / 重放包" ==> Agent_Srv
    Agent_Srv == "2. WebSocket 二进制帧回传带进程标签的原始报文" ==> Agent_Hub
    Agent_Srv --> EBPF_Mgr
    Agent_Srv --> Proxy
    EBPF_Mgr == "挂载探针 / 刷新 PID 与下游规则" ==> Ingress_Hook
    EBPF_Mgr ==> Connect_Hook

    Client -- "3a. 入站请求" --> TargetProc
    Ingress_Hook -. "3b. 被动复制入站收发字节" .-> EBPF_Mgr
    TargetProc -- "4a. 出站 connect()" --> Connect_Hook
    Connect_Hook -- "4b. 重定向至 Agent" --> Proxy
    Proxy -- "5a. [录制] 自身 PID 豁免后转发" --> Downstream
    Proxy -. "5b. [重放] 二进制 Mock 应答 + 覆写 ullSNO" .-> TargetProc
    Proxy -- "6. [重放] 向监听端口注入录制请求" --> TargetProc
```

采集路径说明：
- **入站 (INGRESS)**：eBPF 按「目标 PID + 监听端口」**被动复制**收发字节，不代理、不改变业务链路（钩子选型于阶段二 ADR 定稿）。
- **出站 (EGRESS)**：`cgroup/connect4` 把目标 PID 访问已登记下游 `IP:Port` 的连接重定向到 Agent，录制时由 Agent 透传到真实下游并记录，重放时由 Agent 用 Mock 应答。
- **长连接前置条件 (A-01)**：出站重定向只对**下发监控之后新建的连接**生效。标准录制流程为「下发监控 → 重启目标进程或触发重连 → 执行业务任务 → 停止录制」。

### 2.3 架构元素与运行映射数据模型

**SSOT 选型依据**：`code-proto` 专注 Protobuf 仓库管理、`.proto` 解析与接口变更 MR 追踪，没有「子系统-功能组-功能模块」概念，也没有运行态进程映射；`code-bench` 只管理用户、组织与审计。两者都不能作为架构元素的可靠来源，因此由 `code-traefik` 在自己的 `traefik_*` 表中闭环维护。

下图只描述实体关系与关键约束，完整字段定义在阶段二 `02-design` 中定稿：

```mermaid
erDiagram
    TRAEFIK_SUBSYSTEMS ||--o{ TRAEFIK_FUNCTIONAL_GROUPS : "包含"
    TRAEFIK_FUNCTIONAL_GROUPS ||--o{ TRAEFIK_FUNCTIONAL_GROUPS : "parent_id 自嵌套"
    TRAEFIK_FUNCTIONAL_GROUPS ||--o{ TRAEFIK_MODULES : "包含"
    TRAEFIK_MODULES ||--o{ TRAEFIK_MODULE_INTERFACES : "声明 PROVIDE / DEPEND"
    TRAEFIK_AGENTS ||--o{ TRAEFIK_PROCESSES : "纳管"
    TRAEFIK_PROCESSES ||--o{ TRAEFIK_MODULES : "承载 (1 : N)"
    TRAEFIK_PROTO_SCHEMAS ||--o{ TRAEFIK_PROTO_OPERATIONS : "解析出"
    TRAEFIK_RECORDING_SESSIONS ||--o{ TRAEFIK_TRAFFIC_RECORDS : "归档"
    TRAEFIK_PROCESSES ||--o{ TRAEFIK_TRAFFIC_RECORDS : "采集自"
    TRAEFIK_RECORDING_SESSIONS ||--o{ TRAEFIK_REPLAY_TASKS : "作为重放资产"

    TRAEFIK_MODULE_INTERFACES {
        bigint module_id FK
        string relation_type "PROVIDE | DEPEND"
        string service_name "ascServiceName"
        string operation "ascOperation"
    }
    TRAEFIK_PROCESSES {
        bigint agent_id FK
        string name "进程标识"
        int listen_port "UK(agent_id, listen_port)"
        string cmd_pattern "/proc 命令行匹配规则"
    }
    TRAEFIK_PROTO_OPERATIONS {
        bigint schema_id FK
        string service_name "与 Service 名字符串直接相等"
        string operation "与 Method 名字符串直接相等"
        string req_message
        string rsp_message
    }
```

关键约束：
1. **三级逻辑架构**：子系统是顶层边界；功能组归属子系统，支持多级自嵌套（`parent_id` + `path` + `level`）；功能模块是最小契约单元，可挂在**任意层级**的功能组下。
2. **接口契约在功能模块层面**：`PROVIDE` 为模块对外实现的 CROS RPC 接口，`DEPEND` 为模块需要调用的下游接口，均以 `(ascServiceName, ascOperation)` 字符串表示。**`PROVIDE` 的 `(service_name, operation)` 全局唯一 (A-05)**。
3. **物理部署**：1 个进程承载 N 个模块，每个模块只属于 1 个进程 (A-03)；进程只有 1 个监听端口 (A-02)，同一 Agent 下端口互斥；PID 不落库为权威数据，由 Agent 按端口或 `cmd_pattern` 实时反查。
4. **`.pb` 与接口解耦**：模块接口不直接外键引用 `.pb`，解码时按 `(service_name, operation)` 查找当前生效的映射；每条流量记录保存解码所用的 schema 版本。

---

## 3. 需求功能矩阵 (Feature Matrix)

特性优先级定义：
- **P0 (Must-Have)**：首期主链路「建模 → 采集 → 解码 → 验证 → 重放 → Diff」必需；每条验收指标都必须有对应的自动化测试（特权类用例的执行方式见 NFR 6）。
- **P1 (Should-Have)**：第二阶段规划，含安全加固与体验增强。
- **P2 (Nice-to-Have)**：远期演进。

---

### 3.1 架构元素、接口契约与拓扑领域

| 编号 | 特性名称 | 涉及组件 | 优先级 | 功能描述与技术规范 | TDD 验收指标 |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **F-01** | **三级架构元素建模**（`traefik_subsystems` / `traefik_functional_groups` / `traefik_modules`） | `Server`<br>`Frontend` | **P0** | 1. 子系统：编码（全局唯一）、名称、负责人、描述；<br>2. 功能组：归属子系统，支持多级自嵌套，维护 `parent_id`、`level`、`path`、排序；编码在同一子系统内唯一；最大深度 8 级；<br>3. 功能模块：编码全局唯一，挂在任意层级的功能组下，冗余记录所属子系统；<br>4. 移动功能组时级联更新子孙节点的 `path` / `level`。 | 1. 三类元素 CRUD 单测全部通过；<br>2. 把功能组移到自身或其子孙下返回 `400`；超过最大深度返回 `400`；<br>3. 移动功能组后，所有子孙的 `path` / `level` 正确；<br>4. 删除非空功能组、或删除已声明接口的模块返回 `409`；<br>5. 编码重复返回 `409`。 |
| **F-02** | **进程部署映射与模块接口契约**（`traefik_processes` / `traefik_module_interfaces`） | `Server`<br>`Frontend` | **P0** | 1. 进程：归属 1 个 Agent，维护名称、唯一监听端口 (A-02)、`/proc` 命令行匹配规则；每个模块绑定 0..1 个进程 (A-03)；<br>2. 模块接口：`PROVIDE` / `DEPEND` 两类，以 `(ascServiceName, ascOperation)` 字符串声明；<br>3. 约束：同一模块内 `(relation_type, service, operation)` 唯一；**`PROVIDE` 的 `(service, operation)` 全局唯一** (A-05)；<br>4. 静态依赖推导：模块 A 的 `DEPEND` 匹配到模块 B 的 `PROVIDE` 时生成边 `A → B`；匹配不到任何 `PROVIDE` 的 `DEPEND` 归入「外部 / 未登记系统」占位节点。 | 1. 同一 Agent 下监听端口重复返回 `409`；<br>2. 第二个模块声明已被供给的 `PROVIDE` 接口返回 `409`；<br>3. 静态依赖推导单测：正常边、外部占位边、同进程内两模块之间的边（标记为「进程内」）均正确；<br>4. 删除正在录制或重放中的进程返回 `409`。 |
| **F-03** | **Protobuf 描述集注册与接口映射**（`traefik_proto_schemas` / `traefik_proto_operations`） | `Server`<br>`Frontend` | **P0** | 1. 上传 `.pb`（`FileDescriptorSet`），校验体积 `<= 4MB` 与 `protodesc` 合法性；同名描述集每次上传生成新版本，可切换生效版本；<br>2. 从 `ServiceDescriptor` / `MethodDescriptor` 自动生成 `(ascServiceName, ascOperation) → (Req, Rsp Message)` 映射，名称**字符串直接相等、不做转换** (A-07)；<br>3. 生效映射中 `(service, operation)` 全局唯一，冲突时拒绝激活；<br>4. 流量记录保存解码所用的 schema 版本，切换版本不改写历史解码结果（可手动重新解码）。 | 1. 合法 `.pb` 上传后，映射接口返回正确的 `(service, operation)` 清单；<br>2. 损坏文件或超过 4MB 返回 `400`；<br>3. 激活与其他生效描述集存在 `(service, operation)` 冲突的版本返回 `409`；<br>4. 切换版本后，历史记录的 schema 版本字段保持不变。 |
| **F-04** | **静态架构拓扑视图** | `Server`<br>`Frontend` | **P0** | 1. 逻辑视角：按「子系统 → 功能组树 → 功能模块」展开，绘制 F-02 推导的模块依赖边（含外部占位节点）；<br>2. 物理视角：「Agent（主机）→ 进程 → 承载的模块」部署归属图。 | 1. 拓扑 API 对给定建模数据返回期望的节点与边集合；<br>2. 画布只使用 `--color-*` 语义 Token，适配亮 / 暗主题。 |
| **F-05** | **实测调用拓扑还原与架构一致性验证** | `Server`<br>`Frontend` | **P0** | 基于一次录制批次 (F-11) 的报文：<br>1. **目的模块定位**：按 `(ascServiceName, ascOperation)` 查唯一的 `PROVIDE` 模块；查不到时归为「未登记接口」；<br>2. **来源模块判定**（进程承载多个模块时）依次采用：① 出站请求的 `FatherSpanID` 关联到同进程某条入站请求所在的模块；② 该进程内**只有一个**模块声明了对应 `DEPEND`；③ 以上都不满足时，标记为「进程级归属」并给出候选模块集合；<br>3. **比对结果分类**：`Verified`（实测且已声明）、`Violation`（实测但未声明，架构漂移告警）、`Uncovered`（已声明但本次未触发）、`Not Observable`（同进程内两模块之间的依赖，A-04 决定网络上不可见，不计入 Uncovered）、`Unattributed`（来源只能归到进程级）。 | 1. 输入预置的上中下游三进程录制数据集（含 1 条未声明调用、1 条同进程内依赖、1 条无 Trace 且多模块声明同一 `DEPEND` 的调用），验证 API 分别标出 `Violation`、`Not Observable`、`Unattributed`，其余边为 `Verified` / `Uncovered`；<br>2. 每类结果都有独立的单测用例。 |

---

### 3.2 Agent 纳管与受控信道领域

| 编号 | 特性名称 | 涉及组件 | 优先级 | 功能描述与技术规范 | TDD 验收指标 |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **F-06** | **Agent 实例纳管**（`traefik_agents`） | `Server`<br>`Frontend` | **P0** | 1. 登记 Agent 名称、访问地址（`IP:Port`，唯一）、描述；<br>2. 主控周期性心跳探活（默认 10s），维护 `ONLINE` / `OFFLINE` 状态与最后心跳时间；<br>3. 握手时交换 Agent 版本与信道协议版本，协议主版本不一致时拒绝下发任务并提示；<br>4. 上报运行能力：是否具备 `CAP_BPF` + `CAP_NET_ADMIN`、内核版本。 | 1. Agent CRUD 单测通过，地址重复返回 `409`；<br>2. 停止 Mock Agent 后，在 3 个心跳周期内状态变为 `OFFLINE`；<br>3. 协议主版本不一致时，下发录制返回明确错误码；<br>4. 删除仍纳管进程的 Agent 返回 `409`。 |
| **F-07** | **主控—Agent 受控信道（HTTP + WebSocket）** | `Server`<br>`Agent` | **P0** | 1. 同仓双入口 `cmd/server` / `cmd/agent`；<br>2. **HTTP REST**：主控向 Agent 下发心跳、进程探查、监控元信息、录制启停、重放任务；<br>3. **WebSocket（二进制帧）**：主控连接 Agent，Agent 推送原始报文事件与重放结果；首期不实现 Agent 侧 SSE；<br>4. **背压与弹性**：Agent 为每个录制会话设置有界缓冲（默认 64MB），缓冲满时丢弃新帧并累计 `dropped_frames` 上报；信道断开后主控自动重连；主控失联超过宽限期（默认 5 分钟）时，Agent 自动停止全部监控并卸载探针；<br>5. **首期连接控制**：不启用 Token，源 IP 白名单中间件（`--allow-ips`，默认只放行 `127.0.0.1` 与配置的主控 IP），预留 `TODO(security)` 鉴权扩展位。 | 1. 本地集成测试（监听 `127.0.0.1`）中，主控通过 HTTP 下发并建立 WebSocket，完整收到 Agent 事件；<br>2. 非白名单源 IP 访问 Agent 返回 `403`；<br>3. 模拟主控消费阻塞，`dropped_frames` 正确累加且 Agent 内存不超过缓冲上限；<br>4. 断开信道后主控自动重连成功；超过宽限期后 Agent 探针已卸载。 |
| **F-08** | **多进程监控元信息供给、目标定位与存量连接诊断** | `Server`<br>`Agent` | **P0** | 1. 主控按 Agent 聚合下发 `[]ProcessMonitorSpec`：进程 ID、监听端口、`cmd_pattern`、下游 `IP:Port` 列表（不下发 `.pb`）；<br>2. Agent 通过 `/proc` 按监听端口或命令行反查实时 PID；录制期间每 2s 复核一次，PID 变化（进程重启）时自动更新 BPF Map 并上报 `PROCESS_RESTARTED` 事件；<br>3. **存量连接诊断 (A-01)**：Agent 检测目标进程在监控开始前已经建立、指向已登记下游的连接，上报数量并提示「需重启目标进程或触发重连」；<br>4. 支持单独增删某个进程的监控，不影响其他进程；<br>5. 支持主控请求 Agent 探查候选进程列表（PID、命令行、监听端口）。 | 1. 向同一 Agent 下发 3 个进程，均正确定位 PID 并进入录制；停止其中 1 个，其余 2 个持续采集；<br>2. 录制期间重启某目标进程，Agent 上报 `PROCESS_RESTARTED`，新 PID 的流量继续被采集；<br>3. 预先建立长连接后再下发监控，Agent 上报存量连接数 ≥ 1；<br>4. 监听端口不存在时，上报该进程的 `NOT_FOUND` 诊断状态。 |

---

### 3.3 Agent 采集引擎与 CROS RPC 协议栈领域

| 编号 | 特性名称 | 涉及组件 | 优先级 | 功能描述与技术规范 | TDD 验收指标 |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **F-09** | **Agent 嵌入式 `cilium/ebpf` 采集引擎**（`pkg/ebpf`） | `Agent` | **P0** | 基于 `github.com/cilium/ebpf` 在 Agent 内挂载单套探针，服务本机多个目标进程：<br>1. **多 PID 过滤**：BPF Hash Map 保存目标 PID 集合、监听端口与下游规则，`O(1)` 查找，支持运行期热增删；未命中的进程与连接原样放行；<br>2. **入站被动旁路复制**：对「目标 PID + 监听端口」上的连接复制收发字节到用户态（ring buffer），不代理、不改变入站链路；<br>3. **出站透明重定向**：`cgroup/connect4` 把目标 PID 访问已登记下游 `IP:Port` 的新建连接重定向到 Agent 本地代理，并保留原始目的地址；<br>4. **自环豁免**：Agent 自身 PID 写入 Map，内核态识别后直接放行并累计 `stat_bypassed_self`，保证同机被监控进程互调（`PID_A → PID_B`）不死锁，并同时录到 A 的出站与 B 的入站；<br>5. **特权要求与自洁**：必须具备 `CAP_BPF` + `CAP_NET_ADMIN`，否则拒绝开始录制并上报原因；全部监控停止或 Agent 退出（含 `SIGTERM`）时卸载全部钩子。 | 1. 同机运行 `PID_A`、`PID_B`（`A → B` 调用）与无关进程 `PID_C`：A 的出站、B 的入站都被录到，不死锁，`PID_C` 无任何重定向；<br>2. `stat_bypassed_self` 正确累加；<br>3. 无特权环境下开始录制返回明确错误；<br>4. 监控停止后 `Attached == false`，Agent 收到 `SIGTERM` 后无残留钩子。 |
| **F-10** | **CROS RPC 分帧与头解析库（`pkg/crosrpc`）及主控 Protobuf 解码** | `Server`<br>`Agent` | **P0** | 1. **共享分帧与头解析库**（Agent 与主控共用，零 `.pb` 依赖）：<br>   - 公共头：识别 8B `unMagicInfo`（7B `"CSE.TCP"` + 1B `ucProtocol`），遇到前序脏字节时逐字节扫描重新同步；大端解析 `ullSNO`、`uiLength`、`usHeadLen`、`ucMsgType`、`isPacked`；校验 `usHeadLen >= 会话头最小长度`、`usHeadLen <= uiLength <= RPC_PAYLOAD_MAX_LEN (32MB - 4KB) + usHeadLen`、`ucMsgType ∈ [2, RPC_MSG_TYPE_MNG_BUTT)`；正确处理粘包与半包；<br>   - 会话头与扩展头：按 `#pragma pack(4)` 与 `usHeadLen` 长度判定版本（前 / 后向兼容），解析 `RPC_REQ_HEAD_S`（V00~V05：`ascServiceName[64]`、`ascOperation[64]`、`ucReqType` 等）、`RPC_RSP_HEAD_S`（V00~V04：`uiStatusCode` / `ullStatusCode`、收发时间戳、`usRspMsgNo`）、`RPC_MNG_HEAD_S`，以及 `0..N` 段扩展头（`TraceID` / `SpanID` / `FatherSpanID`、入队时间戳）；<br>2. **主控 Protobuf 解码**（只在主控引入 `dynamicpb`）：按 F-03 生效映射，对 `24 + usHeadLen` 之后的 Payload 做 JSON 解码与重编码；无映射或不可解码时保留完整头解析并以 Raw Hex 展示，不中断链路。 | 1. 单测覆盖：前序脏字节重新同步、逐字节碎片流、多包粘连流、V01 与 V05/V04 会话头、扩展头解析；<br>2. 非法 `ucMsgType`、超限 `uiLength`、`usHeadLen > uiLength`、`usHeadLen` 小于最小长度的报文全部被拒绝，无 panic、无超量分配（fuzz 测试运行 ≥ 60s 无崩溃）；<br>3. `cmd/agent` 的依赖图中不包含 `dynamicpb` / `protoregistry`；<br>4. 无 `.pb` 映射的报文解码结果为 Raw Hex，头字段完整。 |

---

### 3.4 录制、重放与回归断言领域

| 编号 | 特性名称 | 涉及组件 | 优先级 | 功能描述与技术规范 | TDD 验收指标 |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **F-11** | **任务级多进程协同录制与落盘**（`traefik_recording_sessions` / `traefik_traffic_records`） | `Server`<br>`Agent`<br>`Frontend` | **P0** | 面向「单次执行一个业务任务、同时录制链路上多个进程」的场景：<br>1. **录制批次**：选择同机或跨机的多个进程创建批次，按 A-01 引导用户在开始后重启或重连目标进程；批次有最长时长（默认 30 分钟）与最大帧数（默认 10 万帧）上限，达到上限自动停止；<br>2. **采集与回传**：各 Agent 采集参与进程的 `INGRESS` / `EGRESS` 原始报文，信封头携带 `session_id`、`process_id`、`pid`、`direction`、源 / 目地址、采集时间戳；<br>3. **解析与关联**：主控用 `pkg/crosrpc` 解析，同一连接内按 `ullSNO` 配对 REQ / RSP（兼容 `ucReqType` Oneway 与 `RSP_SEG` 分片），按 F-03 解码 Payload，按 F-05 定位模块；<br>4. **落盘**：原始帧与解析结果按 `session_id` + `process_id` 落盘；单帧超过采集阈值（默认 4MB）时截断保存并标记为「不可重放」。 | 1. 两进程上下游录制批次触发 1 次业务请求：两进程的入站 / 出站帧都归档到同一 `session_id` 下各自的 `process_id`，REQ / RSP 配对正确；<br>2. 支持按批次、子系统、模块、进程、`ascServiceName`、`ascOperation`、`ullSNO`、`TraceID` 筛选，并使用 URL 同步分页；<br>3. 达到时长或帧数上限时批次自动停止，状态为 `COMPLETED_BY_LIMIT`；<br>4. 超阈值帧被标记为不可重放。 |
| **F-12** | **重放编排与 Agent 本地执行**（`traefik_replay_tasks`） | `Server`<br>`Agent`<br>`Frontend` | **P0** | 在测试环境 (A-06)、下游已关闭或隔离的前提下：<br>1. **重放资产**：从录制批次中选定 1 个目标进程及其 1..N 条 `INGRESS` 请求，主控组装原始请求帧 + 该进程录制期间的 `EGRESS` 应答帧集合 + 监听端口 + 下游 `IP:Port`，下发给目标 Agent（不下发 `.pb`）；<br>2. **执行**：Agent 为下游规则挂载出站重定向，提示重启或重连目标进程 (A-01)，然后向监听端口建立连接并按录制顺序写入请求帧（若录制中存在该连接的 MNG 帧则原样发送，A-08）；<br>3. **回收**：Agent 回传目标进程的实际应答（单包 `RSP` 或多段 `RSP_SEG`）与 Mock 命中 / 未命中明细；<br>4. **结果状态**：`PASS`、`FAILED`（Diff 不一致）、`MOCK_MISS`（存在未命中的下游调用）、`TIMEOUT`（单请求等待应答超时，默认 30s，可配置）、`ERROR`（连接失败或 Agent 异常）；多种情况同时出现时按 `ERROR > TIMEOUT > MOCK_MISS > FAILED > PASS` 取最高者，并保留全部明细。 | 1. 关闭模拟下游后发起重放：Agent 完成请求注入，下游调用全部命中 Mock，结果为 `PASS`；<br>2. 目标端口不可达时结果为 `ERROR`；<br>3. 目标进程不应答时，在超时阈值后结果为 `TIMEOUT`；<br>4. 多请求重放时，每条请求都有独立的结果明细。 |
| **F-13** | **Agent 零 `.pb` 二进制 Mock 匹配与 `ullSNO` 自愈**（`pkg/replay`） | `Agent` | **P0** | 1. **匹配键**：下游请求会话头中的 `(ascServiceName, ascOperation)` + `24 + usHeadLen` 之后 Payload 字节的哈希；由此天然忽略公共头 `ullSNO`、会话头时间戳与扩展头 Trace 等每次运行都会变化的字段；<br>2. **兜底匹配**：Payload 哈希未命中时，按同一接口的录制调用次序取下一条未使用的应答；<br>3. **未命中策略**：同一接口的录制应答已用完或不存在时，**不转发到真实下游、不返回应答**，记录 `MOCK_MISS` 明细（接口、Payload 摘要、时间），由目标进程自身的超时逻辑处理；<br>4. **自愈改写**：命中后在应答帧公共头偏移 `8..15` 覆写当前请求的大端 `ullSNO`；会话头中需要与请求对应的回显字段（如 `aucTraceBuff`、`ullClientSendTime`），偏移按 `usHeadLen` 判定的会话头版本确定后回填（字段清单以协议原文为准，阶段二固化）；`RSP_SEG` 分片逐片改写；<br>5. **Oneway**：`ucReqType` 为 Oneway 的请求只记录、不应答；<br>6. **MNG 帧** (A-08)：按 `ucMsgType` 取录制中的同类应答原样回放并覆写 `ullSNO`，没有则忽略。 | 1. 目标进程重放时生成全新的随机 `ullSNO`（如 `0x000000010009A2B3`）、新 TraceID 与新时间戳，Agent 仍命中 Mock，应答帧中的 `ullSNO` 与请求一致；<br>2. 同接口 Payload 含动态参数时按调用次序兜底命中；<br>3. 调用次数超过录制次数时产生 `MOCK_MISS` 明细，且没有任何流量流向真实下游；<br>4. 3 段 `RSP_SEG` 分片全部被正确改写。 |
| **F-14** | **协议头级 + Protobuf 字段级双层回归 Diff** | `Server`<br>`Frontend` | **P0** | 把 Agent 回传的实际应答（`Actual`）与录制基线（`Expected`）比对：<br>1. **第一层（协议头）**：比对 `ucMsgType`、`uiStatusCode` / `ullStatusCode`、`usRspMsgNo`、`isPacked` 与 Payload 原始字节；<br>2. **第二层（Protobuf 字段）**：Payload 字节不同且有 `.pb` 映射时，解码为 JSON 树逐字段计算 `[]FieldDiff{FieldPath, Expected, Actual}`；没有 `.pb` 映射时只给出字节级 Diff；<br>3. **固定豁免**：会话头 / 扩展头中的服务端时间戳（`ullServerRecvTime`、`ullServerSendTime`、`ullServerEnqueueTime`）不参与比对；<br>4. **忽略规则**：可按接口 `(service, operation)` 配置忽略的字段路径，重放任务可临时追加；<br>5. **顺序语义**：`repeated` 字段按顺序比对，`map` 字段不考虑顺序。 | 1. 应答 `uiStatusCode` 变为非零，结果为 `FAILED` 并指出状态码字段；<br>2. `discount_cents` 由 `6500` 变为 `6600`，结果为 `FAILED` 并给出 `FieldPath` 与期望 / 实际值；<br>3. 只有服务端时间戳不同，结果为 `PASS`；<br>4. 被配置为忽略的字段不同，结果为 `PASS`；`map` 只是顺序不同，结果为 `PASS`。 |

---

### 3.5 微前端控制台与平台治理领域

| 编号 | 特性名称 | 涉及组件 | 优先级 | 功能描述与技术规范 | TDD 验收指标 |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **F-15** | **一体化微前端控制台** | `Frontend`<br>`Server` | **P0** | 在 `code-bench` 微前端体系内（暴露 `./App` 与 `./menu`，兼容嵌入与独立两种运行方式），提供：<br>1. **架构中心**：三级架构元素、模块接口、进程映射、Agent 管理，静态拓扑与一致性验证结果；<br>2. **流量监控台**：通过主控 `/api/events` SSE 实时展示流量；右侧 `Drawer` 分段展示 ① 24B 公共头偏移位图、② 会话头字段与收发耗时、③ 扩展头 Trace 与入队时间、④ Protobuf JSON 与 16B 对齐 Raw Hex 对照；<br>3. **重放台**：选择资产下发重放，展示结果状态、Mock 命中明细与双层 Diff；<br>4. `.pb` 管理：上传、版本切换与 `(service, operation)` 映射列表。 | 1. `tsc --noEmit` 与 `eslint` 零警告；<br>2. 解剖抽屉对预置报文正确展示四段内容（组件测试）；<br>3. 所有页面只使用 `--color-*` 语义 Token，列表使用标准 5 页滑动窗口 URL 分页（`15/25/50/100`，默认 `25`）。 |
| **F-16** | **RBAC 权限与操作审计** | `Server`<br>`Frontend` | **P0** | 1. **权限矩阵**（基于 `commonAuth.AuthMiddleware` 与 `RequireAdmin("traefik_admin")`）：<br>   - 普通认证用户：查看架构与拓扑、`.pb` 映射、流量记录与重放报告；基于已有录制资产**发起重放**（测试环境，A-06）；<br>   - `traefik_admin` / `super_admin`：架构元素与进程增删改、Agent 纳管、`.pb` 上传与版本切换、**开始 / 停止录制**、删除录制数据、全局设置；<br>2. **审计**：所有写操作通过 `commonAudit.RecordDiff` 记录字段级 Diff；开始 / 停止录制与发起重放作为操作事件写入审计（含操作人、目标 Agent / 进程、批次或任务 ID）。 | 1. 未认证请求返回 `401`；普通用户调用管理员接口（如开始录制、上传 `.pb`）返回 `403`；普通用户发起重放成功；<br>2. 修改架构元素后 `audit_logs` 中有正确的字段级 Old/New Diff；<br>3. 开始录制与发起重放各产生 1 条审计记录。 |

---

### 3.6 演进规划 (P1 / P2 Backlog)

| 编号 | 特性名称 | 优先级 | 说明 |
| :--- | :--- | :---: | :--- |
| **F-17** | **Agent Token 鉴权加固** | **P1** | Agent 从环境变量或安全配置加载高强度 Token（禁止硬编码）；HTTP 指令与 WebSocket 握手校验 `X-Traefik-Agent-Token`（`subtle.ConstantTimeCompare`）。验收：Token 缺失或错误时返回 `401`。 |
| **F-18** | **结构化接口文档浏览** | **P1** | 基于 `.pb` 反射展示接口清单、入参 / 出参 Message 嵌套树、字段 Tag、类型（含 `repeated`、`map`、`enum`）及注释，可从模块详情跳转。与 `code-proto` 的区别：以运行时 `(ascServiceName, ascOperation)` 为视角，并与功能模块、流量记录关联，不管理 `.proto` 源码与 MR。 |
| **F-19** | **报文敏感字段脱敏与查看权限细分** | **P1** | 按字段路径配置脱敏规则；Payload 明文查看可限定角色。 |
| **F-20** | **录制批次一键回归套件** | **P2** | 把一个录制批次的多个进程、多条请求组织为回归套件批量重放，并汇总报告。 |
| **F-21** | **从 `code-proto` 导入 `.pb`** | **P2** | 通过 `code-proto` 已有接口拉取编译好的描述集，减少手动上传。 |
| **F-22** | **无特权用户态代理降级模式** | **P2** | 不具备 eBPF 特权时，由业务方把下游地址配置为 Agent 代理端口完成采集与重放。 |

---

## 4. 核心业务验收场景矩阵 (Verification Scenarios)

为阶段四 TDD 提供端到端验收基线，定义以下 **8 个核心场景（`SC-01` ~ `SC-08`）**：

| 场景编号 | 场景名称 | 触发条件与输入 | 预期行为与断言 |
| :--- | :--- | :--- | :--- |
| **SC-01** | **免密探针、未认证拦截与 RBAC 越权** | 1. `GET /api/health`<br>2. 无 JWT 访问受保护接口<br>3. 普通用户新增架构元素、上传 `.pb`、开始录制 | 1. `200 OK` 且 `service: "Code-Traefik"`<br>2. `401`<br>3. 全部 `403` |
| **SC-02** | **架构建模约束与静态拓扑推导** | 管理员创建子系统、三级嵌套功能组、模块，绑定进程，声明 `PROVIDE` / `DEPEND`；尝试把功能组移到其子孙下；尝试让第二个模块供给已被供给的接口；上传 `.pb` | 成环移动返回 `400`；重复供给返回 `409`；`.pb` 映射正确；静态拓扑包含正常边、外部占位边与「进程内」边；审计记录完整 |
| **SC-03** | **Agent 纳管、信道与白名单** | 启动 `code-traefik-agent`（监听 `127.0.0.1`），主控登记并下发监控元信息、建立 WebSocket；模拟非白名单 IP 访问；停止 Agent | 握手成功并定位目标 PID；非白名单返回 `403`；Agent 停止后 3 个心跳周期内变为 `OFFLINE` |
| **SC-04** | **CROS RPC 分帧与解码** | 向 `pkg/crosrpc` 输入：带前序脏字节的流、1 字节碎片流、多包粘连流、V01 / V05 会话头、含 Trace 扩展头的报文、`usHeadLen > uiLength` 的畸形包 | 正确切出完整帧并提取 `ullSNO`、`ascServiceName`、`ascOperation`、`TraceID`；主控正确解码 Payload；畸形帧被拒绝且无 panic |
| **SC-05** | **单 Agent 多进程协同录制** | 同一 Agent 下监控 `Process_A`（含 `Module_A`）与 `Process_B`（含 `Module_B`）；先建立一条存量长连接再下发监控；重启两进程后执行 `外部 → Process_A → Process_B` 业务请求 | 下发后上报存量连接诊断；重启后 A 的入站 / 出站、B 的入站都被采集且不死锁；同一 `session_id` 下按 `process_id` 正确归档，REQ / RSP 配对正确 |
| **SC-06** | **架构一致性验证** | 使用 SC-05 的录制结果，静态模型中 `Module_A` 未声明对 `Module_B` 接口的依赖，另有一个已声明但未触发的依赖与一条同进程内依赖 | `A → B` 标为 `Violation`；未触发依赖标为 `Uncovered`；同进程内依赖标为 `Not Observable` |
| **SC-07** | **零 `.pb` 重放与 `ullSNO` 自愈** | 关闭下游，对 SC-05 中 `Process_A` 的入站请求发起重放；目标进程生成全新随机 `ullSNO` 与时间戳 | Agent 注入请求，下游调用按 `(service, operation)` + Payload 哈希命中 Mock，应答 `ullSNO` 被正确覆写，结果为 `PASS` |
| **SC-08** | **业务突变与 Mock 未命中检出** | ① 修改目标进程使 `uiStatusCode` 非零或 `discount_cents` 变化；② 修改目标进程使其多调用一次下游接口 | ① 结果为 `FAILED`，定位到状态码或字段路径，服务端时间戳差异被忽略；② 结果为 `MOCK_MISS`，明细中包含多出的那次调用，且没有流量流向真实下游 |

---

## 5. 非目标边界界定 (Non-Goals)

明确**不做**以下内容：

- **Non-Goal 1（只支持 CROS RPC over IPv4 TCP 明文）**：不设计可插拔的多协议框架，不支持 HTTP/REST、gRPC 或其他私有协议；首期只覆盖经内核 Socket 的 **IPv4 TCP 明文**通信底座，不覆盖 `Mesh`、`Domain`（Unix Socket）、IPv6、TLS 加密流，也不介入不经 Socket 的共享内存 `FRPC`；Payload 只解码 Protobuf（`RPC_IDL_MSG_TYPE_PB`）。
- **Non-Goal 2（首期不实现 Agent Token 鉴权）**：首期基于部署 IP 保密 + 源 IP 白名单，Token 鉴权列为 `F-17 / P1`，代码预留 `TODO(security)`。
- **Non-Goal 3（主控不抓包、不发重放包）**：主控是无特权控制面，不挂载 eBPF、不扫描业务 `/proc`、不跨网直接向业务进程发送报文；监测与重放全部由 Agent 完成。
- **Non-Goal 4（不引入 Keploy，不重复建设 MR 追踪）**：不依赖外部 Keploy 工具链；接口变更 MR 追踪与 `.proto` 源码管理归属 `code-proto`。
- **Non-Goal 5（Agent 无状态、无 Schema）**：Agent 不连数据库、不持有数据库凭据、不接收或解析 `.pb`；主控所有业务表冠以 `traefik_` 前缀。
- **Non-Goal 6（不污染宿主机全局网络）**：eBPF 只作用于主控下发的目标 PID 集合、监听端口与下游 `IP:Port`，其余连接原样放行。
- **Non-Goal 7（不依赖外部系统同步架构元数据）**：`code-traefik` 是架构元素与拓扑的 SSOT，不与 `code-proto`、`code-bench` 建立元数据轮询或强同步。
- **Non-Goal 8（不隔离非 CROS RPC 依赖的副作用）**：重放只在测试环境执行 (A-06)；目标进程对 DB、缓存、MQ 等的访问不做 Mock 或回滚。
- **Non-Goal 9（不做 MNG 握手语义仿真）**：MNG 管理消息只解析展示并原样回放 (A-08)；含挑战—应答或一次性随机数的握手不保证重放成功。
- **Non-Goal 10（不支持容器 PID 命名空间与多副本模块）**：首期只支持宿主机 PID 命名空间内的进程；不支持同一功能模块多副本部署 (A-03)。

---

## 6. 非功能性需求与安全防护红线 (NFR & Security Guardrails)

1. **报文边界与防内存耗尽**：
   - 分帧器执行 F-10 的长度契约校验，畸形长度立即丢弃并重置该连接的缓存；单帧采集阈值可配置（默认 4MB），防止整数溢出与超大包 OOM；
   - `.pb` 上传限制 `<= 4MB` 并做 `protodesc` 校验；所有 API 入参白名单校验，只用 GORM 参数化查询，禁止 SQL 拼接与未校验的系统命令调用。
2. **性能基线（首期目标值，阶段二压测校准）**：

   | 指标 | 目标 |
   | :--- | :--- |
   | 出站透明代理附加延迟（同机，P99） | ≤ 1 ms |
   | Agent 常驻内存（不含会话缓冲，监控 ≤ 10 个进程） | ≤ 64 MB |
   | Agent CPU（1000 帧/s 采集） | ≤ 单核 10% |
   | 主控解析解码吞吐 | ≥ 2000 帧/s |
   | 采集到前端 SSE 展示延迟 | ≤ 1 s |

3. **数据存储与保留**：原始帧与解析结果存入主控 PostgreSQL；录制批次受时长 / 帧数上限约束（F-11）；流量数据默认保留 30 天（可配置），由主控定时任务清理，清理动作写审计。
4. **最小特权与测试绑定**：
   - 主控以非特权身份运行，禁止硬编码 JWT 密钥或凭据；只有 Agent 使用 `CAP_BPF` / `CAP_NET_ADMIN`；
   - 自动化测试中的临时主控、Mock Agent 与模拟 CROS RPC 服务一律监听 `127.0.0.1`，禁止 `0.0.0.0`。
5. **前端安全渲染与主题**：来自报文的 `ascServiceName`、`ascOperation`、`acProcName` 等字符串视为不可信输入，只依赖 React JSX 自动转义，禁止 `dangerouslySetInnerHTML`、`innerHTML` 与原生 `alert()` / `confirm()`；所有界面只使用 `theme.css` 中的 `--color-*` 语义 Token，禁止硬编码 `#fff` / `#000` 等色值。
6. **测试策略与隔离数据库**：
   - 全量 Go 代码通过 `go test -race ./...`；信道广播、BPF Map 读写、会话缓冲必须并发安全；
   - 涉及数据库的单测统一使用 `code-common/backend/testdb.SetupIsolatedDB(t)`，禁止指向生产或共享库；
   - **eBPF 特权用例**以独立 build tag（如 `ebpf`）隔离，在具备 `CAP_BPF` 的特权 runner 上执行；普通环境下自动 `t.Skip` 并给出原因；与内核无关的逻辑（规则计算、分帧、Mock 匹配、`ullSNO` 改写）必须在普通环境中有完整单测；
   - `pkg/crosrpc` 分帧器提供 Go fuzz 测试。
7. **后台进程与内核探针卫生**：构建、单测或联调结束时，停止全部后台进程（`:8091` 主控、`:5181` 前端、临时 Agent 与模拟业务进程），确认 eBPF 钩子全部卸载，不残留端口占用或内核钩子。

---

## 7. 术语表 (Glossary)

| 术语 | 说明 |
| :--- | :--- |
| **SSOT** | Single Source of Truth，单一真实源；本模块是架构元素的唯一权威来源 |
| **`RPC_COMM_HEAD_S`** | 24B 公共消息头，含魔术字 `unMagicInfo`、`ullSNO`、`uiLength`、`usHeadLen`、`ucMsgType`、`isPacked` |
| **`unMagicInfo`** | 8B 魔术字：7B `"CSE.TCP"` + 1B `ucProtocol` |
| **`ullSNO`** | 公共头偏移 `8..15` 的 8B 大端消息序号，用于同一连接内 REQ / RSP 配对 |
| **`uiLength`** | 公共头之后的报文长度（会话头 + 扩展头 + Payload），不含 24B 公共头 |
| **`usHeadLen`** | 会话头 + 扩展头总长度；Payload 从偏移 `24 + usHeadLen` 开始；同时用于判定会话头版本 |
| **`ucMsgType`** | 消息类型（REQ / RSP / RSP_SEG / MNG 等），合法范围 `[2, RPC_MSG_TYPE_MNG_BUTT)` |
| **`ucReqType`** | 请求类型：同步 / 异步 / Oneway（Oneway 不期待应答） |
| **`RSP_SEG`** | 分片应答，一个请求对应多段应答帧，以 `usRspMsgNo` 标识段序 |
| **MNG 消息** | 管理消息（`ucMsgType=5/6/7`），用于连接鉴权、UUID 协商等 |
| **DOTR 扩展头** | 携带分布式追踪信息（`TraceID` / `SpanID` / `FatherSpanID`）与入队时间戳的扩展头段 |
| **`FRPC`** | 基于共享内存、不经 Socket 的 CROS RPC 通信底座（不在本模块范围） |
| **INGRESS / EGRESS** | 目标进程监听端口上的入站报文 / 目标进程主动访问下游的出站报文 |
| **存量连接** | 下发监控之前已经建立的长连接，出站重定向对其无效 |
| **Mock 命中 / 未命中** | 重放时下游请求能否在录制应答集合中找到匹配项 |
