# Milestone 1 作战卡：CROS RPC 协议栈、主从二进制信封与 13 表物理分区数据模型

- **文档编号**：`02-m1-crosrpc-and-domain-models.md`
- **文档版本**：v1.0.0
- **更新日期**：2026-10-29
- **归属阶段**：阶段三/四：M1 TDD 任务拆解与验收标准 (`04-tasks`)
- **覆盖特性与场景**：`F-01`(模型层)、`F-02`(模型层)、`F-03`(模型层)、`F-06`(模型层)、`F-07`(信封协议)、`F-10`(分帧与头解析)、`F-11`(分区表)、`SC-04`(分帧基础)
- **设计参考基线**：[`01-system-architecture.md` §4.3](../02-design/01-system-architecture.md)、[`02-data-plane-and-cros-rpc-engine.md` §1](../02-design/02-data-plane-and-cros-rpc-engine.md)、[`05-database-schema.md` §1~§6](../02-design/05-database-schema.md)、[`01-cros-rpc-protocol-spec.md`](../07-references/01-cros-rpc-protocol-spec.md)

---

## 1. 交付包与文件清单 (Deliverable Packages & Files)

| 操作类型 | 包 / 文件路径 | 职责与实现范围（详细结构见设计文档链接） |
| :---: | :--- | :--- |
| **DELETE** | `handlers/item.go`<br>`handlers/item_test.go` | 彻底移除 `code-template` 脚手架示例条目控制器与旧单测，并从路由注册中剥离 `/api/items*` |
| **REFACTOR** | `models/item.go` -> `models/setting.go` | 移除 `TemplateItem`（`traefik_items`）；仅保留 `TraefikSetting`（`traefik_settings`）与模块参数校验 |
| **NEW** | `pkg/crosrpc/constants.go`<br>`pkg/crosrpc/comm_head.go` | CROS RPC 常量、24B `RPC_COMM_HEAD_S` 大端解析与 `ValidateCommHead` 边界校验（见 [`02-data-plane-and-cros-rpc-engine.md` §1.1](../02-design/02-data-plane-and-cros-rpc-engine.md)） |
| **NEW** | `pkg/crosrpc/session_head.go`<br>`pkg/crosrpc/extend_head.go` | `RPC_REQ_HEAD_S` (V00~V05)、`RPC_RSP_HEAD_S` (V00~V04 表驱动 `LookupRspLayout`)、`RPC_MNG_HEAD_S` (88B) 及 `0..N` 段扩展头 (`TRACE_KEY`, `TIME_STAMP`) 解析（见 [`02-data-plane-and-cros-rpc-engine.md` §1.3~§1.4](../02-design/02-data-plane-and-cros-rpc-engine.md)） |
| **NEW** | `pkg/crosrpc/framer.go` | `StreamFramer` 流式状态机（脏字节滑窗重同步、半包/粘包、`DrainOversize` 超 4MB 截断排空）与多段拼接帧无损切分函数 `SplitFrames`（见 [`02-data-plane-and-cros-rpc-engine.md` §1.2](../02-design/02-data-plane-and-cros-rpc-engine.md)） |
| **NEW** | `pkg/crosrpc/crosrpc_test.go`<br>`pkg/crosrpc/framer_fuzz_test.go` | `pkg/crosrpc` 单元测试与 Go 原生 Fuzz 模糊测试 |
| **NEW** | `pkg/agentproto/protocol.go`<br>`pkg/agentproto/envelope.go`<br>`pkg/agentproto/envelope_test.go` | 主从 HTTP REST DTO 与 WebSocket 56B 大端二进制信封头 `EnvelopeHeader` 编解码（见 [`01-system-architecture.md` §4.2~§4.3](../02-design/01-system-architecture.md)） |
| **NEW** | `models/types.go`<br>`models/arch.go`<br>`models/agent.go`<br>`models/proto_schema.go`<br>`models/recording.go`<br>`models/replay.go` | `U64Numeric` 无符号 64 位精度类型及 13 张 `traefik_*` 核心业务表 GORM 模型与唯一/部分唯一索引声明（见 [`05-database-schema.md` §3~§4](../02-design/05-database-schema.md)） |
| **NEW** | `models/db.go`<br>`models/partition_manager.go`<br>`models/models_test.go` | 13 张业务表 + `traefik_settings` 自动迁移、`traefik_traffic_records` `PARTITION BY RANGE (created_at)` 母表 DDL、按日分区预建及 30 天 TTL `DROP TABLE` 管理器（见 [`05-database-schema.md` §6](../02-design/05-database-schema.md)） |

