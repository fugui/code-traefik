# 业务流量监控与重放 (`code-traefik`) HTTP / SSE 与主从通信接口契约规范

- **文档编号**：`01-http-api-contract.md`
- **文档版本**：v1.0.0
- **更新日期**：2026-10-29
- **归属阶段**：阶段三：接口与数据契约冻结 (`03-api`)
- **状态**：已冻结 (Frozen)
- **上位基线**：[`01-feature-matrix.md`](../01-features/01-feature-matrix.md) (`F-01`~`F-16`、`SC-01`~`SC-08`)、[`01-system-architecture.md`](../02-design/01-system-architecture.md)、[`04-control-plane-and-web-console.md`](../02-design/04-control-plane-and-web-console.md)、[`05-database-schema.md`](../02-design/05-database-schema.md)

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
4. **标准错误与 `409` 排他租约冲突响应**：
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
| `GET` | `/api/admin/settings` | `AdminOnly` | 无 | `200`: `{"settings": []TraefikSetting}`<br>`401/403`: 权限不足 | - |
| `PUT` | `/api/admin/settings` | `AdminOnly` | Body: `{"items":[{setting_key, setting_val, description}]}`（`1..50` 项） | `200`: `{"message":"模块配置已保存","settings":[]}`<br>`400`: 键名格式非法；`401/403`: 权限不足 | `settings:update` |

---

### 2.2 三级架构元素、模块接口契约与静态拓扑域 (`F-01`, `F-02`, `F-04`, `SC-02`)

> 实体结构与树形算法参见 [`03-architecture-elements-and-topology.md` §1~§4](../02-design/03-architecture-elements-and-topology.md) 及 [`05-database-schema.md` §3.1~§3.2](../02-design/05-database-schema.md)。

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
| `GET` | `/api/arch/modules` | `JWT` | Query: `subsystem_id?, group_id?, include_descendants?, process_id?, search?, page?, page_size?` | `200`: 标准分页 `items: []Module`（含 `group`, `subsystem`, 公共基线 `process` 及接口计数） | - |
| `GET` | `/api/arch/modules/:id` | `JWT` | Path: `id` | `200`: `{module: Module, interfaces: []ModuleInterface}`<br>`404`: 模块不存在 | - |
| `POST` | `/api/arch/modules` | `AdminOnly` | Body: `{group_id, code, name, owner_id?, process_id?, description?}`（冗余写入所属 `subsystem_id`） | `201`: `Module`<br>`400`: 格式非法或绑定私有进程为公共基线；`403/404`；`409`: `code` 全局重复 | `traefik_arch:create` |
| `PUT` | `/api/arch/modules/:id` | `AdminOnly` | Body: `{group_id, code?, name, owner_id?, process_id?, description?}` | `200`: `Module`<br>`400/403/404`；`409`: `code` 冲突 | `traefik_arch:update` |
| `DELETE` | `/api/arch/modules/:id` | `AdminOnly` | Path: `id` | `200`: `{"message":"功能模块已删除"}`<br>`403/404`；`409`: 已声明 `ModuleInterface` 或仍被 `traefik_process_modules` 关联 | `traefik_arch:delete` |
| `GET` | `/api/arch/modules/:id/interfaces` | `JWT` | Path: `id`；Query: `relation_type?` (`PROVIDE` \| `DEPEND`) | `200`: `{"items": []ModuleInterface}`<br>`404`: 模块不存在 | - |
| `POST` | `/api/arch/modules/:id/interfaces` | `AdminOnly` | Body: `{relation_type, service_name, operation, diff_ignore_paths?: string[], description?}` | `201`: `ModuleInterface`<br>`400`: `service_name` 不符合 `{package}-{serviceName}` 或超 63B；`403/404`；`409`: 同模块重复或 `PROVIDE` 已被其他模块声明 (A-05，返回冲突模块信息) | `traefik_arch:create` |
| `PUT` | `/api/arch/module-interfaces/:ifaceId` | `AdminOnly` | Body: `{diff_ignore_paths: string[], description?}` | `200`: `ModuleInterface`<br>`400/403/404` | `traefik_arch:update` |
| `DELETE` | `/api/arch/module-interfaces/:ifaceId` | `AdminOnly` | Path: `ifaceId` | `200`: `{"message":"接口契约已删除"}`<br>`403/404` | `traefik_arch:delete` |
| `GET` | `/api/arch/topology/static` | `JWT` | Query: `view` (`LOGICAL` \| `PHYSICAL`，默认 `LOGICAL`), `subsystem_id?` | `200`: `{"view":string,"nodes":[],"edges":[]}`（含 `CROSS_PROCESS`、`INTRA_PROCESS`、`EXTERNAL_UNREGISTERED`，参见 [`03-architecture-elements-and-topology.md` §4](../02-design/03-architecture-elements-and-topology.md)）<br>`400`: `view` 参数非法 | - |

