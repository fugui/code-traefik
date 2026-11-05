# 业务流量与接口契约治理 (`code-traefik`) 特性清单与需求范围界定规范 (Feature Matrix & Scope)

- **文档编号**：`01-feature-matrix.md`
- **文档版本**：v2.12.1
- **更新日期**：2026-11-05
- **当前阶段**：阶段一：特性清单与边界明确
- **状态**：已批准待实施 (Approved for M10~M17)
- **协议依据**：详见项目内置参考文档 [`docs/07-references/01-cros-rpc-protocol-spec.md`](../07-references/01-cros-rpc-protocol-spec.md)（文档编号：`01-cros-rpc-protocol-spec.md` v1.0.0，基于 DOPHI 平台 `cros_rpccore.ph` / `cros_rpctcp` 规范，本文所有字段名、偏移与取值均以该规范原文为准）

### 修订记录

| 版本 | 日期 | 修订要点 |
| :--- | :--- | :--- |
| v2.0.0 | 2026-10-27 | 重构为主控 + Agent 分离、架构拓扑驱动模型 |
| v2.1.0 | 2026-10-27 | 按 CROS RPC 协议规范收敛协议边界 |
| v2.2.0 | 2026-10-28 | 剥离 Agent 侧 `.pb` 依赖，收敛为纯二进制收集与重放 |
| v2.3.0 | 2026-10-28 | 支持单 Agent 多进程并发监控与任务级协同录制 |
| v2.4.0 | 2026-10-28 | 明确架构元素 SSOT、三级模型、模块级接口契约与多模块进程映射 |
| v2.5.0 | 2026-10-28 | 依据阶段一出口检视修订：新增「关键业务假设」；补充入站旁路采集与长连接前置条件；新增 Agent 实例纳管；PROVIDE 全局唯一与来源模块判定规则；重放结果状态与 Mock 未命中策略；`.pb` 版本管理；信道收敛为 HTTP + WebSocket；重排 P0/P1/P2；补充性能、存储保留与 eBPF 测试策略 NFR；补充术语表 |
| v2.5.1 | 2026-10-29 | 挂载项目内置协议规范 (docs/07-references/01-cros-rpc-protocol-spec.md)；明确服务名映射为 package 点分全名 ({package}.{serviceName}，现已全系统统一为完全点分规范)；依据协议原文固化 F-13 应答头回填字段清单 (ullSNO/aucTraceBuff/ullClientSendTime) 与精准偏移 |
| v2.6.0 | 2026-10-29 | 增补多人并发与双模持久化规范：Agent 与录制素材均划分公共级 (`SHARED`) 与个人级 (`PRIVATE`)，普通用户可自建自用私有 Agent（共享公共架构元素 SSOT）；确立 Agent 动态排他租约 (`Mutex Lease`，同 Agent 同时间仅 1 名用户操作，冲突返回 `409`)；支持有权限的录制素材在相同架构元素的进程间跨 Agent 重放；明确 `traefik_traffic_records` 采用 `raw_req_packet` / `raw_rsp_packet` (`bytea`) + `payload_json` (`jsonb`) 双模往返存储与双轨分发管道 |
| v2.7.0 | 2026-10-29 | 依据架构检视与用户批复修订：1. 明确管理员特权（`traefik_admin` 可穿透查看/审计所有 `PRIVATE` 素材与 Agent，普通用户严格防横向越权）；2. 明确共享环境（`SHARED` Agent）排他重放支持；3. 主控直接连接 Agent（内网互通）；4. 数据库表数量放开，规范化拆解为 13 张核心物理表（新增 `traefik_process_modules` 关联表与 `traefik_replay_items` 重放条目表）并引入 PostgreSQL 原生范围物理分区（`PARTITION BY RANGE (created_at)`）；5. 架构一致性验证结果扩充 `Not Monitored` 分类 |
| v2.8.0 | 2026-11-01 | 依据 M8 控制台信息架构重组修订（详见 ADR-07）：1. 菜单按操作频次重构为「`流量作业`（`流量录制` / `流量调试` / `流量重放`）➔ `架构与契约`（`架构拓扑` / `架构元素` / `Proto 契约`）➔ `环境与治理`（`Agent 与进程` / `模块参数配置`）」3 组 8 项，默认落地 `/record` 并无损兼容旧深链重定向；2. 拆分 `流量录制`（批次资产台账与跨页流转，`F-15.1`）与 `流量调试`（异常过滤、四段解剖、Trace 串联与单包快速重放调试闭环，`F-15.2`）；3. 剥离全屏 `架构拓扑` 与纯逻辑 `架构元素`，将物理业务进程（含进程分组 `group_name`）CRUD 唯一收口至 `Agent 与进程`（`F-15.3`）；4. 将基于 `.pb` 在线手填 JSON 主动组包发包调试归入 P2 演进项（`F-23`） |
| v2.9.0 | 2026-11-02 | 依据 M10 架构进程蓝图解耦与 eBPF 自动预挂待命设计修订（详见 ADR-08 / ADR-09）：1. **领域解耦**：在 `/architecture` 左树顶部引入 `[逻辑架构 \| 物理进程]` 切换器，新增 `traefik_arch_process_groups` 与 `traefik_arch_processes` 全局物理进程蓝图 SSOT（承载 `FM` 模块组装与默认端口/启动特征），`traefik_processes` 收敛为宿主机 Agent `1:1` 挂载实例（`UNIQUE(agent_id, arch_process_id)`）并支持本地覆盖；2. **eBPF 默认自动预挂 (`ARMED`)**：前置完成存量连接诊断与一次重连就绪，停录回退 `ARMED` 不断开长连接；3. **三台同构分组进程树**：`/architecture`、`/agents` 与 `/record` 共享统一的分组进程树交互（`F-15.3`~`F-15.5`） |
| v2.9.1 | 2026-11-02 | 依据 ADR-09 架构检视修订：1. **多公共环境对等建模**：明确 `1 台 Agent = 1 套独立运行环境`（允许多套 `SHARED` 公共测试环境与多套 `PRIVATE` 个人环境各自 `1:1` 挂载同一 `ArchProcess`），Agent 支持可选 `fallback_agent_id` 指向联调关联公共环境；2. **按需入站钩子与三态代理闸门**：`ARMED` 待命期不挂载高频读写 tracepoint（零读写拦截损耗），`EgressProxy` 支持 `PASSTHROUGH \| CAPTURING \| REPLAY_MOCK` 三态切换（重放期帧级 Mock 接管既有代理连接并拒绝 `NEED_RECONNECT` 进程重放，守护零外泄红线），卸挂与 5m 失联执行优雅排水；3. **统一 7 态探针枚举、点分服务名与新系统免迁移干净重建** |
| v2.9.2 | 2026-11-02 | 依据 ADR-10 (v1.1.0) 架构检视定稿修订：1. **增强 .pb 描述集资产化治理**：增补 `F-03.5`（按制品聚合全景台账与服务级分页全局目录双模工作台）、`F-03.6`（`own_proto_files`/`external_imports` 去重拓扑闭包解析、联合类型池反射、同名版本 `DiffIgnorePaths` 向前自动继承与 `BASE_LIB` 基础公共库底座）、`F-03.7`（全局单一生效池、咨询锁串行化、`CompileTrialPool` 预编译演练、停用反向依赖门禁与批量防冲突仲裁），单文件上传上限提升至 `16MB`；2. **正式取消 `F-21`**（因 `code-proto` 外部排期推迟，取消联动强依赖，系统内形成完整自闭环的 IDL 描述集治理） |
| v2.9.3 | 2026-11-03 | 依据 ADR-11 (v1.0.0) 修订：1. **单进程开箱即用双录 (`INGRESS + EGRESS`) 与重放自动闭环**：`F-09` / `F-11` 升级目标 PID 默认全出站透明代理拦截（零下游拓扑前置即可同时采集入站与出站往返，非 CROS RPC 连接基于 `bytes.Index` SIMD 滑窗零副作用透传）与存量 `ESTABLISHED` 入站 Socket FD 预热；`F-08` 修正存量连接诊断为严格按 `/proc/<pid>/fd` 自有 Socket Inode 求交集并剔除已受控代理连接；`F-12` 自动合并历史 `EGRESS` 目的地址构建重放 Mock 端点集并增加写前 `"CSE.TCP"` 零外泄守卫；2. **双工作台职责消歧与零额外勾选体验**：`F-15.4` / `F-15.5` 明确 `/agents` 常驻待命与 `/record` 按次圈选文案，新增可用进程 `1..5` 自动预勾选、`[🔗 勾选关联下游]` 及 `/agents` ⇄ `/record` 双向一键深链闭环 |
| v2.10.0 | 2026-11-04 | 依据 ADR-12 (v1.0.0) 修订（对应 M13）：1. **协议阶段耗时补齐与发起序锚定**：`F-10` 补齐 V03/V04 `RPC_RSP_HEAD_S` 偏移 `40..47` (`ullServerRecvTime`) 与 `48..55` (`ullServerSendTime`) 解析及帧首字节到达定戳；`F-11` 在 `REQ` 到达瞬间原子分配单调发起 `SeqNo` 并记忆同连接 `MNG.acProcName` 对端身份；2. **DOTR 权威事务调用树、时序泳道与参数/实体血缘**：新增 `F-15.6`（`/debug` 默认采用「可折叠事务调用树 + 内联迷你甘特瀑布条」按根事务不割裂分页，提供平铺切换、多进程 `EGRESS ⇔ INGRESS` 跨跳缝合、同层兄弟 `串行/并发/补偿` 分类、业务软错误码透视、交互式 SVG 时序泳道 + 四段解剖联动抽屉、单事务参数传递血缘与跨事务有状态业务实体生命线）；3. **`RecordID` 级 `ScopedMock` 事务闭包重放打通**：`F-12` / `F-13` / `F-15.2` 端到端打通 `ScopedEgressMocks[recordID]` 局部因果池隔离、重放前下游闭包预览与跨事务前置状态依赖链一键连带回放 |
| v2.11.0 | 2026-11-05 | **战略定位升维**：将“业务流量监控与重放”升维为“**基于真实流量的活体契约、架构一致性与防劣化自愈平台**”；确立“接口资产治理”、“架构漂移雷达”与“无依赖自愈防劣化”三大支柱，将“录制与重放”准确界定为底层零侵入数据手段 |
| v2.12.0 | 2026-11-05 | **契约治理功能体系全景扩充**：依据治理深化研讨定稿，新增 `F-24`（契约治理全景大盘）、`F-25`（黄金契约套件资产库）、`F-26`（设计态契约破坏性变更阻断）与 `F-27`（CI/CD 自动化强阻断质量门禁），收敛 `F-20` 至 `F-25`，规划 M14~M17 工程作战与 `SC-09`~`SC-10` 验收场景 |
| v2.12.1 | 2026-11-05 | **依据 ADR-14 (v1.1.0) 架构深度检视加固**：1. `F-24` 升级为公私域隔离的**三支柱子分封顶健康指数**（40+30+30），明确 M14（4 组 9 项）➔ M15（4 组 10 项）无死链菜单演进并对齐 `F-15`；2. `F-25` 明确 `traefik_golden_suite_items` 采用**完全自包含二进制+闭包快照**（免疫 30 天流量分区 `DROP TABLE`）并解耦 `ReplayTask` 双源外键；3. `F-26` 补齐 Service/Method 签名与 `ReservedRange` 级 AST 规则，增设管理员 `allow_breaking + breaking_reason` 回滚/重构审计逃生舱；4. `F-27` 补齐 Agent 重放期 `ObservedEgressCalls` 回传以单次闭环 `GATED_VIOLATION` 判定，增加 `wait_lease_seconds` 排队防并发踩踏、`503 vs 422` 状态码正交与 `X-Traefik-CI-Token` SHA-256 常量时间鉴权 |


---

## 1. 系统愿景与产品定位 (Vision & Positioning)

### 1.1 项目愿景与产品本质
**业务流量与接口契约治理 (`code-traefik`)** 是挂载于 CodeBench 开发者综合工作台的企业级业务子系统，**只服务于业务实际使用的 DOPHI `CROS RPC` 协议**（`24B 公共消息头 RPC_COMM_HEAD_S` + `会话头 RPC_REQ/RSP/MNG_HEAD_S` + `0..N 段扩展头` + `序列化 Payload`），不做任何其他协议扩展。

传统契约管理（Swagger、Postman、Proto 仓库等）多停留在“设计态的静态文档”，管不住生产运行时的真实调用，无法察觉微服务间的私自跨层依赖与接口破坏。
本系统的本质是**「以契约治理为核心、以运行态真实流量为活水、以零侵入仿真为闭环的活体契约治理与防劣化平台」**：
* **“流量录制与重放”是底层技术手段**：提供零代码侵入、零业务改造的真实报文提取与本地仿真注入；
* **“契约治理、架构防漂移与防劣化验证”是真正的业务目的**：

