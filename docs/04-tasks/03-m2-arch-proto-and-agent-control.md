# Milestone 2 作战卡：三级架构元素、`.pb` 动态编解码与公私域 Agent 排他租约管控

- **文档编号**：`03-m2-arch-proto-and-agent-control.md`
- **文档版本**：v1.0.0
- **更新日期**：2026-10-29
- **归属阶段**：阶段三/四：M2 TDD 任务拆解与验收标准 (`04-tasks`)
- **覆盖特性与场景**：`F-01`、`F-02`、`F-03`、`F-04`、`F-06`、`F-10`(主控解码)、`F-16`(架构/Agent/Schema RBAC 与审计)、`SC-01`、`SC-02`、`SC-03`(控制面部分)、`SC-04`(解码部分)
- **设计参考基线**：[`01-system-architecture.md` §3~§5.1](../02-design/01-system-architecture.md)、[`03-architecture-elements-and-topology.md` §1~§4](../02-design/03-architecture-elements-and-topology.md)、[`04-control-plane-and-web-console.md` §2 & §5](../02-design/04-control-plane-and-web-console.md)、[`01-http-api-contract.md` §2.1~§2.4](../03-api/01-http-api-contract.md)

---

## 1. 交付包与文件清单 (Deliverable Packages & Files)

| 操作类型 | 包 / 文件路径 | 职责与实现范围（详细算法见设计文档链接） |
| :---: | :--- | :--- |
| **NEW** | `internal/server/topology/tree.go` | 三级架构元素服务、功能组物化路径生成（`"/<id1>/<id2>/"`）、`MoveFunctionalGroup` 咨询锁防成环与 8 级深度上限级联更新、非空删除 `409` 守卫（见 [`03-architecture-elements-and-topology.md` §1.1~§1.3](../02-design/03-architecture-elements-and-topology.md)） |
| **NEW** | `internal/server/topology/static_topology.go`<br>`internal/server/topology/topology_test.go` | 模块接口 `{package}-{serviceName}` 格式与 `PROVIDE` 全局唯一校验、`traefik_process_modules` 公私域挂载隔离、`BuildStaticTopology`（逻辑/物理双视图，推导 `CROSS_PROCESS` / `INTRA_PROCESS` / `EXTERNAL_UNREGISTERED`）与 `CompileProcessDownstreams`（见 [`03-architecture-elements-and-topology.md` §2~§4](../02-design/03-architecture-elements-and-topology.md)） |
| **NEW** | `internal/server/decoder/registry.go`<br>`internal/server/decoder/decoder_test.go` | `.pb` `<=4MB` 体积与 `protodesc.NewFiles` 校验、`{package}-{serviceName}` 映射提取、`ActivateSchemaVersion` 冲突检测、`atomic.Pointer[CompiledRegistry]` 无锁注册表、`DecodePayloadToJSON` 动态编解码与无 Schema Raw Hex 降级（见 [`04-control-plane-and-web-console.md` §2](../02-design/04-control-plane-and-web-console.md)） |
| **NEW** | `internal/server/hub/lease.go`<br>`internal/server/hub/manager.go`<br>`internal/server/hub/hub_test.go` | `AgentEndpointGuard` SSRF 防护、`ClaimCode` 认领核验、`AcquireAgentLeases` 升序 `SELECT FOR UPDATE` 原子排他锁与惰性自愈、10s 周期心跳与双向租约对账（`X-Expected-Lease-*` / `BootID` 重启检测）、3 周期离线自动释放租约、`ServerRecoveryPipeline` 启动恢复及 SSE 广播总线（见 [`01-system-architecture.md` §3.2 & §4](../02-design/01-system-architecture.md)） |
| **NEW** | `handlers/arch_handler.go`<br>`handlers/proto_handler.go`<br>`handlers/agent_handler.go`<br>`handlers/process_handler.go` | 对应 [`01-http-api-contract.md` §2.2~§2.4](../03-api/01-http-api-contract.md) 的全部 REST 控制器，接入 `ScopeReadGuard` / `ScopeWriteGuard` / `AdminOnly` 与 `commonAudit.SetAuditContext` 字段级 Diff 审计 |
| **NEW** | `handlers/arch_proto_agent_test.go` | 覆盖 `F-01`~`F-04`、`F-06`、`F-16` 及 `SC-01`~`SC-04` 的物理隔离数据库 API 集成测试 |

---

## 2. TDD 先行测试矩阵 (Test-First Matrix)

> **强制约束**：以下所有涉及数据库的测试必须调用 `testdb.SetupIsolatedDB(t)`；所有测试内启动的 Mock Agent HTTP 服务必须绑定 `127.0.0.1:0`（测试配置开启 `DevAllowLoopback = true`）。

