# 业务流量监控与重放 (`code-traefik`) 系统总体架构与主从协同设计

- **文档编号**：`01-system-architecture.md`
- **文档版本**：v1.0.0
- **更新日期**：2026-10-29
- **归属阶段**：阶段二：系统架构与数据模型设计 (`02-design`)
- **状态**：已定稿 (Frozen)
- **上位基线**：[`01-feature-matrix.md`](../01-features/01-feature-matrix.md) (v2.5.1)、[`01-cros-rpc-protocol-spec.md`](../07-references/01-cros-rpc-protocol-spec.md) (v1.0.0)

---

## 1. 微前端双模运行拓扑 (Dual-Mode Micro-Frontend Topology)

控制面 `code-traefik-server` 遵循 CodeBench 微前端家族统一规范，通过 Go 1.16+ `//go:embed all:frontend/dist` 单二进制托管与 `@originjs/vite-plugin-federation` 模块联邦机制，无缝兼容**宿主门户嵌入**与**独立开发调试**双模运行。

```mermaid
flowchart LR
    subgraph Browser["浏览器客户端 (Light / Dark 双主题)"]
        HostApp["CodeBench 宿主门户 (:8000)"]
        RemoteApp["traefik 联邦子应用 (./App & ./menu)"]
        StandaloneApp["StandaloneShell 独立外壳 (:5181 / :8091)"]
    end

    subgraph Gateway["平台接入层"]
        Caddy["Caddy 统一反向代理 (/traefik/*)"]
    end

    subgraph ServerBin["code-traefik-server 单二进制 (:8091)"]
        StripPrefix["commonServer 前缀剥离中间件 (/traefik -> /)"]
        SecHeaders["安全响应头中间件 (CSP / Frame / Nosniff)"]
        StaticFS["embed.FS 静态资源与 SPA History Fallback"]
        AuthGuard["commonAuth JWT 校验 + RequireAdmin('traefik_admin')"]
        AuditMW["commonAudit 异步字段级 Diff 审计中间件"]
        Router["Gin REST API & SSE 实时事件流 (/api/*)"]
    end

    subgraph SharedInfra["平台共享基础设施"]
        PG[("PostgreSQL 共享实例 (users / audit_logs / traefik_*)")]
    end

    HostApp -- "动态加载 /traefik/assets/remoteEntry.js" --> Caddy
    RemoteApp -- "fetch 拦截重写 /traefik/api/* + Bearer JWT" --> Caddy
    Caddy -- "反向代理透传" --> StripPrefix
    StandaloneApp -- "直连 /api/* (开发 :5181 Proxy 或单二进制 :8091)" --> StripPrefix
    StripPrefix --> SecHeaders
    SecHeaders -- "静态资产请求" --> StaticFS
    SecHeaders -- "/api/* 业务请求" --> AuthGuard --> AuditMW --> Router
    Router --> PG
    AuditMW --> PG
```

### 1.1 双模运行路由与上下文契约

| 维度 | 门户嵌入模式 (`isEmbedded = true`) | 独立开发调试模式 (`isEmbedded = false`) |
| :--- | :--- | :--- |
| **入口识别** | 宿主传入 `props.isEmbedded = true` 或检测 `window.__POWERED_BY_PORTAL__ === true` | 直接访问 `http://192.168.56.18:5181/traefik/` 或 `http://192.168.56.18:8091/` |
| **导航与布局** | 宿主通过 `./menu` 渲染左侧二级导航，子应用仅挂载 `EmbeddedRoutes` 内容区 | 挂载 `StandaloneShell`（内置独立侧边栏、亮/暗主题切换与 `UnifiedLogin` 登录页） |
| **API 路径治理** | `setupFetchInterceptor({ appPrefix: '/traefik' })` 自动重写 `/api/*` 为 `/traefik/api/*` | 前端直接请求 `/api/*`，Vite Dev Server 代理至 `127.0.0.1:8091` 或由单二进制直接响应 |
| **后端前缀剥离** | `commonServer.Run(Options{ Prefix: "traefik" })` 自动剥离 `/traefik`，内部统一匹配 `/api/*` | 无需剥离或兼容 `/traefik` 访问，共享同一套 `RegisterAPIRoutes` 处理器 |
| **实时推送通道** | 基于 `fetch` + `ReadableStream` 请求 `/traefik/api/events`，通过 HTTP Header 携带 `Bearer JWT`（严禁 URL Query 传 Token） | 同样通过 Header 携带 `Bearer JWT` 订阅 `/api/events` SSE 实时流量与任务事件 |

