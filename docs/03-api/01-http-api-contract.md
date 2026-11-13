# 架构与活体契约治理 (`code-traefik`) HTTP / SSE 与主从通信接口契约规范

- **文档编号**：`01-http-api-contract.md`
- **文档版本**：v1.6.0
- **更新日期**：2026-11-11
- **归属阶段**：阶段三：接口与数据契约冻结 (`03-api`)
- **状态**：已批准待实施 (Approved for M14~M20)
- **上位基线**：[`01-feature-matrix.md`](../01-features/01-feature-matrix.md) (v2.15.0, `F-01`~`F-16`、`F-24`~`F-27`、`SC-01`~`SC-12`)、[`01-system-architecture.md`](../02-design/01-system-architecture.md) (v1.6.0)、[`03-architecture-elements-and-topology.md`](../02-design/03-architecture-elements-and-topology.md) (v1.6.0)、[`04-control-plane-and-web-console.md`](../02-design/04-control-plane-and-web-console.md) (v1.8.0)、[`05-database-schema.md`](../02-design/05-database-schema.md) (v1.6.0)、[`09-arch-process-ssot-and-ebpf-premount-standby-adr.md`](../05-adr/09-arch-process-ssot-and-ebpf-premount-standby-adr.md) (ADR-09 v1.1.0)、[`10-enhanced-proto-schema-file-management-adr.md`](../05-adr/10-enhanced-proto-schema-file-management-adr.md) (ADR-10 v1.1.0)、[`11-single-process-bidirectional-capture-and-recording-ux-adr.md`](../05-adr/11-single-process-bidirectional-capture-and-recording-ux-adr.md) (ADR-11 v1.0.0)、[`12-packet-causality-call-tree-and-sequence-ux-adr.md`](../05-adr/12-packet-causality-call-tree-and-sequence-ux-adr.md) (ADR-12 v1.0.0)、[`14-governance-dashboard-golden-suite-and-ci-gate-adr.md`](../05-adr/14-governance-dashboard-golden-suite-and-ci-gate-adr.md) (ADR-14 v1.1.0)、[`15-scalable-topology-focus-and-layout-persistence-adr.md`](../05-adr/15-scalable-topology-focus-and-layout-persistence-adr.md) (ADR-15 v1.2.0)、[`16-multi-implementation-service-contract-adr.md`](../05-adr/16-multi-implementation-service-contract-adr.md) (ADR-16 v1.0.0)

### 修订记录

| 版本 | 日期 | 修订说明 |
| :--- | :--- | :--- |
| v1.5.1 | 2026-11-09 | 同步 ADR-15 (v1.1.0) 拓扑画布双层布局持久化 API（`/api/arch/topology/layouts*`）与 `structure_version` / `gzip` 协商契约 |
| v1.6.0 | 2026-11-11 | 依据 ADR-16 (v1.0.0) 同步多实现服务契约、接口库建模与重放 Mock 隔离契约（`F-02.1` / `F-04.4` / `F-05.1` / `SC-12` / `M20`）：1. 模块 CRUD 增加 `module_kind`（`BUSINESS \| CONTRACT_LIB`）及库模块禁绑进程/禁删守卫；2. 新增 `/api/arch/service-contracts` CRUD 与 `/api/arch/service-contracts/convert` 存量独占平滑转换接口；3. 服务供给 `POST /api/arch/modules/:id/services` 增加 `impl_key` 与 `A-09` 进程互斥 `409 CONTRACT_IMPL_PROCESS_CONFLICT` 响应，依赖 `POST /api/arch/modules/:id/dependencies` 增加 `contract_id` 分发依赖；4. 拓扑图 DTO 增加 `module_kind`、`implements`、`REALIZATION` 边及 `contract_id / impl_key`；5. 录制批次增加 `endpoint_snapshots`，一致性报告升级为八分类（新增 `IMPL_UNRESOLVED` 与 `impl_unresolved_count`），流量记录增加 `dst_contract_id`；6. 主从重放协议 `DownstreamEndpoint` 与 `MockRuleDownstream` 增加可选 `dst_tag` 向后兼容隔离字段 |

---

## 1. 全局路由、鉴权守卫、安全基线与通用响应契约

### 1.1 路由前缀与双模接入规范
- **独立运行路径**：`/api/<endpoint>`（前端开发 `:5181` 代理或单二进制 `:8091` 直连）。
- **门户嵌入路径**：`/traefik/api/<endpoint>`（由 `commonServer.Run(Options{ Prefix: "traefik" })` 自动剥离 `/traefik` 前缀，参见 [`01-system-architecture.md` §1.1](../02-design/01-system-architecture.md)）。
- **认证凭证传输**：所有受保护接口（含 `/api/events` SSE 实时流）统一通过 HTTP Header `Authorization: Bearer <JWT_TOKEN>` 传递凭证；**严禁在 URL Query 参数中传递 Token 或敏感凭据**。

### 1.2 RBAC 与公私域资源守卫标识符定义

| 守卫标识 | 中间件与数据层校验逻辑 | 拦截响应状态码 |
| :--- | :--- | :---: |
| `Public` | 免密开放路由（健康检查、登录配置与账密认证） | - |
| `JWT` | `commonAuth.AuthMiddleware`（強制校验 `HS256` 签名与 `exp` 声明，拒绝 `none` 算法） | 未携带或无效返 `401` |
| `AdminOnly` | `JWT` + `commonAuth.RequireAdmin("traefik_admin")`（仅 `traefik_admin` 或 `super_admin` 可访问） | 普通用户返 `403` |
| `ScopeReadGuard` | `JWT` + 公私域可见性过滤：`SHARED` 资源全员可读；`PRIVATE` 资源（Agent / 进程 / 录制素材）仅 `owner_id == currentUser.ID` 本人及 `traefik_admin` 可读 | 普通用户横向越权访问他人 `PRIVATE` 资源返 `403` |
| `ScopeWriteGuard` | `JWT` + 公私域写权限核验：`SHARED` 资源仅 `traefik_admin` 可写；`PRIVATE` 资源仅 `owner_id == currentUser.ID` 本人可修改/启停/租用（管理员可删除违规私有资源或强制释放异常租约，但不得代持他人私有 Agent 发起录制/重放） | 越权修改或租用返 `403` |
| `CIOrAdminGuard` | 支持 `AdminOnly`（管理员 `Authorization: Bearer <JWT>`）**或** 专用请求头 `X-Traefik-CI-Token: <TOKEN>`（服务端计算 SHA-256 并与 `traefik_settings.ci_gate_token_sha256` 执行 `subtle.ConstantTimeCompare` 常量时间比对，严禁 URL Query 传参、源码硬编码或日志打印明文，独立挂载滑动窗口速率限制） | 缺失/错误返 `401`；普通用户 JWT 返 `403`；超频返 `429` |

> **详细角色权限与审计快照策略**：参见 [`04-control-plane-and-web-console.md` §5.1 & §5.2](../02-design/04-control-plane-and-web-console.md) 及 [`05-database-schema.md` §5](../02-design/05-database-schema.md)。

### 1.3 标准分页、64 位整数编码与错误响应信封

1. **通用分页 Query 参数**：`page`（默认 `1`，`>=1`）、`page_size`（固定允许 `15 / 25 / 50 / 100`，默认 `25`）。
2. **标准分页响应 (`200 OK`)**：
   ```json
   {
     "items": [],
     "total": 42,
     "page": 1,
     "page_size": 25
   }
   ```
3. **无符号 64 位整数精度防腐**：所有 `ullSNO` (`sno`)、`ullStatusCode` (`status_code`) 及大整数 ID 在 JSON 响应中按 [`05-database-schema.md` §1.4](../02-design/05-database-schema.md) 序列化为十进制字符串（或标准 JSON Number），前端 TypeScript 严格禁止使用 `any`。
4. **标准错误、`409` 排他租约冲突与 `A-09` 实现进程互斥冲突响应**：
   - 常规错误（`400 / 401 / 403 / 404 / 409 / 500 / 503`）：`{"error": "具体的中文错误原因说明", "code": "ERROR_CODE"}`
   - Agent 排他租约争用冲突（`409 Conflict`，参见 [`01-system-architecture.md` §3.2](../02-design/01-system-architecture.md)）：
     ```json
     {
       "error": "目标 Agent [192.168.56.19:9091] 正被用户 [张三] 的录制任务占用",
       "code": "AGENT_LEASE_CONFLICT",
       "conflict_agent_id": 2,
       "lease_type": "RECORDING",
       "lease_ref_id": 108,
       "lease_holder_id": 15,
       "lease_holder_name": "张三",
       "lease_acquired_at": "2026-10-29T10:00:00Z",
       "lease_expires_at": "2026-10-29T10:32:00Z"
     }
     ```
   - 多实现契约同进程互斥冲突（`409 Conflict`，`A-09 / F-02.1`，参见 [`ADR-16`](../05-adr/16-multi-implementation-service-contract-adr.md) §3.4）：
     ```json
     {
       "error": "多实现契约 [pkg.IfaceSvc] 的实现模块不能部署在同一架构进程 [sut-biz-b] 中（已存在实现 impl_key='b' / 模块 MOD_B）",
       "code": "CONTRACT_IMPL_PROCESS_CONFLICT",
       "conflict_contract_id": 12,
       "conflict_contract_name": "pkg.IfaceSvc",
       "conflict_impl_key": "b",
       "conflict_module_id": 102,
       "conflict_module_code": "MOD_B",
       "arch_process_id": 7,
       "arch_process_code": "sut-biz-b"
     }
     ```

---

## 2. 控制面 REST / SSE 接口契约字典 (`code-traefik-server` 对外 `/api/*`)

### 2.1 基础探针、认证会话与平台治理域 (`SC-01`, `F-16`)

| 方法 | 路由路径 | 守卫要求 | Query / Body 核心参数 | 响应 JSON 结构与状态码 | `AuditAction` (`module:action`) |
| :---: | :--- | :---: | :--- | :--- | :--- |
| `GET` | `/api/health` | `Public` | 无 | `200`: `{"status":"healthy","service":"Code-Traefik","module_key":"traefik","version":"...","timestamp":"..."}` | - |
| `GET` | `/api/auth/config` | `Public` | 无 | `200`: `{"oauth2_enabled":bool,"password_login_enabled":bool,"dept_api_url":string}` | - |
| `POST` | `/api/login` | `Public` | Body: `{username, email?, password}` | `200`: `{"token":string,"user":{id,username,name,roles}}`<br>`400`: 参数缺失；`401`: 账密错误；`403`: 账号禁用或本地登录关闭 | `auth:login` |
| `GET` | `/api/oauth2/authorize` | `Public` | 无 | `302`: 跳转 SSO 授权页；`404`: OAuth2 未启用 | - |
| `GET` | `/api/oauth2/callback` | `Public` | Query: `code, state` | `302`: 签发 JWT 并重定向前端 | - |
| `GET` | `/api/me` | `JWT` | 无 | `200`: `{id, username, name, employee_id, department_id, department_name, roles, is_super_admin, is_module_admin, required_ad_role: "traefik_admin"}`<br>`401`: 未认证 | - |
| `PATCH` | `/api/password` | `JWT` | Body: `{old_password, new_password}`（长度 `8..128`） | `200`: `{"message":"密码修改成功"}`<br>`400`: 长度不合规；`401`: 原密码错误 | `auth:update_password` |
| `GET` | `/api/users` | `JWT` | Query: `search?, id?, page?, pageSize?` | `200`: `{"items":[{id,name,username,employee_id,department}],"total":int}`（脱敏邮箱与密码） | - |
| `GET` | `/api/admin/settings` | `AdminOnly` | 无 | `200`: `{"settings": []TraefikSetting, "ci_token_configured": bool}`（自动过滤隐藏 `ci_gate_token_sha256` 内部哈希项）<br>`401/403`: 权限不足 | - |
| `PUT` | `/api/admin/settings` | `AdminOnly` | Body: `{"items":[{setting_key, setting_val, description}]}`（`1..50` 项，禁止直接写保留键 `ci_gate_token_sha256`） | `200`: `{"message":"模块配置已保存","settings":[]}`<br>`400`: 键名格式非法；`401/403`: 权限不足 | `settings:update` |
| `POST` | `/api/admin/ci-token/rotate` | `AdminOnly` | Body: `{"description"?: string}`（由 `crypto/rand` 生成 32B 高熵机器 Token，仅存 SHA-256 哈希，明文仅本次响应返回一次） | `200`: `{"ci_token": "ctk_...", "rotated_at": "..."}`（强制响应头 `Cache-Control: no-store`）<br>`401/403`: 非管理员 | `settings:rotate_ci_token` |
| `DELETE` | `/api/admin/ci-token` | `AdminOnly` | 无（立即吊销清空 `ci_gate_token_sha256`） | `200`: `{"message":"CI 门禁凭据已吊销"}`<br>`401/403`: 非管理员 | `settings:revoke_ci_token` |

