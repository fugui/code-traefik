# 业务流量监控与重放 (`code-traefik`) 特性清单与需求范围界定规范 (Feature Matrix & Scope)

- **文档编号**：`01-feature-matrix.md`
- **文档版本**：v2.3.0
- **更新日期**：2026-10-28
- **当前阶段**：阶段一：特性清单与边界明确
- **状态**：定稿待审 (Ready for Review)

---

## 1. 系统愿景与产品定位 (Vision & Positioning)

### 1.1 项目愿景与整体构想
**业务流量监控与重放 (`code-traefik`)** 是挂载于 CodeBench 开发者综合工作台的核心业务子系统，**唯一且深度专精于业务实际使用的 DOPHI `CROS RPC` 网络通信协议**（由 `24B 公共消息头 RPC_COMM_HEAD_S` + `会话头 RPC_REQ/RSP/MNG_HEAD_S` + `0..N 段扩展消息头 Extend Head` + `序列化 Payload` 组成，不做任何其他无关协议扩展）。

为了解耦平台控制中心与业务运行机器的部署拓扑，并将内核特权与依赖收敛至最小边界，系统采用 **「控制面主框架 (`code-traefik-server`) + 独立纯二进制数据面代理 (`code-traefik-agent` / Sidecar)」同仓双二进制主从分离架构**：

1. **主框架控制面（`code-traefik`，保持在 `code-bench` 大家族内）**：
   - 作为全系统的统一操作入口与元数据大脑，提供微前端控制台（`/traefik`），独占管理**三层架构元素（子系统/域 -> 服务节点 [`ascServiceName`] -> 部署实例）** 与 **接口契约/文档（Protobuf `.pb` 描述集及 `(ascServiceName, ascOperation)` 接口映射）**，据此构建**静态系统架构图与动态服务调用拓扑**。
   - 承担**多进程协同录制编排**、**Payload 明文解码**、**业务拓扑关系验证**与**流量重放/回归报告界面**职责：面向实际业务录制“单次聚焦执行一个业务任务、同时开启链路上多个进程录制”的高信噪比场景，主动连接本地或远端的 `Agent`，批量或按实例供给纯网络与进程级监控元信息（目标进程 PID 列表/监听端口、下游依赖 `IP:Port` 列表等，**无需下发 `.pb`**），完整收集多进程调用关系并校验架构拓扑；重放时针对选定目标进程下发二进制重放包，并对回传结果执行双层 Diff 断言。
2. **流量监测与重放执行端（`code-traefik-agent` / Sidecar，支持单机多进程并发监控的独立应用）**：
   - 与主框架位于同一代码仓库（`cmd/agent`），编译为**零前端、零数据库、零 `.pb` Schema 依赖**的极简独立守护应用，每台业务机器仅需运行 **1 个 `Agent` 实例**即可**同时监控与录制该机器上的多个业务进程（`1 Agent : N Processes`）**。
   - **受控连接边界**：仅接受来自本地（`127.0.0.1`）和远程 `code-traefik` 主控发起的 **HTTP + SSE / WebSocket** 连接（*第一阶段暂不实现动态 Token 鉴权，基于部署 IP 保密假设并辅以轻量源 IP 白名单校验；预留第二阶段 Token 授权机制*）。
   - **多进程纯二进制收集与隔离重放**：通过单套内嵌 `cilium/ebpf` 探针与内核 BPF Hash Map，在业务宿主机本地对多个目标进程 `PID` 同步实施零侵入拦截（兼容同机被监控进程间互调），按进程/实例打标后通过多路复用流将**完整原始二进制报文**收集上报主控；收到模拟重放指令时，针对被测目标进程在本地**实际执行二进制请求注入、管理握手响应、下游依赖二进制匹配 Mock 以及固定偏移 `ullSNO` 自愈重写**。

### 1.2 核心价值主张 (Core Value Propositions)
1. **主从解耦与单机多进程纯二进制 Agent（`1 Agent : N Processes`, Zero Schema）**：同一代码仓构建 `code-traefik-server` 与 `code-traefik-agent`；单台业务机仅需部署 1 个零 `.pb` 依赖的轻量 `Agent` 即可并发监控本机多个 CROS RPC 进程，资源占用极低且免除跨机同步 `.pb` 的负担。
2. **任务级多进程协同录制与业务拓扑完整性验证**：契合“单次执行单一业务任务、数据量精炼可控”的真实录制模式，支持一键开启同机/跨机多个关联进程的协同录制，一次性完整收集全链路上下游交互数据，自动还原真实业务调用关系并与静态架构图进行拓扑一致性比对验证，同时为链路上每个进程沉淀可独立回放的资产。
3. **100% 原生契合 `CROS RPC` 协议规范（零冗余协议扩展）**：严格遵循 CROS RPC 线性报文规范，原生解析 `"CSE.TCP" + ucProtocol` 魔术字、大端 `ullSNO` / `uiLength` / `usHeadLen` 分帧、多版本兼容会话头（自动提取 `ascServiceName`、`ascOperation`、同步/异步/Oneway 类型、状态码 `uiStatusCode`/`ullStatusCode` 与三段收发时间戳）以及扩展头（`TraceID`/`SpanID`/`FatherSpanID` 与服务端入队时间戳）。
4. **Agent 本地嵌入式 `cilium/ebpf` 多 PID 引擎与防死锁自环豁免**：摒弃外部 Keploy 工具依赖，在 `Agent` 内挂载单套 `cilium/ebpf` 探针，基于 BPF Hash Map 实现多目标 `PID` 的 `O(1)` 并发过滤与动态热启停，内置 `monitor_pid`（Agent 自身 PID）内核态自环豁免，确保同机多进程互调录制与透传零死锁。
5. **Agent 纯二进制闭环重放与 `ullSNO` 偏移级自愈**：利用 CROS RPC 将所有每次运行变化的协议元数据集中在 `24 + usHeadLen` 之前的头部这一结构特性，Agent 在无需 `.pb` 的前提下直接通过会话头 `(ascServiceName, ascOperation)` + Payload 二进制切片（及录制调用次序）匹配 Mock，实时覆写应答公共头 Offset `8..15` 的 `ullSNO`，并由主控完成最终字段级回归 Diff 报告。