---

## 2. 主从双子系统物理架构与代码分层 (Dual-Subsystem Architecture)

为彻底隔离「控制面元数据治理 / 动态解码」与「宿主机内核态流量采集 / 二进制重放」，系统在单仓库（Monorepo）下拆分为两个物理职责正交的可执行程序：**`code-traefik-server`** 与 **`code-traefik-agent`**。

### 2.1 双子系统物理职责与防腐边界

| 架构维度 | 控制面主控 (`cmd/server` -> `code-traefik-server`) | 数据面代理 (`cmd/agent` -> `code-traefik-agent`) |
| :--- | :--- | :--- |
| **部署拓扑与规模** | 集中式无特权部署（对接 CodeBench 网关与共享 PostgreSQL） | 每台受控业务宿主机部署 1 个守护进程（`1 Agent : N 目标进程`） |
| **操作系统特权** | 普通非特权用户运行，**零内核钩子、零业务宿主机 `/proc` 访问** | 需具备 `CAP_BPF` + `CAP_NET_ADMIN`（挂载 eBPF 与读取本机 `/proc`） |
| **持久化与状态** | 有状态：唯一连接 PostgreSQL（读写 11 张 `traefik_*` 表及共享表） | **完全无数据库**：不连任何 DB、不持有数据库凭据，仅维护运行期内存态 |
| **Protobuf 依赖** | 引入 `google.golang.org/protobuf/types/dynamicpb` 动态反序列化与语义 Diff | **零 `.pb` 依赖**：编译依赖图严禁出现 `dynamicpb` / `protoregistry` / `gorm` |
| **协议解析深度** | 调用 `pkg/crosrpc` 解析头 + 调用 `internal/server/decoder` 解码 Payload | 仅调用 `pkg/crosrpc` 解析 `24 + usHeadLen` 之前的协议头，Payload 视为不透明字节切片 |
| **录制与重放职责** | 批次协同编排、落盘归档、模块穿透定位、架构一致性验证、双层回归 Diff 报告 | 入站旁路复制、出站透明代理、重放连接注入、二进制 Mock 匹配与头部偏移自愈 |

### 2.2 工程目录分层与包依赖拓扑

```text
code-traefik/
├── cmd/
│   ├── server/main.go               # 控制面可执行入口：内嵌 frontend/dist，挂载 Gin、DB、Audit 与调度器
│   └── agent/main.go                # 数据面可执行入口：挂载 IP 白名单、eBPF 管理器、透明代理与重放器
├── pkg/                             # 主从共享或数据面独立无状态核心库（严禁反向依赖 internal/server 或 models）
│   ├── crosrpc/                     # 共享 CROS RPC 分帧状态机、24B 公共头、多版本会话头与扩展头解析库
│   ├── agentproto/                  # 主从 HTTP REST DTO 与 WebSocket 二进制信封帧编解码契约
│   ├── ebpf/                        # Agent 专属：cilium/ebpf 探针加载、BPF Map 热更新、/proc 扫描与自洁控制
│   └── replay/                      # Agent 专属：零 .pb 二进制 Mock 匹配器、请求注入器与应答头偏移自愈改写器
├── internal/
│   └── server/                      # Server 专属核心业务引擎
│       ├── hub/                     # Agent 连接池、10s 心跳探活、WebSocket 接收器与 SSE 广播中心
│       ├── recorder/                # 多进程协同录制编排器、REQ/RSP 配对器与异步批量落盘流水线
│       ├── decoder/                 # .pb 描述集注册校验、版本切换与 dynamicpb 动态编解码中心
│       ├── topology/                # 三级架构树治理、静态拓扑推导、报文穿透定位与架构一致性验证引擎
│       └── diff/                    # 协议头级 + Protobuf 语义字段级双层回归 Diff 断言与重放调度器
├── config/                          # Server 与 Agent 各自的配置结构体与安全默认值加载
├── models/                          # 11 张 traefik_* GORM 实体模型、索引迁移与级联校验辅助函数
├── handlers/                        # Server HTTP/SSE 控制器、参数白名单校验、RBAC 守卫与 commonAudit 接入
└── frontend/                        # React 18 + TypeScript + Vite 5 微前端控制台工程
```