---

### 2.2 三级逻辑架构、架构物理进程蓝图、多实现服务契约与静态拓扑域 (`F-01`, `F-02`, `F-02.1`, `F-04`, `F-04.4`, `SC-02`, `SC-12`)

> 实体结构、树形算法、多实现契约建模（`module_kind`、`traefik_service_contracts`、`impl_key`、`A-09` 实现进程互斥、`DISPATCH` 分发依赖）与两层正交物理建模参见 [`03-architecture-elements-and-topology.md` §1~§4](../02-design/03-architecture-elements-and-topology.md)、[`05-database-schema.md` §3.1~§3.3](../02-design/05-database-schema.md) 及 [`ADR-16`](../05-adr/16-multi-implementation-service-contract-adr.md)。

| 方法 | 路由路径 | 守卫要求 | Query / Body 核心参数 | 响应 JSON 结构与状态码 | `AuditAction` (`module:action`) |
| :---: | :--- | :---: | :--- | :--- | :--- |
| `GET` | `/api/arch/subsystems` | `JWT` | Query: `search?, owner_id?, page?, page_size?` | `200`: 标准分页 `items: []Subsystem`（含 `owner` 简要信息） | - |
| `POST` | `/api/arch/subsystems` | `AdminOnly` | Body: `{code, name, owner_id?, description?}`（`code` 自动转大写，`^[A-Z0-9_-]{2,64}$`） | `201`: `Subsystem`<br>`400`: 编码/名称格式非法；`403`: 非管理员；`409`: `code` 全局已存在 | `traefik_arch:create` |
| `PUT` | `/api/arch/subsystems/:id` | `AdminOnly` | Body: `{code?, name, owner_id?, description?}` | `200`: `Subsystem`<br>`400/403/404`；`409`: 新 `code` 已被占用 | `traefik_arch:update` |
| `DELETE` | `/api/arch/subsystems/:id` | `AdminOnly` | Path: `id` | `200`: `{"message":"子系统已删除"}`<br>`403/404`；`409`: 存在下属功能组或模块禁止删除 | `traefik_arch:delete` |
| `GET` | `/api/arch/functional-groups/tree` | `JWT` | Query: `subsystem_id`（必填） | `200`: `{"subsystem_id":uint64,"tree":[]FunctionalGroupTreeNode}`（含各节点 `level`, `path`, `module_count`, `children`）<br>`400/404`: 子系统无效 | - |
| `POST` | `/api/arch/functional-groups` | `AdminOnly` | Body: `{subsystem_id, parent_id?, code, name, sort_order?, description?}` | `201`: `FunctionalGroup`（自动计算 `level` 与 `path="/<id1>/<id2>/"`）<br>`400`: 父节点跨子系统或结果 `level > 8`；`403/404`；`409`: 同子系统内 `code` 重复 | `traefik_arch:create` |
| `PUT` | `/api/arch/functional-groups/:id` | `AdminOnly` | Body: `{code?, name, sort_order?, description?}` | `200`: `FunctionalGroup`<br>`400/403/404`；`409`: 同子系统内 `code` 冲突 | `traefik_arch:update` |
| `PATCH` | `/api/arch/functional-groups/:id/move` | `AdminOnly` | Body: `{new_parent_id: uint64 \| null, sort_order?: int}`（执行 `MoveFunctionalGroup` 咨询锁级联更新） | `200`: `{"group":FunctionalGroup,"updated_descendants":int}`<br>`400`: 移至自身、移至子孙成环、跨子系统或子树总深 `> 8`；`403/404` | `traefik_arch:move` |
| `DELETE` | `/api/arch/functional-groups/:id` | `AdminOnly` | Path: `id` | `200`: `{"message":"功能组已删除"}`<br>`403/404`；`409`: 存在子功能组或挂载模块禁止删除 | `traefik_arch:delete` |
| `GET` | `/api/arch/modules` | `JWT` | Query: `subsystem_id?, group_id?, include_descendants?, module_kind?` (`BUSINESS \| CONTRACT_LIB`), `arch_process_id?, unassigned_only?, search?, page?, page_size?` | `200`: 标准分页 `items: []Module`（含 `module_kind`, `group`, `subsystem`, 归属标准架构进程 `arch_process` 及接口/契约计数；`unassigned_only=true` 时自动过滤 `module_kind = 'BUSINESS' AND arch_process_id IS NULL`，排除不可部署的 `CONTRACT_LIB`） | - |
| `GET` | `/api/arch/modules/:id` | `JWT` | Path: `id` | `200`: `{module: Module, services: []ModuleService, dependencies: []ModuleDependency, defined_contracts?: []ServiceContract, agent_mounts: []ProcessMountSummary}`（当 `module.module_kind == 'CONTRACT_LIB'` 时返回 `defined_contracts` 及各契约实现数与分发调用方数；`agent_mounts` 按 `ScopeReadGuard` 过滤可见挂载实例）<br>`404`: 模块不存在 | - |
| `POST` | `/api/arch/modules` | `AdminOnly` | Body: `{group_id, code, name, module_kind?: "BUSINESS"\|"CONTRACT_LIB" (默认 "BUSINESS"), owner_id?, arch_process_id?, description?}`（冗余写入所属 `subsystem_id`） | `201`: `Module`<br>`400`: 格式非法、`module_kind` 非白名单枚举，或 `module_kind == "CONTRACT_LIB"` 同时指定了非空 `arch_process_id`；`403/404`；`409`: `code` 全局重复 | `traefik_arch:create` |
| `PUT` | `/api/arch/modules/:id` | `AdminOnly` | Body: `{group_id, code?, name, module_kind?: "BUSINESS"\|"CONTRACT_LIB", owner_id?, arch_process_id?, description?}` | `200`: `Module`<br>`400`: `CONTRACT_LIB` 尝试绑定 `arch_process_id`；`403/404`；`409`: `code` 冲突、`CONTRACT_LIB` 下已登记 `ServiceContract` 时尝试改回 `BUSINESS`、业务模块已绑定进程或存在 `PROVIDE/DEPEND` 时尝试改为 `CONTRACT_LIB`，或修改 `arch_process_id` 触发 `A-09` `CONTRACT_IMPL_PROCESS_CONFLICT` | `traefik_arch:update` |
| `POST` | `/api/arch/modules/:id/process-binding` | `AdminOnly` | Body: `{arch_process_id: uint64 \| null}` | `200`: `Module` (绑定或解绑所属标准架构进程蓝图 `ArchProcess`，事务内执行 `AssertContractImplProcessDisjoint`)<br>`400`: 目标模块为 `CONTRACT_LIB` 接口库模块禁止绑定进程；`403/404`；`409`: 违反 `A-09` 同一多实现契约的两个实现模块落入同一 `ArchProcess`（返回 `code: "CONTRACT_IMPL_PROCESS_CONFLICT"` 及冲突契约名、`impl_key`、模块编码） | `traefik_arch:update` |
| `DELETE` | `/api/arch/modules/:id` | `AdminOnly` | Path: `id` | `200`: `{"message":"功能模块已删除"}`<br>`403/404`；`409`: 存在被其他 FM 依赖的服务、仍装载于 `ArchProcess`，或 `CONTRACT_LIB` 模块下仍存在已登记的 `ServiceContract` | `traefik_arch:delete` |
| `GET` | `/api/arch/service-contracts` | `JWT` | Query: `definer_module_id?, subsystem_id?, search?, page?, page_size?` | `200`: 标准分页 `items: []ServiceContract`（含 `definer_module`、`impl_count`、`dispatch_consumer_count`、`direct_consumer_count` 及实现的 `impl_keys` 摘要） | - |
| `GET` | `/api/arch/service-contracts/:id` | `JWT` | Path: `id` | `200`: `{contract: ServiceContract, definer_module: Module, implementations: []ContractImplMatrixItemDTO, dispatch_consumers: []DownstreamConsumer, direct_consumers: []DownstreamConsumer}`（`implementations` 含 `{service_id, service_name, is_custom_service_name, impl_key, module_id, module_code, module_name, subsystem_name, arch_process_id, arch_process_code, arch_process_name, owner_name, latest_verify_status}`）<br>`404`: 契约不存在 | - |
| `POST` | `/api/arch/service-contracts` | `AdminOnly` | Body: `{service_name, definer_module_id, impl_key_label?: string, description?: string}`（`service_name` 校验同 `A-07`；事务首行 `SELECT pg_advisory_xact_lock(hashtext(service_name))`） | `201`: `ServiceContract`<br>`400`: `service_name` 格式非法或 `definer_module_id` 并非 `module_kind == 'CONTRACT_LIB'` 的接口库模块；`403`: 非管理员；`404`: 定义方模块不存在；`409`: 该 `service_name` 已登记契约，或已被某业务模块作为独占/自定义运行期 `ModuleService` 供给（返回提示引导调用 `/api/arch/service-contracts/convert` 平滑转换） | `traefik_arch:create` |
| `PUT` | `/api/arch/service-contracts/:id` | `AdminOnly` | Path: `id`；Body: `{definer_module_id?: uint64, impl_key_label?: string, description?: string}`（`service_name` 创建后不可改） | `200`: `ServiceContract`<br>`400`: 新定义方非 `CONTRACT_LIB`；`403/404` | `traefik_arch:update` |
| `DELETE` | `/api/arch/service-contracts/:id` | `AdminOnly` | Path: `id` | `200`: `{"message":"多实现服务契约已删除"}`<br>`403/404`；`409`: 仍存在关联的实现行（`traefik_module_services.contract_id`）或分发依赖（`traefik_module_dependencies.contract_id`）禁止删除 | `traefik_arch:delete` |
| `POST` | `/api/arch/service-contracts/convert` | `AdminOnly` | Body: `{service_id: uint64, definer_module_id: uint64, first_impl_key: string, impl_key_label?: string, convert_dependencies_to_dispatch?: bool (默认 false), description?: string}`（见 [`ADR-16`](../05-adr/16-multi-implementation-service-contract-adr.md) §7.4：单事务内持有 `hashtext(service_name)` 咨询锁，将既有独占 `ModuleService` 升级为 `ServiceContract` 并将原供给行原地转为首个实现 `impl_key=first_impl_key`；可选将该服务的现有内部依赖批量转为 `DISPATCH` 分发依赖） | `200`: `{contract: ServiceContract, first_implementation: ModuleService, converted_dependencies: int}`<br>`400`: `definer_module_id` 非 `CONTRACT_LIB`、`first_impl_key` 格式非法（须匹配 `^[A-Za-z0-9._-]{1,64}$`）或目标 `service_id` 原本已是契约实现；`403/404`；`409`: 同名契约已存在 | `traefik_arch:convert_contract` |
| `POST` | `/api/arch/service-contracts/:id/attach` | `AdminOnly` | Path: `id`；Body: `{service_id: uint64, impl_key: string, convert_dependencies_to_dispatch?: bool}`（将存量独占服务如 `DH_SyncService` 无损归入既有契约 `SyncService` 的自定义命名实现，`is_custom_service_name=true`，保留原 `service_id` 与直连依赖不丢失） | `200`: `{contract: ServiceContract, implementation: ModuleService, converted_dependencies: int}`<br>`400`: `impl_key` 格式非法或服务已是契约实现；`403/404`；`409`: 同契约下 `impl_key` 或 `module_id` 已存在 | `traefik_arch:attach_contract_impl` |
| `GET` | `/api/arch/process-groups` | `JWT` | Query: `search?` | `200`: `{"items": []ArchProcessGroup}`（按 `sort_order, id` 排序，含 `process_count`） | - |
| `POST` | `/api/arch/process-groups` | `AdminOnly` | Body: `{code, name, sort_order?, description?}`（`code` 自动转大写，`^[A-Z0-9_-]{2,64}$`） | `201`: `ArchProcessGroup`<br>`400`: 格式非法；`403`: 非管理员；`409`: `code` 或 `name` 重复 | `traefik_arch:create` |
| `PUT` | `/api/arch/process-groups/:id` | `AdminOnly` | Body: `{code?, name, sort_order?, description?}` | `200`: `ArchProcessGroup`<br>`400/403/404`；`409`: `code` 或 `name` 冲突 | `traefik_arch:update` |
| `DELETE` | `/api/arch/process-groups/:id` | `AdminOnly` | Path: `id` | `200`: `{"message":"物理进程组已删除"}`<br>`403/404`；`409`: 组下仍存在标准架构进程禁止删除 | `traefik_arch:delete` |
| `GET` | `/api/arch/processes/tree` | `JWT` | Query: `search?` | `200`: `{"tree": []ArchProcessGroupDTO}`（完整 `ArchProcessGroup -> ArchProcess -> Modules` 树及挂载计数，供 `/architecture?tree=physical` 与 `AgentProcessMountDrawer` 单次拉取） | - |
| `GET` | `/api/arch/processes` | `JWT` | Query: `group_id?, module_id?, search?, page?, page_size?` | `200`: 标准分页 `items: []ArchProcess`（预加载 `group` 与 `modules`） | - |
| `GET` | `/api/arch/processes/:id` | `JWT` | Path: `id` | `200`: `{arch_process: ArchProcess, modules: []Module, provided_services: []ModuleService, dependencies: []ModuleDependency, agent_mounts: []Process}`（`agent_mounts` 强制经 `ScopeReadGuard` 过滤，普通用户绝不泄露他人 `PRIVATE` Agent 信息）<br>`404`: 不存在 | - |
| `POST` | `/api/arch/processes` | `AdminOnly` | Body: `{group_id, code, name, default_listen_port, default_cmd_pattern?, default_extra_downstreams?: string[], module_ids?: uint64[], sort_order?, description?}`（`default_listen_port` 范围 `0..65535`，`0` 为纯客户端；`default_cmd_pattern` `<=256` 字符，支持子串或 `re:` Go RE2 正则预校验） | `201`: `ArchProcess`<br>`400`: 端口越界、纯客户端缺少 `default_cmd_pattern`、`re:` 正则语法非法或 `module_ids` 含 `CONTRACT_LIB` 模块；`403/404`；`409`: `code` 重复、任一 `module_id` 已被分配给其他标准架构进程 (`A-03`)，或 `module_ids` 含同一多实现契约的多个实现模块（违反 `A-09`，返回 `CONTRACT_IMPL_PROCESS_CONFLICT`） | `traefik_arch:create` |
| `PUT` | `/api/arch/processes/:id` | `AdminOnly` | Body: `{group_id, code?, name, default_listen_port, default_cmd_pattern?, default_extra_downstreams?: string[], module_ids?: uint64[], sort_order?, description?}` | `200`: `{arch_process: ArchProcess, auto_pinned_overrides?: [{process_id, agent_id, pinned_port}]}`（单事务同步更新未设置 `override_listen_port` 挂载实例的 `effective_listen_port`；若新默认非零端口与某 Agent 同机其他进程端口冲突，**自动将该冲突挂载实例的 `override_listen_port` 固化为原有效端口**而不阻塞蓝图演进，并对空闲在线 Agent 递增 `mounts_revision` 异步刷新 `/monitors/arm`）<br>`400`: 含 `CONTRACT_LIB` 模块或参数非法；`403/404`；`409`: `code` 冲突、模块单一归属冲突 (`A-03`) 或违反 `A-09` (`CONTRACT_IMPL_PROCESS_CONFLICT`) | `traefik_arch:update` |
| `DELETE` | `/api/arch/processes/:id` | `AdminOnly` | Path: `id` | `200`: `{"message":"标准架构进程已删除"}`<br>`403/404`；`409`: 已被任何环境 Agent 挂载实例化（`traefik_processes` 存在引用）或被历史 `ReplayTask` 引用禁止删除 | `traefik_arch:delete` |
| `GET` | `/api/arch/modules/:id/services` | `JWT` | Path: `id` | `200`: `{"items": []ModuleService}`（含 `contract_id`, `impl_key`, `contract` 简要信息及被依赖下游模块摘要统计）<br>`404`: 模块不存在 | - |
| `POST` | `/api/arch/modules/:id/services` | `AdminOnly` | Body: `{service_name, contract_id?: uint64, impl_key?: string, diff_ignore_paths?: string[], description?}`（若指定 `contract_id`，支持自定义运行期 `service_name` 如 `DH_SyncService` 或留空/同契约名沿用共享名；若未指定 `contract_id`，事务首行按 `service_name` 匹配是否已登记为契约：匹配则自动绑定契约名实现，否则创建独占契约 `contract_id=nil`） | `201`: `ModuleService`<br>`400`: `service_name`/`impl_key` 不合法、目标模块为 `CONTRACT_LIB`、指定 `contract_id` 或匹配到多实现契约却缺合法 `impl_key`，或独占服务传非空 `impl_key`；`403/404`；`409`: ① 独占/自定义运行期 `service_name` 已被占用（修订 `A-05`）；② `(contract_id, impl_key)` 或 `(contract_id, module_id)` 重复；③ 同名共享实现违反 `A-09`（返回 `code: "CONTRACT_IMPL_PROCESS_CONFLICT"`；自定义名实现豁免） | `traefik_arch:create` |
| `PUT` | `/api/arch/module-services/:serviceId` | `AdminOnly` | Body: `{service_name?: string, impl_key?: string, diff_ignore_paths: string[], description?}`（多实现服务允许修改运行期 `service_name` 与 `impl_key` 并触发 `decoder.Registry` 别名重载；独占服务禁止修改 `service_name` 或传入非空 `impl_key`） | `200`: `ModuleService`<br>`400`: `service_name`/`impl_key` 格式非法；`403/404`；`409`: 新 `service_name` 全局冲突、同契约下新 `impl_key` 已被占用或切回共享契约名触发 `A-09` 冲突 | `traefik_arch:update` |
| `DELETE` | `/api/arch/module-services/:serviceId` | `AdminOnly` | Path: `serviceId` | `200`: `{"message":"供给服务契约已删除"}`<br>`403/404`；`409`: 仍有内部模块直接依赖此服务（`service_id`） | `traefik_arch:delete` |
| `GET` | `/api/arch/modules/:id/dependencies` | `JWT` | Path: `id` | `200`: `{"items": []ModuleDependency}`（展开关联提供方 FM / 标准架构进程，或展开多实现 `contract` 及其当前实现清单）<br>`404`: 模块不存在 | - |
| `POST` | `/api/arch/modules/:id/dependencies` | `AdminOnly` | Body: `{is_external: boolean, service_id?: uint64, contract_id?: uint64, external_system?: string, external_service?: string, description?: string}`（`service_id` 具体实现依赖、`contract_id` 多实现契约分发依赖 `DISPATCH`、`is_external=true` 外部依赖严格三选一） | `201`: `ModuleDependency`<br>`400`: 消费方为 `CONTRACT_LIB`、未满足三选一、自身循环依赖或必填项缺失；`403/404`；`409`: 重复依赖相同 `service_id` / `contract_id`，或同一消费方对同一多实现契约同时声明 `DISPATCH` 分发依赖与具体实现依赖（语义重复） | `traefik_arch:create` |
| `DELETE` | `/api/arch/module-dependencies/:depId` | `AdminOnly` | Path: `depId` | `200`: `{"message":"服务依赖关系已解除"}`<br>`403/404` | `traefik_arch:delete` |
| `GET` | `/api/arch/services/available` | `JWT` | - | `200`: `{"items": []ModuleService, "contracts": []ServiceContract}`（`items` 按子系统与 FM 分组列出全系统已声明的 `ModuleService`，`contracts` 列出全部已登记的多实现 `ServiceContract` 及实现数，供添加依赖弹窗切换「依赖接口（分发到全部实现）」与「依赖具体实现」） | - |
| `GET` | `/api/arch/topology/graph` | `JWT` | Query: `view` (`LOGICAL` \| `PHYSICAL`，默认 `LOGICAL`), `env_agent_id?` (`uint64`，物理视图可选叠加指定环境的实际挂载端点)，支持请求头 `If-None-Match` | `200`: `TopologyGraphDTO`（全系统分层复合拓扑图，含 `nodes: []GraphNode`, `edges: []GraphEdge`, `model_version: string`（全内容指纹，作为 `ETag`）, `structure_version: string`（仅 `(id, parent_id)` 树结构指纹，供布局过期判定，`F-04.3`）；`MODULE` 节点 `meta` 输出 `owner_id`、`module_kind` (`"BUSINESS"\|"CONTRACT_LIB"`)、`defined_contract_count`（库模块）与 `implements: []{contract_id, service_name, impl_key, definer_module_id}`（实现模块）；逻辑视角包含 `CONTRACT_LIB` 节点、独立 `Kind="REALIZATION"` 实现关系边（`实现模块 -> 库模块`）及展开后的分发调用边（携带 `contract_id, impl_key, definer_module_id` 供前端一键切换 `topo_contract_mode=AGG\|EXPAND`）；物理视角**不输出 `CONTRACT_LIB` 节点（也不入 `undeployed_container`）**，分发调用恒展开为真实进程间连线；支持 `Accept-Encoding: gzip`）<br>`304`: 架构模型未发生变更<br>`400`: `view` 参数非法；`403`: 对 `env_agent_id` 无 `ScopeReadGuard` 可见权 | - |
| `GET` | `/api/arch/topology/layouts` | `JWT` | Query: `view` (`LOGICAL` \| `PHYSICAL`，必填) | `200`: `{canonical_layout: TopologyLayoutDetail \| null, personal_views: []TopologyLayoutSummary}`（一次性返回该 `view` 的团队基准布局完整详情与**当前登录用户本人**的个人书签摘要列表（不含 `node_positions` / `expand_state` / `filter_state` / `viewport`），按 `is_default DESC, updated_at DESC` 排序；管理员同样只返回本人书签）<br>`400`: `view` 非法 | - |
| `GET` | `/api/arch/topology/layouts/personal/:id` | `JWT` + 本人 | Path: `id` | `200`: `TopologyLayoutDetail`（仅 `owner_id == currentUser.ID`）<br>`403`: 读取他人书签（Anti-IDOR）；`404`: 不存在或目标为 `SHARED` 记录 | - |
| `PUT` | `/api/arch/topology/layouts/canonical` | `AdminOnly` | Body: `{view: "LOGICAL"\|"PHYSICAL", expected_revision: int (首次创建为 0), structure_version: string, node_positions: Record<string, SavedNodePosition>, expand_state?: SavedExpandState, viewport?: SavedViewport, name?: string, description?: string}`（请求体 `<= 1MB`；`node_positions` 为前端合并后的**完整**坐标） | `200`: `TopologyLayoutDetail`（按 `view` Upsert 团队基准布局，`revision + 1`，记录 `updated_by` 并写入摘要审计）<br>`400`: 参数或载荷校验失败（见 §2.2.1）；`403`: 非管理员；`409`: `LAYOUT_REVISION_CONFLICT`（`expected_revision` 过期或并发首次创建）；`413`: `PAYLOAD_TOO_LARGE` | `traefik_arch:publish_canonical_layout` |
| `DELETE` | `/api/arch/topology/layouts/canonical` | `AdminOnly` | Query: `view` (`LOGICAL` \| `PHYSICAL`) | `200`: `{"message":"团队基准布局已重置"}`<br>`400`: 参数非法；`403`: 非管理员；`404`: 该视图尚未发布团队基准布局 | `traefik_arch:reset_canonical_layout` |
| `POST` | `/api/arch/topology/layouts/personal` | `JWT` | Body: `{name: string, view: "LOGICAL"\|"PHYSICAL", focus_scope?: string (默认 "GLOBAL"), is_default?: bool, structure_version: string, node_positions: Record<string, SavedNodePosition> (相对团队基准的差量), expand_state?: SavedExpandState, filter_state?: SavedFilterState, viewport?: SavedViewport, description?: string}`（请求体 `<= 1MB`） | `201`: `TopologyLayoutDetail`（服务端强制 `scope="PRIVATE", owner_id=currentUser.ID`，忽略 Body 中的 `scope` / `owner_id` / `revision`；事务内咨询锁串行化配额计数；`is_default=true` 时同事务将同 `(view, focus_scope)` 其余书签置 `false`）<br>`400`: 参数校验失败或个人书签已达上限 `20` 个（`PERSONAL_VIEW_QUOTA_EXCEEDED`）；`409`: 同 `view` 下书签 `name` 重复；`413`: `PAYLOAD_TOO_LARGE` | `traefik_arch:create_personal_layout` |
| `PUT` | `/api/arch/topology/layouts/personal/:id` | `JWT` + 本人 | Path: `id`；Body: `{name?: string, focus_scope?: string, is_default?: bool, structure_version?: string, node_positions?: Record<string, SavedNodePosition>, expand_state?: SavedExpandState, filter_state?: SavedFilterState, viewport?: SavedViewport, description?: string}`（`view` 不可修改） | `200`: `TopologyLayoutDetail`（仅 `owner_id == currentUser.ID` 本人可更新，`revision + 1`）<br>`400`: 参数校验失败；`403`: 修改他人书签（Anti-IDOR）；`404`: 不存在或目标为 `SHARED` 记录；`409`: 新 `name` 重名；`413`: `PAYLOAD_TOO_LARGE` | `traefik_arch:update_personal_layout` |
| `DELETE` | `/api/arch/topology/layouts/personal/:id` | `JWT` + 本人或 `traefik_admin` | Path: `id` | `200`: `{"message":"个人视图书签已删除"}`（本人或 `traefik_admin` 可删除）<br>`403`: 越权删除他人书签；`404`: 不存在或目标为 `SHARED` 记录 | `traefik_arch:delete_personal_layout` |
| `GET` | `/api/arch/topology/static` | `JWT` | *(已废弃/向后兼容)* Query: `view`, `subsystem_id?` | `200`: `{"view":string,"nodes":[],"edges":[]}`（旧版单子系统扁平拓扑，建议迁移至 `/api/arch/topology/graph`） | - |

