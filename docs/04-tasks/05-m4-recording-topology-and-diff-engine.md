# Milestone 4 作战卡：协同录制双轨管道、架构一致性验证与双层回归 Diff 引擎

- **文档编号**：`05-m4-recording-topology-and-diff-engine.md`
- **文档版本**：v1.0.0
- **更新日期**：2026-10-29
- **归属阶段**：阶段三/四：M4 TDD 任务拆解与验收标准 (`04-tasks`)
- **覆盖特性与场景**：`F-05`、`F-11`、`F-12`、`F-14`、`F-16`(录制/重放公私域隔离与审计)、`SC-05`、`SC-06`、`SC-07`、`SC-08`
- **设计参考基线**：[`03-architecture-elements-and-topology.md` §3.3 & §5](../02-design/03-architecture-elements-and-topology.md)、[`04-control-plane-and-web-console.md` §1 & §3 & §4.3](../02-design/04-control-plane-and-web-console.md)、[`05-database-schema.md` §3.5](../02-design/05-database-schema.md)、[`01-http-api-contract.md` §2.5~§2.7](../03-api/01-http-api-contract.md)

---

## 1. 交付包与文件清单 (Deliverable Packages & Files)

| 操作类型 | 包 / 文件路径 | 职责与实现范围（详细算法见设计文档链接） |
| :---: | :--- | :--- |
| **NEW** | `internal/server/recorder/orchestrator.go`<br>`internal/server/recorder/assembler.go`<br>`internal/server/recorder/anatomy.go`<br>`internal/server/recorder/retention.go`<br>`internal/server/recorder/recorder_test.go` | 多进程协同录制编排、时长/帧数双阈值自动停录（`COMPLETED_BY_LIMIT`）、Track A (SSE 权限过滤快轨) + Track B (`ExchangeAssembler` 连接哈希分片按 `ullSNO` 聚合 `UNARY`/`STREAM_RSP`/`ONEWAY`/`REQ_ONLY`/`RSP_ORPHAN`/`MNG` 双模微批落盘)、`PacketAnatomyDrawerDTO` 四段解剖组装与按需重解码、30 天分区 `DROP TABLE` 清理器（见 [`04-control-plane-and-web-console.md` §1.1~§1.4](../02-design/04-control-plane-and-web-console.md)） |
| **NEW** | `internal/server/topology/attribution.go`<br>`internal/server/topology/consistency.go`<br>`internal/server/topology/consistency_test.go` | 目的模块唯一定位 (`ResolveDestinationModule`)、来源模块三级优先级归属 (`ResolveSourceModule`：单模块进程收敛 -> ① `FatherSpanID == Ingress.SpanID` -> ② 进程内唯一 `DEPEND` -> ③ `PROCESS_LEVEL` 候选集) 与 `VerifySessionTopology` 六分类比对引擎（见 [`03-architecture-elements-and-topology.md` §5](../02-design/03-architecture-elements-and-topology.md)） |
| **NEW** | `internal/server/diff/engine.go`<br>`internal/server/diff/scheduler.go`<br>`internal/server/diff/diff_test.go` | `ValidateReplayProcessCompatibility` 跨 Agent 同构校验、局部 `ScopedMocks` 组装（剔除截断与 `REQ_ONLY` 帧）、`traefik_replay_items` 流式落盘、`ERROR > TIMEOUT > MOCK_MISS > FAILED > PASS` 全序状态聚合、第一层协议头断言（固定豁免服务端时间戳）+ 第二层 `protoreflect` 语义树 Diff（含 `GetUnknown()` 未知字段检出、`IgnoreMatcher`、`map` 无序 vs `repeated` 保序）及离线 `rediff`（见 [`04-control-plane-and-web-console.md` §1.3 & §3](../02-design/04-control-plane-and-web-console.md)） |
| **NEW** | `handlers/recording_handler.go`<br>`handlers/traffic_handler.go`<br>`handlers/replay_handler.go`<br>`handlers/sse_handler.go`<br>`handlers/recording_replay_test.go` | 对应 [`01-http-api-contract.md` §2.5~§2.7](../03-api/01-http-api-contract.md) 的录制、一致性报告、流量检索/四段解剖、重放任务/条目分页与 `/api/events` SSE 控制器及隔离数据库集成测试 |