---

### 2.3 Protobuf 描述集 (`.pb`) 与方法映射域 (`F-03`, `SC-02`)

> 校验与激活规则参见 [`04-control-plane-and-web-console.md` §2.1](../02-design/04-control-plane-and-web-console.md) 及 [`05-database-schema.md` §3.4](../02-design/05-database-schema.md)。

| 方法 | 路由路径 | 守卫要求 | Query / Body 核心参数 | 响应 JSON 结构与状态码 | `AuditAction` (`module:action`) |
| :---: | :--- | :---: | :--- | :--- | :--- |
| `GET` | `/api/proto-schemas` | `JWT` | Query: `name?, active_only?, search?, page?, page_size?` | `200`: 标准分页 `items: []ProtoSchema`（不透出 `descriptor_bytes`） | - |
| `POST` | `/api/proto-schemas` | `AdminOnly` | `multipart/form-data`: `file` (必填 `.pb` 二进制 `<=4MB`), `name`, `version_tag?`, `description?`, `auto_activate?` (`bool`) | `201`: `{schema: ProtoSchema, operations: []ProtoOperation}`<br>`400`: 文件 `>4MB`、`FileDescriptorSet` 损坏或 `protodesc.NewFiles` 失败；`403`: 非管理员；`409`: `auto_activate=true` 且与其他生效描述集存在 `(service_name, operation)` 冲突 | `traefik_schema:upload` |
| `POST` | `/api/proto-schemas/:id/activate` | `AdminOnly` | Path: `id`（原子切换同名版本并重建内存 `CompiledRegistry`） | `200`: `{schema: ProtoSchema, activated_operations: int}`<br>`403/404`；`409`: 与其他生效描述集存在 `(service_name, operation)` 冲突（返回 `conflicts: [{service_name, operation, conflict_schema_name, conflict_version}]`） | `traefik_schema:activate` |
| `DELETE` | `/api/proto-schemas/:id` | `AdminOnly` | Path: `id` | `200`: `{"message":"描述集版本已删除"}`<br>`403/404`；`409`: 当前处于 `is_active = true` 生效态禁止删除 | `traefik_schema:delete` |
| `GET` | `/api/proto-operations` | `JWT` | Query: `schema_id?, active_only?, service_name?, operation?, search?, page?, page_size?` | `200`: 标准分页 `items: []ProtoOperation`（`service_name` 格式为 `{package}-{serviceName}`） | - |

---

### 2.4 公私域 Agent 纳管、排他租约与进程部署域 (`F-02`, `F-06`, `F-08`, `SC-03`)

> 租约状态机、SSRF 防护与 `ClaimCode` 认领机制参见 [`01-system-architecture.md` §3 & §4.1](../02-design/01-system-architecture.md) 及 [`05-database-schema.md` §3.3](../02-design/05-database-schema.md)。