#### 2.2.1 拓扑布局 JSON 载荷 Schema 与校验规则 (`F-04.3`, [`ADR-15`](../05-adr/15-scalable-topology-focus-and-layout-persistence-adr.md) §6)

| 类型 | 字段定义 | 校验规则 |
| :--- | :--- | :--- |
| `SavedNodePosition` | `{x: number, y: number, pinned?: boolean}` | `x / y` 必须为有限实数（拦截 `NaN / ±Inf`）；坐标为相对 `parentId` 父容器的局部坐标 |
| `node_positions` | `Record<nodeId, SavedNodePosition>` | 条目数 `<= 5000`；键非空且长度 `<= 128` |
| `SavedExpandState` | `{level: int, overrides?: Record<nodeId, boolean>}` | `level ∈ [0, 16]`（相对聚焦根深度档位）；`overrides` 条目数 `<= 5000` |
| `SavedFilterState` | `{subsystem_ids?: string[], only_my_scope?: boolean, status?: "ALL" \| "ABNORMAL" \| "VIOLATION_ONLY", ego_node_id?: string, ego_hops?: 1 \| 2, hide_isolated?: boolean, hub_fold?: boolean}` | `subsystem_ids` 条目数 `<= 100`；枚举值严格白名单 |
| `SavedViewport` | `{x: number, y: number, zoom: number}` | 有限实数；`zoom ∈ [0.15, 2.0]` |
| `focus_scope` | `string` | 缺省规范化为 `"GLOBAL"`；必须匹配 `^(GLOBAL\|(sub\|grp\|pgrp\|proc)_[0-9]+\|ext_container\|undeployed_container)$`；`SHARED` 记录恒为 `"GLOBAL"` |
| `TopologyLayoutSummary` | `{id, scope, view, name, description, focus_scope, is_default, structure_version, revision, updated_at}` | 列表接口使用，不含任何大 JSON 载荷 |
| `TopologyLayoutDetail` | `TopologyLayoutSummary` + `{schema_version, node_positions, expand_state, filter_state, viewport, created_by, updated_by, created_at}` | 详情接口使用 |