---

## 2. 系统目标拓扑与工作原理 (Target Architecture Overview)

```mermaid
flowchart TB
    subgraph Portal["🖥️ CodeBench 微前端控制台 (/traefik | :5181 -> :8091)"]
        UI_Arch["架构元素管理、静态架构图与动态调用拓扑画布"]
        UI_Doc["Protobuf (.pb) 契约与 CROS RPC 接口文档中心"]
        UI_Monitor["流量监控操作台与 CROS RPC 四段报文解剖抽屉"]
        UI_Replay["流量重放编排台与 Protobuf 字段级 Diff 报告"]
    end

    subgraph ControlPlane["🏛️ code-traefik 主框架控制面 (cmd/server :8091 | 无内核特权)"]
        Arch_Mgr["三层架构元素与拓扑构建引擎 (域 / 服务 ascServiceName / 实例)"]
        Proto_Center["Protobuf (.pb) 解析器与 (ServiceName, Operation) 接口文档引擎"]
        Agent_Hub["Agent 连接编排器 (HTTP 下发元信息/重放 + SSE/WS 接收流量)"]
        Diff_Eng["双层回归断言引擎 (CROS RPC 状态码校验 + Protobuf 字段级 Diff)"]
        Store[("PostgreSQL 共享库 (traefik_* 表前缀物理隔离)")]
    end

    subgraph DataPlane["🛰️ 业务机器：code-traefik-agent 独立守护应用 (1 Agent : N 进程 | 零 .pb 依赖)"]
        Agent_Srv["受控接入服务 (HTTP + 多路复用 SSE/WS | 仅接受本地与主控连接)"]
        EBPF_Mgr["嵌入式 cilium/ebpf 探针管理器 (多 Target PID 哈希表并发过滤 & Agent 自环豁免)"]
        CROS_Frame["轻量二进制分帧器 pkg/crosrpc (CSE.TCP 切帧 & 固定头提取，不解析 Payload)"]
        Replay_Worker["多进程二进制采集代理 & 隔离重放执行器 (按进程打标收集 / 单目标 ullSNO 覆写 Mock)"]
    end

    subgraph BizHost["⚙️ 业务机器内核与被监控的多个 CROS RPC 业务进程"]
        BPF_Hook["Linux Kernel eBPF (cgroup/connect4 匹配 BPF Hash Map 中多个 target_pid)"]
        TargetProc["同机 1..N 个目标业务进程 (PID 1..N | 各自监听端口 & 同机互调/调下游)"]
        Downstream["下游依赖 CROS RPC 服务 (离线重放时可关闭或物理隔离)"]
    end

    Portal == "REST API (JWT + RBAC) & 前端 SSE 实时推送" ==> ControlPlane
    ControlPlane --> Store
    Arch_Mgr & Proto_Center --> Agent_Hub

    Agent_Hub == "1. HTTP 下发多进程监控元信息列表 (PID集合/监听端口/下游IP:Port) 或重放包" ==> Agent_Srv
    Agent_Srv == "2. SSE / WebSocket 多路复用回传带进程标签的原始 CROS RPC 二进制报文" ==> Agent_Hub
    Agent_Hub --> Diff_Eng

    Agent_Srv --> EBPF_Mgr & Replay_Worker
    Replay_Worker --> CROS_Frame
    EBPF_Mgr == "挂载 BPF 程序 / 刷新目标 PID 与下游规则" ==> BPF_Hook

    Replay_Worker -- "3a. 录制捕获或实际注入重放 CROS RPC 二进制报文" --> TargetProc
    TargetProc -- "3b. 发起下游 TCP connect()" --> BPF_Hook
    BPF_Hook -- "4. 内核态透明劫持至本地 Agent" --> Replay_Worker
    Replay_Worker -- "5a. [监控态] Agent PID 豁免直连透传" --> Downstream
    Replay_Worker -. "5b. [重放态] 本地二进制匹配 Mock + 覆写固定偏移 ullSNO 返回应答" .-> TargetProc
```

---

## 3. 需求功能矩阵 (Feature Matrix)

特性优先级定义：
- **P0 (Must-Have)**：第一阶段核心基石特性，首期必须完整落地并具备 100% 自动化测试覆盖。
- **P1 (Should-Have)**：第二阶段规划特性（含 Agent Token 鉴权加固）与高阶增强能力。
- **P2 (Nice-to-Have)**：远期演进与大规模压测扩展特性。

---

### 3.1 架构元素建模、接口文档与可视化拓扑领域 (Architecture Elements, Interface Docs & Topology)