> [!IMPORTANT]
> **编译期防腐门禁**：`cmd/agent`、`pkg/crosrpc`、`pkg/agentproto`、`pkg/ebpf`、`pkg/replay` 五个包的依赖闭包中严禁引入 `gorm.io/*`、`code-common/backend/gormdb` 及 `google.golang.org/protobuf/types/dynamicpb`。阶段四 CI 单测将通过 `go list -deps ./cmd/agent` 自动化断言该架构红线。

---

## 3. 主从管控通信信道协议规范 (Control & Data Channel Protocol)

主控与 Agent 之间采用 **「HTTP REST 控制下行 + WebSocket 二进制流事件上行」** 双通道协同协议（协议常量定义于 `pkg/agentproto`，首期主协议版本 `ProtocolVersion = "1.0"`）。

### 3.1 连接安全控制（源 IP 白名单与鉴权扩展位）

1. **监听与源 IP 白名单中间件 (`IPAllowlistMiddleware`)**：
   - Agent 启动参数支持 `--listen 0.0.0.0:9091`（测试环境强制绑定 `127.0.0.1:<port>`）与 `--allow-ips 127.0.0.1,192.168.56.18`（支持单 IP 与 CIDR 网段解析为 `[]netip.Prefix`）。
   - 所有进入 Agent 的 HTTP 请求及 WebSocket `Upgrade` 握手均直接提取底层 TCP `RemoteAddr` 的真实源 IP（忽略可伪造的 `X-Forwarded-For`），未命中白名单立即返回 `403 Forbidden` 并丢弃连接。
2. **协议主版本校验**：握手响应携带 `protocol_version: "1.0"`；主控在比对主版本号（Major Version，即 `"1"`）不一致时，标记该 Agent 为版本不兼容状态并拒绝下发录制或重放指令。
3. **安全演进预留**：在 `IPAllowlistMiddleware` 之后预留 `AgentTokenAuthMiddleware` 钩子位（标记 `// TODO(security): F-17 P1 阶段启用 X-Traefik-Agent-Token 常量时间比对与 mTLS 双向认证`）。

### 3.2 HTTP REST 控制下行接口契约 (`Server -> Agent`)

| 方法 | Agent 路由路径 | 调用时机与职责 | 核心请求 / 响应载荷 (`pkg/agentproto`) |
| :---: | :--- | :--- | :--- |
| `GET` | `/agent/v1/handshake` | 纳管校验与每 10s 周期心跳探活 | 返回 `HandshakeResp{AgentVersion, ProtocolVersion, KernelVersion, HasEbpfCap, CapDetail, ActiveSessions, UptimeSec}` |
| `GET` | `/agent/v1/processes/discover` | 管理员在页面探查候选业务进程 | 返回宿主机当前持有 IPv4 TCP `LISTEN` 端口的进程清单 `[]DiscoveredProcess{PID, ListenPort, Cmdline, ExeName}` |
| `PUT` | `/agent/v1/monitors/sync` | 启动协同录制或动态增补进程监控 | 请求 `SyncMonitorReq{SessionID, MaxFrameBytes, MaxBufferBytes, Processes: []ProcessMonitorSpec}`；返回各进程 PID 定位状态与存量连接诊断 `[]ProcessDiagStatus` |
| `DELETE` | `/agent/v1/monitors/:sessionId/processes/:processId` | 单独移除某进程的监控（不影响同机其他进程） | 从 BPF Map 热移除该进程的 PID 与下游规则；若已无任何活跃监控则自动卸载探针 |
| `POST` | `/agent/v1/monitors/:sessionId/stop` | 录制批次正常结束或触发上限停止 | 刷空该会话缓冲剩余帧，返回累计统计 `StopMonitorResp{CapturedFrames, DroppedFrames, BypassedSelfCount}` |
| `POST` | `/agent/v1/replays/execute` | 下发单进程离线隔离重放任务 | 请求 `ExecuteReplayReq{TaskID, ProcessSpec, TimeoutMs, IngressRequests: [][]byte, EgressMocks: [][]byte, MngMocks: [][]byte}`（纯二进制帧切片，无 `.pb`） |
| `POST` | `/agent/v1/replays/:taskId/cancel` | 用户手动中止正在执行的重放任务 | 立即关闭注入连接、卸载该重放任务的下游重定向规则并释放本地 Mock 监听器 |