- **请求体上限**：全部布局写接口经 `http.MaxBytesReader(1MB)`，超限返回 `413 {"error": "...", "code": "PAYLOAD_TOO_LARGE"}`；
- **防批量赋值**：Handler 仅绑定专用请求 DTO，`scope`、`owner_id`、`revision`、`created_by`、`updated_by` 一律由服务端决定；
- **审计**：全部写操作接入 `commonAudit`，仅记录摘要（`view`、`focus_scope`、`name`、坐标条目数、`structure_version`、`revision`），**不得**将 `node_positions` 等大 JSON 写入字段级 Diff；
- **前端降级**：深链 `topo_layout_id` 收到 `403 / 404` 时降级至团队基准并提示，不视为错误中断。

---

### 2.3 Protobuf 描述集 (`.pb`) 制品台账、依赖闭包与契约目录域 (`F-03`, `F-03.1`~`F-03.7`, `ADR-10`, `SC-02`)

> 跨集去重拓扑闭包解析、`CompileTrialPool` 预编译仲裁、停用反向依赖门禁与服务级聚合规则参见 [`04-control-plane-and-web-console.md` §2.1~§2.4 & §4.4](../02-design/04-control-plane-and-web-console.md)、[`05-database-schema.md` §3.4](../02-design/05-database-schema.md) 及 [`ADR-10`](../05-adr/10-enhanced-proto-schema-file-management-adr.md)。