| 编号 | 特性名称 | 涉及组件 | 优先级 | 功能描述与技术规范 | TDD 验收指标 / 衡量标准 |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **F-01** | **三层架构元素建模与多实例共享 Agent 纳管 (`traefik_domains` / `traefik_services` / `traefik_instances`)** | `Server`<br>`Frontend`<br>`Models` | **P0** | 在 `code-traefik` 主框架建立完整的业务架构资产台账，采用三层粒度建模：<br>1. **子系统/域 (`traefik_domains`)**：划分业务领域、归属负责人与描述；<br>2. **服务节点 (`traefik_services` & `traefik_service_edges`)**：定义逻辑服务并与 CROS RPC 会话头中的 **`ascServiceName`（最大 64 字节目的服务名）** 绑定，维护关联 `.pb` 契约及**声明式上下游服务依赖关系**；<br>3. **部署实例 (`traefik_instances`)**：支持**多个不同服务实例关联绑定同一台机器上的同一个 `Agent` 接入地址（`host:port`，`1 Agent : N Instances`）**，各实例独立维护自身的目标进程定位策略（`PID` 或互斥监听端口 `listen_port` / 进程名特征）及下游真实 `IP:Port` 列表，支持 URL 分页与多维筛选。 | 1. 三层架构元素及依赖边的 CRUD、`ascServiceName`/编码唯一性冲突拦截（`409`）、同一 `Agent` 下多实例注册与监听端口冲突校验单测 100% 通过。<br>2. 删除仍处于录制或重放中的服务实例时安全拦截提示。 |
| **F-02** | **Protobuf (`.pb`) 契约注册与 CROS RPC 接口文档中心 (`traefik_proto_schemas`)** | `Server`<br>`Frontend`<br>`Models` | **P0** | 集中纳管 CROS RPC 接口层（`RPC_IDL_MSG_TYPE_PB`）使用的 Protobuf 编译描述集（`FileDescriptorSet` / `.pb`）并生成结构化接口文档：<br>1. 支持上传/更新 `.pb` 二进制描述文件（严格校验体积上限与 `protodesc` 结构合法性）；<br>2. **原生 `(ascServiceName, ascOperation)` 契约映射**：支持从 `.pb` 的 `ServiceDescriptor` / `MethodDescriptor` 自动推导或显式配置 **`(ascServiceName, ascOperation)` -> `(Req Message FullName, Rsp Message FullName)`** 映射表；<br>3. **结构化接口文档浏览**：自动反射提取服务接口列表、入参/出参 Message 嵌套树、字段名、Tag 编号、数据类型（含 `repeated`、`map`、`enum` 枚举值）及接口说明。 | 1. 上传合法 `.pb` 文件后，接口文档 API 准确返回按 `ascServiceName` / `ascOperation` 组织的接口清单与字段结构树；损坏或超限文件立即返回 `400`。<br>2. 未配置 `.pb` 或 `isPacked=0` 的报文在监控时自动保留完整 CROS RPC 头解析并降级展示 Payload Raw Hex，不中断链路。 |
| **F-03** | **可视化架构图、多进程实测调用拓扑收集与一致性验证 (`Architecture & Topology Verification`)** | `Server`<br>`Frontend` | **P0** | 基于架构元素、接口契约与多进程同时录制的 CROS RPC 报文构建双视角拓扑及关系验证：<br>1. **静态架构图**：根据子系统、服务节点（`ascServiceName`）、部署实例与依赖边生成可视化架构关系图；<br>2. **动态拓扑收集与架构一致性验证**：当针对一次业务任务同时开启多个进程录制后，自动融合各进程捕获的 CROS RPC 报文头（`ascServiceName`、`ascOperation`、收发时间戳、状态码）与 DOTR 扩展头（`TraceID`/`SpanID`/`FatherSpanID`）还原**完整实测调用链拓扑**，并与静态架构声明的依赖边自动比对（区分展示：**已验证吻合的依赖边**、**实测新发现但架构未声明的隐藏调用边**、**未触发的静态边**），支持点击节点一键跳转接口文档、录制批次或重放台。 | 1. 输入包含上中下游多进程协同录制流量的数据集（含一条未在静态架构声明的隐藏调用），拓扑验证接口准确还原完整调用边并标记出隐藏依赖差异。<br>2. 拓扑画布严格使用 `--color-*` 语义 Token 适配亮/暗双模主题。 |

---

### 3.2 远程 Agent 管控信道与监控元信息供给领域 (Control-Agent Channel & Metadata Provisioning)

