# ADR-12：报文时序因果树、内联甘特瀑布、全链路时序泳道与事务闭包重放架构决策

- **状态**：已批准 (Accepted)
- **日期**：2026-11-04
- **决策者**：CodeBench 架构委员会 / `code-traefik` 核心研发组
- **文档版本**：v1.0.0
- **关联基线**：[`01-feature-matrix.md`](../01-features/01-feature-matrix.md) (v2.10.0)、[`01-cros-rpc-protocol-spec.md`](../07-references/01-cros-rpc-protocol-spec.md) (v1.0.0)、[`02-data-plane-and-cros-rpc-engine.md`](../02-design/02-data-plane-and-cros-rpc-engine.md) (v1.5.0)、[`04-control-plane-and-web-console.md`](../02-design/04-control-plane-and-web-console.md) (v1.7.0)、[`03-api/01-http-api-contract.md`](../03-api/01-http-api-contract.md) (v1.3.0)、[`11-single-process-bidirectional-capture-and-recording-ux-adr.md`](./11-single-process-bidirectional-capture-and-recording-ux-adr.md) (ADR-11 v1.0.0)

---

## 1. 背景与问题陈述 (Context & Problem Statement)

在 `M12` 完成单/多进程开箱即用双向采集（`INGRESS + EGRESS`）落地后，控制面已能完整归档被测进程的入口请求与下游出站调用。但在 `独立流量调试工作台` (`/debug`) 的真实业务联调中，平铺表格呈现出四大体验断层，导致研发与测试人员难以直观理解多条报文之间的**先后顺序、调用归属、参数流转与状态依赖**：

1. **时序因果倒挂（子调用排在父调用之前）**：
   - 慢轨配对器 `ExchangeAssembler` 在收到应答终结帧 `RSP` 时才刷出完整往返记录（子调用 `EGRESS` 总是比等待它的父入口 `INGRESS` 先收到应答并先落盘），且未在 `REQ` 到达时赋予单调发起序号（`SeqNo = 0`）；
   - `GET /api/traffic-records` 执行 `ORDER BY seq_no ASC` 退化为按物理落盘（即完成顺序 / 后序遍历）返回，造成界面上先看到若干条无头 `EGRESS`，最后才看到触发它们的父 `INGRESS`（如 `GetItemStock`、`GetBalance` 排在父请求 `GetCompositeQuote` 之前），违背人类按“起因 $\rightarrow$ 经过 $\rightarrow$ 结果”阅读调用链的直觉。
2. **层级、并发与补偿语义湮灭，且连接内流水号裸露引发误解**：
   - 平铺列表将主从跨层调用碾平在同一视觉层级，用户无法一眼分辨哪些 `EGRESS` 属于哪笔入口事务、同层下游调用是**串行接力 (`SEQUENTIAL`)**、**并发扇出 (`PARALLEL`)** 还是前序失败后的 **Saga 逆向补偿 (`COMPENSATION`)**；
   - `ullSNO` 在 CROS RPC 协议中仅在单条 TCP 连接（`ConnKey`）内自增，不同连接完全独立计数，直接裸露展示 `1002` 会导致用户误将某条出站长连接上的 `GetBalance (1002)` 与入站连接上的 `CreateOrder (1002)` 当作同一请求；
   - 列表接口直接返回裸 `models.TrafficRecord` 而未水合 `src_module_name / dst_module_name / process_name` 及 `MNG.acProcName` 对端身份，导致「架构链路」列全部显示为 `-`。
3. **协议阶段耗时缺口与业务数据流血缘黑盒**：
   - `pkg/crosrpc` 的 `RspLayout` 尚未提取 V03/V04 应答会话头偏移 `40..47` 的 `ullServerRecvTime` 与 `48..55` 的 `ullServerSendTime`，无法联合 `RPC_TIME_STAMP_HEAD_S.ullServerEnqueueTime` 拆解“网络传输 vs 线程池入队等待 vs 业务纯执行”；
   - 已解码的 `payload_json` 未建立**单事务内参数传递血缘**（如 `LockStock.rsp.lock_id` $\rightarrow$ `ConfirmStock.req.lock_id`）与**跨事务有状态业务实体生命线**（如 `CreateOrder.rsp.order_id` $\rightarrow$ `GetOrderDetail.req.order_id` $\rightarrow$ `CancelOrder.req.order_id`）；同时对协议层 `status_code == 0` 但业务载荷返回错误码（如 `rsp.code != 0`）的软失败缺乏醒目提示。