| 方法 | 路由路径 | 守卫要求 | Query / Body 核心参数 | 响应 JSON 结构与状态码 | `AuditAction` (`module:action`) |
| :---: | :--- | :---: | :--- | :--- | :--- |
| `GET` | `/api/proto-schemas` | `JWT` | Query: `latest_only?` (`bool`，`true` 时按 `name` 折叠聚合返回生效版本或最新版本), `name?`, `category?` (`BASE_LIB` \| `BUSINESS` \| `GATEWAY`), `active_only?`, `search?` (参数化匹配 `name/version_tag/file_name/proto_files/service_name`), `sort_by?` (白名单: `updated_at\|created_at\|name\|file_size\|operation_count`), `sort_dir?` (`asc\|desc`), `page?, page_size?` | `200`: 标准分页 `items: []ProtoSchemaLedgerItemDTO`（永不透出 `descriptor_bytes`；服务端 `O(1)` 批量富化 `proto_file_count, service_count, claimed_service_count, claimed_ratio, version_count, latest_version, active_version, created_by_name, resolved_dependencies`）<br>`400`: 非法 `category` 或非白名单 `sort_by` | - |
| `GET` | `/api/proto-schemas/dependencies-graph` | `JWT` | Query: `active_only?` (`bool`，默认 `true`) | `200`: `{pool_summary: {active_schema_count, active_base_lib_count, active_service_count, active_operation_count, claimed_service_count, orphan_service_count, claimed_ratio}, nodes: []ProtoSchemaSummaryNode, edges: [{from_schema_id, to_schema_id, matched_protos: string[]}]}` | - |
| `GET` | `/api/proto-schemas/:id/download` | `JWT` | Path: `id` | `200`: `application/octet-stream` 二进制流（原始无损 `.pb`，强制响应头 `Content-Disposition: attachment; filename="<sanitized>"`, `X-Content-Type-Options: nosniff`, `Cache-Control: no-store`）<br>`404`: 描述集不存在 | - |
| `GET` | `/api/proto-schemas/:id/summary` | `JWT` | Path: `id` | `200`: `{schema: ProtoSchemaLedgerItemDTO, proto_files: string[], external_imports: string[], resolved_dependencies: []ResolvedSchemaDependencyDTO, dependents: []ResolvedSchemaDependencyDTO, versions: []ProtoSchema, services_summary: []ServiceSummaryItem, claimed_ratio: number}`<br>`404`: 描述集不存在 | - |
| `POST` | `/api/proto-schemas` | `AdminOnly` | `multipart/form-data`: `file` (必填 `.pb/.bin/.desc/.protoset` 二进制 `<=16MB`), `name`, `category?` (`BASE_LIB\|BUSINESS\|GATEWAY`，默认 `BUSINESS`), `version_tag?`, `description?`, `auto_activate?` (`bool`), `allow_breaking?` (`bool`), `breaking_reason?` (`string`) | `201`: `{schema: ProtoSchema, operations: []ProtoOperation, inherited_ignore_op_count: int, breaking_warnings?: []BreakingRuleViolation}`（咨询锁递增 `version`，仅提取自有文件 `Operation`，支持 `0` Service `BASE_LIB`，同名 `version>1` 自动向前继承 `diff_ignore_paths`）<br>`400`: 文件 `>16MB`、扩展名非法、`FileDescriptorSet` 损坏、缺失外部依赖（`missing_imports`）或 `allow_breaking=true` 但 `breaking_reason` `<8` 字符；`403`: 非管理员；`409`: `auto_activate=true` 时方法冲突（`conflicts`）、前置依赖未激活（`broken_dependents`）或检出未豁免 AST 破坏性变更（`BREAKING_CHANGE_DETECTED`：返回 `breaking_rules` + `affected_dependents`，F-26） | `traefik_schema:upload` |
| `PATCH` | `/api/proto-schemas/:id` | `AdminOnly` | Path: `id`；Body: `{category?: "BASE_LIB"\|"BUSINESS"\|"GATEWAY", version_tag?: string, description?: string}` | `200`: `ProtoSchema`（更新非二进制治理元数据）<br>`400`: `category` 枚举非法；`403`: 非管理员；`404`: 不存在 | `traefik_schema:update` |
| `POST` | `/api/proto-schemas/:id/activate` | `AdminOnly` | Path: `id`；Body (可选): `{allow_breaking?: bool, breaking_reason?: string}`（咨询锁事务内执行：**① F-26 同名生效版 AST 破坏性比对 ➔ ② 方法唯一性校验 ➔ ③ `CompileTrialPool` 联合预编译干跑**，原子替换同名旧版并重载 `CompiledRegistry`） | `200`: `{schema: ProtoSchema, activated_operations: int, forced_breaking?: bool}`（若 `allow_breaking=true` 放行则强制写入 `P1` 级高危审计）<br>`400`: `allow_breaking=true` 但未填合法 `breaking_reason`；`403/404`；`409`: 检出 AST 破坏性改动（`code: "BREAKING_CHANGE_DETECTED"`, `breaking_rules`, `affected_dependents`）、与其他生效描述集存在 `(service_name, operation)` 冲突（`conflicts`）或破坏下游依赖闭包（`broken_dependents`） | `traefik_schema:activate` |
| `POST` | `/api/proto-schemas/:id/deactivate` | `AdminOnly` | Path: `id`（将当前生效描述集停用移出全局生效池；前置通过 `CompileTrialPool` 执行反向依赖保护校验） | `200`: `{schema: ProtoSchema, deactivated_operations: int}`<br>`403/404`；`409`: 该版本本来未生效，或当前正被其他已生效描述集依赖而不可停用（返回 `broken_dependents: []BrokenDependentItemDTO`） | `traefik_schema:deactivate` |
| `POST` | `/api/proto-schemas/batch-activate` | `AdminOnly` | Body: `{"schema_ids": uint64[], "allow_breaking"?: bool, "breaking_reason"?: string}`（`1..100` 个 ID，支持基础库与多个业务包一次性联合 AST 防腐校验并原子激活入池） | `200`: `{activated_schemas: []ProtoSchema, total_active_operations: int, forced_breaking?: bool}`<br>`400`: 列表为空、超 `100` 个、包含同名多版本或 `allow_breaking=true` 缺少 `breaking_reason`；`403`: 非管理员；`404`: 含不存在 ID；`409`: 候选生效池存在 AST 破坏性改动（`breaking_rules` + `affected_dependents`）、方法冲突（`conflicts`）或依赖闭包不自洽（`broken_dependents`），全批次原子回滚 | `traefik_schema:batch_activate` |
| `DELETE` | `/api/proto-schemas/:id` | `AdminOnly` | Path: `id` | `200`: `{"message":"描述集版本已删除"}`<br>`403/404`；`409`: 当前处于 `is_active = true` 生效态禁止直接删除（须先调用 `deactivate` 或切换版本） | `traefik_schema:delete` |
| `GET` | `/api/proto-services` | `JWT` | Query: `schema_id?`, `active_only?` (默认 `true`), `category?`, `claimed_status?` (`ALL\|CLAIMED\|UNCLAIMED`), `service_name?`, `search?` (跨层匹配 `service_name/proto_file/operation/req_msg/rsp_msg`), `page?, page_size?` | `200`: 标准分页 `items: []ServiceContractGroupDTO`（按 `service_name` 粒度分页，保证同一服务下全部 `operations` 完整不跨页割裂，附带 `sourceSchema` 与 `claimedByModule`） | - |
| `GET` | `/api/proto-operations` | `JWT` | *(保留兼容)* Query: `schema_id?, active_only?, service_name?, operation?, search?, page?, page_size?` | `200`: 标准分页 `items: []ProtoOperation`（扁平方法列表查询） | - |
| `GET` | `/api/proto-operations/:id/schema-tree` | `JWT` | Path: `id` | `200`: `OperationSchemaTreeDTO`（基于联合类型池或历史版本动态回退闭包反射的完整 `reqMessage` 与 `rspMessage` 字段字典树、类型、Tag 及注释）<br>`404`: 方法不存在或对应 Message 定义丢失 | - |
| `PUT` | `/api/proto-operations/:id/ignore-paths` | `AdminOnly` | Path: `id`；Body: `{"diff_ignore_paths": string[]}` | `200`: `ProtoOperation`（更新方法级专属回归对比忽略字段清单并重载注册表）<br>`400`: 路径格式非法；`403`: 非管理员；`404`: 方法不存在 | `traefik_schema:update_op_ignores` |

---

### 2.4 多环境公私域 Agent 纳管、排他租约、1:1 架构进程挂载与 eBPF 预挂待命域 (`F-02`, `F-06`, `F-08`, `F-09`, `F-15.5`, `SC-03`)

> 租约状态机、SSRF 防护、`1 台 Agent = 1 套独立环境` 的 `1:1` 架构进程挂载继承以及 eBPF 预挂待命与三态代理（`PASSTHROUGH / CAPTURING / REPLAY_MOCK`）机制参见 [`01-system-architecture.md` §3 & §4](../02-design/01-system-architecture.md)、[`03-architecture-elements-and-topology.md` §3](../02-design/03-architecture-elements-and-topology.md) 及 [`05-database-schema.md` §3.3](../02-design/05-database-schema.md)。

| 方法 | 路由路径 | 守卫要求 | Query / Body 核心参数 | 响应 JSON 结构与状态码 | `AuditAction` (`module:action`) |
| :---: | :--- | :---: | :--- | :--- | :--- |
| `GET` | `/api/agents` | `ScopeReadGuard` | Query: `scope?` (`SHARED` \| `PRIVATE`), `status?` (`ONLINE` \| `OFFLINE`), `lease_state?` (`IDLE` \| `LEASED`), `search?`, `page?`, `page_size?` | `200`: 标准分页 `items: []Agent`（含 `fallback_agent_id`、`mounts_revision`、`expected_armed_digest`、`armed_digest` 与挂载七态就绪摘要；普通用户仅返回 `SHARED` 与本人 `PRIVATE`；`traefik_admin` 可穿透查看全量） | - |
| `GET` | `/api/agents/:id` | `ScopeReadGuard` | Path: `id` | `200`: `{agent: Agent, processes: []Process}`（按 `effective_listen_port ASC, id ASC` 排序，完整预加载 `arch_process.group`、`arch_process.modules` 并调用 `FillFromArchProcess()` 填充 `code/name/group_name/listen_port/cmd_pattern/has_override` 及统一七态就绪快照列；数据库查询失败显式返 `500`）<br>`403`: 普通用户访问他人 `PRIVATE` Agent；`404`: 不存在 | - |
| `POST` | `/api/agents` | `ScopeWriteGuard` | Body: `{name, endpoint, scope: "SHARED"\|"PRIVATE", fallback_agent_id?: uint64\|null, claim_code?, description?}` | `201`: `Agent`（同步完成首次 `/agent/v1/handshake` 能力与版本校验）<br>`400`: SSRF 拦截（回环/云元数据/非法端口）、`fallback_agent_id` 指向非 `SHARED` Agent 或主版本 `!= "1.0"`；`403`: 普通用户尝试创建 `SHARED` Agent 或 `PRIVATE` `claim_code` 校验不匹配；`409`: `name` 或 `endpoint` 重复 | `traefik_agent:create` |
| `PUT` | `/api/agents/:id` | `ScopeWriteGuard` | Body: `{name, endpoint, fallback_agent_id?: uint64\|null, claim_code?, description?}` | `200`: `Agent`（修改 `fallback_agent_id` 后自动重算下游规格并刷新预挂摘要）<br>`400`: `fallback_agent_id` 指向自身或 `PRIVATE` Agent；`403/404`；`409`: 名称/地址冲突或当前持有活跃租约禁止修改端点 | `traefik_agent:update` |
| `DELETE` | `/api/agents/:id` | `ScopeWriteGuard` | Path: `id` | `200`: `{"message":"Agent 已移除"}`<br>`403/404`；`409`: 仍挂载 `Process` 或持有活跃租约 (`lease_type != ''`) | `traefik_agent:delete` |
| `POST` | `/api/agents/:id/handshake` | `ScopeReadGuard` | Path: `id`（立即触发探活、双向租约对账及 Single-Flight `ArmedDigest` 自动补挂对账） | `200`: `{agent: Agent, handshake: HandshakeResp}`<br>`403/404`；`503`: Agent 离线或连接超时 | - |
| `GET` | `/api/agents/:id/discover-processes` | `ScopeReadGuard` | Path: `id`（`SHARED` Agent 仅 `AdminOnly` 可调用；`PRIVATE` 仅本人或管理员可调用，供 `AgentProcessMountDrawer` 扫描匹配宿主机 `/proc` 进程） | `200`: `{"items": []DiscoveredProcess}`（含 `pid, listen_port, cmdline, exe_name`）<br>`403/404/503` | - |
| `PUT` | `/api/agents/:id/process-mounts` | `ScopeWriteGuard` | Path: `id`；Body: `{expected_revision?: uint64, mounts: [{arch_process_id: uint64, probe_desired_state?: "ARMED"\|"DISARMED", override_listen_port?: uint16\|null, override_cmd_pattern?: string\|null, advertise_ip?: string, extra_downstreams?: string[], description?: string}]}`（批量全量同步该环境 Agent 的 `1:1` 架构进程挂载清单；支持多套 `SHARED` 环境各自挂载同一 `ArchProcess`） | `200`: `{items: []Process, mounts_revision: uint64, expected_armed_digest: string, armed_digest: string, diagnostics: []ProcessDiagStatus}`（单事务校验 `expected_revision` 乐观锁、`UNIQUE(agent_id, arch_process_id)` 与同机非零 `effective_listen_port` 互斥，递增 `mounts_revision`；新挂载项缺省按 `TraefikSetting` 默认预挂策略初始化；当 Agent `ONLINE` 且 `IDLE` 时 Single-Flight 下发 `/agent/v1/monitors/arm` 完成预挂待命并回写七态快照）<br>`400`: 端口越界、`re:` 正则非法或列表含重复 `arch_process_id`；`403`: 无写权限；`404`: Agent/`ArchProcess` 不存在；`409`: `expected_revision` 版本过期（`MOUNT_REVISION_CONFLICT`）、同机非零端口冲突或所属 Agent 处于活跃租约中 | `traefik_agent:sync_mounts` |
| `POST` | `/api/agents/:id/probes/diagnose` | `ScopeReadGuard` | Path: `id`（立即向在线 Agent 请求已挂载架构进程的 PID/存量长连接就绪体检，刷新 DB 快照并推送 `agent.probes_updated`） | `200`: `{armed_digest: string, diagnostics: []ProcessDiagStatus}`<br>`403/404`；`503`: Agent 离线 | - |
| `POST` | `/api/agents/:id/probes/arm-all` | `ScopeWriteGuard` | Path: `id`；Body: `{group_id?: uint64}`（整机或按 `ArchProcessGroup` 批量设为 `ARMED`，重置退避并同步 Agent 预挂） | `200`: `{mounts_revision: uint64, armed_digest: string, diagnostics: []ProcessDiagStatus}`<br>`403/404`；`409`: Agent 当前持有活跃录制/重放租约 | `traefik_agent:toggle_probe` |
| `POST` | `/api/agents/:id/probes/disarm-all` | `ScopeWriteGuard` | Path: `id`；Body: `{group_id?: uint64, force?: bool}`（整机或按 `ArchProcessGroup` 批量设为 `DISARMED` 手动卸挂；`force=true` 允许当前持锁人或管理员紧急中止活跃任务并立即卸挂） | `200`: `{mounts_revision: uint64, armed_digest: string, diagnostics: []ProcessDiagStatus}`<br>`403`: 无权限或非持锁人/管理员尝试 `force`；`404`；`409`: Agent 持有活跃租约且未显式指定 `force=true` | `traefik_agent:toggle_probe` |
| `POST` | `/api/agents/:id/lease/force-unlock` | `AdminOnly` | Path: `id`；Body: `{reason: string}`（强制清空 DB 租约、终止关联未决任务并调用 Agent `/agent/v1/purge`；随后下一次心跳将按 `probe_desired_state` 自动恢复 `ARMED` 待命） | `200`: `{"message":"排他租约已强制释放","agent":Agent}`<br>`400`: `reason` 为空；`403`: 非管理员；`404`: 不存在 | `traefik_agent:force_unlock_lease` |
| `GET` | `/api/processes` | `ScopeReadGuard` | Query: `agent_id?, scope?, arch_process_id?, group_id?, module_id?, probe_status?, search?, page?, page_size?` | `200`: 标准分页 `items: []Process`（预加载 `agent`、`arch_process.group`、`arch_process.modules` 及有效端口/命令/七态预挂就绪状态） | - |
| `POST` | `/api/processes` | `ScopeWriteGuard` | Body: `{agent_id, arch_process_id, probe_desired_state?: "ARMED"\|"DISARMED", override_listen_port?: uint16\|null, override_cmd_pattern?: string\|null, advertise_ip?, extra_downstreams?: string[], description?}`（单条挂载架构标准进程，支持多套 `SHARED` 环境各自 `1:1` 挂载） | `201`: `Process`（递增 `mounts_revision`，在线空闲 Agent 自动触发 `/agent/v1/monitors/arm` 预挂）<br>`400`: 端口越界或 `re:` 正则非法；`403`: 无写权限；`404`: Agent/`ArchProcess` 不存在；`409`: 该 Agent 同机已挂载同一 `arch_process_id`（`1:1` 唯一约束）、同机非零 `effective_listen_port` 冲突或所属 Agent 处于活跃租约中 | `traefik_agent:create` |
| `PUT` | `/api/processes/:id` | `ScopeWriteGuard` | Body: `{override_listen_port?: uint16\|null, override_cmd_pattern?: string\|null, advertise_ip?, extra_downstreams?: string[], description?}`（更新本机差异化覆盖项，传 `null` 恢复继承 `ArchProcess` 默认值） | `200`: `Process`（重算 `effective_listen_port`、递增 `mounts_revision` 并在空闲在线态自动刷新 `/agent/v1/monitors/arm`）<br>`400/403/404`；`409`: 同机非零有效端口冲突或所属 Agent 处于活跃租约中 | `traefik_agent:update` |
| `POST` | `/api/processes/:id/arm` | `ScopeWriteGuard` | Path: `id`（将单挂载实例设为 `probe_desired_state = "ARMED"` 并同步 Agent 预挂） | `200`: `{process: Process, diagnostic?: ProcessDiagStatus}`<br>`403/404`；`409`: 所属 Agent 持有活跃租约 | `traefik_agent:toggle_probe` |
| `POST` | `/api/processes/:id/disarm` | `ScopeWriteGuard` | Path: `id`；Body: `{force?: bool}`（将单挂载实例设为 `probe_desired_state = "DISARMED"`；若该进程未参与当前活跃会话可直接卸挂，若正参与活跃作业则支持持锁人或管理员传 `force=true` 紧急卸挂） | `200`: `{process: Process}`<br>`403/404`；`409`: 该进程正参与活跃录制/重放且未传 `force=true` | `traefik_agent:toggle_probe` |
| `DELETE` | `/api/processes/:id` | `ScopeWriteGuard` | Path: `id`（解除该环境 Agent 对标准架构进程的挂载并同步卸挂探针；历史会话由 `process_snapshots` 快照保底，历史 `ReplayTask` 外键自动 `SET NULL`） | `200`: `{"message":"进程挂载已移除"}`<br>`403/404`；`409`: 所属 Agent 当前持有涉及该进程的活跃排他租约 | `traefik_agent:delete` |