| 编号 | 特性名称 | 涉及组件 | 优先级 | 功能描述与技术规范 | TDD 验收指标 / 衡量标准 |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **F-04** | **同仓独立 `Agent/Sidecar` 应用与 HTTP + SSE/WebSocket 受控信道** | `Server`<br>`Agent` | **P0** | 同一代码仓库下提供 `cmd/server`（主控）与 `cmd/agent`（独立采集重放应用）双构建入口：<br>1. `Agent` 运行于业务机器，只接受来自本地（`127.0.0.1`）和远程 `code-traefik` 主控的连接；<br>2. **通信协议**：采用 **HTTP REST**（主控下发心跳探活、宿主机进程探查、监控元信息配置、重放任务启停）+ **SSE / WebSocket**（Agent 向主控实时流式推送捕获的 CROS RPC 报文事件与重放结果）；<br>3. **第一阶段连接控制**：首期暂不启用 Token 认证（视部署 IP 保密），内置轻量级源 IP 白名单中间件（`--allow-ips`，默认仅放行 `127.0.0.1` 及指定的 `code-traefik` 主机 IP），并在协议头预留鉴权扩展位（标注 `TODO(security)`）。 | 1. 本地集成单测中（强制监听 `127.0.0.1`），`Server` 通过 HTTP 下发指令并建立 SSE/WS 流，100% 实时接收 `Agent` 事件并支持断线状态感知。<br>2. 非白名单源 IP 请求访问 `Agent` 控制接口时返回 `403 Forbidden`。 |
| **F-05** | **多进程监控元信息编排供给与目标进程动态定位** | `Server`<br>`Agent` | **P0** | 支持主控按录制任务一次性勾选**同机或跨机的多个服务实例（多进程）**批量启动协同录制，或单独增删某个进程的监控：<br>1. **多进程元信息列表 (`[]ProcessMonitorSpec`)**：主控按所属 `Agent` 聚合下发各目标进程的实例 ID、`PID`（或监听端口 `listen_port` / 进程名，由 Agent 在本机扫描 `/proc` 自动反查各自实时 `PID`）、监听入口地址及下游依赖 `IP:Port` 列表（**无需下发 `.pb`**）；<br>2. **多路复用打标回传**：Agent 通过单条 SSE/WebSocket 信道回传所有活跃监控进程的原始二进制包，外层携带轻量信封头（`session_id`、`instance_id`、`pid`、`direction`、源/目地址与采集时间戳），精确区分不同进程流量；<br>3. **远程进程批量探查**：支持主控请求 Agent 扫描探查业务机器上的候选进程列表与各绑定 `PID` 的实时存活状态。 | 1. 主控向同一 Agent 一次性下发 3 个不同监听端口的目标进程元信息，Agent 准确解析出 3 个进程的真实 `PID` 并同时进入录制态；动态停止其中 1 个进程不影响其余 2 个进程持续采集。<br>2. 任一目标进程退出或端口不存在时，Agent 精准上报对应实例的诊断状态。 |
| **F-06** | **Agent 远程连接 Token 授权安全加固（阶段二演进）** | `Server`<br>`Agent`<br>`Frontend` | **P1** | *（第一阶段暂不实现，列为阶段二演进）* 在 `code-traefik-agent` 中启用访问授权 Token 机制：<br>1. Agent 通过环境变量或安全配置加载高强度 Token（严禁代码硬编码）；<br>2. 主控纳管实例时配置对应凭据，所有 HTTP 控制指令与 SSE/WebSocket 握手均强制校验 `X-Traefik-Agent-Token`（常量时间比对 `subtle.ConstantTimeCompare`）。 | 启用 Token 开关后，未携带 Token 或携带错误 Token 的连接请求 100% 返回 `401 Unauthorized`。 |

---

### 3.3 Agent 嵌入式 eBPF 引擎与同仓共享 CROS RPC 协议栈领域 (Agent eBPF & CROS RPC Codec)

| 编号 | 特性名称 | 涉及组件 | 优先级 | 功能描述与技术规范 | TDD 验收指标 / 衡量标准 |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **F-07** | **Agent 嵌入式 `cilium/ebpf` 多 PID 并发劫持与防死锁自环豁免** | `Agent`<br>(`pkg/ebpf`) | **P0** | 在独立的 `code-traefik-agent` 进程内基于 `github.com/cilium/ebpf`（纯 Go BPF 汇编构建，零 Clang 依赖）挂载单套内核探针并发服务本机多个目标进程：<br>1. **BPF Hash Map 多 PID 精准匹配**：在内核 BPF Map 中维护活跃监控的**多个 `target_pid` 集合及其下游规则**（`O(1)` 查找，支持运行期热增删条目）；仅命中 Map 中已注册 `PID` 及目标下游 `IP:Port` 的连接才被透明重定向至 Agent 本地代理，宿主机其他无关进程 100% 原样放行；<br>2. **Agent 自环豁免与同机多进程互调兼容 (`monitor_pid`)**：将 `Agent` 自身 PID 写入 BPF Map，内核态识别 `current_tgid == monitor_pid` 时立即放行并递增 `stat_bypassed_self`；确保同机两个被同时监控的进程互调（`PID_A -> PID_B`）时零死锁自环，完整录下 `PID_A` 的出站与 `PID_B` 的入站报文；<br>3. **双模自适应与退出自洁**：具备 `CAP_BPF`+`CAP_NET_ADMIN` 时运行于 `ebpf_kernel` 模式，无特权环境自动降级为 `userspace_proxy` 模式；全部监控停止或 Agent 退出时 100% 自动卸载内核钩子。 | 1. 业务机同时运行被监控进程 `PID_A`、`PID_B`（含 `PID_A -> PID_B` 同机调用）与无关进程 `PID_C`，`PID_A` 与 `PID_B` 的流量均被准确拦截录制且零死锁，`PID_C` 零干扰。<br>2. Agent 透传连接 `stat_bypassed_self` 准确累加；卸载后确认 `Attached == false`。 |
| **F-08** | **`CROS RPC` 二进制分帧协议头库 (`pkg/crosrpc`) 与主控侧 Protobuf 解码器** | `Server`<br>`Agent`<br>(`pkg/crosrpc`) | **P0** | 严格遵循 `CROS RPC` 报文规范（唯一锁定该协议，不做其他协议扩展），按主从职责严格解耦分帧与解码：<br>1. **同仓共享纯二进制分帧与头解析库 (`pkg/crosrpc`，`Agent` 与 `Server` 共用，零 `.pb` 依赖)**：<br>   - **24B 公共头 (`RPC_COMM_HEAD_S`) 分帧与重同步**：识别 8B `unMagicInfo`（7B `"CSE.TCP"` + 1B `ucProtocol`，支持流式前序脏字节逐字节扫描定位魔术字）；按大端序解析 `ullSNO (8B)`、`uiLength (4B, 不含公共头)`、`usHeadLen (2B, 会话头+扩展头总长)`、`ucMsgType (1B, [2, MNG_BUTT))`、`isPacked (1B)`；强制校验 `usHeadLen <= uiLength <= RPC_PAYLOAD_MAX_LEN (32MB-4KB) + usHeadLen` 及 `usHeadLen <= 65535`，彻底免疫 TCP 粘包与碎片半包，切出整包二进制字节流；<br>   - **多版本兼容会话头 (`Session Head`) 与扩展头 (`Extend Head`) 解析**：遵循 `#pragma pack(4)` 与 `usHeadLen` 长度驱动的前/后向版本兼容机制，解析 `RPC_REQ_HEAD_S`（V00~V05：提取 `ascServiceName[64]`、`ascOperation[64]`、`ucReqType` 等）、`RPC_RSP_HEAD_S`（V00~V04：提取 `uiStatusCode`/`ullStatusCode`、三段收发时间戳、`usRspMsgNo`）、`RPC_MNG_HEAD_S`（`ucMsgType=5/6/7`）以及 `0~N` 段 `RPC_EXTEND_COMM_HEAD_S`（提取 `TraceID`/`SpanID`/`FatherSpanID` 与入队时间戳）；<br>2. **主控专属 Protobuf 动态编解码引擎 (`Server` 独占)**：仅在 `code-traefik-server` 内引入 `dynamicpb`，根据 `(ascServiceName, ascOperation)` 绑定的 `.pb` 描述集对偏移 `24 + usHeadLen` 之后的 Payload 二进制切片执行动态 JSON 解码与重编码。 | 1. 单测覆盖：含前序干扰字节的 `"CSE.TCP"` 魔术字扫描定位、1 Byte 碎片半包流、多包粘连流、V1 与 V5/V4 会话头兼容解析及扩展头解析；确认 `cmd/agent` 编译产物不依赖任何 `.pb` 注册表。<br>2. 非法 `ucMsgType` 或超限 `uiLength` / 畸形 `usHeadLen > uiLength` 报文 100% 安全拦截，零 panic / 零 OOM。 |

