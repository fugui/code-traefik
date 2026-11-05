# M16 作战卡：设计态契约破坏性变更阻断雷达、下游血缘推导与回滚逃生舱

- **里程碑编号**：`M16`
- **状态**：✅ 已交付 (Completed)
- **前置依赖**：`M15` 已交付
- **关联特性**：`F-26`、`SC-09`
- **关联设计规范**：[`14-governance-dashboard-golden-suite-and-ci-gate-adr.md`](../05-adr/14-governance-dashboard-golden-suite-and-ci-gate-adr.md) (ADR-14 v1.1.0)、[`01-http-api-contract.md`](../03-api/01-http-api-contract.md) (v1.4.0)

---

## 1. 里程碑目标与 TDD 实施要点

1. **Protobuf `FileDescriptorSet` 全维度 AST 破坏性比对引擎 (`internal/server/decoder/breaking.go`)**：
   - 针对同一逻辑制品 `name` 存在当前生效版本 `activeSchema` 的场景，对比候选版本自有 `.proto` 的 AST 结构，精准检出 6 类 `FATAL` 级违规：
     1. `SERVICE_OR_METHOD_REMOVED`：删除已生效的 `Service` 或 RPC `Method`；
     2. `METHOD_SIGNATURE_CHANGED`：变更 RPC `Method` 的入参/出参 Message 全名，或变更 `client_streaming` / `server_streaming`；
     3. `TAG_MUTATION`：既有字段数字 Tag 变更或原 Tag 被其他字段名复用；
     4. `TYPE_INCOMPATIBLE`：标量类型、基数标签（单值 ⇄ `repeated`）、`map<K,V>` 键值类型或 `oneof` 拆并发生不兼容变异；
     5. `REMOVED_WITHOUT_RESERVE`：删除既有 Message 字段，且原数字 Tag **未落入新版 `DescriptorProto.GetReservedRange()` `[Start, End)` 区间内**（仅声明 `reserved "field_name"` 不保留数字 Tag 仍严格判定为 `FATAL`）；
     6. `ENUM_BREAKING_CHANGE`：既有枚举项数值改变，或删除枚举项但未在 `EnumDescriptorProto.GetReservedRange()` `[Start, End]` 闭区间内保留原数值；
   - 向后兼容演进（新增 Service/Method、新增 `optional`/`repeated` 字段、带数值 `reserved` 的废弃删除）判定为 `PASS`。
2. **下游消费者影响面血缘推导与管理员审计逃生舱 (`handlers/proto_handler.go`)**：
   - 递归定位引用了受损 Message/Enum 的自有 `ServiceName`，联合查询 `traefik_module_services` 与 `traefik_module_dependencies`，提取所有下游依赖模块（含 `module_code`、`module_name`、`arch_process_code`、`owner_name`）；
   - 在 `ActivateProtoSchema`、`BatchActivateProtoSchemas` 及 `UploadProtoSchema(auto_activate=true)` 的咨询锁事务内执行门禁：
     - 默认检出 `FATAL` 规则时返回 `409 Conflict`（`code: "BREAKING_CHANGE_DETECTED"`，`breaking_rules` + `affected_dependents`）；
     - 纯上传草稿（`auto_activate=false`）仅在 `201` 响应附带 `breaking_warnings` 提前预警而不阻断上传；
     - **管理员逃生舱**：当 `traefik_admin` 显式传入 `allow_breaking=true` 且 `len(strings.TrimSpace(breaking_reason)) >= 8` 时允许紧急版本回滚或大版本强推，强制写入 `P1` 级操作审计日志（记录完整违规项与原因）。
3. **前端破坏性变更拦截与影响面透视抽屉 (`frontend/src/pages/ProtoSchemasPage/BreakingChangeAlertDrawer.tsx`)**：
   - 激活或批量激活收到 `409 BREAKING_CHANGE_DETECTED` 时自动滑出告警抽屉，结构化展示违规规则表、受波及下游模块与负责人徽章，并为管理员提供需填写原因的二次确认「强制回滚/激活」折叠区。

---

## 2. 交付包与先行测试清单 (TDD Red -> Green)

| 分层 | 目标代码文件 | 先行测试文件与核心用例 |
| :--- | :--- | :--- |
| **AST 引擎与激活门禁** | `internal/server/decoder/breaking.go`<br>`internal/server/decoder/registry.go`<br>`handlers/proto_handler.go` | `internal/server/decoder/breaking_change_test.go` & `handlers/proto_breaking_test.go`：<br>- `TestDetectBreakingChanges_ServiceMethodRemoved_AndSignatureChanged`<br>- `TestDetectBreakingChanges_TagMutation_TypeIncompatible_AndMapOneof`<br>- `TestDetectBreakingChanges_RemovedField_RequiresNumericReservedRange`<br>- `TestSC09_BreakingChangeDetection_DownstreamAlert_AndAllowBreakingRollbackAudit` |
| **前端拦截抽屉** | `frontend/src/pages/ProtoSchemasPage/BreakingChangeAlertDrawer.tsx` | `frontend/src/__tests__/BreakingChangeDrawer.test.tsx`：<br>- `testBreakingChangeDrawer_RendersRulesDependents_AndAdminOverrideReasonValidation` |

---

## 3. 准出质量门禁命令 (Exit Gate)

```bash
TEST_DB_DSN="host=127.0.0.1 port=5432 user=postgres password=PWD dbname=postgres sslmode=disable" make test
make lint && make build && make clean
```