---

### 2.5 多进程协同录制、架构一致性八分类验证、事务调用树与流量解剖域 (`F-05`, `F-05.1`, `F-10`, `F-11`, `F-15.2`, `F-15.4`, `F-15.6`, `SC-04`~`SC-06`, `SC-12`)

> 录制编排、`endpoint_snapshots` 端点字典固化、发起序 `SeqNo`、DOTR 调用树组装、跨跳缝合、参数/实体血缘与四段解剖 DTO 参见 [`02-data-plane-and-cros-rpc-engine.md` §2](../02-design/02-data-plane-and-cros-rpc-engine.md)、[`03-architecture-elements-and-topology.md` §5](../02-design/03-architecture-elements-and-topology.md) 及 [`04-control-plane-and-web-console.md` §1.2, §1.5, §4.3 & §4.6](../02-design/04-control-plane-and-web-console.md)。

| 方法 | 路由路径 | 守卫要求 | Query / Body 核心参数 | 响应 JSON 结构与状态码 | `AuditAction` (`module:action`) |
| :---: | :--- | :---: | :--- | :--- | :--- |
| `GET` | `/api/recording-sessions` | `ScopeReadGuard` | Query: `scope?` (`SHARED` \| `PRIVATE`), `status?`, `process_id?`, `search?`, `page?, page_size?` | `200`: 标准分页 `items: []RecordingSession` | - |
| `GET` | `/api/recording-sessions/:id` | `ScopeReadGuard` | Path: `id` | `200`: `RecordingSession`（含 `process_snapshots`、`endpoint_snapshots`、`process_diagnostics` 与 `leased_agent_ids`）<br>`403`: 访问他人 `PRIVATE` 批次；`404`: 不存在 | - |
| `POST` | `/api/recording-sessions` | `JWT` | Body: `{name, scope: "SHARED"\|"PRIVATE", target_process_ids: uint64[], max_duration_sec?: int, max_frames?: int, max_frame_bytes?: int, description?}` | `201`: `RecordingSession`（固化 `process_snapshots` 蓝图快照与 `endpoint_snapshots: []EndpointSnapshotItem` 确定性「网络端点 ➔ `ArchProcess`」字典快照，原子升序锁定涉及的全部 Agent `RECORDING` 排他租约，下发 `/monitors/sync`：实时复检 `/proc` 后按序开启 `EgressProxy` `CAPTURING` 分帧与按需入站读写钩子，已 `ARMED` 进程 `0ms` 零重启开录，`DISARMED` 进程自动兜底临时挂载，并返回最新连接诊断 `need_reconnect`）<br>`400`: 进程列表为空或 Agent 离线/无 eBPF 权限；`403`: 含他人 `PRIVATE` Agent 进程；`409`: 任一目标 Agent 已被其他任务租用（返回持锁人详情） | `traefik_record:start_session` |
| `POST` | `/api/recording-sessions/:id/stop` | `ScopeWriteGuard` | Path: `id`（发起人本人或 `traefik_admin` 可调用） | `200`: `RecordingSession`（下发 `/monitors/:sessionId/stop`：先卸载入站读写钩子 -> `200ms` 在途应答排空 -> 将 `EgressProxy` 拨回 `PASSTHROUGH` 纯透传待命且**不断开既有业务 TCP 长连接** -> 等待 `EVENT_SESSION_DRAINED`、执行离线归属对账（含多实现契约按 `endpoint_snapshots` 目的进程消歧与 `dst_contract_id` 回填）并释放全部 Agent 排他租约）<br>`403/404`；`409`: 批次已处于终态 | `traefik_record:stop_session` |
| `DELETE` | `/api/recording-sessions/:id/processes/:processId` | `ScopeWriteGuard` | Path: `id, processId`（录制中单独热移除某进程采集，F-08.4） | `200`: `{"message":"进程监控已移除","session":RecordingSession}`<br>`403/404/409` | `traefik_record:stop_session` |
| `PATCH` | `/api/recording-sessions/:id/publish` | `ScopeWriteGuard` | Path: `id`（将本人合格的 `PRIVATE` 录制批次发布为团队 `SHARED` 素材） | `200`: `RecordingSession`（`scope` 更新为 `"SHARED"`）<br>`400`: 批次未完成；`403/404` | `traefik_record:publish_session` |
| `DELETE` | `/api/recording-sessions/:id` | `ScopeWriteGuard` | Path: `id`（`PRIVATE` 创建人或管理员可删；`SHARED` 仅管理员可删） | `200`: `{"message":"录制批次已删除"}`<br>`403/404`；`409`: 正在录制中或仍被 `ReplayTask` 引用 | `traefik_record:delete_session` |
| `GET` | `/api/recording-sessions/:id/consistency-report` | `ScopeReadGuard` | Path: `id`；Query: `refresh?` (`bool`) | `200`: `ConsistencyReportDTO`（含 `VERIFIED / OUTBOUND_VERIFIED / VIOLATION / UNCOVERED / NOT_MONITORED / NOT_OBSERVABLE / UNATTRIBUTED / IMPL_UNRESOLVED` **八分类**统计、新增 `impl_unresolved_count: int` 与携带 `contract_id?: uint64, impl_key?: string` 的边列表，见 [`03-architecture-elements-and-topology.md` §5.3](../02-design/03-architecture-elements-and-topology.md)）<br>`403/404` | - |
| `GET` | `/api/recording-sessions/:id/transactions` | `ScopeReadGuard` | Path: `id`；Query: `process_id?, subsystem_id?, module_id?, contract_id?, direction?, exchange_type?, service_name?, operation?, sno?, trace_id?, entity_key?, has_error?` (`bool`), `is_replayable?, keyword?, page?, page_size?`（选项 `15/25/50/100`，默认 `25`） | `200`: `{items: []TransactionTreeGroupDTO, total, page, page_size, entity_chains: []EntityLifecycleChainDTO}`（`/debug` 默认主视图：基于 DOTR `TraceKey` + 时间窗兜底构建多级因果调用树、跨进程 `EGRESS ⇔ INGRESS` 缝合、兄弟 `串行/并发/补偿` 分类与内联甘特瀑布坐标；**严格按根业务事务分页，保证同一事务父子节点绝不跨页割裂**；命中深层子调用过滤时完整保留祖先链；轻量列投影不加载 `raw_*_packet` 与大体积 `payload_json`）<br>`400`: 参数非法；`403`: 访问他人 `PRIVATE` 批次；`404`: 批次不存在 | - |
| `GET` | `/api/recording-sessions/:id/transactions/:rootRecordId/sequence` | `ScopeReadGuard` | Path: `id, rootRecordId`；Query: `replay_task_id?`（可选叠加指定重放任务结果，须同时通过该任务 `ScopeReadGuard`） | `200`: `TransactionSequenceDTO`（含参与者泳道 `participants`、按发起序交互边 `steps`、V03/V04 三段耗时拆解、单事务内 `payload_json` 参数传递血缘边 `parameter_lineages` 及可选重放 `SCOPED_HIT / MOCK_MISS / FAILED` 叠加图层）<br>`403/404` | - |
| `GET` | `/api/traffic-records` | `ScopeReadGuard` | Query: `session_id` (必填或按可见批次过滤), `scope?, subsystem_id?, module_id?, contract_id?, process_id?, direction?, exchange_type?, service_name?, operation?, sno?, trace_id?, has_error?` (`bool`, 过滤 `status_code != 0 OR exchange_type = 'REQ_ONLY' OR biz_error = true`), `keyword?, is_replayable?, sort_dir?` (`asc`\|`desc`, 默认 `asc`), `page?, page_size?` | `200`: 标准分页 `items: []TrafficRecordSummary`（**默认严格按请求发起时刻 `req_captured_at_ns ASC, seq_no ASC, id ASC` 排序**，完整水合 `src_module_name / dst_module_name / dst_contract_id / impl_key / process_name / peer_proc_name / conn_index / biz_error`，不加载 `raw_req_packet` / `raw_rsp_packet` `bytea` 列）<br>`400`: 筛选参数非法；`403`: `session_id` 为他人 `PRIVATE` 素材 | - |
| `GET` | `/api/traffic-records/:id/anatomy` | `ScopeReadGuard` | Path: `id`；Query: `session_id?` | `200`: `PacketAnatomyDrawerDTO`（含 `reqSide` 与 `rspSide` 四段完整解剖：24B 公共头位图、会话头与 V03/V04 三段耗时 `networkAndFramingUs / queueWaitUs / serverExecUs`、扩展头 Trace、Protobuf JSON 与 16B 对齐 Raw Hex，见 [`04-control-plane-and-web-console.md` §4.3](../02-design/04-control-plane-and-web-console.md)）<br>`403/404` | - |
| `GET` | `/api/traffic-records/:id/replay-closure` | `ScopeReadGuard` | Path: `id`（目标可重放 `INGRESS` 记录 ID） | `200`: `ReplayClosurePreviewDTO`（返回该入口请求绑定的专属下游 `scoped_egress_mocks` 闭包清单（含多实现契约 `dst_tag`）、跨事务有状态实体前置依赖链 `prerequisite_chain` 及一键串行连带回放推荐 `recommended_record_ids`）<br>`400`: 非 `INGRESS` 记录；`403`: 他人 `PRIVATE` 素材；`404`: 不存在 | - |
| `POST` | `/api/traffic-records/:id/redecode` | `ScopeReadGuard` | Path: `id`（使用当前生效 `.pb` 版本按需重解码单条往返记录，F-03.4） | `200`: `PacketAnatomyDrawerDTO`<br>`400`: 无匹配生效 `.pb`；`403/404` | - |