其中，主控按 Agent 聚合下发的多进程监控规格结构体定义如下：

```go
package agentproto

// DownstreamEndpoint 描述目标进程允许访问并需被透明拦截的下游 CROS RPC 地址
type DownstreamEndpoint struct {
	IP   string `json:"ip"`   // IPv4 点分十进制地址，如 "192.168.56.19" 或 "127.0.0.1"
	Port uint16 `json:"port"` // 下游监听端口，1..65535
}

// ProcessMonitorSpec 单进程监控元信息（零 .pb Schema 依赖）
type ProcessMonitorSpec struct {
	ProcessID   uint64               `json:"process_id"`   // 主控 traefik_processes.id
	ListenPort  uint16               `json:"listen_port"`  // 唯一监听端口 (A-02)
	CmdPattern  string               `json:"cmd_pattern"`  // /proc/<pid>/cmdline 匹配表达式
	Downstreams []DownstreamEndpoint `json:"downstreams"`  // 静态拓扑推导 + 显式登记的下游地址列表
}

// ProcessDiagStatus 下发监控后的实时定位与存量长连接诊断结果
type ProcessDiagStatus struct {
	ProcessID        uint64 `json:"process_id"`
	Status           string `json:"status"`            // "MONITORING" | "NOT_FOUND" | "CAP_DENIED" | "ERROR"
	ResolvedPID      uint32 `json:"resolved_pid"`      // 实时定位到的 OS PID（未找到为 0）
	EstablishedConns int    `json:"established_conns"` // 指向已登记下游的存量 TCP_ESTABLISHED 连接数 (A-01)
	NeedReconnect    bool   `json:"need_reconnect"`    // EstablishedConns > 0 时为 true，提示重启或触发重连
	Message          string `json:"message"`
}
```

### 3.3 WebSocket 二进制流上行信封协议 (`Agent -> Server`)

主控在活跃录制或重放期间长连 Agent 的 `ws://<agent_endpoint>/agent/v1/stream`。为避免高频采集下 JSON + Base64 编码带来的 CPU 与带宽膨胀，WebSocket 统一使用 `BinaryMessage` 帧传输 **56 字节定长网络大端信封头 (`EnvelopeHeader`) + 变长载荷**。