4. **单包/多包重放缺少 `RecordID` 级专属 `ScopedMock` 闭环与状态前置提示**：
   - 尽管 Agent 侧 `pkg/replay.ScopedMockMatcher` 已预置 `AddScopedMock(recordID, ...)` 与 `SetActiveRecordID(recordID)`，但 `pkg/agentproto.ExecuteReplayReq` 与主控 `diff.ReplayScheduler` 仍平铺下发全批次最多 500 条 `EGRESS` Mock，存在跨事务顺序兜底“张冠李戴”隐患，且重放抽屉无法预览本事务绑定的下游 Mock 闭包，也无法在用户直接重放 `CancelOrder` 时提示前置 `CreateOrder` 状态依赖。

经与业务团队确认真实生产运行基线与交互偏好，明确以下三项设计前提：
- **前提 1**：真实业务环境默认开启 CROS RPC 扩展头 `RPC_EXTEND_HEAD_TYPE_TRACE_KEY`（携带 `TraceID`、`SpanID`、`FatherSpanID`），可直接作为因果组装的第一权威真实源；
- **前提 2**：`/debug` 主界面默认直接采用 **「可折叠事务调用树 + 内联迷你甘特瀑布条」** 作为主表格，辅以 `[🌳 事务调用树 | 📋 原始时序平铺]` 切换开关；
- **前提 3**：在里程碑 **`M13`** 中一次性完成从数据面协议栈、控制面调用图与血缘引擎、前端树表与交互式时序泳道图到事务闭包重放的全链路闭环。

---

## 2. 架构决策细则 (Decision Details)

### 2.1 决策一：数据面协议阶段时间戳补齐、首字节定戳与发起序/对端身份锚定

1. **补齐 V03/V04 `RPC_RSP_HEAD_S` 服务端收发时间戳 (`pkg/crosrpc`)**：
   - 在 `RspLayout`（`188B` V03 与 `200B` V04）中显式登记 `ServerRecvTimeOffset = 40`（`40..47`, 8B 大端）与 `ServerSendTimeOffset = 48`（`48..55`, 8B 大端），并在 `RspHead` 导出 `ServerRecvTime uint64`、`ServerSendTime uint64`；
   - 联合扩展头 `RPC_TIME_STAMP_HEAD_S`（`extType = 1`）的 `ullServerEnqueueTime`，在单调同量纲条件下自动计算三段微秒级耗时拆解：**网络及框架传输耗时 (`NetworkAndFramingUs`)**、**服务端入队等待耗时 (`QueueWaitUs = ServerEnqueue - ServerRecv`)** 与 **业务纯执行耗时 (`ServerExecUs = ServerSend - ServerEnqueue`)**。
2. **流式分帧首字节到达定戳 (`FirstByteCapturedAtNs`)**：
   - `StreamFramer` 在定位到 `"CSE.TCP"` 魔术字首字节进入 `ReadCommHead` 状态瞬间锁定该帧的 `frameStartNs`，在多 TCP 分片凑齐输出完整帧时以 `frameStartNs`（而非末字节到达时刻）填充信封 `CapturedAtNs`，确保大报文分片场景下依然严格保持 `Parent.ReqCapturedAtNs <= Child.ReqCapturedAtNs <= Child.RspCapturedAtNs <= Parent.RspCapturedAtNs`。
3. **慢轨解码器接入真实 `payload_json` 落盘、单调序号 (`SeqNo`) 与连接级 `MNG.acProcName` 身份缓存 (`ExchangeAssembler`)**：
   - 为 `ExchangeAssembler` 注入 `dec *decoder.Registry`（通过 `handlers/common.go` 构造注入）：在慢轨聚合生成记录时调用 `dec.DecodePayloadToJSON` 完成请求与应答双向载荷解析，真实落盘 `payload_json: {"req": {...}, "rsp": {...}}`，彻底根治既有代码中 `reqJSON` 未赋值导致 `payload_json` 恒为 `null` 的文档与实现脱节问题；基于已解码 JSON 载荷提取顶层业务软错误（`code != 0` 或 `success == false`）与实体键值（如 `order_id`）写入 `headers_json.meta`；
   - `ExchangeAssembler` 为每个录制批次维护原子递增序号器 `sessionSeq`：**在收到 `REQ` 首帧创建挂起项瞬间（以及 `ONEWAY` / `MNG` / `RSP_ORPHAN` 到达瞬间）立即分配 `SeqNo = sessionSeq.Add(1)`**，而非等到 `RSP` 完成落盘时分配；所有查询默认按 `ORDER BY req_captured_at_ns ASC, seq_no ASC, id ASC` 排序，彻底根治后序倒挂；
   - 当连接捕获到 `ucMsgType ∈ {5, 6, 7}` (`MNG`) 握手管理帧且 `acProcName` 非空时，在连接上下文中缓存对端自报进程名并随同连接后续记录写入 `headers_json.meta.peer_proc_name`，为未纳管外部上游补齐可读身份标识。