| 方法 | 路由路径 | 守卫要求 | Query / Body 核心参数 | 响应 JSON 结构与状态码 | `AuditAction` (`module:action`) |
| :---: | :--- | :---: | :--- | :--- | :--- |
| `GET` | `/api/agents` | `ScopeReadGuard` | Query: `scope?` (`SHARED` \| `PRIVATE`), `status?` (`ONLINE` \| `OFFLINE`), `lease_state?` (`IDLE` \| `LEASED`), `search?`, `page?`, `page_size?` | `200`: 标准分页 `items: []Agent`（普通用户仅返回 `SHARED` 与本人 `PRIVATE`；`traefik_admin` 可穿透查看全量） | - |
| `GET` | `/api/agents/:id` | `ScopeReadGuard` | Path: `id` | `200`: `{agent: Agent, processes: []Process}`<br>`403`: 普通用户访问他人 `PRIVATE` Agent；`404`: 不存在 | - |
| `POST` | `/api/agents` | `ScopeWriteGuard` | Body: `{name, endpoint, scope: "SHARED"\|"PRIVATE", claim_code?, description?}` | `201`: `Agent`（同步完成首次 `/agent/v1/handshake` 能力与版本校验）<br>`400`: SSRF 拦截（回环/云元数据/非法端口）或主版本 `!= "1.0"`；`403`: 普通用户尝试创建 `SHARED` Agent 或 `PRIVATE` `claim_code` 校验不匹配；`409`: `name` 或 `endpoint` 重复 | `traefik_agent:create` |
| `PUT` | `/api/agents/:id` | `ScopeWriteGuard` | Body: `{name, endpoint, claim_code?, description?}` | `200`: `Agent`<br>`400/403/404`；`409`: 名称/地址冲突或当前持有活跃租约禁止修改端点 | `traefik_agent:update` |
| `DELETE` | `/api/agents/:id` | `ScopeWriteGuard` | Path: `id` | `200`: `{"message":"Agent 已移除"}`<br>`403/404`；`409`: 仍纳管 `Process` 或持有活跃租约 (`lease_type != ''`) | `traefik_agent:delete` |
| `POST` | `/api/agents/:id/handshake` | `ScopeReadGuard` | Path: `id`（立即触发一次探活与双向租约对账） | `200`: `{agent: Agent, handshake: HandshakeResp}`<br>`403/404`；`503`: Agent 离线或连接超时 | - |
| `GET` | `/api/agents/:id/discover-processes` | `ScopeReadGuard` | Path: `id`（`SHARED` Agent 仅 `AdminOnly` 可调用；`PRIVATE` 仅本人或管理员可调用） | `200`: `{"items": []DiscoveredProcess}`（含 `pid, listen_port, cmdline, exe_name`）<br>`403/404/503` | - |
| `POST` | `/api/agents/:id/lease/force-unlock` | `AdminOnly` | Path: `id`；Body: `{reason: string}`（强制清空 DB 租约、终止关联未决任务并调用 Agent `/agent/v1/purge`） | `200`: `{"message":"排他租约已强制释放","agent":Agent}`<br>`400`: `reason` 为空；`403`: 非管理员；`404`: 不存在 | `traefik_agent:force_unlock_lease` |
| `GET` | `/api/processes` | `ScopeReadGuard` | Query: `agent_id?, scope?, module_id?, search?, page?, page_size?` | `200`: 标准分页 `items: []Process`（预加载 `agent` 与 `process_modules.module`） | - |
| `POST` | `/api/processes` | `ScopeWriteGuard` | Body: `{agent_id, name, listen_port, advertise_ip?, cmd_pattern, module_ids: uint64[], extra_downstreams?: string[], description?}` | `201`: `Process`（持久化 `traefik_process_modules`；`SHARED` 进程同步维护 `Module.ProcessID`，`PRIVATE` 进程不改写公共基线）<br>`400`: 端口越界或 `module_ids` 为空；`403`: 无目标 Agent 写权限；`404`: Agent/Module 不存在；`409`: 同 Agent 下 `listen_port`/`name` 重复，或同部署域内同一 `Module` 重复挂载到多个进程 (A-03) | `traefik_agent:create` |
| `PUT` | `/api/processes/:id` | `ScopeWriteGuard` | Body: `{name, listen_port, advertise_ip?, cmd_pattern, module_ids: uint64[], extra_downstreams?: string[], description?}` | `200`: `Process`<br>`400/403/404`；`409`: 端口/模块挂载冲突，或所属 Agent 处于录制/重放租约中 | `traefik_agent:update` |
| `DELETE` | `/api/processes/:id` | `ScopeWriteGuard` | Path: `id` | `200`: `{"message":"进程映射已删除"}`<br>`403/404`；`409`: 所属 Agent 持有活跃租约或被 `ReplayTask` 引用 | `traefik_agent:delete` |