```text
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|      Magic (0x4354 "CT")      |  ProtoVer (1) |   EventType   |  0..3
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                 SessionOrTaskID (uint64, BE)                  |  4..11
|                                                               |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                    ProcessID (uint64, BE)                     |  12..19
|                                                               |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                   CapturedPID (uint32, BE)                    |  20..23
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|   Direction   |     Flags     |         Reserved (0)          |  24..27
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                  CapturedAtNs (int64, BE)                     |  28..35
|                                                               |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                     SrcIPv4 (4 Bytes)                         |  36..39
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                     DstIPv4 (4 Bytes)                         |  40..43
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|       SrcPort (uint16, BE)    |       DstPort (uint16, BE)    |  44..47
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                    ConnSeq (uint32, BE)                       |  48..51
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                   PayloadLen (uint32, BE)                     |  52..55
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|             Payload Bytes (Raw CROS RPC Frame / JSON)         |  56..56+N
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

* **`EventType` 枚举定义**：
  * `0x01 (EVENT_TRAFFIC_FRAME)`：采集到的单条完整（或超限截断）CROS RPC 原始帧，`Payload` 直接为 `RPC_COMM_HEAD_S` 起始的原始字节流；`Direction` 取值 `1 = INGRESS`、`2 = EGRESS`；`Flags` 位掩码 `bit0 = IS_TRUNCATED`（单帧超 `MaxFrameBytes` 截断不可重放）。
  * `0x02 (EVENT_PROCESS_LIFECYCLE)`：目标进程生命周期事件（如 `PROCESS_RESTARTED` 新旧 PID 切换、`PROCESS_EXITED`），`Payload` 为轻量 JSON 元数据。
  * `0x03 (EVENT_SESSION_STATS)`：每 2s 周期上报会话采集计数、当前缓冲水位与累计背压丢帧数 `dropped_frames`。
  * `0x04 (EVENT_REPLAY_ITEM_DONE)`：重放单条请求完成事件，携带实际捕获的 `RSP` / `RSP_SEG` 原始字节流或超时/连接错误状态。
  * `0x05 (EVENT_REPLAY_COMPLETED)`：重放任务汇总完成事件，携带完整 `MockHit` / `MockMiss` 明细列表。

### 3.4 背压缓冲、断线重连与失联宽限期探针自洁

```mermaid
stateDiagram-v2
    [*] --> Idle: Agent 启动 (未挂载 eBPF)
    Idle --> ActiveConnected: 收到监控/重放指令 & WS 已连接
    ActiveConnected --> ActiveBuffering: WS 连接闪断 / 主控消费阻塞
    ActiveBuffering --> ActiveConnected: 5分钟宽限期内主控重连成功 (排空有界缓冲)
    ActiveBuffering --> SelfCleaning: 失联超时 (>= 5m) 或收到 SIGTERM/SIGINT
    ActiveConnected --> SelfCleaning: 全部会话停止 或收到 SIGTERM/SIGINT
    SelfCleaning --> Idle: 关闭本地代理 -> Detach eBPF Links -> 清空 BPF Maps (Attached=false)