---

### 2.6 跨 Agent 同构离线重放与双层回归 Diff 域 (`F-12`~`F-14`, `SC-07`, `SC-08`, `SC-12`)

> 同构进程校验（含多实现契约按源进程快照实现模块校验）、二进制 Mock 自愈（含 `@dst_tag` 多实现隔离）与双层 Diff 算法参见 [`02-data-plane-and-cros-rpc-engine.md` §3](../02-design/02-data-plane-and-cros-rpc-engine.md)、[`03-architecture-elements-and-topology.md` §3.3](../02-design/03-architecture-elements-and-topology.md) 及 [`04-control-plane-and-web-console.md` §1.3 & §3](../02-design/04-control-plane-and-web-console.md)。

| 方法 | 路由路径 | 守卫要求 | Query / Body 核心参数 | 响应 JSON 结构与状态码 | `AuditAction` (`module:action`) |
| :---: | :--- | :---: | :--- | :--- | :--- |
| `GET` | `/api/replay-tasks` | `ScopeReadGuard` | Query: `session_id?, golden_suite_id?, trigger_source?, target_process_id?, agent_id?, lifecycle_state?, result_status?, page?, page_size?` | `200`: 标准分页 `items: []ReplayTask` | - |
| `GET` | `/api/replay-tasks/:id` | `ScopeReadGuard` | Path: `id` | `200`: `ReplayTask`（含汇总计数、`violation_count`、`observed_egress_calls` 与 `mock_events`）<br>`403/404` | - |
| `POST` | `/api/replay-tasks` | `JWT` | Body: `{name, session_id, source_process_id, target_process_id, selected_record_ids: uint64[], trigger_source?: "MANUAL"\|"SINGLE_DEBUG", extra_ignore_paths?: string[], timeout_ms?: int}` | `201`: `ReplayTask`（校验素材可见性 + 目标 Agent 权限 + 基于 `session.process_snapshots` 解耦校验 `ValidateReplayProcessCompatibility` 模块同构性（多实现契约按源进程快照中的实现模块校验） + **校验目标进程处于 `READY` 无未受控直连** + 锁定目标 Agent `REPLAY` 排他租约 + **按 DOTR 子树/时间窗组装每条 `RecordID` 专属 `ScopedEgressMocks`（多实现契约注入目的 `ArchProcess.code` 作为 `dst_tag`）及兜底池**并下发二进制重放包）<br>`400`: 目标进程未承载源请求所需公共模块、含不可重放截断帧或 Agent 无特权；`403`: 他人 `PRIVATE` 素材或他人 `PRIVATE` Agent；`404`: 资源不存在；`409`: 目标 Agent 正被其他录制/重放任务占用，**或目标进程处于 `WAITING_PROC` / `NEED_RECONNECT` (`established_conns > 0`，`code: "TARGET_PROCESS_NOT_READY"`，阻断直连外泄风险)** | `traefik_replay:execute_replay` |
| `POST` | `/api/replay-tasks/:id/cancel` | `ScopeWriteGuard` | Path: `id`（任务发起人或 `traefik_admin`） | `200`: `ReplayTask`（调用 Agent `/replays/:taskId/cancel` 并释放 DB 排他租约）<br>`403/404`；`409`: 任务已结束 | `traefik_replay:cancel_replay` |
| `GET` | `/api/replay-tasks/:id/items` | `ScopeReadGuard` | Path: `id`；Query: `result_status?` (`PASS`\|`FAILED`\|`MOCK_MISS`\|`TIMEOUT`\|`ERROR`), `service_name?`, `operation?`, `page?, page_size?` | `200`: 标准分页 `items: []ReplayItem`（独立行存储，含 `field_diffs` 与 `mock_trace`）<br>`403/404` | - |
| `GET` | `/api/replay-tasks/:id/items/:itemId` | `ScopeReadGuard` | Path: `id, itemId` | `200`: `{item: ReplayItem, baseline_rsp_anatomy: FrameSideAnatomyDTO, actual_rsp_anatomy?: FrameSideAnatomyDTO}`<br>`403/404` | - |
| `POST` | `/api/replay-tasks/:id/rediff` | `ScopeWriteGuard` | Path: `id`；Body: `{extra_ignore_paths: string[]}`（基于已保存的 `raw_actual_packet` 离线重算双层 Diff 并更新状态聚合） | `200`: `ReplayTask`<br>`400/403/404` | `traefik_replay:execute_replay` |

---

### 2.7 SSE 实时事件流接口 (`GET /api/events`)

- **路径与鉴权**：`GET /api/events`，挂载 `commonAuth.AuthMiddleware`（前端通过 `fetch` + `ReadableStream` 在 `Authorization: Bearer <JWT>` Header 中传递令牌，严禁 URL Query 传参）。
- **过滤参数**：Query `session_id?`（订阅指定录制批次实时帧与状态）、`task_id?`（订阅指定重放任务进度）。若传入他人 `PRIVATE` `session_id`，立即返回 `403 Forbidden`。
- **事件帧类型 (`event: <type>\ndata: <json>\n\n`)**：
  1. `traffic.preview`：Track A 快轨实时流量预览摘要（`{session_id, process_id, direction, sno, service_name, operation, captured_at_ns}`）；
  2. `traffic.persisted`：Track B 慢轨往返配对落盘完成通知（`{session_id, record_id, sno, exchange_type, latency_us, status_code, decode_status}`）；
  3. `session.lifecycle`：录制批次状态流转、进程 PID 重启热更新（`PROCESS_RESTARTED`）、存量连接诊断或达到时长/帧数上限自动停录（`COMPLETED_BY_LIMIT`）；
  4. `agent.lease_changed`：Agent 在线状态或排他租约变更通知（`{agent_id, status, lease_type, lease_holder_name, lease_expires_at}`）；
  5. `replay.item_done`：单条重放请求执行与断言完成通知（`{task_id, item_id, record_id, result_status, latency_us}`）；
  6. `replay.completed`：重放任务整包完成汇总（`{task_id, lifecycle_state, result_status, total_requests, passed_requests, mock_hit_count, mock_miss_count}`）；
  7. `agent.probes_updated`：Agent 预挂探针就绪快照更新通知（`{agent_id, armed_digest, diagnostics: []ProcessDiagStatus}`，仅向对该 Agent 具备 `ScopeReadGuard` 可见性的订阅客户端广播）。

---

### 2.8 契约治理全景大盘、黄金用例套件与 CI/CD 强阻断质量门禁域 (`F-24`~`F-27`, `ADR-14`, `ADR-16`, `SC-09`, `SC-10`, `SC-12`)

> 三维封顶健康分模型、多实现契约实现覆盖率、黄金套件自包含快照沉淀与 CI 门禁双重强阻断机制详见 [`ADR-14`](../05-adr/14-governance-dashboard-golden-suite-and-ci-gate-adr.md)、[`ADR-16`](../05-adr/16-multi-implementation-service-contract-adr.md) 及 [`05-database-schema.md` §3.6](../02-design/05-database-schema.md)。