---

## 2. TDD 先行测试矩阵 (Test-First Matrix)

> **强制约束**：所有数据库单测强制调用 `testdb.SetupIsolatedDB(t)`；所有测试内启动的临时 `Server` 与 `Agent` 强制绑定 `127.0.0.1:0`。

| 目标测试文件 | 测试函数名 | 输入场景与前置条件 | 预期断言结果 (Assertions) |
| :--- | :--- | :--- | :--- |
| `internal/server/recorder/recorder_test.go` | `TestExchangeAssemblerDualModeAndLimitAutoStop`<br>*(`testdb.SetupIsolatedDB(t)`)* | 1. 投递同连接内相同 `ullSNO` 的 `REQ + RSP`、3 段 `REQ + 2*RSP_SEG + RSP`、`ucReqType=3` Oneway 请求、连接关闭未应答 `REQ_ONLY`、孤立 `RSP_ORPHAN`、超阈值截断帧 (`IS_TRUNCATED`)；<br>2. 将批次 `MaxFrames` 设为 `5` 并连续投递 `6` 帧 | 1. 单行同行持久化非空 `raw_req_packet`、`raw_rsp_packet` 与结构化 `payload_json: {req, rsp}`，`STREAM_RSP` 段数 `RspSegCount=3` 且经 `SplitFrames` 无损还原；截断帧与 `REQ_ONLY` 标记 `is_replayable = false` (`F-11.3`, `F-11.4`)；<br>2. 达到第 5 帧立即自动调用 Agent `/stop`，批次终态为 `COMPLETED_BY_LIMIT` 且 Agent 排他租约已释放 (`F-11.1`, `F-11.3`) |
| `internal/server/recorder/recorder_test.go` | `TestPacketAnatomyBuilderAndRedecode`<br>*(`testdb.SetupIsolatedDB(t)`)* | 对一条含 DOTR 扩展头的双模往返记录调用 `BuildPacketAnatomy`，随后激活新 `.pb` 版本并调用 `RedecodeTrafficRecord` | `reqSide` 与 `rspSide` 四段数据（24B 位图 6 色块、会话头耗时拆解、DOTR Trace、16B 对齐 Hex 行与 JSON）完整；未手动重解码前历史 `SchemaVersion` 不变，显式调用重解码后更新为新版本 |
| `internal/server/topology/consistency_test.go` | `TestResolveSourceModuleThreeTierPriority`<br>*(`testdb.SetupIsolatedDB(t)`)* | 针对承载 `[Mod_A, Mod_B]` 的多模块进程发出的出站调用：<br>1. 带 `FatherSpanID` 匹配同进程入站 `Mod_A` 的 `SpanID`；<br>2. 无 Trace 但仅 `Mod_B` 声明了该 `DEPEND`；<br>3. 无 Trace 且两模块均声明（或均未声明）该 `DEPEND` | 1. 归属 `SrcModule = Mod_A` (`TRACE_SPAN`)；<br>2. 归属 `SrcModule = Mod_B` (`UNIQUE_DEPEND`)；<br>3. 标记 `SrcAttributionMode = "PROCESS_LEVEL"`，`SrcModule = nil`，返回候选模块集合 `[Mod_A, Mod_B]` (`F-05.2`) |
| `internal/server/topology/consistency_test.go` | `TestSC06_ArchitectureConsistencyVerification`<br>*(`testdb.SetupIsolatedDB(t)`)* | 输入预置的上中下游录制数据集（含 1 条已声明且实测调用、1 条未声明跨进程调用、1 条同进程内两模块依赖、1 条指向未纳入本批次监控进程的依赖、1 条已监控但本次未触发的依赖、1 条无 Trace 多模块歧义调用） | `VerifySessionTopology` 精确输出全部 6 类结果：`VERIFIED=1`、`VIOLATION=1`、`NOT_OBSERVABLE=1`、`NOT_MONITORED=1`、`UNCOVERED=1`、`UNATTRIBUTED=1` (`F-05.3`, `SC-06`) |
| `internal/server/diff/diff_test.go` | `TestValidateReplayProcessCompatibility`<br>*(`testdb.SetupIsolatedDB(t)`)* | 1. 用户 B 尝试选用用户 A 的 `PRIVATE` 录制素材，或投递至用户 A 的 `PRIVATE` Agent；<br>2. 用户 B 选用公共 `SHARED` 素材（供给模块为 `Mod_Order`）投递至本人 `PRIVATE` Agent 上仅承载 `Mod_Stock` 的异构进程；<br>3. 投递至本人 `PRIVATE` Agent（或空闲 `SHARED` Agent）上承载 `Mod_Order` 的同构进程 | 1. 横向越权拦截返回 `403 Forbidden` (`F-12.1`)；<br>2. 模块不兼容拦截返回 `400 Bad Request` 并指出缺失 `Mod_Order` (`F-12.1`, `SC-07`)；<br>3. 同构校验通过 (`nil`) |
| `internal/server/diff/diff_test.go` | `TestLayer1HeaderDiffAndTimestampExemption` | 1. 录制基线与实际应答仅 `ullServerRecvTime`、`ullServerSendTime`、`ullServerEnqueueTime`、`ullSNO`、`aucTraceBuff` 不同，Payload 相同；<br>2. 实际应答 `uiStatusCode` 由 `0` 变为 `5001` | 1. 固定豁免字段被忽略，短路裁决为 `PASS` (`F-14.3`, `SC-08`)；<br>2. 第一层直接裁决为 `FAILED`，指出状态码差异 (`F-14.1`) |
| `internal/server/diff/diff_test.go` | `TestLayer2ProtoSemanticDiffMapRepeatedAndIgnore` | 1. `discount_cents` 由 `6500` 变为 `6600`；<br>2. `map<string, int32>` 序列化字节顺序不同但键值一致；<br>3. `repeated` 列表元素顺序对调；<br>4. 差异字段 `header.nonce` 位于 `extra_ignore_paths` 中；<br>5. 实际应答包含 `.pb` 未定义的未知 Tag 字段 | 1. 裁决为 `FAILED`，输出 `FieldPath="pricing.discount_cents"`, `Expected=6500`, `Actual=6600` (`F-14.2`, `SC-08`)；<br>2. `map` 无序比对裁决为 `PASS` (`F-14.5`)；<br>3. `repeated` 保序比对裁决为 `FAILED` (`F-14.5`)；<br>4. 命中忽略规则裁决为 `PASS` (`F-14.4`)；<br>5. 检出 `<unknown_fields>` 裁决为 `FAILED` |
| `handlers/recording_replay_test.go` | `TestSC05_SC07_SC08_EndToEndRecordingAndReplayPipeline`<br>*(`testdb.SetupIsolatedDB(t)`)* | 拉起受控 `Server` + `Agent` (`127.0.0.1`)：<br>1. 协同录制 `Process_A -> Process_B` 请求并停止 (`SC-05`)；<br>2. 跨 Agent 将 `Process_A` 请求投递至同构测试进程重放，验证正常 `PASS` (`SC-07`)；<br>3. 注入字段突变与多调用一次下游触发 `FAILED` 与 `MOCK_MISS` (`SC-08`)；<br>4. 分页查询 `/api/replay-tasks/:id/items?result_status=MOCK_MISS` 并调用 `/rediff` | 1. `traefik_traffic_records` 同行归档双模数据；<br>2. 跨 Agent 重放命中 Mock 且头部自愈，结果为 `PASS`；<br>3. 多请求状态按 `ERROR > TIMEOUT > MOCK_MISS > FAILED > PASS` 聚合，明细独立写入 `traefik_replay_items` 支持按状态分页；重放结束后 Agent 租约 100% 释放，审计日志完整记录 `start_session` / `stop_session` / `execute_replay` |

---

## 3. 验收门禁命令 (Acceptance Criteria)

```bash
# AC-M4-01: 运行录制编排、一致性验证、双层 Diff 与控制面全量隔离数据库竞态测试
TEST_DB_DSN="host=127.0.0.1 port=5432 user=postgres password=postgres dbname=postgres sslmode=disable" \
  go test -v -race ./internal/server/recorder/... ./internal/server/topology/... ./internal/server/diff/... ./handlers/...
```