---

### 2.5 多进程协同录制、架构一致性验证与流量解剖域 (`F-05`, `F-10`, `F-11`, `SC-04`~`SC-06`)

> 录制编排、双模分区归档与四段解剖 DTO 参见 [`03-architecture-elements-and-topology.md` §5](../02-design/03-architecture-elements-and-topology.md) 及 [`04-control-plane-and-web-console.md` §1.2 & §4.3](../02-design/04-control-plane-and-web-console.md)。

| 方法 | 路由路径 | 守卫要求 | Query / Body 核心参数 | 响应 JSON 结构与状态码 | `AuditAction` (`module:action`) |
| :---: | :--- | :---: | :--- | :--- | :--- |
| `GET` | `/api/recording-sessions` | `ScopeReadGuard` | Query: `scope?` (`SHARED` \| `PRIVATE`), `status?`, `process_id?`, `search?`, `page?`, `page_size?` | `200`: 标准分页 `items: []RecordingSession` | - |
| `GET` | `/api/recording-sessions/:id` | `ScopeReadGuard` | Path: `id` | `200`: `RecordingSession`（含 `process_diagnostics` 与 `leased_agent_ids`）<br>`403`: 访问他人 `PRIVATE` 批次；`404`: 不存在 | - |
| `POST` | `/api/recording-sessions` | `JWT` | Body: `{name, scope: "SHARED"\|"PRIVATE", target_process_ids: uint64[], max_duration_sec?: int, max_frames?: int, max_frame_bytes?: int, description?}` | `201`: `RecordingSession`（原子升序锁定涉及的全部 Agent `RECORDING` 租约，下发 `/monitors/sync` 并返回存量连接诊断 `need_reconnect`）<br>`400`: 进程列表为空或 Agent 离线/无 eBPF 权限；`403`: 含他人 `PRIVATE` Agent 进程；`409`: 任一目标 Agent 已被其他任务租用（返回持锁人详情） | `traefik_record:start_session` |
| `POST` | `/api/recording-sessions/:id/stop` | `ScopeWriteGuard` | Path: `id`（发起人本人或 `traefik_admin` 可调用） | `200`: `RecordingSession`（停止采集、等待 `EVENT_SESSION_DRAINED`、执行离线归属对账并释放全部 Agent 租约）<br>`403/404`；`409`: 批次已处于终态 | `traefik_record:stop_session` |
| `DELETE` | `/api/recording-sessions/:id/processes/:processId` | `ScopeWriteGuard` | Path: `id, processId`（录制中单独热移除某进程监控，F-08.4） | `200`: `{"message":"进程监控已移除","session":RecordingSession}`<br>`403/404/409` | `traefik_record:stop_session` |
| `PATCH` | `/api/recording-sessions/:id/publish` | `ScopeWriteGuard` | Path: `id`（将本人合格的 `PRIVATE` 录制批次发布为团队 `SHARED` 素材） | `200`: `RecordingSession`（`scope` 更新为 `"SHARED"`）<br>`400`: 批次未完成；`403/404` | `traefik_record:publish_session` |
| `DELETE` | `/api/recording-sessions/:id` | `ScopeWriteGuard` | Path: `id`（`PRIVATE` 创建人或管理员可删；`SHARED` 仅管理员可删） | `200`: `{"message":"录制批次已删除"}`<br>`403/404`；`409`: 正在录制中或仍被 `ReplayTask` 引用 | `traefik_record:delete_session` |
| `GET` | `/api/recording-sessions/:id/consistency-report` | `ScopeReadGuard` | Path: `id`；Query: `refresh?` (`bool`) | `200`: `ConsistencyReportDTO`（含 `VERIFIED / VIOLATION / UNCOVERED / NOT_MONITORED / NOT_OBSERVABLE / UNATTRIBUTED` 六分类统计与边列表，见 [`03-architecture-elements-and-topology.md` §5.3](../02-design/03-architecture-elements-and-topology.md)）<br>`403/404` | - |
| `GET` | `/api/traffic-records` | `ScopeReadGuard` | Query: `session_id` (必填或按可见批次过滤), `scope?, subsystem_id?, module_id?, process_id?, direction?, exchange_type?, service_name?, operation?, sno?, trace_id?, keyword?, is_replayable?, page?, page_size?` | `200`: 标准分页 `items: []TrafficRecordSummary`（不加载 `raw_req_packet` / `raw_rsp_packet` `bytea` 列）<br>`400`: 筛选参数非法；`403`: `session_id` 为他人 `PRIVATE` 素材 | - |
| `GET` | `/api/traffic-records/:id/anatomy` | `ScopeReadGuard` | Path: `id`；Query: `session_id?` | `200`: `PacketAnatomyDrawerDTO`（含 `reqSide` 与 `rspSide` 四段完整解剖：24B 公共头位图、会话头与耗时、扩展头 Trace、Protobuf JSON 与 16B 对齐 Raw Hex，见 [`04-control-plane-and-web-console.md` §4.3](../02-design/04-control-plane-and-web-console.md)）<br>`403/404` | - |
| `POST` | `/api/traffic-records/:id/redecode` | `ScopeReadGuard` | Path: `id`（使用当前生效 `.pb` 版本按需重解码单条往返记录，F-03.4） | `200`: `PacketAnatomyDrawerDTO`<br>`400`: 无匹配生效 `.pb`；`403/404` | - |