```

1. **有界会话字节预算缓冲 (`BoundedSessionBuffer`)**：
   - Agent 为每个活跃录制批次分配独立的有界环形字节队列，容量上限默认 `64MB`（`--max-session-buffer-bytes=67108864`）。
   - 当 WebSocket 发送阻塞或闪断导致当前会话已驻留字节数 `usedBytes + frameLen > maxBytes` 时，采集协程执行 **非阻塞丢弃新帧 (`Drop-Newest`)**，原子递增 `session.droppedFrames.Add(1)`，绝不阻塞业务代理转发协程，也绝不突破 Agent 内存红线。
2. **主控指数退避自动重连**：
   - `AgentHub` 检测到 WebSocket 断开后立即启动重连循环（退避间隔 `500ms -> 1s -> 2s -> 5s` 封顶），重连成功后立即消费缓冲中积压的信封帧并同步最新 `dropped_frames`。
3. **失联宽限期看门狗 (`GracePeriodWatchdog`) 与探针自洁**：
   - Agent 维护 `lastControlContactAt` 原子时间戳（任意合法 HTTP 心跳请求或健康 WebSocket Ping/Pong 均刷新该时间）。
   - 若处于活跃监控/重放状态且 `now - lastControlContactAt > 5m`（`--control-grace-period=5m`），或进程收到 `SIGTERM` / `SIGINT` 信号，立即触发幂等自洁序列 `PurgeAllMonitors()`：停止所有重放注入器 -> 关闭本地透明代理端口 -> 逐一调用 `ebpf.Link.Close()` 卸载全部内核钩子 -> 清空并关闭全部 `ebpf.Map` -> 置状态位 `Attached = false`。

---

## 4. 三大核心业务全链路时序图 (End-to-End Sequence Diagrams)

### 4.1 场景一：Agent 纳管、能力握手、心跳探活与失联自洁时序

```mermaid
sequenceDiagram
    autonumber
    actor Admin as 模块管理员 (traefik_admin)
    participant Server as code-traefik-server (AgentHub)
    participant DB as PostgreSQL (traefik_agents)
    participant Agent as code-traefik-agent
    participant Kernel as Linux Kernel (BPF Subsystem)

    Admin->>Server: POST /api/agents (登记名称与 IP:Port)
    Server->>Agent: GET /agent/v1/handshake
    Agent->>Kernel: Capget 校验 CAP_BPF + CAP_NET_ADMIN & 读取 uname
    Kernel-->>Agent: 返回权限位图与内核版本
    Agent-->>Server: 200 OK (ProtocolVersion="1.0", HasEbpfCap=true)
    Server->>Server: 校验协议主版本 == "1"
    Server->>DB: INSERT traefik_agents (status='ONLINE', last_heartbeat_at=NOW())
    Server-->>Admin: 201 Created (写入 commonAudit 审计日志)

    loop 每 10s 周期探活
        Server->>Agent: GET /agent/v1/handshake
        alt 探活正常
            Agent-->>Server: 200 OK (刷新 lastControlContactAt)
            Server->>DB: UPDATE last_heartbeat_at = NOW(), status = 'ONLINE'
        else 连续 3 次探活超时/拒绝 (30s)
            Server->>DB: UPDATE status = 'OFFLINE'
        end
    end

    Note over Server,Agent: 异常分支：主控与 Agent 网络完全中断超过 5 分钟宽限期
    Agent->>Agent: GracePeriodWatchdog 触发 (>= 5m 无心跳且无 WS 连接)
    Agent->>Kernel: 关闭代理监听 + link.Close() 卸载全部 eBPF 探针 + 清空 BPF Maps
    Note right of Agent: 确保宿主机业务进程网络恢复纯直连零残留
```

### 4.2 场景二：多进程协同录制与报文穿透定位全链路时序

```mermaid
sequenceDiagram
    autonumber
    actor Admin as 模块管理员
    participant UI as 微前端流量监控台
    participant Server as code-traefik-server
    participant Agent as code-traefik-agent
    participant Kernel as eBPF (Ingress Copy + cgroup/connect4)
    participant ProcA as 目标进程 A (Module_A)
    participant ProcB as 目标进程 B (Module_B)

    Admin->>UI: 选择 Process_A 与 Process_B 创建协同录制批次
    UI->>Server: POST /api/recording-sessions (启动录制)
    Server->>Agent: WS Connect /agent/v1/stream (建立二进制事件流)
    Server->>Agent: PUT /agent/v1/monitors/sync ([Spec_A, Spec_B])
    Agent->>Agent: 扫描 /proc 定位 PID_A、PID_B 并检测存量 TCP_ESTABLISHED 连接
    Agent->>Kernel: 首次挂载探针 + 写入 agent_pid(自环豁免)、target_pids_map、downstream_rules_map
    Agent-->>Server: 返回定位成功 & NeedReconnect=true (若存在存量连接)
    Server-->>UI: 提示 A-01 引导信息「请重启目标进程或触发长连接重连」

    Admin->>ProcA: 触发重连或重启进程 (Agent 2s 巡检自动热更新 PID 并上报 PROCESS_RESTARTED)
    Note over ProcA,ProcB: 外部上游发起真实业务请求 -> ProcA -> ProcB

    ProcA->>ProcA: 监听端口收到上游请求 (INGRESS REQ)
    Kernel-->>Agent: 入站旁路复制收字节至 RingBuf (零代理零延迟)
    Agent-->>Server: WS Binary [Dir=INGRESS, Proc=A, Raw REQ Frame]

    ProcA->>Kernel: connect(IP_B:Port_B) 发起下游调用
    Kernel->>Agent: cgroup/connect4 透明重定向至 Agent 本地代理 (记录 socket_cookie 原始目的地址)
    Agent->>Kernel: Agent 代理主动 connect(IP_B:Port_B)
    Kernel->>ProcB: 命中 agent_pid 自环豁免 (stat_bypassed_self++)，直连 ProcB 监听端口
    ProcA->>Agent: 发送下游请求字节流 (EGRESS REQ)
    Agent->>ProcB: 原样透传字节流
    Agent-->>Server: WS Binary [Dir=EGRESS, Proc=A, Raw REQ Frame]
    Kernel-->>Agent: ProcB 监听端口入站旁路复制 (INGRESS REQ)
    Agent-->>Server: WS Binary [Dir=INGRESS, Proc=B, Raw REQ Frame]

    ProcB-->>Agent: 返回下游应答 (INGRESS RSP 旁路复制 + EGRESS RSP 代理透传)
    Agent-->>ProcA: 透传应答字节流
    ProcA-->>ProcA: 返回上游最终应答 (INGRESS RSP 旁路复制)
    Agent-->>Server: 批量推送各自 RSP 原始帧

    Server->>Server: pkg/crosrpc 分帧解析 -> 同连接按 ullSNO 配对 REQ/RSP
    Server->>Server: 匹配生效 .pb 执行 dynamicpb JSON 解码 -> 按 F-05 穿透定位源/目模块
    Server->>UI: SSE /api/events 实时推送流量摘要 & 批量写入 traefik_traffic_records