---

## 2. TDD 先行测试矩阵 (Test-First Matrix)

| 目标测试文件 | 测试函数名 | 输入场景与前置条件 | 预期断言结果 (Assertions) |
| :--- | :--- | :--- | :--- |
| `pkg/crosrpc/crosrpc_test.go` | `TestValidateCommHeadBoundaries` | 输入合法 REQ/RSP/MNG 24B 头，以及非法魔术字、`TransportProto > 1`、`ucMsgType` 越界 (`1` 或 `15`)、`usHeadLen < 最小会话头`、`usHeadLen > uiLength`、`uiLength > MaxPayloadLen + usHeadLen` 的畸形头 | 合法头返回 `nil`；所有越界与畸形头准确返回对应错误，无整数溢出 |
| `pkg/crosrpc/crosrpc_test.go` | `TestStreamFramerDirtyResyncFragmentAndOversize` | 1. 头部前含 17B 随机脏字节的流；<br>2. 每次 `Write` 1 字节的极端碎片流；<br>3. 3 个完整帧拼接的粘包流；<br>4. `uiLength = 6MB > MaxCaptureBytes(4MB)` 且紧跟 1 条正常帧的流 | 1. 准确计入 `DirtyBytes=17` 并切出完整帧；<br>2. 凑齐后零丢失输出帧；<br>3. 一次循环连续输出 3 帧；<br>4. 超大帧保留前 4MB 并标记 `IsTruncated=true`，流式排空尾部 2MB 后紧随的正常帧在偏移 0 完美对齐切出 (`SC-04`) |
| `pkg/crosrpc/crosrpc_test.go` | `TestParseReqRspMngAndExtendHeads` | 构造 V01(`160B`) / V05(`256B`) `RPC_REQ_HEAD_S`、V01(`156B`) / V02(`164B`) / V03(`188B`) / V04(`200B`) `RPC_RSP_HEAD_S`、定长 `88B` `RPC_MNG_HEAD_S`，并附带 `TRACE_KEY` 与 `TIME_STAMP` 两段扩展头 | 准确提取 `ascServiceName` (`{package}-{serviceName}`)、`ascOperation`、`aucTraceBuff`、`ullSendTimeStamp`、`ullStatusCode`、`usRspMsgNo`、`TraceID`/`SpanID`/`FatherSpanID` 及 `ServerEnqueueTime`；非标 `usRspHeadLen=170` 调用 `LookupRspLayout` 返回 `ErrUnknownRspLayout` |
| `pkg/crosrpc/crosrpc_test.go` | `TestSplitFramesMultiSegRsp` | 将 2 段 `RSP_SEG` (`ucMsgType=4`) 与 1 段终结 `RSP` (`ucMsgType=3`) 连续字节切片输入 `SplitFrames` | 零拷贝歧义切分出 3 个独立帧切片，每段 `usRspMsgNo` 递增且长度与各自 `24 + uiLength` 完全一致 |
| `pkg/crosrpc/framer_fuzz_test.go` | `FuzzStreamFramer` | 随机变异的任意字节切片流（含伪造 `"CSE.TCP"` 与极端 `uiLength`/`usHeadLen` 组合）投入 `StreamFramer` 与 `ParsePacket` | 持续运行 `>= 60s` 零 panic、零越界切片、单次内存分配受 `MaxCaptureBytes` 严格约束 (`F-10.2`) |
| `pkg/agentproto/envelope_test.go` | `TestEnvelopeHeaderMarshalUnmarshal` | 构造 `Magic=0x4354`、`ProtoVer=1`、`EventType=0x01..0x07`、`Flags` (`IS_TRUNCATED \| REF_KIND \| HAS_GAP`)、四元组 IPv4/Port 与变长载荷；以及 `<56B` 或错误 Magic 的损坏帧 | 序列化固定 56B 大端头 + 载荷，反序列化字段 100% 无损还原；损坏帧返回明确错误 |
| `models/models_test.go` | `TestAll13ModelsTablePrefixAndMigration`<br>*(强制使用 `testdb.SetupIsolatedDB(t)`)* | 在临时隔离库执行 `SetupWithDB(db)`，反射遍历全部 13 张业务模型 + `TraefikSetting` 的 `TableName()`，并检查 `traefik_items` 表是否存在 | 全部 14 张表名均以 `"traefik_"` 开头；数据库表与索引创建成功；确认旧模板表 `traefik_items` 不存在 |
| `models/models_test.go` | `TestU64NumericMaxUint64RoundTrip`<br>*(强制使用 `testdb.SetupIsolatedDB(t)`)* | 写入 `SNO = math.MaxUint64 (18446744073709551615)` 与 `StatusCode = math.MaxUint64` 的 `TrafficRecord` 并重新查询序列化为 JSON | 数据库 `NUMERIC(20,0)` 无溢出写入与读取，JSON 序列化为 `"18446744073709551615"` 字符串零精度丢失 |
| `models/models_test.go` | `TestPartialUniqueIndexesAndFKConstraints`<br>*(强制使用 `testdb.SetupIsolatedDB(t)`)* | 1. 两个模块声明相同 `DEPEND(svc, op)` vs 声明相同 `PROVIDE(svc, op)`；<br>2. 两个不同名称的 `ProtoSchema` 同时激活相同 `(svc, op)`；<br>3. 删除仍挂载 `FunctionalGroup` 的 `Subsystem` | 1. 相同 `DEPEND` 成功，相同 `PROVIDE` 被 `uk_traefik_provide_service_op` 拦截报错；<br>2. 被 `uk_traefik_active_proto_op` 拦截报错；<br>3. 被 `ON DELETE RESTRICT` 拦截报错 |
| `models/models_test.go` | `TestTrafficRecordsRangePartitionAndDropPurge`<br>*(强制使用 `testdb.SetupIsolatedDB(t)`)* | 调用 `PartitionManager` 预建未来 7 天子分区与 35 天前的过期子分区，各写入 10 条 `TrafficRecord`，执行 30 天 TTL 清理 | `pg_inherits` 确认子分区挂接且带 `idx_traefik_tr_payload_gin` 索引；TTL 清理通过 `DROP TABLE` 物理移除 35 天前子分区，近期分区数据完好 (`F-11.3`) |

---

## 3. 验收门禁命令 (Acceptance Criteria)

```bash
# AC-M1-01: 校验共享协议库与信封库零 gorm / 零 dynamicpb 依赖
! go list -deps ./pkg/crosrpc ./pkg/agentproto | grep -E "gorm|dynamicpb|protoregistry"

# AC-M1-02: 运行协议栈与信封库竞态单测
go test -v -race ./pkg/crosrpc/... ./pkg/agentproto/...

# AC-M1-03: 运行分帧器 Fuzz 测试（至少 60 秒零崩溃）
go test -fuzz=FuzzStreamFramer -fuzztime=60s ./pkg/crosrpc

# AC-M1-04: 运行 13 张表与物理分区管理器的隔离数据库单测
TEST_DB_DSN="host=127.0.0.1 port=5432 user=postgres password=postgres dbname=postgres sslmode=disable" \
  go test -v -race ./models/...
```