---

### 3.4 实时流量监测回传、Agent 实际重放与回归断言领域 (Traffic Monitoring, Agent Replay & Diff)

| 编号 | 特性名称 | 涉及组件 | 优先级 | 功能描述与技术规范 | TDD 验收指标 / 衡量标准 |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **F-09** | **任务级多进程协同录制、调用关系拼装与主控解码落盘 (`traefik_recording_sessions` & `traefik_traffic_records`)** | `Agent`<br>`Server`<br>`Models` | **P0** | 面向“单次执行单一业务任务、同时录制链路多进程”的高信噪比采集场景，引入**录制任务批次 (`traefik_recording_sessions`)**：<br>1. **多进程并发纯二进制收集**：录制任务激活后，关联的一个或多个 `Agent` 同时捕获所有参与进程的入站（`INGRESS`）与出站下游（`EGRESS`）**原始二进制报文**，打上进程/实例标签流式回传主控；<br>2. **全链路拼装与主控 `.pb` 解码**：主控解析三层协议头，同连接内按 **`ullSNO`** 配对 `REQ` 与 `RSP`（兼容 `ucReqType=3` Oneway 及 `RSP_SEG` 分片），利用扩展头 `TraceID`/`SpanID`/`FatherSpanID`（及跨进程调用地址/时序）将该业务任务下多个进程的入站与出站报文拼接为完整端到端业务调用图谱，并结合 `.pb` 字典反序列化为 **Protobuf JSON 明文**；<br>3. **单次多进程录制、按节点独立回放**：同一录制批次沉淀的多进程报文按 `session_id` + `instance_id` 结构化落盘，既可用于全局业务关系验证，亦可从中任选任意单个进程抽取其 `INGRESS` 激励与 `EGRESS` Mock 独立发起 `F-10` 离线重放。 | 1. 创建包含上下游 2 个进程的协同录制批次并触发 1 次业务请求，两个进程的入站/出站二进制帧 100% 按各自 `instance_id` 归档至同一 `session_id`，主控准确完成 `.pb` 解码与跨进程链路关联。<br>2. 支持按录制批次、服务节点、实例、`ascServiceName`、`ascOperation`、`ullSNO`、`TraceID` URL 分页检索。 |
| **F-10** | **主控重放编排与 Agent 纯二进制实际重放 (`traefik_replay_tasks`)** | `Server`<br>`Agent`<br>`Frontend` | **P0** | 在**真实下游 CROS RPC 服务已关闭或物理隔离**的前提下，由主控编排二进制重放包、**`Agent` 在业务机本地实际执行重放**：<br>1. 主控将待重放的入站 CROS RPC **原始二进制请求报文**、关联的下游录制 **二进制 Mock 应答集合**（含必要的 `RPC_MNG_HEAD_S` 握手应答与业务 `RSP`/`RSP_SEG` 二进制帧）及目标监听端口下发给目标 `Agent`（无需下发 `.pb`）；<br>2. `Agent` 在业务机器本地向目标进程监听端口建立连接（自动完成必要的管理消息二进制握手）并写入重放请求字节流；<br>3. 当目标进程向下游 `IP:Port` 发起调用时，被 `Agent` 本地 eBPF/代理透明拦截并由本地二进制 Mock 引擎闭环应答；<br>4. `Agent` 收集目标进程返回的实际二进制应答报文（支持单包 `RSP` 或多段 `RSP_SEG`）与下游 Mock 命中明细原样回传主控。 | 在完全关闭模拟下游服务的情况下，通过主控触发重放任务，Agent 仅凭二进制重放包完成连接握手、请求注入与下游 Mock 拦截应答，命中率达 100%，目标进程正常返回业务应答。 |
| **F-11** | **Agent 零 `.pb` 二进制 Mock 匹配与固定偏移 `ullSNO` 自愈** | `Agent`<br>(`pkg/replay`) | **P0** | 利用 CROS RPC **“所有协议层动态变化字段均位于 `24 + usHeadLen` 之前的头部，业务 Payload 位于 `24 + usHeadLen` 之后”** 的线性结构特征，使 Agent 在**完全不知晓 `.pb` 内容**的前提下实现智能 Mock 自愈：<br>1. **管理消息握手自动应答**：对目标进程连接下游初期发送的 `RPC_MNG_HEAD_S`（`ucMsgType=5/6/7` 鉴权/UUID 协商请求），自动匹配对应二进制管理应答并覆写匹配的 `ullSNO`；<br>2. **固定头寻址 + Payload 字节切片匹配**：提取下游请求会话头明文的 **`(ascServiceName, ascOperation)`** 与偏移 `24 + usHeadLen` 之后的 **Payload 原始二进制切片哈希** 匹配 Mock（天然跳过每次运行变化的公共头 `ullSNO`、会话头时间戳/UUID/幂等序号及扩展头动态 Trace/时间戳；若同一接口 Payload 含动态业务参数未精确命中，则按同接口录制调用次序兜底匹配）；<br>3. **固定偏移实时自愈重写**：命中二进制 Mock 应答帧后，Agent 直接在字节数组 Offset `8..15` 覆写当前下游请求的实际大端 **`ullSNO`**（并按固定偏移同步回填会话头 `aucTraceBuff` / `ullClientSendTime`）后写回目标进程 Socket。 | 即使目标进程在回放时生成全新的随机 `ullSNO`（如 `0x000000010009A2B3`）、新 TraceID 与新发送时间戳发起下游调用，Agent 在零 `.pb` 依赖下依然 100% 命中二进制 Mock 并返回携带一致 `ullSNO` 的应答帧，成功唤醒业务客户端。 |
| **F-12** | **CROS RPC 协议头级 + Protobuf 字段级双层回归 Diff 断言器** | `Server`<br>`Frontend` | **P0** | 主控端收到 `Agent` 回传的重放实际应答报文（`Actual`）后，与录制基线报文（`Expected`）执行双层回归比对：<br>1. **第一层（CROS RPC 协议头与状态码校验）**：比对 `ucMsgType`、应答头返回码 `uiStatusCode` / `ullStatusCode`、分片序号 `usRspMsgNo`、`isPacked` 及 Payload 原始字节；<br>2. **第二层（Protobuf 语义字段级 Diff）**：当 Payload 字节存在差异时，自动反序列化为 JSON 结构树逐字段计算 `[]FieldDiff{FieldPath, ExpectedValue, ActualValue}`，自动豁免会话头/扩展头固有动态时间戳（`ullServerRecvTime`、`ullServerSendTime`、`ullServerEnqueueTime`）并支持配置忽略业务层动态字段。 | 当目标进程业务逻辑发生突变（如应答 `uiStatusCode` 变为非零错误码，或 Protobuf 字段 `discount_cents` 由 `6500` 变为 `6600`）时，重放报告精准判定 `FAILED` 并定位具体变异的头字段或 Protobuf 字段路径。 |