| 方法 | 路由路径 | 守卫要求 | Query / Body 核心参数 | 响应 JSON 结构与状态码 | `AuditAction` (`module:action`) |
| :---: | :--- | :---: | :--- | :--- | :--- |
| `GET` | `/api/dashboard/overview` | `JWT` | Query: `subsystem_id?` (`uint64`，可选按子系统过滤) | `200`: `{health_score: {total: int, grade: "EXCELLENT"\|"GOOD"\|"SUBHEALTH"\|"CRITICAL", arch_sub_score: int, asset_sub_score: int, regression_sub_score: int, action_hints: string[]}, drift_radar: {verified_edges: int, active_violation_edges: int, impl_unresolved_edges: int, top_violated_processes: []ViolatedProcessStat}, contract_assets: {total_active_services: int, claimed_services: int, orphan_services: int, claimed_ratio: number, multi_impl_contract_count: int, multi_impl_total_impls: int, multi_impl_verified_impls: int, multi_impl_verified_ratio: number, top_orphan_services: []OrphanServiceSummary}, regression_defense: {gate_evaluations_30d: int, blocked_count_30d: int, pass_rate_30d: number, unhealthy_suite_processes_7d: int}}`（严格仅聚合 `SHARED` 公共核验与 `GOLDEN_SUITE / CI_GATE` 任务，排除个人 `PRIVATE` 噪音，零 N+1） | - |
| `GET` | `/api/golden-suites` | `JWT` | Query: `arch_process_id?, status?` (`ACTIVE\|ARCHIVED`), `search?, page?, page_size?` | `200`: 标准分页 `items: []GoldenSuite`（预加载 `arch_process`、用例数 `item_count` 与最近一次套件回归状态摘要，永不返回明细 `bytea`） | - |
| `GET` | `/api/golden-suites/:id` | `JWT` | Path: `id` | `200`: `{suite: GoldenSuite, items: []GoldenSuiteItemSummary}`（按 `order_no ASC` 排序返回元数据、`mask_rules_json` 与下游 Mock 方法数摘要，不透出 `raw_*_packet` 大字段）<br>`404`: 套件不存在 | - |
| `POST` | `/api/golden-suites/curate` | `JWT` | Body: `{suite_id?: uint64, new_suite?: {code, name, arch_process_id, version?, extra_ignore_paths?, description?}, session_id: uint64, record_ids: uint64[], mask_rules_json?: []MaskRule}`（从 `/debug` 事务树一键沉淀：普通用户可从可读批次沉淀，写入既有套件或新建套件建议限 `AdminOnly` 或模块负责人；**单事务完整深拷贝入站 `raw_req_packet`、基线 `raw_rsp_packet`、成对 `ScopedMocks` 与 `EgressEndpoints`**） | `201`: `{suite: GoldenSuite, added_items: int}`<br>`400`: 记录不可重放、跨不同 `ArchProcess` 或编码非法；`403`: 对源 `PRIVATE` 批次无读取权或无套件写权限；`404`: 批次/记录不存在；`409`: 新建 `code` 重复 | `traefik_suite:curate` |
| `PUT` | `/api/golden-suites/:id` | `AdminOnly` | Path: `id`；Body: `{name, version, status: "ACTIVE"\|"ARCHIVED", extra_ignore_paths?: string[], description?}` | `200`: `GoldenSuite`<br>`400/403/404` | `traefik_suite:update` |
| `PUT` | `/api/golden-suites/:id/items/:itemId` | `AdminOnly` | Path: `id, itemId`；Body: `{name?, order_no?, mask_rules_json?: []MaskRule}` | `200`: `GoldenSuiteItemSummary`<br>`400/403/404/409` | `traefik_suite:update_item` |
| `DELETE` | `/api/golden-suites/:id/items/:itemId` | `AdminOnly` | Path: `id, itemId` | `200`: `{"message":"黄金用例已移除"}`（同步维护 `suite.item_count`）<br>`403/404` | `traefik_suite:delete_item` |
| `DELETE` | `/api/golden-suites/:id` | `AdminOnly` | Path: `id` | `200`: `{"message":"黄金契约套件已删除"}`<br>`403/404`；`409`: 处于 `ACTIVE` 状态且正有运行中回归任务 | `traefik_suite:delete` |
| `POST` | `/api/golden-suites/:id/execute` | `JWT` | Path: `id`；Body: `{env_agent_id: uint64, timeout_ms?: int}`（脱离源录制批次，直接从 `traefik_golden_suite_items` 自包含快照组装重放包下发目标环境同构挂载进程，`trigger_source="GOLDEN_SUITE"`） | `201`: `ReplayTask`<br>`400`: 套件无用例或目标环境未挂载对应 `ArchProcess`；`403`: 对目标 `PRIVATE` Agent 无权限；`404`: 不存在；`409`: Agent 租约冲突或目标进程 `NEED_RECONNECT` | `traefik_suite:execute` |
| `POST` | `/api/gates/evaluate` | `CIOrAdminGuard` | Body: `{target_arch_process_id: uint64, env_agent_id: uint64, golden_suite_id?: uint64, timeout_seconds?: int (默认 60, 5..300), wait_lease_seconds?: int (默认 60, 0..300)}`（同步阻塞式 CI/CD 强阻断门禁评估，`trigger_source="CI_GATE"`，仅允许指定 `SHARED` 环境 Agent） | `200`: `{gate_status: "PASSED", task_id: uint64, violation_count: 0, passed_requests: int, total_requests: int, markdown_report: string}`<br>`422`: **质量门禁红牌阻断** `{gate_status: "GATED_VIOLATION" \| "GATED_BREAKING_DIFF", task_id: uint64, violations: []ObservedViolationDTO, failed_items: []GateFailedItemDTO, markdown_report: string}`<br>`400/401/403`；`404`: 标准进程或 `ACTIVE` 黄金套件不存在 (`SUITE_NOT_FOUND`)；`503`: **环境瞬态不可用**（Agent 离线、等锁超 `wait_lease_seconds` 返回 `GATED_ENV_BUSY`、或进程未就绪返回 `GATED_ENV_NOT_READY`，供 CI 自动重试） | `traefik_gate:evaluate` |

---

## 3. 主从管控与二进制流交互契约 (`Server <-> Agent`)

> 载荷结构体与 56B 二进制信封布局详见 [`01-system-architecture.md` §4.2 & §4.3](../02-design/01-system-architecture.md)。Agent 所有接口均受源 IP 白名单中间件（`--allow-ips`）保护，非白名单源 IP 统一返回 `403 Forbidden`。

| 方向 | 协议 / 方法 | Agent 路由路径 | 请求头 / 载荷契约 | 状态码与响应契约 |
| :---: | :---: | :--- | :--- | :--- |
| `Server -> Agent` | `GET` | `/agent/v1/handshake` | Header: `X-Claim-Code?` (纳管 `PRIVATE` Agent 时必传), `X-Expected-Lease-Type` (`""\|"RECORDING"\|"REPLAY"`), `X-Expected-Lease-Ref` (`uint64`) | `200`: `HandshakeResp`（含 `BootID, ArmedProcessCount, ArmedDigest`；若期望租约为 `""` 而本地持有孤儿录制/重放租约，则自动关闭活跃采集/重放并回退至 `ARMED` 预挂待命或全量清理）<br>`403`: 源 IP 不在白名单或 `X-Claim-Code` 不匹配 |
| `Server -> Agent` | `GET` | `/agent/v1/processes/discover` | 无 | `200`: `{"items": []DiscoveredProcess}` |
| `Server -> Agent` | `PUT` | `/agent/v1/monitors/arm` | Body: `ArmStandbyReq{Revision: uint64, Processes: []ProcessMonitorSpec}`（幂等同步低频建连钩子预挂待命集合与目标 PID 默认全出站 `EgressProxy` `PASSTHROUGH` 纯透传监听，不挂载全局 `read/write` 钩子、**不占用 `activeLease` 排他租约**；通过 `/proc/<pid>/fd` 自有 Socket Inode 返回精准七态诊断；若 `Processes` 为空则关闭监听、卸载 `bpf_link` 并对存量透传连接平滑排空 `Graceful Drain`） | `200`: `{"revision": uint64, "armed_digest": string, "diagnostics": []ProcessDiagStatus}`<br>`409`: `Revision < lastAppliedRevision` 过期版本，或 Agent 当前持有活跃 `RECORDING`/`REPLAY` 租约禁止覆盖待命表<br>`503`: 缺少 `CAP_BPF` / `CAP_NET_ADMIN` / `CAP_PERFMON` / `CAP_SYS_PTRACE` 特权（返回 `CAP_DENIED` 诊断） |
| `Server -> Agent` | `GET` | `/agent/v1/monitors/diagnose` | 无（不改变探针状态，基于 `/proc/<pid>/fd` 自有 Socket Inode 实时复扫已挂载进程的 PID 与未代理直连出站连接数） | `200`: `{"revision": uint64, "armed_digest": string, "diagnostics": []ProcessDiagStatus}` |
| `Server -> Agent` | `PUT` | `/agent/v1/monitors/sync` | Body: `SyncMonitorReq{SessionID, MaxFrameBytes, MaxBufferBytes, Processes: []ProcessMonitorSpec}`（锁定本地 `RECORDING` 租约；实时复检 `/proc/<pid>/fd` 后按序先注入 `activeSessionID` 开启 `EgressProxy` `CAPTURING` 全出站分帧、再预热存量已建立入站 FD 至 `ing_conn_map` 并动态挂载入站读写钩子，无需预配 `Downstreams` 即默认双录 `INGRESS + EGRESS`，已 `ARMED` 进程 `0ms` 零重启激活） | `200`: `{"diagnostics": []ProcessDiagStatus}`<br>`409`: 与 Agent 当前内存 `activeLease` 冲突 (`SingleTenantGuard`)<br>`503`: 缺少 eBPF 特权 |
| `Server -> Agent` | `DELETE` | `/agent/v1/monitors/:sessionId/processes/:processId` | Path: `sessionId, processId` | `200`: `{"removed":true,"remaining_processes":int}`（将该进程 `session_id` 置回 `0`、不断连回退 `EgressProxy` `PASSTHROUGH` 待命；若活跃采集进程剩余为 0 则卸载入站读写钩子并释放本地 `RECORDING` 租约） |
| `Server -> Agent` | `POST` | `/agent/v1/monitors/:sessionId/stop` | Path: `sessionId`（主控停录必须严格携带路径参数 `:sessionId`） | `200`: `StopMonitorResp{CapturedFrames, DroppedFrames, BypassedSelfCount}`（先置 `session_id = 0` 并卸载入站读写钩子 -> `200ms` 在途应答排空 -> 将 `EgressProxy` 拨回 `PASSTHROUGH` 纯透传并清零 `activeSessionID` 且**不断开既有业务 TCP 长连接** -> 排空 WS 缓冲后发 `EVENT_SESSION_DRAINED` 并释放本地 `RECORDING` 租约） |
| `Server -> Agent` | `POST` | `/agent/v1/replays/execute` | Body: `ExecuteReplayReq{TaskID, ProcessSpec, TimeoutMs, IngressItems, ScopedEgressMocks, GlobalFallbackMocks, MngMocks}`（向后兼容平铺 `IngressRequests/EgressMocks`；纯二进制切片，零 `.pb`；`ProcessSpec.Downstreams`（`DownstreamEndpoint{IP, Port, DstTag?}`）为静态推导 $\cup$ 源录制批次历史 `EGRESS` `DstAddr` 并集，`ScopedEgressMocks / GlobalFallbackMocks`（`MockRuleDownstream{ReqPacket, RspPackets, DstTag?}`）在多实现契约下携带不透明目的进程标签 `dst_tag`（= 目的 `ArchProcess.code`）；Agent 按出站连接原始目的地址查找端点 `dst_tag`，优先以 `service:operation@dst_tag` 匹配局部/全局 Mock 池，缺失标签时自动回退至 `service:operation` 完全向后兼容；核验目标进程无指向该集合的未代理直连连接后，将命中集合的既有 `EgressProxy` 长连接切入 `REPLAY_MOCK`、其余连接挂载写前 `"CSE.TCP"` 零外泄守卫，新建连接统一导向物理零外拨本地 `MockServer`；注入每条 `IngressItem` 前调用 `SetActiveRecordID(item.RecordID)` 激活其局部 Mock 池） | `200`: `{"status":"RUNNING","diagnostics":ProcessDiagStatus}`<br>`409`: 本地租约冲突，或目标进程处于 `WAITING_PROC` / `NEED_RECONNECT` (`established_conns > 0`)<br>`503`: 缺少 eBPF 特权 |
| `Server -> Agent` | `GET` | `/agent/v1/replays/:taskId/result` | Path: `taskId` | `200`: `ReplayTaskResultResp`（含 `Details` 入站明细及 **`ObservedEgressCalls []ObservedEgressCall{ServiceName, Operation, DstAddr, CallCount, MatchStatus}` 重放期实测出站调用汇总**，供控制面单次闭环判定 `F-27` `GATED_VIOLATION`）；`404`: 任务不存在 |
| `Server -> Agent` | `POST` | `/agent/v1/replays/:taskId/cancel` | Path: `taskId` | `200`: `{"cancelled":true}`（立即断开注入连接与脱离下游序列的被接管 Mock 连接、释放本地 `REPLAY` 租约并恢复原 `ARMED` `PASSTHROUGH` 待命映射） |
| `Server -> Agent` | `POST` | `/agent/v1/purge` | 无（主控启动恢复、管理员强制解锁或紧急全卸载调用；注：5m 心跳失联走 `Graceful Drain` 排空，而 `/purge` 与 `SIGINT/SIGTERM` 走立即硬关闭） | `200`: `{"purged":true}`（幂等硬关闭全部代理/Mock 连接与端口、`link.Close()` 卸载全部 eBPF 探针、清空 Maps 与 `armedSpecs`、重置本地租约） |
| `Agent -> Server` | `WS (Binary)` | `/agent/v1/stream` | WebSocket 二进制帧：56B 大端 `EnvelopeHeader` (`Magic=0x4354 "CT"`, `ProtoVer=1`, `EventType=0x01..0x07`, 流量帧保证 `SessionOrTaskID > 0` 且 `Direction` 恒定为 `1=INGRESS` 入站收发或 `2=EGRESS` 出站收发) + 变长载荷 | `101 Switching Protocols`（单帧读取上限 `MaxFrameBytes + 56 + 1024`） |