```

### 4.3 场景三：离线隔离自愈重放与双层回归 Diff 全链路时序

```mermaid
sequenceDiagram
    autonumber
    actor Dev as 开发/测试工程师 (普通认证用户)
    participant UI as 微前端重放编排台
    participant Server as code-traefik-server (Replay & Diff)
    participant Agent as code-traefik-agent (pkg/replay)
    participant Kernel as eBPF (cgroup/connect4)
    participant ProcA as 测试环境目标进程 A (待回归版本)

    Note over ProcA: 测试环境前置条件 (A-06)：下游真实服务已关闭或物理隔离
    Dev->>UI: 选定录制批次中 Process_A 的 INGRESS 请求并配置附加忽略字段
    UI->>Server: POST /api/replay-tasks (发起重放)
    Server->>Server: 从 DB 提取原始 INGRESS REQ 帧 + Process_A 录制期全部 EGRESS RSP/MNG 帧
    Server->>Agent: POST /agent/v1/replays/execute (纯二进制重放包，零 .pb)
    Agent->>Kernel: 为 Process_A 下游 IP:Port 挂载 cgroup/connect4 重定向至本地 Mock 端口
    Note over Dev,ProcA: 引导重启或重连 Process_A 使出站连接接入 Agent Mock 监听器

    loop 按录制顺序逐条注入选定请求
        Agent->>ProcA: 建立 TCP 连接至 ListenPort，(若有 MNG 帧先原样发送)，写入原始 INGRESS REQ
        ProcA->>ProcA: 执行新版业务逻辑，生成全新随机 ullSNO'、新 TraceBuff' 与新 SendTimeStamp'
        ProcA->>Agent: 经重定向连接发送下游 EGRESS REQ'
        Agent->>Agent: pkg/crosrpc 解析 24+usHeadLen 前头部，提取 (ascServiceName, ascOperation) + Payload SHA256
        alt 命中精确 Payload 哈希 或 同接口调用次序兜底队列
            Agent->>Agent: 原地自愈覆写应答帧(含多段 RSP_SEG)：[8..15]=ullSNO'、RSP[8..23]=TraceBuff'、RSP[32..39]=SendTimeStamp'
            Agent-->>ProcA: 返回自愈后的二进制 Mock 应答帧
        else 录制应答已耗尽或接口未录制 (MOCK_MISS)
            Agent->>Agent: 严禁转发至真实下游！记录 MOCK_MISS 明细并保持连接静默等待目标超时
        end
        ProcA-->>Agent: 返回目标进程实际应答 Actual RSP / RSP_SEG (或等待 TimeoutMs 超时)
        Agent-->>Server: WS Binary 上报 EVENT_REPLAY_ITEM_DONE (含 Actual 原始帧)
    end

    Agent->>Kernel: 清理本次重放下游重定向规则
    Agent-->>Server: WS Binary 上报 EVENT_REPLAY_COMPLETED (含全部 MockHit / MockMiss 明细)
    Server->>Server: 第一层协议头比对 (ucMsgType / StatusCode / Payload 字节，豁免服务端时间戳)
    Server->>Server: 第二层 dynamicpb 语义比对 (过滤忽略字段，repeated 保序、map 无序计算 FieldDiff)
    Server->>Server: 按 ERROR > TIMEOUT > MOCK_MISS > FAILED > PASS 聚合状态并落盘
    Server-->>UI: 返回完整双层 Diff 回归报告