### 2.2 决策二：基于 DOTR `TraceKey` 的权威因果树、跨进程跳点缝合与双层数据流血缘引擎 (`internal/server/callgraph`)

在控制面新增纯内存无副作用领域包 `internal/server/callgraph`，不破坏现有 15 张核心业务表物理结构（仅在落盘时向已有 `headers_json` 注入轻量 `meta` 摘要，并幂等补充复合索引 `idx_traefik_tr_session_req_ns (session_id, req_captured_at_ns, id)`）：

```mermaid
flowchart LR
    Raw["分区表轻量列投影<br/>(ORDER BY req_captured_at_ns ASC)"] --> Hydrate["模块/进程快照水合<br/>+ Conn#k 短编号生成"]
    Hydrate --> Stitch["多进程跨跳缝合<br/>(P1.EGRESS ⇔ P2.INGRESS)"]
    Stitch --> Tree["DOTR TraceKey 权威建树<br/>+ 无 Trace 时间窗降级兜底"]
    Tree --> Classify["同层兄弟分类 (串行/并发/补偿)<br/>+ 关键路径与瀑布百分比计算"]
    Classify --> Entity["跨事务有状态业务实体链提取<br/>(PRODUCER ➔ CONSUMER)"]
```

1. **DOTR 权威父子建树与无 Trace 时间窗优雅降级**：
   - **主路径 (`DOTR_EXACT`)**：以 `RPC_EXTEND_HEAD_TYPE_TRACE_KEY` 的 `TraceID` 聚合同一分布式事务，按 `node.FatherSpanID == parent.SpanID`（兼容同进程内入口 `INGRESS` 与其直接触发的 `EGRESS` 共享上下文或挂载于同进程活跃入口 Span）构建有向无环调用树，内置 `visited` 破环保护与最大递归深度硬上限 `max_depth = 16`；
   - **降级兜底 (`TEMPORAL_INFERRED`)**：针对极少数未携带 `TraceID` 的 `EGRESS` 记录或 `MNG` 帧，在同 `(session_id, process_id)` 内匹配满足 `[Parent.ReqNs - 500μs, Parent.RspNs + 500μs]` 的最内层入口 `INGRESS` 节点挂载；仍无归属的记录作为独立单节点根事务输出，确保全量报文 100% 可见、零遗漏。
2. **多进程协同录制下的跨跳缝合 (`Cross-Process Hop Stitching`)**：
   - 由于 `EgressProxy` 代拨下游时会使用新的本地临时端口，上游进程 $P_1$ 的 `EGRESS` 与下游进程 $P_2$ 的 `INGRESS` 四元组不同；
   - 引擎优先按 **`(TraceID, SpanID / FatherSpanID, ServiceName, Operation)`** 精确配对跨进程同跳记录（无 Trace 时回退按 `(ServiceName, Operation, ReqPayloadSHA256, DstPort, |ΔReqNs| <= 50ms)` 配对），合并为复合跨跳节点 `[P1 ➔ P2]` 并自动算出单跳网络开销 `net_hop_us = max(0, e.latency_us - i.latency_us)`，同时支持在树表或解剖抽屉中一键切换查看客户端侧 `EGRESS` 与服务端侧 `INGRESS` 原始帧。
3. **同层兄弟时序语义分类与业务软错误透视**：
   - 对同一父节点下的有序子调用序列判定 **`PARALLEL`**（时间窗口重叠 `> 50μs`）、**`COMPENSATION`**（发生于前序兄弟协议错误或业务软错误之后，且方法名命中 `Cancel* / Release* / Rollback* / Refund* / Unlock* / Compensate* / Revert*`）与 **`SEQUENTIAL`**（默认串行接力）；
   - 提取 `payload_json.rsp` 顶层常见业务状态字段（`code / err_code / ret_code / error_code` 非零或 `success == false`）标记 `biz_error = true` 与 `biz_status_summary`，解释“协议层返回 `0` 但业务触发回滚补偿”的根因。