---

### 3.5 CodeBench 微前端操作界面与平台治理领域 (Portal Console, RBAC & Audit)

| 编号 | 特性名称 | 涉及组件 | 优先级 | 功能描述与技术规范 | TDD 验收指标 / 衡量标准 |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **F-13** | **一体化微前端控制台（架构拓扑 / 接口文档 / CROS RPC 解剖台 / 重放台）** | `Frontend`<br>`Server` | **P0** | 保持在 `code-bench` 微前端体系内（暴露 `./App` 与 `./menu`，兼容嵌入与独立双模运行），提供四大核心操作视图：<br>1. **架构与调用拓扑中心**：三层架构元素管理、服务架构图、基于 CROS RPC 流量的动态调用拓扑与 Agent 状态看板；<br>2. **CROS RPC 接口文档中心**：按 `ascServiceName` / `ascOperation` 检索 `.pb` 接口定义与 Message 字段树；<br>3. **流量监控台与 CROS RPC 四段报文解剖抽屉**：通过主控 `/api/events` SSE 实时展示监控流量，点击滑出右侧 `Drawer` 分段可视化呈现：**① 24B 公共头 (`RPC_COMM_HEAD_S` 偏移位图)**、**② 会话头 (`Req/Rsp/Mng` 字段明细与收发耗时拆解)**、**③ 扩展头 (`TraceID/SpanID/FatherSpanID` & 入队时间)**、**④ Protobuf JSON 明文与 16B 对齐 Raw Hex 对照**；<br>4. **流量重放操作台**：编排下发 Agent 重放任务，高亮展示协议头与 Protobuf 字段级回归 Diff。 | 1. 主控 `GET /api/health` 返回 `200 OK`；前端 `tsc --noEmit` 与 `eslint` 零警告通过。<br>2. 所有页面、拓扑画布与解剖抽屉 100% 使用 `theme.css` 的 `--color-*` 语义变量适配亮/暗双模主题，列表统一采用标准 5 页滑动窗口 URL 分页器。 |
| **F-14** | **多级 RBAC 权限守卫与全量字段级 Diff 操作审计** | `Server`<br>`Frontend` | **P0** | 1. **RBAC 权限隔离**：集成 `commonAuth.AuthMiddleware` 与 `RequireAdmin("traefik_admin")`；普通认证用户可查看架构拓扑、查阅接口文档、观测流量及执行授权范围内的重放测试；架构元素增删改、Agent 实例配置与监控元信息下发、`.pb` 字典变更及全局设置（`/admin/settings`）仅限 `traefik_admin` 或 `super_admin` 操作；<br>2. **字段级操作审计**：主控侧所有写操作统一接入 `commonAudit.RecordDiff` 记录变更前后字段级 Diff。 | 1. 未认证请求返回 `401`；普通用户调用 `traefik_admin` 专属接口返回 `403 Forbidden`。<br>2. 修改架构元素、实例 Agent 配置或系统设置后，`audit_logs` 准确落盘字段级 Old/New Diff。 |