---

### 2.6 跨 Agent 同构离线重放与双层回归 Diff 域 (`F-12`~`F-14`, `SC-07`, `SC-08`)

> 同构进程校验、二进制 Mock 自愈与双层 Diff 算法参见 [`02-data-plane-and-cros-rpc-engine.md` §3](../02-design/02-data-plane-and-cros-rpc-engine.md)、[`03-architecture-elements-and-topology.md` §3.3](../02-design/03-architecture-elements-and-topology.md) 及 [`04-control-plane-and-web-console.md` §1.3 & §3](../02-design/04-control-plane-and-web-console.md)。

| 方法 | 路由路径 | 守卫要求 | Query / Body 核心参数 | 响应 JSON 结构与状态码 | `AuditAction` (`module:action`) |
| :---: | :--- | :---: | :--- | :--- | :--- |
| `GET` | `/api/replay-tasks` | `ScopeReadGuard` | Query: `session_id?, target_process_id?, agent_id?, lifecycle_state?, result_status?, page?, page_size?` | `200`: 标准分页 `items: []ReplayTask` | - |
| `GET` | `/api/replay-tasks/:id` | `ScopeReadGuard` | Path: `id` | `200`: `ReplayTask`（含汇总计数与 `mock_events`）<br>`403/404` | - |
| `POST` | `/api/replay-tasks` | `JWT` | Body: `{name, session_id, source_process_id, target_process_id, selected_record_ids: uint64[], extra_ignore_paths?: string[], timeout_ms?: int}` | `201`: `ReplayTask`（校验素材可见性 + 目标 Agent 权限 + `ValidateReplayProcessCompatibility` 模块同构性 + 锁定目标 Agent `REPLAY` 排他租约并下发二进制重放包）<br>`400`: 目标进程未承载源请求所需公共模块、含不可重放截断帧或 Agent 无特权；`403`: 他人 `PRIVATE` 素材或他人 `PRIVATE` Agent；`404`: 资源不存在；`409`: 目标 Agent 正被其他录制/重放任务占用 | `traefik_replay:execute_replay` |
| `POST` | `/api/replay-tasks/:id/cancel` | `ScopeWriteGuard` | Path: `id`（任务发起人或 `traefik_admin`） | `200`: `ReplayTask`（调用 Agent `/replays/:taskId/cancel` 并释放 DB 排他租约）<br>`403/404`；`409`: 任务已结束 | `traefik_replay:cancel_replay` |
| `GET` | `/api/replay-tasks/:id/items` | `ScopeReadGuard` | Path: `id`；Query: `result_status?` (`PASS`\|`FAILED`\|`MOCK_MISS`\|`TIMEOUT`\|`ERROR`), `service_name?`, `operation?`, `page?`, `page_size?` | `200`: 标准分页 `items: []ReplayItem`（独立行存储，含 `field_diffs` 与 `mock_trace`）<br>`403/404` | - |
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
  6. `replay.completed`：重放任务整包完成汇总（`{task_id, lifecycle_state, result_status, total_requests, passed_requests, mock_hit_count, mock_miss_count}`）。