| 目标测试文件 | 测试函数名 | 输入场景与前置条件 | 预期断言结果 (Assertions) |
| :--- | :--- | :--- | :--- |
| `internal/server/topology/topology_test.go` | `TestArchHierarchyCRUDAndMoveValidation`<br>*(`testdb.SetupIsolatedDB(t)`)* | 1. 创建子系统、3 级功能组树与模块；<br>2. 将功能组 G1 移动到其子孙 G3 下；<br>3. 将 G1 移动到跨子系统功能组下；<br>4. 构造 8 级树后尝试将含 2 级子树的功能组移至第 7 级节点下；<br>5. 合法移动 G2 至新父节点 | 1. 初始 `path` 与 `level` 准确；<br>2. 成环移动拦截返回 `ErrCycleMoveToDescendant` (`400`)；<br>3. 跨子系统移动返回 `ErrCrossSubsystemMove` (`400`)；<br>4. 超 8 级深度返回 `ErrExceedMaxTreeDepth` (`400`)；<br>5. G2 及全部子孙 `path` 与 `level` 单事务级联更新正确 (`F-01`) |
| `internal/server/topology/topology_test.go` | `TestDeleteNonEmptyArchElementsConflict`<br>*(`testdb.SetupIsolatedDB(t)`)* | 尝试删除含功能组的子系统、含子组或模块的功能组、已声明接口或已关联进程的功能模块 | 全部返回 `409 Conflict`；清空子项或解除关联后删除返回 `200 OK` (`F-01.4`) |
| `internal/server/topology/topology_test.go` | `TestModuleInterfaceProvideGlobalUnique`<br>*(`testdb.SetupIsolatedDB(t)`)* | 模块 A 声明 `PROVIDE (dophi.order.v1-TradeService, CreateOrder)`；模块 B 与模块 C 声明相同 `DEPEND`；随后模块 B 尝试声明相同 `PROVIDE` | 模块 B/C 声明相同 `DEPEND` 均返回 `201`；模块 B 声明重复 `PROVIDE` 返回 `409 Conflict` 并指出已被模块 A 占用 (`F-02.2`, `A-05`) |
| `internal/server/topology/topology_test.go` | `TestProcessModuleBindingPublicVsPrivate`<br>*(`testdb.SetupIsolatedDB(t)`)* | 1. 在 `SHARED` Agent 下创建 `Proc_A` 绑定 `Mod_1`；同一 Agent 下再建同端口进程或另一 `SHARED` 进程重复绑定 `Mod_1`；<br>2. 普通用户在本人 `PRIVATE` Agent 下创建 `PrivProc_A` 同样挂载 `Mod_1` | 1. 同 Agent 端口重复或 `SHARED` 域重复挂载同一模块返回 `409`；`Mod_1.ProcessID == Proc_A.ID`；<br>2. `PrivProc_A` 创建成功 (`201`)，写入 `traefik_process_modules`，且全局 `Mod_1.ProcessID` 仍保持为 `Proc_A.ID` 不被污染 (`F-02.1`, `A-03`) |
| `internal/server/topology/topology_test.go` | `TestBuildStaticTopologyLogicalAndPhysical`<br>*(`testdb.SetupIsolatedDB(t)`)* | 构造同进程的 `Mod_1 -> Mod_2` 依赖、跨进程的 `Mod_1 -> Mod_3` 依赖、无任何供给方的 `Mod_3 -> UnknownSvc:Op` 依赖，请求逻辑与物理静态拓扑 | 返回准确的节点与三类边：`INTRA_PROCESS`（进程内）、`CROSS_PROCESS`（跨进程）、`EXTERNAL_UNREGISTERED`（指向外部占位节点），且私有 Agent 进程不混入公共基线拓扑 (`F-04`, `SC-02`) |
| `internal/server/decoder/decoder_test.go` | `TestProtoSchemaUploadAndDashServiceName`<br>*(`testdb.SetupIsolatedDB(t)`)* | 1. 上传 package 为 `dophi.order.v1`、service 为 `TradeService`、method 为 `CreateOrder` 的合法 `.pb`；<br>2. 上传 `> 4MB` 文件或损坏字节流 | 1. 自动生成 `service_name = "dophi.order.v1-TradeService"`（保留点分 package + 中划线连接）、`operation = "CreateOrder"` (`A-07`, `F-03.1`)；<br>2. 超限或损坏文件返回 `400 Bad Request` (`F-03.2`) |
| `internal/server/decoder/decoder_test.go` | `TestProtoSchemaActivateConflictAndHistoryImmutability`<br>*(`testdb.SetupIsolatedDB(t)`)* | 1. 激活 `order.pb v1`；上传并尝试激活含相同 `(service_name, operation)` 的另一描述集 `trade_dup.pb v1`；<br>2. 上传 `order.pb v2` 并切换激活，检查已落盘的历史 `TrafficRecord.SchemaVersion`；<br>3. 尝试删除处于 `is_active=true` 的版本 | 1. 跨描述集冲突激活返回 `409 Conflict` 及冲突接口明细 (`F-03.3`)；<br>2. 同名切换 v2 成功，历史记录 `SchemaVersion` 仍保持 `1` 不变 (`F-03.4`)；<br>3. 删除生效版本返回 `409` |
| `internal/server/decoder/decoder_test.go` | `TestDynamicPBDecodeAndFallbackHex`<br>*(`testdb.SetupIsolatedDB(t)`)* | 1. 输入匹配生效 `.pb` 且含 `uint64` 大整数的 CROS RPC 请求帧 (`SC-04`)；<br>2. 输入未注册接口的 CROS RPC 帧 | 1. `DecodeStatus == "DECODED_JSON"`，Payload 正确反序列化且 `uint64` 编码为十进制字符串；<br>2. `DecodeStatus == "NO_SCHEMA_RAW_HEX"`，保留完整协议头且不报错 (`F-10.2`) |
| `internal/server/hub/hub_test.go` | `TestAgentSSRFGuardAndClaimCode`<br>*(`testdb.SetupIsolatedDB(t)`)* | 1. 生产配置下尝试纳管 `127.0.0.1:9091`、`169.254.169.254:80`、`224.0.0.1:9000` 或端口 `80`；<br>2. 普通用户纳管 `PRIVATE` Agent 时传入错误 `claim_code` vs 正确 `claim_code` | 1. SSRF 守卫全部拦截返回 `400 Bad Request`；<br>2. 错误 `claim_code` 返回 `403 Forbidden`，正确 `claim_code` 握手通过返回 `201` |
| `internal/server/hub/hub_test.go` | `TestAcquireAgentLeasesMutexAndHeartbeatReconcile`<br>*(`testdb.SetupIsolatedDB(t)`)* | 1. 用户 A 与用户 B 并发交叉申请 `[Agent_1, Agent_2]` 与 `[Agent_2, Agent_1]` 排他租约；<br>2. 关闭 Mock Agent 连续触发 3 次心跳探活；<br>3. Mock Agent 重启更换 `BootID` 后响应心跳 | 1. 升序加锁无死锁，1 人成功 (`201`)，另 1 人立即收到 `409 Conflict` 及持锁人信息 (`F-06.3`, `SC-03`)；<br>2. 3 个周期后 Agent 状态变为 `OFFLINE` 并自动清空排他租约 (`F-06.4`)；<br>3. `BootID` 变化触发悬空任务终止与租约自愈释放 |
| `handlers/arch_proto_agent_test.go` | `TestSC01_HealthAuthAndPublicPrivateRBAC`<br>*(`testdb.SetupIsolatedDB(t)`)* | 执行 `SC-01` 全套动作：免密 `GET /api/health`、无 Token 访问 `/api/me`、普通用户尝试创建子系统/创建 `SHARED` Agent/上传 `.pb`、普通用户 A 创建 `PRIVATE` Agent、普通用户 B 尝试查看/修改/租用用户 A 的 `PRIVATE` Agent、`traefik_admin` 查看用户 A 的 `PRIVATE` Agent 并检查 `audit_logs` | `health` 返 `200` (`service: "Code-Traefik"`)；无 Token 返 `401`；普通用户写公共资源返 `403`；用户 A 建私有 Agent 返 `201`；用户 B 横向越权返 `403`；管理员穿透审计查看返 `200`；所有写操作与强制解锁在 `audit_logs` 中含完整 Old/New Diff (`F-16`, `SC-01`, `SC-02`, `SC-03`) |

---

## 3. 验收门禁命令 (Acceptance Criteria)

```bash
# AC-M2-01: 代码格式化与静态检查
go fmt ./... && go vet ./...

# AC-M2-02: 运行架构拓扑、动态解码器、Agent Hub 与 RBAC 控制器全套隔离数据库单测
TEST_DB_DSN="host=127.0.0.1 port=5432 user=postgres password=postgres dbname=postgres sslmode=disable" \
  go test -v -race ./internal/server/topology/... ./internal/server/decoder/... ./internal/server/hub/... ./handlers/...
```