4. **单事务参数数据流血缘 (`ParameterLineage`) 与跨事务实体生命线 (`EntityLifecycleChain`)**：
   - **单事务内（按需计算）**：在打开指定根事务时序泳道图时，对事务内 `<= 200` 个节点的 `payload_json`（限制递归深度 `<= 6`、单报文标量叶节点 `<= 500` 防复杂度爆炸）执行高熵标量与关键标识字段匹配，生成 `from_record.rsp.lock_id ➔ to_record.req.lock_id` 参数流转有向边；
   - **跨事务级（落盘轻量索引 + 批次聚合）**：识别前序根事务 `rsp` 新产出的业务实体主键（如 `order_id`、`quote_id`、`payment_no`，排除 `user_id / tenant_id / page` 等环境噪声词表）与后续根事务 `req` 对同一实体的消费，串联成有状态生命周期链 `PRODUCER (创建) ➔ CONSUMER (查询/变更/取消)`。

### 2.3 决策三：`/debug` 默认「可折叠事务调用树 + 内联甘特瀑布」与「时序泳道 + 四段解剖」联动工作台

1. **主表格默认树形瀑布视图 (`?view=tree`) 与根事务级不割裂分页**：
   - `/debug` 默认进入 **`🌳 事务调用树`** 模式，右上角提供 `[🌳 事务调用树 | 📋 原始时序平铺]` 分段切换（同步 URL `?view=tree|flat`）；
   - **分页语义严格兼容工作区通用分页规范**：`view=tree` 按**根业务事务 (`TransactionTreeGroupDTO`)** 分页（选项固定 `15 / 25 / 50 / 100`，默认 `25`，5 页连续数字滑动窗口），保证同一事务下的入口与子孙调用 100% 聚合在同一页内（单根事务子节点返回上限 `200` 条防极端循环撑爆）；当搜索过滤命中深层子调用时，自动保留并展开其完整祖先路径；
   - 每行内置 **`InlineWaterfallBar`**（以根事务总跨度为 `100%` 轨道，仅使用真动态百分比定位 `left` / `width`，颜色严格消费 `--color-*` 语义 Token），并将裸 `ullSNO` 升级为 **`#Seq` 发起序 + `Conn#k · SNO`** 组合标识。
2. **交互式 UML 全链路时序泳道与四段解剖双栏联动抽屉 (`TransactionSequenceDrawer`)**：
   - 点击任意事务的 `[📊 时序泳道]` 滑出宽屏抽屉：左栏采用**纯原生 React SVG 组件**（零第三方重型图表库、零 `innerHTML` / `dangerouslySetInnerHTML`）渲染多进程/模块泳道、请求实线与应答虚线、三段耗时色柱（网络 / 排队 / 执行）及可开关的参数传递血缘弧线；
   - 点击泳道图中任意一跳箭头，右栏内嵌的 `PacketAnatomyDrawer` 零延迟联动刷新该跳的 24B 公共头、V03/V04 会话头阶段耗时、DOTR 扩展头及 Protobuf JSON + 16B Raw Hex，并高亮关联血缘字段。

### 2.4 决策四：端到端打通 `RecordID` 级 `ScopedMock` 事务闭包重放与有状态前置链回放

1. **主从重放协议升级为请求+应答成对规则（坚守 Agent 零 DB、零 `.pb` 红线，彻底根除事务内并发错配）**：
   - 原有设计仅下发下游应答帧，导致 Agent 侧初始化时使用应答载荷 SHA256 注册精确键，而运行时被测程序发起的是下游请求帧，两者哈希必然不匹配，致使精确匹配 100% 击穿退化为同方法名 FIFO 顺序出队；当事务内并发或多次调用同一方法（如并发锁定不同 SKU 库存）时，会导致参数与应答完全错配（“张冠李戴”）；
   - `pkg/agentproto.ExecuteReplayReq` 定义成对规则 `MockRuleDownstream{ReqPacket []byte, RspPackets [][]byte}`，新增 `IngressItems []ReplayIngressItem{RecordID, RawReq}` 与 `ScopedEgressMocks map[uint64][]MockRuleDownstream`（加 `GlobalFallbackMocks []MockRuleDownstream` 兜底，保留旧平铺字段向后兼容）；
   - 主控 `diff.ReplayScheduler` 按 DOTR 子树（无 Trace 时回退时间包含窗）为每条选定 `INGRESS` 提取成对的出站请求帧 `RawReqPacket` 与应答帧切片 `RawRspPacket` 组装 `ScopedEgressMocks[recordID]`；
   - Agent `ReplayExecutor` 在初始化时从 `rule.ReqPacket` 提取服务与方法名并计算实际 `ReqPayload` 的 SHA256 注册精确键，用 `rule.RspPackets` 应答；在注入每条 `IngressItem` 前调用 `matcher.SetActiveRecordID(item.RecordID)`，使运行时请求能够 100% 精确命中自身入参对应的应答，彻底消除事务内部并发/同方法错配。