---

## 3. 主从管控与二进制流交互契约 (`Server <-> Agent`)

> 载荷结构体与 56B 二进制信封布局详见 [`01-system-architecture.md` §4.2 & §4.3](../02-design/01-system-architecture.md)。Agent 所有接口均受源 IP 白名单中间件（`--allow-ips`）保护，非白名单源 IP 统一返回 `403 Forbidden`。

| 方向 | 协议 / 方法 | Agent 路由路径 | 请求头 / 载荷契约 | 状态码与响应契约 |
| :---: | :---: | :--- | :--- | :--- |
| `Server -> Agent` | `GET` | `/agent/v1/handshake` | Header: `X-Claim-Code?` (纳管 `PRIVATE` Agent 时必传), `X-Expected-Lease-Type` (`""\|"RECORDING"\|"REPLAY"`), `X-Expected-Lease-Ref` (`uint64`) | `200`: `HandshakeResp`（若期望租约为 `""` 而本地持锁则触发自洁）<br>`403`: 源 IP 不在白名单或 `X-Claim-Code` 不匹配 |
| `Server -> Agent` | `GET` | `/agent/v1/processes/discover` | 无 | `200`: `{"items": []DiscoveredProcess}` |
| `Server -> Agent` | `PUT` | `/agent/v1/monitors/sync` | Body: `SyncMonitorReq{SessionID, MaxFrameBytes, MaxBufferBytes, Processes: []ProcessMonitorSpec}` | `200`: `{"diagnostics": []ProcessDiagStatus}`<br>`409`: 与 Agent 当前内存 `activeLease` 冲突 (`SingleTenantGuard`)<br>`503`: 缺少 `CAP_BPF` / `CAP_NET_ADMIN` 特权 |
| `Server -> Agent` | `DELETE` | `/agent/v1/monitors/:sessionId/processes/:processId` | Path: `sessionId, processId` | `200`: `{"removed":true,"remaining_processes":int}`（若剩余为 0 自动卸载探针并释放本地租约） |
| `Server -> Agent` | `POST` | `/agent/v1/monitors/:sessionId/stop` | Path: `sessionId` | `200`: `StopMonitorResp{CapturedFrames, DroppedFrames, BypassedSelfCount}`（触发异步排空后发 `EVENT_SESSION_DRAINED`） |
| `Server -> Agent` | `POST` | `/agent/v1/replays/execute` | Body: `ExecuteReplayReq{TaskID, ProcessSpec, TimeoutMs, IngressRequests, EgressMocks, MngMocks}`（纯二进制切片，零 `.pb`） | `200`: `{"status":"ARMED","diagnostics":ProcessDiagStatus}`<br>`409`: 本地租约冲突；`503`: 缺少 eBPF 特权 |
| `Server -> Agent` | `GET` | `/agent/v1/replays/:taskId/result` | Path: `taskId` | `200`: `ReplayTaskResultResp`；`404`: 任务不存在 |
| `Server -> Agent` | `POST` | `/agent/v1/replays/:taskId/cancel` | Path: `taskId` | `200`: `{"cancelled":true}`（立即断开注入连接、卸载重定向规则并释放本地租约） |
| `Server -> Agent` | `POST` | `/agent/v1/purge` | 无（主控启动恢复或管理员强制解锁调用） | `200`: `{"purged":true}`（幂等关闭代理/Mock 端口、`link.Close()` 卸载全部 eBPF 探针、清空 Maps、重置本地租约） |
| `Agent -> Server` | `WS (Binary)` | `/agent/v1/stream` | WebSocket 二进制帧：56B 大端 `EnvelopeHeader` (`Magic=0x4354 "CT"`, `ProtoVer=1`, `EventType=0x01..0x07`) + 变长载荷 | `101 Switching Protocols`（单帧读取上限 `MaxFrameBytes + 56 + 1024`） |