目标用户与治理价值矩阵：

| 角色 | 核心痛点 | 本平台提供的治理能力 |
| :--- | :--- | :--- |
| **架构师 / 模块负责人** | 架构设计文档与代码实际调用漂移，私自跨层调用、旁路直连无从核对，架构逐渐腐化 | 维护三级架构元素与模块接口契约 SSOT，拿实测流量自动比对设计态拓扑，毫秒级红牌告警 **`Violation`（违规调用）** 与 **`Uncovered`（僵尸死接口）** |
| **业务开发 / 测试工程师** | 跨服务联调与回归测试严重依赖完整下游环境；接口变更极易破坏兼容性导致线上故障 | 基于 **`ScopedMock` 因果闭包**实现零下游单进程脱机重放，提供协议状态码与 Protobuf 字段级**双层自动化回归 Diff 断言** |
| **平台管理员 / 质量负责人** | 传统抓包排障需要修改业务代码或整机 Root 特权；接口与 Proto 资产分散，无人认领 | 每台业务机器部署 1 个零 Schema 依赖的 Agent；提供多 `.pb` 描述集资产台账、字段字典树透视与未认领“孤儿契约”治理闭环 |

系统采用 **「控制面主框架 (`code-traefik-server`) + 独立数据面代理 (`code-traefik-agent`)」同仓双二进制物理防腐** 架构：

1. **控制面 `code-traefik-server`**（保持在 `code-bench` 微前端家族内）：作为架构元素与接口契约的单一真实源（SSOT，选型依据见 §2.2 与 Non-Goal 7），负责三级架构元素与接口契约建模、Agent 纳管与排他租约、Protobuf 描述集资产编解码、实测拓扑还原与架构一致性核验、重放编排与双层回归 Diff 报告。
2. **数据面 `code-traefik-agent`**（同仓 `cmd/agent`）：零前端、零数据库、零 `.pb` 依赖的守护进程，每台业务机器部署 1 个，同时监控本机多个目标进程（`1 Agent : N Processes`）；负责内核态报文采集、原始二进制回传、本地重放注入、下游 ScopedMock 接管与 `ullSNO` 自动重写自愈。

### 1.2 核心价值主张 (Core Value Propositions)
1. **全景接口契约资产化治理**：支持多 `.pb` 描述集拓扑去重、消息体字段字典树透视、可视化 Diff 忽略基线配置与未认领“孤儿契约”闭环。
2. **零侵入内核态动态探针**：基于 `cilium/ebpf` 引擎，出入站双向无感采集，单机多进程 `O(1)` BPF Map 过滤，免除业务改造成本与跨机同步 Schema 负担。
3. **架构防漂移与违规调用阻断雷达**：自主维护「子系统 → 功能组（树）→ 功能模块」与「1 进程 : N 模块」映射，以功能模块为接口契约主体；依据实测报文的 `(ascServiceName, ascOperation)` 还原跨进程真实调用，精准捕获架构漂移。
4. **零下游因果闭包重放与自愈引擎**：利用协议结构特征与 `ScopedMock` 因果闭包，Agent 零 `.pb` 依赖实现下游 Mock 接管（0 字节外泄真实下游）与 `ullSNO` / `aucTraceBuff` 自动回写自愈。
5. **协议头 + Protobuf 字段级双层回归 Diff**：第一层协议状态码与魔术字校验 + 第二层业务字段语义 Diff，秒级定位破坏性改动。

---

## 2. 关键业务假设、系统拓扑与数据模型

### 2.1 关键业务假设 (Key Assumptions，已与业务方确认)

以下假设是本版需求边界的前提，任一假设被推翻都需要重新评审相关特性：

| 编号 | 假设 | 影响的特性 |
| :--- | :--- | :--- |
| **A-01** | 业务进程之间普遍使用 **CROS RPC 长连接**；录制 / 重放前**可以接受重启目标进程或触发重连** | F-08、F-09、F-11、F-12 |
| **A-02** | 每个操作系统进程**只监听 1 个** CROS RPC 端口 | F-02、F-08 |
| **A-03** | 每个功能模块全局只归属 **1 个标准架构进程 (`ArchProcess`)**；**1 台 Agent 即代表 1 套独立运行环境**（支持多套 `SHARED` 公共测试环境与多套 `PRIVATE` 个人环境），单套环境（单个 Agent）内同一标准进程只部署 **1 份**（`1:1` 挂载，不存在单环境内多副本负载均衡） | F-02、F-05、F-12 |
| **A-04** | 同一进程内的功能模块之间走**进程内函数调用**，不经过网络 | F-02、F-05 |
| **A-05** | 同一 `ascServiceName` **只由 1 个功能模块供给**（全局唯一 PROVIDE），模块间依赖显式绑定「提供方 FM 及其 Service」，或声明依赖外部系统 | F-02、F-03、F-05 |
| **A-06** | 重放**只在测试环境**执行；目标进程对 DB / MQ 等非 CROS RPC 依赖的副作用首期不处理（DB 中主要是很少变化的元数据） | F-12、Non-Goal 8 |
| **A-07** | 服务名匹配 Protobuf 的点分全名：`ascServiceName` 格式为 `{package}.{serviceName}`（若未声明 `package` 则直接为 `{serviceName}`）；`ascOperation` 与 Method 名称**字符串直接相等** | F-03、F-10 |
| **A-08** | 首期聚焦 REQ / RSP 业务调用；MNG 管理消息（`ucMsgType=5/6/7`）只做解析展示与原样回放，不做握手语义仿真 | F-12、F-13、Non-Goal 9 |

### 2.2 主从运行架构与网络交互拓扑