2. **重放闭包透明预览、前置状态依赖防呆与泳道图故障叠加**：
   - 新增 `GET /api/traffic-records/:id/replay-closure`：在重放抽屉启动前清晰列出本次自动挂载的专属下游 `EGRESS` Mock 清单；若检出当前事务消费了同批次前序事务产出的业务实体（如直接重放 `CancelOrder`），温和提示前置依赖链并提供复选框一键将 `[CreateOrder.id, CancelOrder.id]` 按序组合重放；
   - 重放完成后，支持一键在 `TransactionSequenceDrawer` 时序泳道图上叠加渲染 `SCOPED_HIT`、`MOCK_MISS` 或 `RSP_DIFF` 故障发生点。

---

## 3. 备选方案权衡 (Alternatives Considered)

| 备选方案 | 优点 | 否决原因 |
| :--- | :--- | :--- |
| **方案 A：后端保持平铺分页，仅在前端对当前页 25 条记录做内存建树** | 无需新增控制面调用树接口 | **分页割裂致命缺陷**：若某入口事务包含 6 条子调用且恰好跨越第 25/26 条分页边界，或父子因异步完成相隔数十条记录，前端将出现无父孤儿行或残缺假树；且无法支撑跨页的业务实体生命周期链分析。 |
| **方案 B：新增独立物理表 `traefik_trace_trees` / `traefik_data_lineages` 落盘物化边** | 查询时可直接读物化表 | **破坏存储简洁性与分区 TTL 性能**：打破冻结的 15 张核心表架构，且与 `traefik_traffic_records` 按日 RANGE 分区 `DROP TABLE` 产生跨表清理耦合；单录制批次上限 `<= 100,000` 帧，依托轻量列投影 + `headers_json.meta` 摘要即可在 `< 30ms` 内实时构建树，单事务泳道血缘按需仅扫描 `<= 200` 行 `< 5ms`。 |
| **方案 C：向 Agent 下发 JSON 路径规则做业务字段级动态 Mock 匹配** | 可按业务主键匹配下游 Mock | **触碰物理防腐红线**：迫使 `cmd/agent` 解析 Protobuf / JSON 载荷，严重违背「Agent 零 DB、零 `.pb`、Payload 纯不透明字节流」架构宪法；通过主控按 DOTR 因果树下发成对二进制 `{ReqPacket, RspPackets}` 规则切分 `ScopedEgressMocks[recordID]`，既在纯二进制层实现了精确哈希匹配与防错配，又完全守住了 Agent 零 `.pb` 零 DB 红线。 |

---

## 4. 决策影响与演进后果 (Consequences)

### 正向收益
- **一眼看懂全局因果与性能瓶颈**：默认调用树瀑布表彻底消除 `EGRESS` 先于 `INGRESS` 的反直觉乱序，串行、并发、回滚补偿与慢调用占比一目了然；
- **协议到业务全栈可解释**：从 24B 公共头、`MNG.acProcName`、V03/V04 排队与执行耗时，到 `payload_json` 跨调用参数血缘与跨事务实体链全部贯通；
- **重放确定性与成功率跃升**：`ScopedEgressMocks[recordID]` 消除多请求同名下游调用干扰，前置实体链提示大幅降低有状态写读接口单包重放的业务报错率。

### 约束与边界控制
- **安全与资源防爆红线**：调用树递归强制限制 `max_depth <= 16`、单根事务返回子节点上限 `200`、单事务血缘提取限制 JSON 深度 `<= 6` 与标量数 `<= 500`；前端 SVG 时序泳道图 100% 采用 React JSX 原生节点渲染与 `--color-*` 语义变量，严禁任何原始 HTML 注入或硬编码颜色值。