---

## 4. 核心业务验收场景矩阵 (Verification Scenarios)

为确保阶段四 TDD 测试先行具备清晰的端到端验收基线，定义以下 **7 大核心验收场景（`SC-01` ~ `SC-07`）**：

| 场景编号 | 场景名称 | 触发条件与输入特征 | 预期行为与 TDD 断言 |
| :--- | :--- | :--- | :--- |
| **SC-01** | **免密探针、未认证拦截与 RBAC 越权防护** | 1. 请求主控 `GET /api/health`<br>2. 无 JWT 请求受保护接口<br>3. 普通用户请求新增架构服务节点或修改 `.pb` 字典 | 1. 返回 `200 OK` 且 `service: "Code-Traefik"`<br>2. 返回 `401 Unauthorized`<br>3. 管理员守卫拦截返回 `403 Forbidden` |
| **SC-02** | **架构元素建模、`.pb` 接口文档解析与架构拓扑生成** | 管理员创建业务域与服务节点（绑定 `ascServiceName`），上传 `.pb` 描述文件映射 `ascOperation` 入参/出参 Message，绑定部署实例与 Agent 地址后查询拓扑与接口文档 | 成功返回按 `(ascServiceName, ascOperation)` 组织的结构化 Message 字段树；拓扑接口准确输出包含上下游有向依赖边的架构图数据，`RecordDiff` 记录创建审计 |
| **SC-03** | **主控连接 Agent 与纯网络/进程监控元信息下发** | 启动独立 `code-traefik-agent`（监听 `127.0.0.1`），主控从架构实例提取「目标进程/监听端口 + 下游 `IP:Port` 列表」（不下发 `.pb`）通过 HTTP 下发给 Agent 并建立 SSE/WS 流；同时模拟非白名单 IP 连接 Agent | 主控与 Agent 握手成功，Agent 准确加载监控元信息并定位目标进程 `PID`；非白名单源 IP 连接被 Agent 拒绝返回 `403` |
| **SC-04** | **`CROS RPC` 二进制流分帧、会话/扩展头解析与主控 `.pb` 解码** | 向共享 `pkg/crosrpc` 输入：带前序脏字节的 `"CSE.TCP"` 流、1 Byte 碎片半包流、多包粘连流、V1/V5 不同 `usHeadLen` 会话头、含 DOTR Trace 扩展头报文，以及 `usHeadLen > uiLength` 的畸形包 | `pkg/crosrpc` 在零 `.pb` 依赖下精准切出完整二进制包并提取 `ullSNO`、`ascServiceName`、`ascOperation`、`TraceID`，主控侧准确反序列化 Payload JSON；畸形报文被安全丢弃拦截 |
| **SC-05** | **单 Agent 多进程同时录制、业务拓扑关系验证与节点资产抽取** | 在同一 `Agent` 下创建协同录制任务，同时纳入上游进程 `PID_A` 与下游进程 `PID_B`，执行单次端到端业务请求（`外部 -> PID_A -> PID_B`） | Agent 同步捕获并区分回传 `PID_A` 与 `PID_B` 的入站/出站原始二进制帧（`monitor_pid` 豁免零死锁），主控完成 `.pb` 解码、拼接完整调用链并验证 `Service_A -> Service_B` 拓扑关系，且支持从中单独选择 `PID_A` 或 `PID_B` 发起离线重放 |
| **SC-06** | **主控编排二进制重放包、Agent 零 `.pb` 实际重放与 `ullSNO` 自愈** | 关闭真实下游依赖服务，在主控对录制记录发起重放任务（目标进程在回放时生成全新随机下游 `ullSNO` 与发送时间戳） | 主控下发纯二进制重放请求与 Mock 集合；Agent 在本地注入请求并拦截目标进程出站调用，按 `(ascServiceName, ascOperation)` + Payload 二进制切片命中 Mock，覆写应答头 Offset `8..15` 的 `ullSNO`，回放判定 `PASS` |
| **SC-07** | **目标进程业务逻辑突变与双层回归 Diff 检出** | 目标业务进程的应答状态码 `uiStatusCode` 异常或 Protobuf 计算结果发生突变（如折扣字段偏移），主控下发 Agent 执行重放验证 | Agent 回传实际应答报文后，主控双层断言器精准判定 `FAILED`，忽略固有的服务端收发时间戳差异，准确定位突变的状态码或具体 Protobuf 字段路径与期望/实际值 |

---

## 5. 非目标边界界定 (Non-Goals)

为了保持架构高内聚、职责纯粹并杜绝过度设计，明确**不做（Non-Goals）**以下内容：