```mermaid
flowchart TB
    subgraph Portal["CodeBench 微前端控制台 (/traefik | :5181 -> :8091)"]
        UI_Dash["全局概览组: 治理大盘 (/dashboard 默认落地 · 三维健康分)"]
        UI_Flow["契约核验与回归组: 契约采集 / 事务分析 / 防劣化回归 / 黄金用例库"]
        UI_Arch["架构与契约组: 架构拓扑 / 架构元素 / Proto 契约"]
        UI_Env["环境与治理组: Agent 与进程 / 模块参数配置"]
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
- **入站 (INGRESS)**：eBPF 按「目标 PID + 监听端口」**被动复制**收发字节，不代理、不改变业务链路；`ARMED` 待命期**不挂载**高频读写 tracepoint（零系统调用读写损耗），仅在活跃录制期（`CAPTURING`）按需挂载并预热存量入站连接。
- **出站 (EGRESS)**：`cgroup/connect4` 把目标 PID 访问已登记下游 `IP:Port` 的连接重定向到 Agent `EgressProxy`：预挂待命期（`ARMED / PASSTHROUGH`）持续做纯字节双向转发且零分帧零上报；活跃录制期（`CAPTURING`）在既有长连接上动态切帧上报；离线重放期（`REPLAY_MOCK`）对既有代理连接与新建连接统一执行帧级二进制 Mock 自愈应答并彻底阻断向真实下游写数据（零外泄）。
- **长连接前置就绪与免重连录制 (A-01 & ADR-09)**：出站重定向只对**挂载探针之后新建的连接**生效。系统采用**默认自动预挂 (`ARMED`)** 策略：Agent 在线挂载进程后自动挂载建连探针并诊断存量直连连接（若 `established_conns > 0` 提示在空闲准备期重启/重连一次；后于探针启动的进程经 `sched_process_exec/fork` 追踪纳管）；**停止录制时仅关闭采集闸门回退纯字节转发，绝不断开代理长连接**，使 Agent 存活期内一次就绪后后续第 $2 \dots N$ 次录制实现**免重连亚秒级启停**。

### 2.3 架构元素与运行映射数据模型

**SSOT 选型依据**：`code-proto` 专注 Protobuf 仓库管理、`.proto` 解析与接口变更 MR 追踪，没有「子系统-功能组-功能模块-标准业务进程」概念，也没有运行态进程挂载；`code-bench` 只管理用户、组织与审计。两者都不能作为架构元素的可靠来源，因此由 `code-traefik` 在自己的 `traefik_*` 表中闭环维护。

下图只描述实体关系与关键约束，完整物理建模见阶段二 [`05-database-schema.md`](../02-design/05-database-schema.md)：

```mermaid
erDiagram
    TRAEFIK_SUBSYSTEMS ||--o{ TRAEFIK_FUNCTIONAL_GROUPS : "包含"
    TRAEFIK_FUNCTIONAL_GROUPS ||--o{ TRAEFIK_FUNCTIONAL_GROUPS : "parent_id 自嵌套"
    TRAEFIK_FUNCTIONAL_GROUPS ||--o{ TRAEFIK_MODULES : "包含"
    TRAEFIK_MODULES ||--o{ TRAEFIK_MODULE_SERVICES : "声明 PROVIDE"
    TRAEFIK_MODULES ||--o{ TRAEFIK_MODULE_DEPENDENCIES : "声明 DEPEND"
    TRAEFIK_ARCH_PROCESS_GROUPS ||--o{ TRAEFIK_ARCH_PROCESSES : "包含标准进程蓝图"
    TRAEFIK_ARCH_PROCESSES ||--o{ TRAEFIK_MODULES : "arch_process_id 组装模块 (1:N)"
    TRAEFIK_AGENTS ||--o{ TRAEFIK_PROCESSES : "纳管宿主机挂载实例"
    TRAEFIK_ARCH_PROCESSES ||--o{ TRAEFIK_PROCESSES : "arch_process_id 实例化 (单机 1:1)"
    TRAEFIK_PROTO_SCHEMAS ||--o{ TRAEFIK_PROTO_OPERATIONS : "解析出"
    TRAEFIK_RECORDING_SESSIONS ||--o{ TRAEFIK_TRAFFIC_RECORDS : "归档分区"
    TRAEFIK_PROCESSES ||--o{ TRAEFIK_TRAFFIC_RECORDS : "采集自"
    TRAEFIK_ARCH_PROCESSES ||--o{ TRAEFIK_GOLDEN_SUITES : "绑定标准进程黄金套件"
    TRAEFIK_GOLDEN_SUITES ||--o{ TRAEFIK_GOLDEN_SUITE_ITEMS : "自包含报文与闭包快照"
    TRAEFIK_RECORDING_SESSIONS |o--o{ TRAEFIK_REPLAY_TASKS : "可选批次重放源"
    TRAEFIK_GOLDEN_SUITES |o--o{ TRAEFIK_REPLAY_TASKS : "可选黄金套件重放源"
    TRAEFIK_REPLAY_TASKS ||--o{ TRAEFIK_REPLAY_ITEMS : "包含单条断言明细"

    TRAEFIK_ARCH_PROCESS_GROUPS {
        string code "全局唯一进程组编码"
        string name "进程组名称"
    }
    TRAEFIK_ARCH_PROCESSES {
        bigint group_id FK
        string code "全局唯一标准进程编码"
        int default_listen_port "0..65535 (0=纯客户端)"
        string default_cmd_pattern "默认 /proc 匹配特征"
    }
    TRAEFIK_PROCESSES {
        bigint agent_id FK
        bigint arch_process_id FK "UK(agent_id, arch_process_id)"
        string probe_desired_state "ARMED(默认) | DISARMED"
        int override_listen_port "可选本地端口覆盖"
        string override_cmd_pattern "可选本地命令行覆盖"
    }
    TRAEFIK_PROTO_OPERATIONS {
        bigint schema_id FK
        string service_name "格式为 {package}.{serviceName} (点分连接)"
        string operation "与 Method 名字符串直接相等"
        string req_message
        string rsp_message
    }
    TRAEFIK_REPLAY_ITEMS {
        bigint task_id FK
        bigint record_id FK
        string result_status
    }
```

关键约束：
1. **三级逻辑架构**：子系统是顶层边界；功能组归属子系统，支持多级自嵌套（`parent_id` + `path` + `level`）；功能模块是最小契约单元，可挂在**任意层级**的功能组下。
2. **接口契约在功能模块层面**：`PROVIDE` 为模块对外实现的 CROS RPC 服务（`traefik_module_services`），`DEPEND` 为模块需要调用的下游服务（`traefik_module_dependencies`）。**`PROVIDE` 的 `service_name` 全局唯一 (A-05)**；其中 `ascServiceName` 为 `{package}.{serviceName}`（无 `package` 时为 `{serviceName}`），`ascOperation` 为方法名。
3. **架构物理进程蓝图 SSOT 与「单 Agent 单环境」`1:1` 挂载覆盖**：
   - 全局唯一的 `ArchProcessGroup ➔ ArchProcess` 在 `/architecture?tree=physical` 中定义标准进程分组、默认运行参数（`default_listen_port`、`default_cmd_pattern`）以及与 `Module` (`FM`) 的组装关系（1 个 `ArchProcess` 承载 $N$ 个 `Module`，每个 `Module` 全局至多归属 1 个 `ArchProcess`，遵循 `A-03`）；
   - **1 台 `Agent` 即代表 1 套独立环境**：各环境 `Agent`（含多套 `SHARED` 公共测试环境与多套 `PRIVATE` 个人环境）统一从已定义的 `ArchProcess` 树中勾选挂载进程（`traefik_processes`），同一台 `Agent` 上同一 `ArchProcess` 严格 **`1:1` 单实例（`UNIQUE(agent_id, arch_process_id)`）**，不同 `SHARED` Agent（不同公共环境）允许各自独立挂载同一 `ArchProcess`；
   - Agent 支持可选配置 `fallback_agent_id`（指向某套 `SHARED` 公共基准环境用于混合联调下游推导），挂载实例支持本地覆盖 `override_listen_port`、`override_cmd_pattern`、`advertise_ip` 与 `extra_downstreams`，同一 Agent 下非零有效监听端口互斥（`A-02`）；PID 由 Agent 运行期动态扫描 `/proc` 并回写统一 7 态探针就绪快照。
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
| **F-02** | **架构物理进程蓝图、模块服务契约与多环境 1:1 挂载覆盖**（`traefik_arch_process_groups` / `traefik_arch_processes` / `traefik_processes` / `traefik_module_services` / `traefik_module_dependencies`） | `Server`<br>`Frontend` | **P0** | 1. **架构物理进程蓝图 SSOT (`/architecture?tree=physical`)**：全局定义 `进程组 (ArchProcessGroup) ➔ 标准业务进程 (ArchProcess)` 二级树，维护标准进程 `code`、`name`、`default_listen_port`（`0` 为纯客户端）、`default_cmd_pattern`（子串或 `re:` RE2 正则）、`default_extra_downstreams` 及组装承载的 `Module` (`FM`) 集合（每个 `FM` 全局至多归属 1 个 `ArchProcess`，遵循 `A-03`），零 Agent 耦合；<br>2. **「1 台 Agent = 1 套环境」单机 `1:1` 挂载与本地覆盖 (`/agents`)**：各环境 Agent（含多套 `SHARED` 公共测试环境与多套 `PRIVATE` 个人环境）仅允许从已定义的 `ArchProcess` 树中勾选挂载进程（`UNIQUE(agent_id, arch_process_id)` 单机严格 `1:1`；不同 `SHARED` 环境允许各自挂载同一 `ArchProcess`），支持本地覆盖 `override_listen_port`、`override_cmd_pattern`、`advertise_ip`、`extra_downstreams` 以及可选 `fallback_agent_id` 关联公共基准环境；修改 `ArchProcess` 默认端口遇局部冲突时自动钉住冲突实例的 `override_listen_port`；<br>3. **模块服务供给 (`PROVIDE`) 与依赖 (`DEPEND`)**：契约收敛至 Service 级别，全系统内 **`PROVIDE` 的 `service_name` 全局唯一** (A-05)，`DEPEND` 显式关联提供方 FM 的 Service 或外部第三方系统；<br>4. **静态依赖与混合下游精确 `IP:Port` 推导**：静态依赖由 FM 间 Service 依赖推导；Agent 监控下游端点按「同机挂载优先 ➔ 关联 `FallbackAgentID` 公共环境挂载兜底 ➔ 同机全量已挂载端口 + 补充端点」推导，并在预挂期一次性编译超集下发。 | 1. 标准进程组与标准进程 CRUD 单测通过；将同一 `Module` 重复分配给第二个 `ArchProcess` 返回 `409`；删除已被 Agent 挂载的 `ArchProcess` 返回 `409`；<br>2. 同一 Agent 重复挂载同一 `ArchProcess` 或同机非零有效端口冲突返回 `409`；允许多个 `listen_port = 0` 纯客户端并存；两台不同 `SHARED` Agent（如 SIT 与 UAT）挂载同一 `ArchProcess` 成功通过；<br>3. 第二个模块声明已被供给的 `service_name` 返回 `409`；循环自依赖拦截；<br>4. 静态依赖推导与多环境 `FallbackAgentID` 混合下游推导单测全部通过；<br>5. 卸载正在录制或重放租约中的挂载进程返回 `409`（支持 `force` 紧急卸挂）。 |
| **F-03** | **Protobuf 描述集注册与接口映射**（`traefik_proto_schemas` / `traefik_proto_operations`） | `Server`<br>`Frontend` | **P0** | 1. **上传与解析**：上传 `.pb`（`FileDescriptorSet`），校验体积 `<= 16MB` 与 `protodesc` 依赖闭包合法性；同名描述集每次上传生成新递增版本，支持原子切换生效版本；<br>2. **服务映射生成**：从 `ServiceDescriptor` / `MethodDescriptor` 自动提取 `(ascServiceName, ascOperation) → (Req, Rsp Message)` 映射；服务名采用点分全名，**`ascServiceName` 格式为 `{package}.{serviceName}`**（无 `package` 时为 `{serviceName}`），**`ascOperation` 与 Method 名称直接对应** (A-07)；<br>3. **全局唯一与防冲突**：生效映射中 `(service, operation)` 全局唯一，激活存在重叠冲突时返回 `409 Conflict` 并回滚；<br>4. **历史不可变**：流量记录保存解码所用的 schema 版本，切换版本不改写历史解码结果（支持手动重解码）。 | 1. 合法 `.pb` 上传后，映射接口返回正确的 `(service, operation)` 清单（服务名采用 `{package}.{serviceName}` 点分全名）；<br>2. 损坏文件或超过 16MB 返回 `400`；<br>3. 激活与其他生效描述集存在 `(service, operation)` 冲突的版本返回 `409`；<br>4. 切换版本后，历史记录的 schema 版本字段保持不变。 |
| **F-03.1** | **两级嵌套折叠契约表与服务聚合视图**（嵌套 Master-Detail 呈现） | `Frontend`<br>`Server` | **P0** | 1. **两级折叠模型**：将平铺刷屏的映射表重构为“服务主行 (Service Row) ➔ 方法子表 (Operation Table)”两级嵌套树形折叠表；<br>2. **外层服务行**：以 `{package}.{serviceName}` 为粒度聚合，展示契约全名、所属源 Proto 文件、归属架构模块（如已认领）、RPC 方法总数（Unary/Stream 分布）、生效态；行首提供折叠箭头，支持行展开及表头【一键全部展开/折叠】；<br>3. **内层方法表**：展开后展示该服务独享的方法列表（方法名、请求消息体、应答消息体、流式模式、忽略字段数、生效态与操作）；<br>4. **检索穿透与高亮**：输入搜索词时实时跨层过滤，命中的服务行自动展开并高亮匹配方法。 | 1. 相同 `service_name` 的方法 100% 聚合到单行展示，不再出现服务名前缀刷屏；<br>2. 折叠/展开动画流畅，支持多行同时独立展开与一键全部展开/折叠；<br>3. 搜索关键词能穿透展开并高亮命中方法。 |
| **F-03.2** | **消息体参数定义字典树查看与源码注释透视**（Message Schema Inspector Tree） | `Server`<br>`Frontend` | **P0** | 1. **服务端动态字典树反射**：服务端依据 `FileDescriptorSet` 动态提取指定 Message 的全量字段反射元数据树（字段名、字段编号 Tag、类型、规则 `optional/required/repeated/map`、默认值、源码注释 `leading_comments`）；<br>2. **可点击交互**：表格中的 `req_message`、`rsp_message` 及【查看契约】支持直接点击，右侧无缝滑出抽屉式**消息体字典树 (Schema Inspector Tree)**；<br>3. **多层嵌套递归展开**：支持复杂嵌套 Message、Repeated 数组元素及 Map 结构的逐层展开/收起与类型高亮。 | 1. 接口 `GET /api/proto-operations/:id/schema-tree` 返回请求与应答的完整多层树结构与注释；<br>2. 前端树形控件能正确展开多层嵌套 Message，无递归死循环；<br>3. 字段类型（如 `int64`、`string`、`repeated`）带语义化颜色徽章。 |
| **F-03.3** | **可视化 Diff 忽略字段标记与多级继承模型** | `Server`<br>`Frontend` | **P0** | 1. **字段树可视化勾选**：在参数字典树节点行提供【忽略对比】复选框，用户点击即可自动生成精准的字段点分路径（如 `header.timestamp`、`items[*].create_time`），彻底替代人肉手写；<br>2. **方法级持久化**：在 `traefik_proto_operations` 增加 `diff_ignore_paths` 字段并持久化保存；<br>3. **多级继承与合并生效**：回归 Diff 引擎按 `ReplayTask.ExtraIgnorePaths ∪ Operation.DiffIgnorePaths ∪ ModuleService.DiffIgnorePaths` 优先级合并生效，既支持服务级全局忽略，又支持方法级精细化忽略。 | 1. 勾选字段树后自动生成规范路径，调用 `PUT /api/proto-operations/:id/ignore-paths` 保存成功；<br>2. 流量回放断言引擎（`diffMessageReflect`）准确过滤被勾选的动态字段，回归结果判定为 `PASS`；<br>3. 字段树上被标记忽略的节点呈现清晰的已忽略视觉标识。 |
| **F-03.4** | **描述集制品库生命周期管理、原文件导出与架构认领血缘** | `Server`<br>`Frontend` | **P0** | 1. **原二进制文件导出**：提供 `GET /api/proto-schemas/:id/download` 接口，支持用户随时导出下载原始 `.pb` 二进制制品；<br>2. **包含 Proto 源文件清单透视**：描述集详情中提取并展示所打包的 `.proto` 源文件列表与 Service 拓扑；<br>3. **架构认领血缘管理**：联动 `traefik_module_services`，统计并可视化标记当前描述集内服务的认领率，标识已被哪些架构模块认领，高亮提示“未认领孤儿契约”，推动契约与架构基线治理闭环。 | 1. 管理员与研发用户可正常下载已上传的 `.pb` 文件，SHA256 校验与库内一致；<br>2. 页面清晰列出包含的所有 `.proto` 源码文件名；<br>3. 服务契约正确展示其关联的架构模块名称及未认领状态提示。 |
| **F-03.5** | **多描述集制品资产台账、按制品聚合与服务级分页双模工作台** | `Frontend`<br>`Server` | **P0** | 1. **双模视窗与 URL 同步**：`/schemas` 顶部配备全局生效池健康摘要栏与 `[📁 描述集制品台账 (?tab=assets)]` / `[🌳 全局契约目录树 (?tab=catalog)]` 切换（兼容跨工作台深链直达目录树）；<br>2. **按制品聚合全景宽表**：默认开启 `latest_only=true` 按逻辑制品 `name` 折叠聚合（优先展示生效版本，否则取最新版本，附 `共 N 版` 徽章），零 N+1 批量呈现单一枚举 `category` (`BASE_LIB \| BUSINESS \| GATEWAY`)、文件大小、SHA256、自有 `.proto` 数、服务数、方法数、外部依赖血缘、架构认领率、上传人与时间；支持行内快捷【⬆️ 新版本】（预填锁定 `name/category`）、元数据轻量编辑 (`PATCH`)、详情与全版本历史抽屉；<br>3. **服务级分页全局契约目录**：新增 `GET /api/proto-services` 按 `Service` 粒度聚合分页（完整携带名下全部 `operations`、来源 `.pb` 徽章与架构认领模块），支持 `claimed_status=UNCLAIMED` 筛选孤儿契约，根治扁平方法分页劈裂同一服务的问题。 | 1. 同名多版本默认聚合为 1 行代表制品并可切换平铺或打开历史抽屉回滚，白名单排序与穿透搜索（名称/Tag/`.proto`/Service）单测通过；<br>2. `GET /api/proto-services` 保证单服务方法 100% 不跨页割裂，`UNCLAIMED` 孤儿过滤准确；<br>3. 列表查询零 N+1 且绝不返回 `descriptor_bytes` 大字段。 |
| **F-03.6** | **跨描述集去重拓扑闭包解析、`BASE_LIB` 底座与忽略规则向前继承** | `Server` | **P0** | 1. **自有文件与外部依赖分离去重**：解析区分 `proto_files`（自有文件）与 `external_imports`（缺失外部 import），按「自有文件 $\succ$ 生效 `BASE_LIB` $\succ$ 其他生效制品 + 标准 `google/protobuf/*` WKT」优先级按文件名去重与 DAG 拓扑排序构建 `protodesc.NewFiles`；**仅提取自有文件内的 `ProtoOperation`**（消除重复提取父依赖 Service 伪冲突），原生支持 `0` Service 纯类型 `BASE_LIB`；<br>2. **运行期联合编译与历史版本字典树反射**：`Registry.Reload()` 联合编译全局统一 `CompiledRegistry.Files`，`GetOperationSchemaTree` 对未激活历史版本自动合并生效池/`BASE_LIB` 反射字段树；<br>3. **同名升级 `DiffIgnorePaths` 向前自动继承与 16MB 扩容**：上传同名 `version > 1` 时自动按 `(service_name, operation)` 继承上一版本非空 `diff_ignore_paths`；单文件流式上限提升至 `16MB`。 | 1. 纯类型 `base.pb` (`0` Service) + 未带 `--include_imports` 的 `order.pb` + 带重复 `base.proto` 的 `pay.pb` 联合上传激活后，无重名注册报错、无父 Service 重复冲突，运行期解码与字典树反射 100% 正常；<br>2. 上传同名 `v2` 自动保留 `v1` 方法级 `diff_ignore_paths`；<br>3. 依赖缺失返回 `400` 并列明具体缺失 `.proto` 文件名。 |
| **F-03.7** | **系统全局单一生效池、`CompileTrialPool` 预编译与停用反向依赖门禁** | `Server` | **P0** | 1. **全环境单一版本基线与咨询锁串行化**：全系统所有环境/Agent 共享唯一 `atomic.Pointer[CompiledRegistry]`；所有写事务通过 `pg_advisory_xact_lock` 串行化防并发 TOCTOU 竞态；<br>2. **`CompileTrialPool` 激活/批量激活/停用双向硬门禁**：新增 `POST /api/proto-schemas/:id/deactivate`（停用下线）与 `POST /api/proto-schemas/batch-activate`；任何生效池变更提交前，对候选生效池 $S_{\text{next}}$ 强制执行「跨集 `(service_name, operation)` 唯一性校验 + `CompileTrialPool(S_next)` 联合预编译与 Message 符号可解析干跑」，若存在方法冲突或停用/升级基础库破坏下游制品闭包，立即回滚并返回结构化 `409 Conflict`（`conflicts` 或 `broken_dependents`）。 | 1. `base.pb`（未激活）+ `order.pb`（未激活）通过 `batch-activate` 一次性原子联合激活成功；同批次传同名两版本返回 `400`；<br>2. 尝试 `deactivate` 正被生效 `order.pb` 依赖的 `base.pb`，或激活删减了被引用 Message 的 `base v2`，均精确返回 `409` 及 `broken_dependents` 且线上生效池毫发无损；<br>3. 无下游依赖的生效制品可正常 `deactivate` 并随后 `delete`。 |
| **F-04** | **分层复合架构拓扑与一致性画布** | `Server`<br>`Frontend` | **P0** | 1. **全系统分层复合图**：基于 React Flow + ELK.js (Web Worker) 实现全系统单画布正交连线与嵌套矩形框排版，支持平移、缩放、节点拖拽（不落盘）、小地图与 CSS Design Tokens 双模自适应；<br>2. **逻辑架构视图**：按「全系统 → 子系统 → 8级功能组树 → 功能模块」分层展开；默认通过双击或层级滑杆显式展开，支持语义自动展开可选开关；前端实时执行叶子边汇总 (Rollup) 并标注内部依赖数与环路标红；<br>3. **物理部署视图**：100% 由全局架构物理进程蓝图（`ArchProcessGroup → ArchProcess → Module`）直接推导组织（彻底去 Agent 化，尚未纳管任何 Agent 时同样完整出图；支持可选传入 `env_agent_id` 富化展示指定环境 Agent 的 `EffectiveIP:EffectiveListenPort`）；<br>4. **一致性比对叠加层**：F-05 验证结果直接作为拓扑画布的增强图层，按最差优先级状态收敛上卷，并支持穿透联动四段报文解剖。 | 1. 拓扑 Graph API 输出包含树关系与原子叶子边，零 Agent 注册场景下物理视图仍完整输出 `PROCESS_GROUP -> PROCESS -> MODULE` 且零 `AGENT` 节点；<br>2. 纯算法单测：折叠汇总、展开下钻、最差状态收敛及 Tarjan 环路检测覆盖率 `≥ 90%`；<br>3. 画布 100% 采用 `--color-*` 语义 Token，亮/暗主题无缝适配。 |
| **F-05** | **实测调用拓扑还原与架构一致性验证** | `Server`<br>`Frontend` | **P0** | 基于一次录制批次 (F-11) 的报文：<br>1. **目的模块定位**：按 `ascServiceName` 查唯一的 `PROVIDE` 模块；查不到时归为「未登记接口」；<br>2. **来源模块判定**（进程承载多个模块时）依次采用：① 出站请求的 `FatherSpanID` 关联到同进程某条入站请求所在的模块；② 该进程内**只有一个**模块声明了对应 `DEPEND`；③ 以上都不满足时，标记为「进程级归属」并给出候选模块集合；<br>3. **比对结果分类**：`Verified`（实测且已声明）、`Violation`（实测但未声明，架构漂移告警）、`Uncovered`（已声明但本次监控范围内未触发）、`Not Monitored`（目标进程未纳入本次录制监控，不属于未触发）、`Not Observable`（同进程内两模块之间的依赖，A-04 决定网络上不可见，不计入 Uncovered）、`Unattributed`（来源只能归到进程级）。 | 1. 输入预置的上中下游三进程录制数据集（含 1 条未声明调用、1 条同进程内依赖、1 条未监控进程依赖、1 条无 Trace 且多模块声明同一 `DEPEND` 的调用），验证 API 分别标出 `Violation`、`Not Observable`、`Not Monitored`、`Unattributed`，其余边为 `Verified` / `Uncovered`；<br>2. 每类结果都有独立的单测用例。 |

---

### 3.2 Agent 纳管与受控信道领域

| 编号 | 特性名称 | 涉及组件 | 优先级 | 功能描述与技术规范 | TDD 验收指标 |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **F-06** | **Agent 公私域纳管与动态排他租约**（`traefik_agents`） | `Server`<br>`Frontend` | **P0** | 1. **公私域归属**：Agent 分为公共级（`SHARED`，全团队可见共用）与个人级（`PRIVATE`，普通用户可自建管理，**仅创建人本人及管理员可见与使用**）；登记名称、唯一访问地址（`IP:Port`）、作用域与描述；<br>2. **主控直接连接**：主控运行于同一内网，直连各宿主机 Agent 开放的管控端口，并实施 SSRF 内网白名单校验与 Claim Code 动态认领防抢注；<br>3. **动态排他租约 (`Mutex Lease`)**：用户发起录制会话或重放任务时动态锁定目标 Agent，**同一台 Agent（无论 `SHARED` 还是 `PRIVATE`）同一时间只能由 1 名用户执行 1 项操作**；他人或并发任务争用时返回 `409 Conflict` 并提示当前持锁人与任务信息；任务结束、超时或 Agent 离线时自动释放租约，管理员支持强制解锁；<br>4. **双向租约对账心跳**：主控与 Agent 周期性心跳对账（默认 5s），下发期望租约并感知 `boot_id` 宿主机重启，快速收敛孤儿任务与失效租约。 | 1. 公共与私有 Agent CRUD 单测通过，地址重复返回 `409`；普通用户 B 访问或租用普通用户 A 的 `PRIVATE` Agent 返回 `403`；`traefik_admin` 可查看全部 `PRIVATE` Agent；<br>2. 用户 A 正在使用某 `SHARED` Agent 录制/重放时，用户 B 请求租用同一 Agent 返回 `409`；任务结束后用户 B 租用成功；<br>3. 停止 Mock Agent 后 3 个心跳周期内变为 `OFFLINE` 并自动释放悬空租约；<br>4. 协议主版本不一致返回明确错误码；删除仍纳管进程的 Agent 返回 `409`。 |
| **F-07** | **主控—Agent 受控信道（HTTP + WebSocket）** | `Server`<br>`Agent` | **P0** | 1. 同仓双入口 `cmd/server` / `cmd/agent`；<br>2. **HTTP REST**：主控向 Agent 下发心跳、进程探查、监控元信息、录制启停、重放任务；<br>3. **WebSocket（二进制帧）**：主控连接 Agent，Agent 推送原始报文事件与重放结果；首期不实现 Agent 侧 SSE；<br>4. **背压与弹性**：Agent 为每个录制会话设置有界缓冲（默认 64MB），缓冲满时丢弃新帧并累计 `dropped_frames` 上报；信道断开后主控自动重连；主控失联超过宽限期（默认 5 分钟）时，Agent 自动停止全部监控并卸载探针；<br>5. **首期连接控制**：不启用 Token，源 IP 白名单中间件（`--allow-ips`，默认只放行 `127.0.0.1` 与配置的主控 IP），预留 `TODO(security)` 鉴权扩展位。 | 1. 本地集成测试（监听 `127.0.0.1`）中，主控通过 HTTP 下发并建立 WebSocket，完整收到 Agent 事件；<br>2. 非白名单源 IP 访问 Agent 返回 `403`；<br>3. 模拟主控消费阻塞，`dropped_frames` 正确累加且 Agent 内存不超过缓冲上限；<br>4. 断开信道后主控自动重连成功；超过宽限期后 Agent 探针已卸载。 |
| **F-08** | **多进程默认自动预挂载 (`ARMED`)、PID 定位与统一 7 态就绪诊断** | `Server`<br>`Agent` | **P0** | 1. **默认自动预挂 (`ARMED` 待命，不占排他租约)**：当 Agent 处于 `ONLINE` 时，保存/更新进程挂载（`probe_desired_state` 按 `traefik_settings` 缺省 `'ARMED'`）或心跳对账发现 `ArmedDigest` 不一致时，主控经单机 Single-Flight 锁携带单调 `mounts_revision` 下发 `PUT /agent/v1/monitors/arm`（失败指数退避）；支持按进程/组/整机切换 `DISARMED`（含 `force` 紧急熔断卸挂）；<br>2. **`/proc` 实时反查与生命周期追踪**：Agent 通过 `/proc` 反查实时 PID，配合 `sched_process_exec`（用户态匹配 `cmdline`）、`sched_process_fork`（子进程同步继承）、`sched_process_exit`（退出即清防 PID 复用）+ 2s 周期巡检追踪进程生命周期；<br>3. **基于进程自有 FD Inode 的统一 7 态存量连接就绪诊断 (A-01 / ADR-11)**：通过 `/proc/<pid>/fd` 自有 Socket Inode 与 `/proc/<pid>/net/tcp` 求交集（严格隔离同机 `agent_pid` 及其他进程连接，并排除入站 `listen_port` 连接与已受控的 `127.0.0.1:proxyPort` 代理连接），精准统计绕过代理的存量直连出站连接（单进程无预配下游时统计该进程全部未代理直连出站连接），回写 7 态快照：`🟢 READY`、`🟡 NEED_RECONNECT`、`🔵 WAITING_PROC`、`🟣 MONITORING`、`⚪ DISARMED`、`⚫ OFFLINE`、`🔴 CAP_DENIED / ERROR`；<br>4. 支持扫描宿主机候选监听进程并自动匹配架构 `ArchProcess` 树。 | 1. 在线 Agent 挂载进程后无需发起录制即自动进入 `ARMED` 待命（`lease_type == ''`），同机存在 `agent_pid` 或无关进程连接时绝不误报 `NEED_RECONNECT`，目标进程重连接入 `EgressProxy` 后立即转为 `READY`；<br>2. Agent 模拟重启（`BootID` 变更）后，心跳对账自动补推 `/monitors/arm` 恢复预挂；旧 `Revision` 请求被幂等拒绝；<br>3. 手动 `disarm` 某进程后该进程规则热移除，全部 `disarm` 后卸载钩子并优雅排水存量连接；<br>4. 录制期间进程重启或 `fork` 子进程，流量持续采集，进程退出后 PID 条目及时清理。 |

---

### 3.3 Agent 采集引擎与 CROS RPC 协议栈领域

| 编号 | 特性名称 | 涉及组件 | 优先级 | 功能描述与技术规范 | TDD 验收指标 |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **F-09** | **Agent 嵌入式 `cilium/ebpf` 采集引擎（目标 PID 默认全出站透明代理、按需入站钩子与三态闸门）**（`pkg/ebpf`） | `Agent` | **P0** | 基于 `github.com/cilium/ebpf` 在 Agent 内挂载探针，服务本机多个目标进程：<br>1. **按需入站钩子与存量入站 Socket FD 预热**：`ARMED` 待命期**仅挂载低频建连与进程生命周期钩子**（`connect4`/`sockops`/`sched_process_*`），**不挂载高频读写 tracepoint**（待命期零读写拦截损耗）；`StartCapture` 时扫描 `/proc/<pid>/fd` 预热目标监听端口已建立的存量 `TCP_ESTABLISHED` 入站连接至 `ing_conn_map`（配合 `accept/accept4/close` 增量维护，严禁对非监听端口任意 `fd > 2` 盲目建表）并挂载读写 tracepoint，`StopCapture` 时立即卸载读写钩子；<br>2. **目标 PID 默认全出站透明代理与 `EgressProxy` 三态闸门 (`PASSTHROUGH \| CAPTURING \| REPLAY_MOCK`, ADR-11)**：`ARMED`/`CAPTURING` 态下对 `target_pids_map` 目标 PID 发起的全部 IPv4 TCP 出站连接默认透明重定向至 `EgressProxy`（无需预配下游拓扑或白名单；内核态豁免 `agent_pid` 自环与本地代理/监听端口自连，统一 `__be32/__be16` 网络字节序契约）；`PASSTHROUGH` 纯字节转发；`CAPTURING` 原样透传全部字节（兼容 DB/Redis/HTTP 零干扰）并旁路挂载 `StreamFramer` 提取 CROS RPC 帧且注入活跃 `SessionOrTaskID`，**停录切回 `PASSTHROUGH` 绝不断开长连接**；`REPLAY_MOCK` 阻断向真实下游写数据并在本地完成二进制 Mock 自愈应答；<br>3. **业务方向 (`Direction`) 与读写半流 (`StreamHalf`) 正交解耦**：入站监听连接收发帧恒定标记 `Direction = INGRESS (1)`，出站代理连接收发帧恒定标记 `Direction = EGRESS (2)`，确保往返 `ullSNO` 精确配对；<br>4. **优雅排水与自洁底线**：需 `CAP_BPF` + `CAP_NET_ADMIN`；手动 `DISARMED` 或主控失联超 5 分钟时立即卸载 `bpf_link` 与监听端口，存量代理连接优雅排水至自然结束；收到 `SIGINT/SIGTERM` 或 `purge` 时立即全量关闭。 | 1. 仅挂载单个进程（零 `Downstreams` 配置）时，开录同时录齐该进程 `INGRESS`（含开录前已建立的上游连接）与 `EGRESS` 完整 REQ/RSP 往返，且 `SessionOrTaskID` 与 `Direction` 100% 准确；非 CROS RPC（如 SQL/HTTP）连接透明转发且 0 伪帧上报；<br>2. 目标进程读写本地磁盘文件或非监听管道时 0 误采；`stat_bypassed_self` 正确累加；BPF 字节序转换单测覆盖 `127.0.0.1` 与任意 IPv4/端口；<br>3. 无特权返回 `CAP_DENIED`；<br>4. 手动 `DISARMED` 或 5m 失联超时后 `bpf_link` 与监听端口已卸载且存量连接不粗暴 `RST`。 |
| **F-10** | **CROS RPC 分帧与头解析库（`pkg/crosrpc`）及主控 Protobuf 解码** | `Server`<br>`Agent` | **P0** | 1. **共享分帧与头解析库**（Agent 与主控共用，零 `.pb` 依赖）：<br>   - 公共头与首字节定戳：识别 8B `unMagicInfo`（7B `"CSE.TCP"` + 1B `ucProtocol`），遇到前序脏字节、非 CROS RPC 透传字节流或中途切入既有长连接时，采用 **`bytes.Index` SIMD 魔术字滑窗扫描 + 尾部残余切片原地紧缩**（保证纯脏字节流下内存恒定 `<= 8KB` 不膨胀）结合 F-10 头部合法性重新同步；**在定位到帧头首字节进入 `ReadCommHead` 时锚定 `frameStartNs` 作为信封 `CapturedAtNs`**（避免多 TCP 分片导致父子请求时序漂移）；大端解析 `ullSNO`、`uiLength`、`usHeadLen`、`ucMsgType`、`isPacked`；校验 `usHeadLen >= 会话头最小长度`、`usHeadLen <= uiLength <= RPC_PAYLOAD_MAX_LEN (32MB - 4KB) + usHeadLen`、`ucMsgType ∈ [2, RPC_MSG_TYPE_MNG_BUTT)`；正确处理粘包与半包；<br>   - 会话头与扩展头：按 `#pragma pack(4)` 与 `usHeadLen` 长度判定版本（前 / 后向兼容），解析 `RPC_REQ_HEAD_S`（V00~V05：`ascServiceName[64]`、`ascOperation[64]`、`ucReqType` 等）、`RPC_RSP_HEAD_S`（V00~V04：`uiStatusCode` / `ullStatusCode`、**V03/V04 偏移 `32..39` `ullClientSendTime`、`40..47` `ullServerRecvTime`、`48..55` `ullServerSendTime`**、`usRspMsgNo`）、`RPC_MNG_HEAD_S`（含 `acProcName[64]` 对端进程名），以及 `0..N` 段扩展头（`TraceID` / `SpanID` / `FatherSpanID`、入队时间戳 `ullServerEnqueueTime`）；<br>2. **主控 Protobuf 解码**（只在主控引入 `dynamicpb`）：依据会话头中的 `ascServiceName`（格式为 `{package}.{serviceName}`）与 `ascOperation` 匹配 F-03 生效映射，对 `24 + usHeadLen` 之后的 Payload 做 JSON 解码与重编码；无映射或不可解码时保留完整头解析并以 Raw Hex 展示，不中断链路。 | 1. 单测覆盖：前序脏字节重新同步、连续数 MB 非 CROS RPC 字节流滑窗紧缩（`cap(buf) <= 8192`）、逐字节碎片流首字节定戳、多包粘连流、V01 与 V05/V04 会话头（含 `ullServerRecvTime` / `ullServerSendTime` 精确偏移提取）、扩展头解析；<br>2. 非法 `ucMsgType`、超限 `uiLength`、`usHeadLen > uiLength`、`usHeadLen` 小于最小长度的报文全部被拒绝，无 panic、无超量分配（fuzz 测试运行 ≥ 60s 无崩溃）；<br>3. `cmd/agent` 的依赖图中不包含 `dynamicpb` / `protoregistry`；<br>4. 无 `.pb` 映射的报文解码结果为 Raw Hex，头字段完整。 |

---

### 3.4 录制、重放与回归断言领域

| 编号 | 特性名称 | 涉及组件 | 优先级 | 功能描述与技术规范 | TDD 验收指标 |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **F-11** | **任务级单/多进程开箱双录（`INGRESS + EGRESS`）、架构快照与双模物理分区归档**（`traefik_recording_sessions` / `traefik_traffic_records`） | `Server`<br>`Agent`<br>`Frontend` | **P0** | 面向「录制单个被测进程出入站闭环，或单次协同录制链路上多个进程」的场景：<br>1. **开箱即用出入站双录、免重连开录与公私素材域**：选择有权限环境下的 1 个或多个已挂载进程原子获取 Agent 排他租约并创建批次（固化 `process_snapshots` 快照），**无需预配下游拓扑契约或 `extra_downstreams`，默认自动同时采集每个目标进程的入口 (`INGRESS`) 与出口 (`EGRESS`) CROS RPC 往返流量**；开录时预热存量入站 FD、复检出站连接并补偿上报存量未声明连接，对 `ARMED` 就绪进程免重连开启采集闸门，对 `DISARMED` 进程兜底临时挂载；停录调用 `POST /agent/v1/monitors/{sessionId}/stop` 回退 `PASSTHROUGH` 持续转发不断连并释放租约；设最长时长与最大帧数上限自动停录；<br>2. **信封回传与双轨分发**：Agent 回传携带准确 `SessionOrTaskID` 与业务 `Direction` 的 56B 二进制信封帧；主控绑定真实采集 `agent_id`，双轨分发至前端 SSE 与异步 Worker 落盘管道；<br>3. **发起序 `SeqNo`、对端身份记忆与双模物理分区落盘**：按 `(session_id, process_id, direction, conn_key, ullSNO)` 精确配对 REQ / RSP，**在 `REQ` 到达瞬间原子分配批次内单调递增 `seq_no`（根治后序完成落盘导致的父子顺序倒挂）**，并缓存同连接 `MNG.acProcName` 注入后续记录 `headers_json.meta`；同行持久化 `raw_req_packet` / `raw_rsp_packet`（`bytea`）与 `payload_json`（`jsonb`）；按日范围分区支持 30 天 TTL 毫秒级 `DROP TABLE`。 | 1. 仅挂载单个进程时开录：同时完整归档 `INGRESS` 与 `EGRESS` 往返记录且绑定正确 `session_id`、真实 `agent_id` 与严格按请求发起先后递增的 `seq_no`（父 `INGRESS` 恒排在子 `EGRESS` 之前）；两进程 `ARMED` 就绪后连续启停两次录制全程无需重启进程，停录后 Agent 租约立即释放；<br>2. 普通用户 B 无法查看普通用户 A 的 `PRIVATE` 录制批次（返 `403`/`404`），`traefik_admin` 可审计查看全部素材；<br>3. 达时长或帧数上限自动停止并释放租约（`COMPLETED_BY_LIMIT`）；<br>4. 超阈值帧或中途孤立应答帧（`RSP_ORPHAN`）标记为不可重放。 |
| **F-12** | **跨环境同构重放编排、因果闭包 `ScopedMock`、历史 `EGRESS` 端点闭环与零外泄门禁**（`traefik_replay_tasks` / `traefik_replay_items`） | `Server`<br>`Agent`<br>`Frontend` | **P0** | 在测试环境 (A-06) 下实现跨环境（如 SIT `SHARED` 素材投递至 UAT `SHARED` 或研发 `PRIVATE` 环境）同构重放：<br>1. **快照级同构进程匹配与存量直连门禁**：基于录制批次 `process_snapshots`（或源 `ArchProcess`）校验目标进程必须承载相同架构模块且 `listen_port > 0`（不匹配返 `400`）；主控自动取 **「目标进程静态推导下游 $\cup$ 源录制批次实测 `EGRESS` 记录 `DstAddr` 集合」** 作为 Mock 端点集，**若目标进程未运行或持有指向 Mock 端点集的未代理存量直连（`NEED_RECONNECT`），强制拒绝启动重放并返回 `409 Conflict`**；<br>2. **按 `RecordID` 因果闭包切分 `ScopedEgressMocks` 与零外泄接管（ADR-11 / ADR-12）**：主控依据 DOTR `TraceID/FatherSpanID` 子树（无 Trace 时回退请求往返时间窗）为每条选定 `INGRESS` 记录精准构建专属 `ScopedEgressMocks[recordID]` 局部池（加未归属 `GlobalFallbackMocks` 兜底）并组装纯二进制 `ExecuteReplayReq`（零 `.pb`）；Agent 将内核 `connect4` 切为 `MODE_REPLAY`，将命中 Mock 端点集的既有长连接切为 `REPLAY_MOCK`、其余代理连接启用写前 `"CSE.TCP"` 魔术字守卫，并在注入每条 `IngressItem` 前激活对应 `recordID` 局部池；<br>3. **回收与无损恢复**：重放结束后关闭被 Mock 接管过的连接（触发业务自动重连回 `ARMED` 态 `EgressProxy`），恢复待命规则并释放 `REPLAY` 租约；明细持久化至 `traefik_replay_items`；<br>4. **结果状态**：按 `ERROR > TIMEOUT > MOCK_MISS > FAILED > PASS` 聚合最高异常级别。 | 1. 单进程或双进程录制后发起单事务或多事务重放：同批次两笔不同入口请求各自调用同一接口且含动态时间戳参数时，按各自 `ScopedEgressMocks[recordID]` 精准消费对应下游应答（`SCOPED_HIT`）零串包；新增未录制下游调用记为 `MOCK_MISS`，**真实下游收到 0 字节**；若目标进程处于 `NEED_RECONNECT` 则返回 `409`；<br>2. 异构进程返 `400`；端口不可达为 `ERROR`，超时为 `TIMEOUT`，结束后租约释放且恢复 `ARMED`；<br>3. 越权使用他人 `PRIVATE` 素材返 `403`；<br>4. 单条结果写入 `traefik_replay_items` 支持服务端分页。 |
| **F-13** | **Agent 零 `.pb` 分层作用域二进制 Mock 匹配与 `ullSNO` 自愈**（`pkg/replay`） | `Agent` | **P0** | 1. **分层匹配模型（`ScopedMocks[recordID]` 优先 $\rightarrow$ `GlobalFallbackMocks` 兜底）**：`ReplayExecutor` 注入每条 `IngressItem` 前调用 `SetActiveRecordID(recordID)`；对下游请求提取 `(ascServiceName, ascOperation)` + `24 + usHeadLen` 之后 Payload 字节哈希，优先在当前 `recordID` 专属局部池执行「精确哈希匹配 $\rightarrow$ 同方法录制顺序兜底（`SCOPED_HIT`）」，未命中时再查询批次全局兜底池（`GLOBAL_FALLBACK_HIT`）；由此天然忽略每次运行变化的 `ullSNO`、会话头时间戳与扩展头 Trace，同时杜绝多请求间顺序兜底张冠李戴；<br>2. **未命中策略**：局部池与全局兜底池均已用完或不存在时，**不转发到真实下游、不返回应答**，记录 `MOCK_MISS` 明细（接口、Payload 摘要、时间），由目标进程自身的超时逻辑处理；<br>3. **自愈改写**：依据《CROS RPC 协议规范》（[`01-cros-rpc-protocol-spec.md`](../07-references/01-cros-rpc-protocol-spec.md)），命中后执行二进制自愈重写：<br>   - **请求序列号**：在应答公共头偏移 `8..15` 覆写当前请求的大端 `ullSNO`；<br>   - **Trace 缓冲回显**：将应答头 `RPC_RSP_HEAD_S` 偏移 `8..23` 的 `aucTraceBuff[16]` 覆写为请求头 `RPC_REQ_HEAD_S` 偏移 `144..159` 的对应值；<br>   - **发送时间戳回显**：对 V3+ 版本的应答头，将偏移 `32..39` 的 `ullClientSendTime` 覆写为请求头偏移 `248..255` 的 `ullSendTimeStamp`；<br>   - **服务与接口回显**：校验并确保应答头偏移 `56..119` 的 `ascServiceName` 与偏移 `120..183` 的 `ascOperation` 与请求一致；<br>   - **多段应答支持**：`RSP_SEG` 分片逐片改写上述字段；<br>4. **Oneway**：`ucReqType` 为 Oneway 的请求只记录、不应答；<br>5. **MNG 帧** (A-08)：按 `ucMsgType` 取录制中的同类应答原样回放并覆写 `ullSNO`，没有则忽略。 | 1. 目标进程重放时生成全新的随机 `ullSNO`（如 `0x000000010009A2B3`）、新 TraceID 与新时间戳，Agent 仍命中 Mock，应答帧中的 `ullSNO`、`aucTraceBuff` 与 `ullClientSendTime` 与当前请求保持一致；<br>2. 多条 `IngressItem` 并发/顺序调用同名下游接口且含动态参数时，严格隔离在各自 `recordID` 的 `ScopedMocks` 内顺序命中；<br>3. 调用次数超过录制次数时产生 `MOCK_MISS` 明细，且没有任何流量流向真实下游；<br>4. 3 段 `RSP_SEG` 分片全部被正确改写。 |
| **F-14** | **协议头级 + Protobuf 字段级双层回归 Diff** | `Server`<br>`Frontend` | **P0** | 把 Agent 回传的实际应答（`Actual`）与录制基线（`Expected`）比对：<br>1. **第一层（协议头）**：比对 `ucMsgType`、`uiStatusCode` / `ullStatusCode`、`usRspMsgNo`、`isPacked` 与 Payload 原始字节；<br>2. **第二层（Protobuf 字段）**：Payload 字节不同且有 `.pb` 映射时，解码为 JSON 树逐字段计算 `[]FieldDiff{FieldPath, Expected, Actual}`；没有 `.pb` 映射时只给出字节级 Diff；<br>3. **固定豁免**：会话头 / 扩展头中的服务端时间戳（`ullServerRecvTime`、`ullServerSendTime`、`ullServerEnqueueTime`）不参与比对；<br>4. **忽略规则**：可按接口 `(service, operation)` 配置忽略的字段路径，重放任务可临时追加；<br>5. **顺序语义**：`repeated` 字段按顺序比对，`map` 字段不考虑顺序。 | 1. 应答 `uiStatusCode` 变为非零，结果为 `FAILED` 并指出状态码字段；<br>2. `discount_cents` 由 `6500` 变为 `6600`，结果为 `FAILED` 并给出 `FieldPath` 与期望 / 实际值；<br>3. 只有服务端时间戳不同，结果为 `PASS`；<br>4. 被配置为忽略的字段不同，结果为 `PASS`；`map` 只是顺序不同，结果为 `PASS`。 |

---

### 3.5 微前端控制台与平台治理领域

| 编号 | 特性名称 | 涉及组件 | 优先级 | 功能描述与技术规范 | TDD 验收指标 |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **F-15** | **一体化微前端控制台与契约治理四层信息架构** | `Frontend`<br>`Server` | **P0** | 在 `code-bench` 微前端体系内（暴露 `./App` 与 `./menu`，兼容嵌入与独立两种运行方式），按宏观态势与契约治理闭环提供 **4 组 10 项**导航（M14 先行交付 4 组 9 项，M15 挂载第 10 项 `/golden-suites`，严禁暴露空壳页）：<br>1. **全局概览组（置顶默认入口，默认落地 `/dashboard`）**：`治理大盘` (`/dashboard`)；<br>2. **契约核验与回归组（高频活体核验闭环）**：`契约采集` (`/record`)、`事务分析` (`/debug`)、`防劣化回归` (`/replay`)、`黄金用例库` (`/golden-suites`, M15)；<br>3. **架构与契约组（静态基线 SSOT，按宏观➔中观➔微观排列）**：`架构拓扑` (`/topology`)、`架构元素` (`/architecture`)、`Proto 契约` (`/schemas`)；<br>4. **环境与治理组（基础设施沉底）**：`Agent 与进程` (`/agents`)、`模块参数配置` (`/admin/settings`, `adminOnly`)；<br>5. **旧路由平滑重定向**：`/traffic` 自动带参重定向至 `/debug`，`/architecture?tab=topology\|physical\|verify` 自动带参重定向至 `/topology`。 | 1. `tsc --noEmit` 与 `eslint` 零警告，`menu.ts` 与 `StandaloneShell` 严格一致且无死链；<br>2. 旧路径访问 100% 无损保留 Query 参数重定向至新路由；<br>3. 所有页面只使用 `--color-*` 语义 Token，列表使用标准 5 页滑动窗口 URL 分页（`15/25/50/100`，默认 `25`）。 |
| **F-15.1** | **录制素材资产台账与跨工作台流转**（`流量录制` `/record`） | `Frontend`<br>`Server` | **P0** | 1. **活跃录制状态卡**：醒目呈现正在进行的协同录制批次、实时捕获帧数、A-01 存量长连接重连告警、一键停录及【实时跟流调试】入口；<br>2. **录制素材分页台账**：将 `RecordingSession` 升级为一等资产表，支持按 `全部 / 我的私有 (PRIVATE) / 团队共享 (SHARED)` 及状态筛选、URL 分页；<br>3. **一键跨页流转**：每条批次行内置 `[去调试]`（跳 `/debug?session_id=X`）、`[发起重放]`（跳 `/replay?session_id=X&auto_create=1`）、`[架构验证]`（跳 `/topology?session_id=X`）、`[发布共享]` 与 `[删除]` 动作。 | 1. 活跃录制状态下正确展示 A-01 重连提示与【实时跟流调试】跳转；<br>2. 批次台账分页、作用域过滤及 `[去调试]` / `[发起重放]` / `[架构验证]` URL 带参跳转组件单测全部通过。 |
| **F-15.2** | **独立流量调试台、四段深度解剖与事务闭包快速重放**（`流量调试` `/debug`） | `Frontend`<br>`Server` | **P0** | 1. **确定性发起序检索、架构水合与异常过滤**：支持切换历史录制批次或跟随活跃 SSE 实时流；`GET /api/traffic-records` 严格按请求发起时刻 `req_captured_at_ns ASC, seq_no ASC, id ASC` 排序并完整水合 `src_module_name / dst_module_name / process_name / peer_proc_name` 与连接短编号 `Conn#k`；支持 `has_error`（协议非零状态码、`REQ_ONLY` 或业务软错误）快速过滤；<br>2. **四段解剖与三段阶段耗时**：右侧 `PacketAnatomyDrawer` 分段展示 24B 公共头位图、会话头与 V03/V04 三段耗时（网络传输 / 入队排队等待 `QueueWaitUs` / 业务执行 `ServerExecUs`）、DOTR 扩展头 Trace（一键按 `TraceID` 联动）、Protobuf JSON 与 16B Raw Hex 对照及 `.pb` 重解码；<br>3. **事务闭包重放预览与前置状态链回放（ADR-12）**：在可重放 `INGRESS` 事务行及抽屉内点击【事务重放】，通过 `GET /api/traffic-records/:id/replay-closure` 透明展示本事务绑定的专属下游 `EGRESS` Mock 闭包清单，检出跨事务实体前置依赖时支持勾选一键按序连带回放前置事务链，并在原地抽屉及泳道图中展示双层 Diff 与 Mock 命中明细。 | 1. `GET /api/traffic-records` 返回顺序严格符合请求发起时序且水合完整模块/进程名，`has_error=true` 精确过滤异常报文；<br>2. 解剖抽屉正确展示四段结构与 V03/V04 排队/执行阶段耗时拆解；<br>3. 打开重放抽屉可见专属下游 Mock 闭包列表及跨事务前置实体依赖提示，支持一键连带前置链回放。 |
| **F-15.3** | **架构双视图基线（`[逻辑架构 \| 物理进程]`）与全屏拓扑解耦**（`/topology` + `/architecture`） | `Frontend`<br>`Server` | **P0** | 1. **`架构拓扑` 独立全屏化 (`/topology`)**：剥离外层胶囊 Tab，释放全视口高度承载逻辑/物理复合拓扑画布与录制批次一致性比对图层；<br>2. **`架构元素` 左树顶部 `[逻辑架构 \| 物理进程]` 切换 (`/architecture`)**：`?tree=logical` 维护 `子系统 ➔ 8级功能组 ➔ 功能模块 ➔ PROVIDE/DEPEND 契约` 台账；`?tree=physical` 维护全局标准 `进程组 (ArchProcessGroup) ➔ 标准业务进程 (ArchProcess)` 树，右侧 `ArchProcessDetailPane` 集中编辑默认运行参数、绑定承载 `FM` 模块、透视汇总的 `PROVIDE/DEPEND` 服务契约及各 Agent 挂载血缘，并与 `/schemas`、`/agents` 建立双向深链。 | 1. `ArchitectureWorkbenchPage` 左树顶部分段切换 `[逻辑架构 \| 物理进程]` 正常联动 URL `?tree=logical\|physical`，切换至物理进程树可创建/编辑 `ArchProcessGroup`、`ArchProcess` 并组装 `FM` 模块；<br>2. 点击服务契约或宿主机挂载血缘正确跳转 `/schemas` 或 `/agents`。 |
| **F-15.4** | **协同录制抽屉、智能默认预勾选与跨台引导**（`流量录制` `/record`） | `Frontend` | **P0** | 1. **680px 沉浸式右侧抽屉 (`RecordingStartDrawer`) 与文案消歧**：并排两列作用域卡片（`PRIVATE` / `SHARED`）、Sticky 吸底汇总操作栏；清晰标注「本次采集目标进程（从已挂载进程中圈定范围 · 默认同时录制入口 INGRESS 与出口 EGRESS）」，彻底消除与 `/agents` 常驻待命挂载的心智混淆；<br>2. **宿主环境 Agent 选择与双向深链**：自动默认选中在线空闲环境 Agent；`RecordingWorkbenchPage` 解析 `/record?auto_start=1&agent_id=X` 自动滑出抽屉预选对应 Agent（关闭抽屉时自动清理一次性 `auto_start` 参数）；当所选 Agent 尚未挂载任何进程时提供 **`[前往挂载架构进程 →]`** 按钮一键跳至 `/agents?scope=<scope>&agent_id=X&open_mount=1`；<br>3. **智能默认预勾选与关联下游级联勾选（ADR-11）**：按 `ArchProcessGroup` 分层渲染已挂载进程并展示统一 **7 态预挂就绪徽章**；打开抽屉或切换 Agent 时对可用进程（`probe_desired_state !== 'DISARMED'`）实施智能默认勾选——**可用数 `1..5` 时默认自动全选（单进程/小环境 0 次额外点击直接开录），`> 5` 时默认勾选 `READY` 进程**；`DISARMED` 进程置灰禁选；工具栏提供 `[全选可用]`、`[仅选就绪]`、`[🔗 勾选关联下游]`（按模块 `DEPEND` 自动补选同机下游进程）与 `[清空]`。 | 1. 单进程与 `<= 5` 可用进程环境下打开抽屉默认已自动勾选，点击启动无需多余勾选；`> 5` 进程大环境下默认仅勾选 `READY` 进程；`DISARMED` 进程不可勾选；<br>2. `[🔗 勾选关联下游]`、空挂载态跳 `/agents?...&open_mount=1` 及 URL `auto_start=1&agent_id=X` 自动唤起与关闭清理单测全绿。 |
| **F-15.5** | **Agent 架构进程 `1:1` 挂载抽屉、宿主机自动扫描匹配与就绪看板**（`Agent 与进程` `/agents`） | `Frontend`<br>`Server` | **P0** | 1. **分组挂载与探针就绪树台账（常驻待命层）**：`GET /api/agents/:id` 与批量同步接口完整预加载 `arch_process.group` 及 `arch_process.modules` 并填充有效继承属性；右栏清晰定位为「环境进程部署映射与常驻待命探针」，展示有效端口、`[本地已覆盖]` 标识、承载 `FM` 模块与统一 **7 态探针就绪徽章**，支持配置 `fallback_agent_id`，提供按进程/组/整机 `暂停探针 / 恢复预挂`（含 `force` 紧急卸挂）、`重新扫描诊断` 及 **`[🎬 发起录制]`**（跳 `/record?auto_start=1&agent_id=X`）一键流转；<br>2. **`AgentProcessMountDrawer`（680px 抽屉）与深链唤起**：支持响应 `/agents?agent_id=X&open_mount=1`（具备写权限时自动滑出挂载抽屉）；渲染全局架构进程勾选树（单环境 `1:1`），携带 `expected_revision` 乐观并发保护，顶部提供 **`[⚡ 自动扫描宿主机进程并匹配架构树]`**，底部提供跳回 `/architecture?tree=physical` 深链引导；保存时在线空闲 Agent 自动触发预挂。 | 1. `GET /api/agents/:id` 正确返回完整挂载进程列表（修复左卡计数与右表不一致缺陷），右栏顶栏 `[🎬 发起录制]` 正确跳 `/record?auto_start=1&agent_id=X`，访问 `open_mount=1` 自动唤起挂载抽屉；<br>2. `AgentProcessMountDrawer` 支持 `/proc` 自动匹配高亮、内联本地覆盖、`expected_revision` 并发冲突提示及保存自动预挂；<br>3. RBAC 校验：普通用户可操作本人 `PRIVATE` Agent 挂载与探针开关，对 `SHARED` Agent 写入口按权限隐藏且后端返 `403`。 |
| **F-15.6** | **可折叠事务调用树内联甘特瀑布、全链路时序泳道与参数/实体血缘透视**（`流量调试` `/debug`, `ADR-12`） | `Server`<br>`Frontend` | **P0** | 1. **默认事务调用树 + 内联迷你甘特瀑布主表 (`?view=tree`)**：依托生产默认开启的 DOTR `RPC_EXTEND_HEAD_TYPE_TRACE_KEY`（`TraceID / SpanID / FatherSpanID`）为主真实源（辅以单进程 `[ReqNs-500μs, RspNs+500μs]` 降级兜底）组装任意深度调用树（`max_depth <= 16`），支持多进程 `EgressProxy` 场景下 `P1.EGRESS ⇔ P2.INGRESS` 跨跳合并缝合、同层兄弟 `SEQUENTIAL \| PARALLEL \| COMPENSATION` 自动识别与业务软错误徽章；默认按**根业务事务**执行 URL 分页（`15/25/50/100`，默认 `25`，同一事务父子绝不跨页割裂），辅以 `[🌳 事务调用树 \| 📋 原始时序平铺]` 切换与业务实体生命线快捷筛选条；<br>2. **交互式 UML 全链路时序泳道与四段解剖联动抽屉 (`TransactionSequenceDrawer`)**：采用纯原生 React SVG 渲染调用参与者泳道、同步/异步往返箭头、阶段耗时色柱及可开关的**参数传递血缘弧线**（如 `LockStock.rsp.lock_id ➔ ConfirmStock.req.lock_id`）；点击任意步骤零延迟联动右栏四段解剖并支持叠加渲染重放 `SCOPED_HIT / MOCK_MISS / RSP_DIFF` 图层。 | 1. `GET /api/recording-sessions/:id/transactions` 按根事务分页正确组装 DOTR 父子树、跨进程跳点缝合、兄弟并发/补偿标签与跨事务实体链（如 `order_id` 创建 $\rightarrow$ 查询/取消），搜索子方法时完整保留祖先路径；<br>2. `/debug` 默认渲染调用树与内联瀑布条（仅使用 `--color-*` 语义 Token），切换 `?view=flat` 平滑回退发起序平铺表；<br>3. 点击 `[📊 时序泳道]` 正确渲染 SVG 泳道与参数血缘边，点击边联动刷新四段解剖且零 XSS 注入风险。 |
| **F-16** | **RBAC 权限、公私域隔离与操作审计** | `Server`<br>`Frontend` | **P0** | 1. **权限与资源隔离矩阵**（基于 `commonAuth.AuthMiddleware` 与 `RequireAdmin("traefik_admin")`）：<br>   - **普通认证用户**：只读浏览公共架构元素、拓扑与 `.pb` 映射；自行创建、编辑、删除本人的个人 `PRIVATE` Agent 及其挂载公共架构模块的进程；在本人 `PRIVATE` Agent 或当前空闲的全局 `SHARED` Agent 上动态申请排他租约**发起/停止录制**与**发起重放**；管理本人的 `PRIVATE` 录制素材（支持共享为 `SHARED` 素材）并复用全量 `SHARED` 录制素材；严禁修改或租用他人 `PRIVATE` Agent 与他人 `PRIVATE` 录制素材；<br>   - **`traefik_admin` / `super_admin`**：维护全局公共三级架构元素、接口契约、`SHARED` Agent 及其基准进程、`.pb` 上传与版本切换、公共 `SHARED` 录制素材治理、**穿透查看与审计全部用户的 `PRIVATE` Agent 与素材**、**强制释放异常占用的 Agent 租约**及模块全局设置；<br>2. **审计**：所有写操作记录字段级 Diff；录制启停、重放发起、Agent 租约获取与强制解锁均写入操作审计。 | 1. 未认证返回 `401`；普通用户修改公共架构元素、管理 `SHARED` Agent 或上传 `.pb` 返回 `403`；普通用户创建并使用本人 `PRIVATE` Agent 录制/重放成功，访问他人 `PRIVATE` 资源返回 `403`；`traefik_admin` 可审计查看他人 `PRIVATE` 详情；<br>2. 修改架构元素与 Agent 后 `audit_logs` 含完整字段级 Old/New Diff；<br>3. 录制启停、发起重放及管理员强制释放租约均产生对应审计记录。 |

---

### 3.6 契约治理升维、黄金资产与自动化门禁领域

| 编号 | 特性名称 | 涉及组件 | 优先级 | 功能描述与技术规范 | TDD 验收指标 |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **F-24** | **平台级契约治理全景大盘与三维封顶健康指数**（`/dashboard` 默认首页，聚合健康评分、漂移雷达、孤儿清零与防劣化防线） | `Server`<br>`Frontend` | **P0** | 1. **默认置顶落地大盘 (`/dashboard`) 与分步菜单升维**：在 M14 将导航重构为 4 组 9 项并将 `/dashboard` 设为全局默认入口（M15 扩展为 4 组 10 项），面向架构师与研发负责人呈现活体契约宏观态势；<br>2. **公私域严格隔离与三维封顶健康指数 (`Health Score`, 0~100 分, ADR-14)**：**仅统计 `SHARED` 公共录制核验与黄金套件/CI 门禁回归任务（严禁混入个人 `PRIVATE` 素材与临时单包调试重放）**；总分拆解为三个独立封顶子维度：$S_{\text{arch}} = \max(0, 40 - \text{ActiveViolationEdges} \times 10)$（按标准进程最新有效核验去重边计）+ $S_{\text{asset}} = \text{round}(30 \times \text{ClaimedRatio})$（排除 `0` Service `BASE_LIB`）+ $S_{\text{reg}} = \max(0, 30 - \text{UnhealthySuiteProcesses}_{7\text{d}} \times 10)$，映射 `🟢优秀(90~100) / 🟡良好(75~89) / 🟠亚健康(60~74) / 🔴高危(<60)` 并生成整改指南；<br>3. **四大核心视窗与聚合 OpenAPI (`GET /api/dashboard/overview`)**：单次高效输出健康卡片、架构漂移雷达（违规进程 TOP 5，一键穿透 `/topology`）、契约资产清零看板（孤儿契约一键直达 `/schemas?tab=catalog&claimed_status=UNCLAIMED`）与近 30 天防劣化回归防线，支持按 `subsystem_id` 筛选。 | 1. 访问根路由或 `/dashboard` 正常加载四大看板卡片，零 N+1 慢查询；<br>2. 健康分算法单测：验证个人 `PRIVATE` 失败或单一维度超量违规（如 30 个孤儿服务）时，仅扣满对应子维度（30 分）而不会将总分清零；<br>3. 点击孤儿契约或漂移告警卡片，带参无缝跳转对应工作台下钻治理。 |
| **F-25** | **黄金契约套件自包含快照库与事务固化治理**（`traefik_golden_suites` / `traefik_golden_suite_items`，免疫分区清理） | `Server`<br>`Frontend` | **P0** | 1. **事务一键沉淀与完全自包含深拷贝快照 (ADR-14)**：在 `/debug` 事务树中勾选单笔或含前置依赖链的多笔根事务点击【🌟 沉淀为黄金契约】；明细表 `traefik_golden_suite_items` **完整深拷贝固化入站 `raw_req_packet`、基线 `raw_rsp_packet`、成对 `scoped_mocks` 闭包与 `egress_endpoints`（`source_record_id` 仅作可空弱引用）**，确保底层 `traefik_traffic_records` 30 天分区 `DROP TABLE` 或源批次删除后套件 **100% 永久独立可回放**；<br>2. **套件台账与双源重放解耦**：`traefik_golden_suites` 绑定 `arch_process_id`、版本号与套件级忽略规则，明细支持 `mask_rules_json` 动态字段清洗；`ReplayTask` 解耦 `session_id` 为可空并新增 `golden_suite_id` 与 `trigger_source`；<br>3. **套件级一键脱机回归 (`/golden-suites`)**：支持向指定目标环境 Agent 一键发起全套用例按 `order_no` 回归验证，产出套件通过率断言报告并作为 `F-27` 权威输入。 | 1. 事务树沉淀创建黄金套件后，物理删除源 `RecordingSession` 及对应 `traefik_traffic_records` 分区记录，再次触发该黄金套件回归依然 **100% 正常执行并产出双层 Diff**；<br>2. 黄金套件列表 CRUD、版本归档、用例排序与 `mask_rules_json` 动态掩码清洗单测全绿；<br>3. M15 同步挂载 `/golden-suites` 菜单项且前端组件单测通过。 |
| **F-26** | **设计态契约破坏性变更阻断雷达与回滚逃生舱**（`FileDescriptorSet` 全维度 AST 比对与下游消费者推导） | `Server`<br>`Frontend` | **P0** | 1. **全维度 AST 语义比对引擎 (ADR-14)**：在激活候选 `.pb`（`activate`、`batch-activate` 或 `auto_activate=true` 上传）时，与同名当前生效版本比对 `FileDescriptorSet`：<br>   - **RPC 契约层**：删除已生效 Service 或 Method（`SERVICE_OR_METHOD_REMOVED`）、篡改 Method 入参/出参类型或流式模式（`METHOD_SIGNATURE_CHANGED`）；<br>   - **Message 与 Enum 层**：字段数字 Tag 篡改/复用（`TAG_MUTATION`）、类型/基数/`oneof`/`map` 不兼容变异（`TYPE_INCOMPATIBLE`）、删除字段但原数字 Tag 未落入 `DescriptorProto.GetReservedRange()`（`REMOVED_WITHOUT_RESERVE`）、枚举数值索引篡改或删除未保留数值区间（`ENUM_BREAKING_CHANGE`）；<br>2. **强阻断与管理员审计逃生舱 (`allow_breaking`)**：默认检出任意 `FATAL` 规则返回 `409 BREAKING_CHANGE_DETECTED` 回滚激活；支持 `traefik_admin` 显式传入 `allow_breaking=true` + 必填 `breaking_reason`（`>=8` 字符）用于紧急版本回滚或受控重构强推，强制记录 `P1` 级审计日志；<br>3. **下游消费者血缘推导**：基于 `traefik_module_dependencies` 自动推导依赖受损 Service 的下游模块、标准进程与负责人，在前端拦截抽屉可视化告警。 | 1. 激活删减 Method、修改字段 Tag/类型、或删除字段仅写 `reserved "name"` 未保留数字 Tag 的新版 `.pb`，均精确返回 `409 BREAKING_CHANGE_DETECTED`；<br>2. 新增 `optional` 字段或正确声明 `reserved <tag>` 删除字段正常通过；管理员携带 `allow_breaking=true` 与原因可成功回滚至旧版并产生 `P1` 审计记录；<br>3. 拦截响应准确包含受影响的下游模块及负责人清单。 |
| **F-27** | **CI/CD 自动化强阻断质量门禁、实测边回传与排他锁排队**（`POST /api/gates/evaluate`） | `Server`<br>`Agent` | **P0** | 1. **门禁 OpenAPI、安全机器 Token 与有界排队 (ADR-14)**：提供 `POST /api/gates/evaluate`，支持管理员 JWT 或请求头 `X-Traefik-CI-Token`（服务端仅存 SHA-256 摘要并执行 `subtle.ConstantTimeCompare` 常量时间比对，严禁 Query 传参与明文日志）；入参支持 `target_arch_process_id`、`env_agent_id`、可选 `golden_suite_id`、`timeout_seconds` 与 **`wait_lease_seconds`（默认 60s，遇到短期 `REPLAY` 锁争用时服务端退避排队等待，防并发 CI 误挂）**；<br>2. **重放实测出站边回传与正交状态码判定**：Agent 在 `ReplayTaskResultResp` 中回传重放期实测 `ObservedEgressCalls`；<br>   - **环境异常 (`503`)**：Agent 离线、等锁超时（`GATED_ENV_BUSY`）或进程未就绪（`GATED_ENV_NOT_READY`）返回 `503` 供流水线重试；<br>   - **判定 1 架构漂移 (`422 GATED_VIOLATION`)**：`ObservedEgressCalls` 存在未在目标 `ArchProcess` 模块静态 `DEPEND` 中报备的下游 `ServiceName`，返回 `422` 强阻断；<br>   - **判定 2 契约劣化 (`422 GATED_BREAKING_DIFF`)**：黄金套件重放存在协议状态码异常、`MOCK_MISS` 或字段级 `FAILED Diff`，返回 `422` 强阻断；<br>   - **准出 (`200 PASSED`)**：`Violation == 0` 且套件全绿；<br>3. **结构化 Markdown 门禁凭据**：响应附带原生 Markdown 报告与 `/traefik/debug?replay_task_id=X` 直达排障链接。 | 1. 重放期间被测进程发起未声明 `DEPEND` 调用时，无需额外录制即由 `ObservedEgressCalls` 检出并返回 `422 GATED_VIOLATION`；字段返回值破坏返回 `422 GATED_BREAKING_DIFF`；<br>2. 两路并发门禁请求同一 `env_agent_id` 时，第二路经 `wait_lease_seconds` 排队后顺利执行返回 `200 PASSED`（零 `409` 误杀）；长时锁超时返回 `503 GATED_ENV_BUSY`；<br>3. 错误或缺失 `X-Traefik-CI-Token` 返回 `401`。 |

---

### 3.7 演进规划 (P1 / P2 Backlog)

| 编号 | 特性名称 | 优先级 | 说明 |
| :--- | :--- | :---: | :--- |
| **F-17** | **Agent Token 鉴权加固** | **P1** | Agent 从环境变量或安全配置加载高强度 Token（禁止硬编码）；HTTP 指令与 WebSocket 握手校验 `X-Traefik-Agent-Token`（`subtle.ConstantTimeCompare`）。验收：Token 缺失或错误时返回 `401`。 |
| **F-18** | **结构化接口文档浏览** | **P1** | 基于 `.pb` 反射展示接口清单、入参 / 出参 Message 嵌套树、字段 Tag、类型（含 `repeated`、`map`、`enum`）及注释，可从模块详情跳转。与 `code-proto` 的区别：以运行时 `(ascServiceName, ascOperation)` 为视角，并与功能模块、流量记录关联，不管理 `.proto` 源码与 MR。 |
| **F-19** | **报文敏感字段脱敏与查看权限细分** | **P1** | 按字段路径配置脱敏规则；Payload 明文查看可限定角色。 |
| **F-20** | **录制批次一键回归套件** | - | **已收敛 (SUPERSEDED)**：已升维融入 `F-25` 黄金契约套件资产库。 |
| **F-21** | **从 `code-proto` 导入 `.pb`** | - | **已取消 (CANCELLED)** | 因兄弟模块 `code-proto` 排期推迟，取消联动强依赖。`code-traefik` 转为由自身闭环提供多 `.pb` 描述集资产台账管理与 OpenAPI CI/CD 直推能力（详见 ADR-10）。 | - |
| **F-22** | **无特权用户态代理降级模式** | **P2** | 不具备 eBPF 特权时，由业务方把下游地址配置为 Agent 代理端口完成采集与重放。 |
| **F-23** | **基于 `.pb` 契约的在线构造参数主动发包调试** | **P2** | 在 `流量调试` 台不依赖既有录制包，选定已激活 `.pb` 的 `(ascServiceName, ascOperation)` 在线填写/编辑 JSON 请求体，由主控 `dynamicpb` 编码并封装 CROS RPC 帧下发目标 Agent 注入业务进程，类似 CROS RPC 版 `grpcurl` / Postman。 |

---

## 4. 核心业务验收场景矩阵 (Verification Scenarios)

为阶段四 TDD 提供端到端验收基线，定义以下 **10 个核心场景（`SC-01` ~ `SC-10`）**：

| 场景编号 | 场景名称 | 触发条件与输入 | 预期行为与断言 |
| :--- | :--- | :--- | :--- |
| **SC-01** | **免密探针、未认证拦截与公私域 RBAC 越权** | 1. `GET /api/health`<br>2. 无 JWT 访问受保护接口<br>3. 普通用户新增公共架构元素、新增 `SHARED` Agent、上传 `.pb`、操作他人 `PRIVATE` Agent；`traefik_admin` 审计查看他人 `PRIVATE` 素材 | 1. `200 OK` 且 `service: "Code-Traefik"`<br>2. `401`<br>3. 普通用户越权操作返回 `403`；普通用户自建本人 `PRIVATE` Agent 成功 (`201`)；`traefik_admin` 成功查看他人 `PRIVATE` 详情 (`200`) |
| **SC-02** | **架构建模约束与静态拓扑推导** | 管理员创建子系统、三级嵌套功能组、模块，创建物理进程组与标准架构进程并组装模块，声明 `PROVIDE` / `DEPEND`；尝试把功能组移到其子孙下；尝试让第二个模块供给已被供给的接口或把同一模块分配给两个架构进程；上传 `.pb` | 成环移动返回 `400`；重复供给或模块重复分配进程返回 `409`；`.pb` 映射正确（自动按 `{package}.{serviceName}` 生成 `ascServiceName` 并绑定模块接口）；无需注册任何 Agent，逻辑与物理静态拓扑均完整包含正常边、外部占位边与「进程内」边；审计记录完整 |
| **SC-03** | **Agent 纳管、排他租约、信道与白名单** | 启动 `code-traefik-agent`（监听 `127.0.0.1`），登记并建立排他租约下发监控；另一用户并发请求租用同一 Agent；模拟非白名单 IP 访问；停止 Agent | 握手成功并定位目标 PID；并发争用同一 Agent 返回 `409`；非白名单返回 `403`；Agent 停止后 3 个心跳周期内变为 `OFFLINE` 并自动释放租约 |
| **SC-04** | **CROS RPC 分帧、阶段耗时提取与解码** | 向 `pkg/crosrpc` 输入：带前序脏字节的流、1 字节碎片流、多包粘连流、V01 / V05 请求头、V03 / V04 应答头（含 `ullServerRecvTime` / `ullServerSendTime`）、含 DOTR `TraceKey` 与 `TimeStamp` 扩展头的报文、`usHeadLen > uiLength` 的畸形包 | 正确切出完整帧（时间戳锚定首字节到达时刻）并提取 `ullSNO`、`ascServiceName`、`ascOperation`、`TraceID/SpanID/FatherSpanID` 及服务端收发/入队三段耗时；主控正确解码 Payload；畸形帧被拒绝且无 panic |
| **SC-05** | **单/多进程自动预挂就绪、开箱出入站双录 (`INGRESS + EGRESS`)、因果建树与双模分区落盘** | ① 单进程场景：环境 Agent 仅挂载 `Process_A`（零静态 `DEPEND` 契约、零 `extra_downstreams`）发起录制并触发 `外部 → Process_A → 未纳管外部下游` 多笔带 DOTR Trace 请求；② 多进程协同场景：同一环境 Agent 下 `1:1` 挂载 `Process_A` 与 `Process_B`，`READY` 就绪后连续启停两次录制 | ① 单进程录制无需预配下游地址即同时归档 `Process_A` 的 `INGRESS` 与 `EGRESS` 往返（`seq_no` 严格按请求发起序递增，默认调用树视图准确将子 `EGRESS` 收拢于父 `INGRESS` 之下并呈现内联瀑布条与参数血缘）；② 多进程免重连采集并自动缝合 `A.EGRESS ⇔ B.INGRESS` 同跳节点，**停录调用 `/monitors/:sessionId/stop` 回退 `PASSTHROUGH` 持续转发不断连并释放租约**，第二次开启录制无需重启进程依然完整采集；`traefik_traffic_records` 同行归档 `raw_req_packet`、`raw_rsp_packet` 与 `payload_json` |
| **SC-06** | **架构一致性验证** | 使用 SC-05 的录制结果，静态模型中 `Module_A` 未声明对 `Module_B` 接口的依赖，另有一个已声明但未触发的依赖、一条同进程内依赖与一条指向未监控目标进程的依赖 | `A → B` 标为 `Violation`；未触发依赖标为 `Uncovered`；同进程内依赖标为 `Not Observable`；指向未监控进程依赖标为 `Not Monitored` |
| **SC-07** | **跨环境 Agent 同构进程零 `.pb` 重放、事务级 `ScopedMock` 隔离、既有长连接接管与头部自愈** | 将 SC-05 公共环境 A 录制批次中 `Process_A` 的多笔入口事务投递到公共环境 B（另一 `SHARED` Agent）或个人 `PRIVATE` Agent 上同样挂载该 `ArchProcess`（且处于 `READY` 态、持有既有代理长连接）的目标进程重放 | 同构校验与零直连校验通过并锁定目标 Agent 租约；目标进程复用既有 `EgressProxy` 长连接发出的下游请求被 `REPLAY_MOCK` 帧级接管（0 字节外泄至真实下游），每笔入口事务按各自 `ScopedEgressMocks[recordID]` 局部因果池精准命中对应下游应答（零跨事务串包），应答 `ullSNO`、`aucTraceBuff` 与 `ullClientSendTime` 正确自愈，结果为 `PASS` |
| **SC-08** | **业务突变与 Mock 未命中检出** | ① 修改目标进程使 `uiStatusCode` 非零或 `discount_cents` 变化；② 修改目标进程使其多调用一次下游接口 | ① 结果为 `FAILED`，定位到状态码或字段路径，服务端时间戳差异被忽略；② 结果为 `MOCK_MISS`，明细中包含多出的那次调用，且没有流量流向真实下游 |
| **SC-09** | **设计态破坏性变更阻断、下游影响面透视与紧急回滚逃生** | ① 尝试激活删减 RPC 方法、篡改既有字段 Tag 编号、将 `string` 改为 `int64` 或未声明数字 `ReservedRange` 即删除字段的同名新版 `.pb`；② 管理员携带 `allow_breaking=true` 与 `breaking_reason` 执行紧急旧版回滚 | ① 服务端 AST 比对精准检出 `BREAKING_CHANGE_DETECTED`，返回结构化 `409 Conflict` 阻断激活，包含违规规则、符号路径及所有在 `traefik_module_dependencies` 中依赖该接口的下游模块与负责人；② 管理员显式授权回滚成功 (`200`) 并产生 `P1` 级高危审计记录 |
| **SC-10** | **CI/CD 质量门禁端到端评估、并发锁排队与自动化红阻** | 流水线携带 `X-Traefik-CI-Token` 调用 `POST /api/gates/evaluate`：① 重放期被测进程发出未声明 `DEPEND` 调用（`ObservedEgressCalls` 含 `Violation`）；② 回包破坏黄金套件预期（`FAILED Diff`）；③ 两路流水线并发请求同一共享 Agent 合规进程 | ① 单次重放即检出未报备出站调用，返回 `422 GATED_VIOLATION`；② 返回 `422 GATED_BREAKING_DIFF` 并附带破坏性字段 Diff；③ 两路并发请求经 `wait_lease_seconds` 有序排队均返回 `200 PASSED`（零 `409` 误杀）并输出格式化 Markdown 门禁报告 |

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
   - `.pb` 上传限制 `<= 16MB` 并做 `protodesc` 校验；所有 API 入参白名单校验，只用 GORM 参数化查询，禁止 SQL 拼接与未校验的系统命令调用；CI 机器 Token 仅以 SHA-256 哈希持久化并通过 `subtle.ConstantTimeCompare` 常量时间校验，严禁源码硬编码、URL 传参或明文打印日志。
2. **性能基线（首期目标值，阶段二压测校准）**：

   | 指标 | 目标 |
   | :--- | :--- |
   | 出站透明代理附加延迟（同机，P99） | ≤ 1 ms |
   | Agent 常驻内存（不含会话缓冲，监控 ≤ 10 个进程） | ≤ 64 MB |
   | Agent CPU（1000 帧/s 采集） | ≤ 单核 10% |
   | 主控解析解码吞吐 | ≥ 2000 帧/s |
   | 采集到前端 SSE 展示延迟 | ≤ 1 s |

3. **数据存储与保留**：原始帧与解析结果存入主控 PostgreSQL 分区表；录制批次受时长 / 帧数上限约束（F-11）；流量数据默认保留 30 天（可配置），由主控定时任务执行子分区 `DROP TABLE` 物理释放（毫秒级、零锁表、零 WAL 膨胀），清理动作写审计。
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