```

---

## 5. 系统级安全架构与验证计划基线 (Security Guardrails & Verification Plan)

依据工作区安全编码红线，阶段二架构冻结以下强制安全防护契约，并纳入阶段四自动化验收计划：

1. **认证、授权与会话安全**：
   - 控制面所有非公开 `/api/*` 接口（含 `/api/events` SSE 流）强制经 `commonAuth.AuthMiddleware` 校验 `HS256` JWT（显式拒绝 `none` 算法、强制校验 `exp` 过期时间）；`JWT_SECRET` 通过环境变量或密钥管理系统注入，严禁源码硬编码回退字面量（生产集群接入 KMS 标记见 `config/config.go` `TODO(security)`）。
   - 严禁在 URL Query 参数中传递 JWT 或任何凭据；前端登出时清空内存认证上下文并触发页面级重载跳转。
   - 独立密码登录（调试模式）复用平台 `bcrypt` 加盐哈希存储，预留 MFA 与泄露密码库检测扩展位（`// TODO(security): 企业生产门户统一走 OAuth2/OIDC + MFA 强认证`）。
2. **输入校验、文件上传与命令执行零信任**：
   - `.pb` 描述集上传强制校验 `<= 4MB` 体积上限、校验 `FileDescriptorSet` 魔术结构与 `protodesc.NewFiles` 完整性；二进制内容直接存入 PostgreSQL `bytea` 字段，不落地宿主机可执行文件系统（病毒扫描外部网关集成留存 `// TODO(security): 接入企业制品防病毒扫描服务`）。
   - Agent 进程探查与连接诊断 100% 通过 Go 标准库只读解析 `/proc/<pid>/*` 虚拟文件，**全仓严禁调用 `os/exec` 拼接执行外部 Shell 命令**。
   - 数据库交互 100% 使用 GORM 参数化预编译查询，排序字段与状态枚举执行严格白名单校验，SQL 原始错误严禁透出至客户端响应。
3. **浏览器安全响应头与前端防 XSS**：
   - 控制面全局注入 `Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; frame-ancestors 'self'`（注：Vite 微前端样式注入需 `style-src 'unsafe-inline'`，`script-src` 严禁启用 `unsafe-inline` 或 `unsafe-eval`）、`X-Content-Type-Options: nosniff`、`X-Frame-Options: SAMEORIGIN`、`Permissions-Policy: camera=(), microphone=(), geolocation=()`，敏感 API 响应附加 `Cache-Control: no-store`，CORS 严格限定受信任门户域名白名单（严禁 `*`）并挂载 API 速率限制器。
   - 来自网络报文的 `ascServiceName`、`ascOperation`、`acProcName` 及 Protobuf 解码字符串均视为不可信外部输入，前端 100% 依赖 React JSX 原生文本转义，严禁使用 `dangerouslySetInnerHTML`、`innerHTML` 或原生 `alert()` / `confirm()`。
4. **测试绑定红线**：
   - 所有单元与集成测试中拉起的临时 `Server`、Mock `Agent`、模拟业务 TCP 服务及测试数据库均**强制仅监听 `127.0.0.1`**，严禁绑定 `0.0.0.0`；测试结束在 `t.Cleanup` 中确保端口与 eBPF 探针 100% 释放。