- **Non-Goal 1（协议唯一性：仅支持 `CROS RPC` 协议，不做任何多协议扩展）**：业务数据面 100% 锁定公司实际使用的 **CROS RPC 协议**（`"CSE.TCP"` 24B 公共头 + Req/Rsp/Mng 会话头 + 扩展头 + Payload），**不设计可插拔的通用多协议扩展框架，不支持业务数据面的 HTTP/REST、标准 gRPC 或其他第三方私有协议**；在 CROS RPC 多通信底座中，聚焦经由内核 Socket 栈的流式底座（如 `TCP`、`Mesh`、`Domain` 明文流）与 Protobuf（`RPC_IDL_MSG_TYPE_PB`）载荷解码，不介入不经 Socket 的共享内存 `FRPC` 数据面。
- **Non-Goal 2（第一阶段暂不实现 Agent Token 鉴权体系）**：首期假设业务机器上 `Agent/Sidecar` 的部署 IP 保密，仅通过轻量源 IP 白名单限制只接受本地与指定 `code-traefik` 主控连接，暂不在第一阶段开发 Token 签发与握手鉴权（已列入第二阶段 `F-06 / P1`，代码预留 `TODO(security)`）。
- **Non-Goal 3（主控 `code-traefik-server` 不直接抓包或发重放包）**：主框架严格保持为无特权控制面与操作入口，不在 `code-traefik-server` 进程中挂载内核 eBPF 探针、不扫描主控本机业务 `/proc`、也不由主控跨网直接向业务进程发送 CROS RPC 重放报文——所有监测与重放执行均由独立的 `code-traefik-agent` 完成。
- **Non-Goal 4（彻底摒弃外部 Keploy 工具链与重复 Git Webhook 建设）**：不引入外部 Keploy 二进制；不重复建设代码仓 MR 接口变更 Webhook 追踪（该职责归属工作区既有的 `code-proto` 模块）。
- **Non-Goal 5（Agent 绝不直连数据库、不感知 `.pb` Schema，主控严守表前缀隔离）**：`code-traefik-agent` 保持纯无状态与零业务 Schema 感知（不连接 PostgreSQL、不持有任何数据库凭据、不接收或解析 `.pb` 文件，仅收集与重放原始二进制数据）；主控 `code-traefik-server` 在共享库中创建的所有业务表强制冠以 `traefik_` 前缀。
- **Non-Goal 6（不污染业务宿主机全局网络）**：Agent 内嵌 eBPF 探针严禁无差别劫持宿主机全局连接，严格限定在主控元信息指定的活跃监控目标 `PID` 集合与下游 `IP:Port` 列表范围内。

---

## 6. 非功能性需求与安全防护红线 (NFR & Security Guardrails)

1. **CROS RPC 报文边界校验与防大包内存耗尽 (Wire Bounds & OOM Protection)**：
   - 分帧器严格执行 CROS RPC 长度契约校验：`usHeadLen <= 65535`、`ucMsgType ∈ [2, RPC_MSG_TYPE_MNG_BUTT)` 且 `usHeadLen <= uiLength <= RPC_PAYLOAD_MAX_LEN (32MB - 4096) + usHeadLen`（并支持配置采集缓冲单包保护阈值），遇到畸形长度字段立即丢弃并重置连接缓存，严防整数溢出或超大包 OOM；
   - 上传的 `.pb` 描述集文件强制实施体积上限（`<= 4MB`）与 `protodesc` 结构合法性校验；所有 API 入参执行严格白名单校验，强制使用 GORM 参数化查询，严禁任何 SQL 拼接或未校验系统命令调用。
2. **最小特权隔离与测试绑定安全 (Least Privilege & Safe Test Binding)**：
   - `code-traefik-server` 以普通非特权权限运行，严禁硬编码 JWT 密钥或凭据；仅 `code-traefik-agent` 按需使用 `CAP_BPF` / `CAP_NET_ADMIN`；
   - 在自动化单元测试与本地集成测试中，所有临时测试 Server、Mock Agent 与模拟 CROS RPC 业务端口**必须强制监听 `127.0.0.1`**（严禁在测试中监听 `0.0.0.0`）；对第一阶段暂缓的 Agent Token 鉴权能力保留显式 `TODO(security)` 标记。
3. **前端安全渲染与双模主题规范 (XSS Prevention & Theme Tokens)**：
   - 前端严格依赖 React JSX 原生自动转义（特别注意来自网络报文的 `ascServiceName`、`ascOperation`、`acProcName` 字符串均视为不可信外部输入），严禁使用 `dangerouslySetInnerHTML`、`innerHTML` 或原生 `alert()`/`confirm()`；
   - 所有界面、架构拓扑画布、接口文档树与 CROS RPC 四段报文解剖抽屉 100% 采用 `theme.css` 中的 `--color-*` 语义 Token 适配亮色（Light）与暗色（Dark）模式，严禁硬编码 `#fff`/`#000` 等绝对色值；分页列表统一采用 URL 状态同步的 5 页滑动窗口规范（`15/25/50/100`，默认 `25` 条/页）。
4. **并发安全与物理隔离数据库测试 (Concurrency & Isolated TestDB)**：
   - 主控与 Agent 之间的流式连接管理、SSE/WebSocket 广播中心及 BPF Map 读写必须并发安全，全量 Go 代码必须通过 `go test -race ./...`；
   - 所有涉及数据库读写的单元测试必须使用 `code-common/backend/testdb.SetupIsolatedDB(t)` 动态创建独立临时库并自动销毁，严禁指向生产或共享业务库。
5. **后台进程与内核探针卫生管理 (Process & Kernel Hygiene)**：
   - 在任何构建、单测或联调验证结束时，必须 100% 停止并释放拉起的后台进程（包括 `:8091` 主控、`:5181` 前端、临时测试 `Agent` 与模拟业务进程），并确保内核 eBPF 探针 100% 自动卸载，绝不残留端口占用或内核钩子。
