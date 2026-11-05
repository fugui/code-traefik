# ADR-14：契约治理全景大盘、黄金用例套件与 CI/CD 发布强阻断门禁架构决策

- **状态**：已批准 (Accepted)
- **日期**：2026-11-05
- **决策者**：CodeBench 架构委员会 / `code-traefik` 核心研发组
- **文档版本**：v1.1.0（经架构深度检视加固修订）
- **关联基线**：[`01-feature-matrix.md`](../01-features/01-feature-matrix.md) (v2.12.1)、[`01-system-architecture.md`](../02-design/01-system-architecture.md) (v1.5.0)、[`03-architecture-elements-and-topology.md`](../02-design/03-architecture-elements-and-topology.md)、[`04-control-plane-and-web-console.md`](../02-design/04-control-plane-and-web-console.md)、[`05-database-schema.md`](../02-design/05-database-schema.md) (v1.4.0)、[`01-http-api-contract.md`](../03-api/01-http-api-contract.md) (v1.4.0)、[`12-packet-causality-call-tree-and-sequence-ux-adr.md`](./12-packet-causality-call-tree-and-sequence-ux-adr.md) (ADR-12 v1.0.0)

---

## 1. 背景与问题陈述 (Context & Problem Statement)

随着系统战略定位全面升维为**「基于真实流量的活体契约与微服务架构治理平台」**，原以“报文抓包与回放”为核心的产品架构暴露出四个层次的断层，限制了其在企业级架构治理和质量门禁中的价值发挥：

1. **信息架构与产品心智断层（缺少宏观治理全景）**：
   - 系统左侧菜单与默认路由落地于 `/record`（流量录制），给用户带来“这只是一个底层网络抓包器”的错觉；
   - 架构师和研发主管缺乏一个自顶向下的**平台级治理大盘 (Governance Dashboard)**，无法直观掌握全系统的契约覆盖率、架构漂移违规率（`Violation` 数量趋势）、未认领孤儿契约数量以及整体微服务健康评分。
2. **测试资产沉淀断层（原始录制与权威基线脱节，且受 30 天分区清理威胁）**：
   - 现有的 `RecordingSession`（录制批次）包含大量非业务噪音、心跳轮询和偶发性异常数据，且底层 `traefik_traffic_records` 受 30 天按日分区 `DROP TABLE` 生命周期约束，无法长期作为稳定的“黄金契约”资产；
   - 缺乏独立于临时抓包分区的**黄金契约套件 (Golden Contract Suite)** 自包含快照模型，无法将典型核心业务事务经过清洗、掩码和编排后固化为永久可回放的权威回归基线。
3. **设计态防线断层（缺乏 Protobuf AST 破坏性变更检测与演进逃生机制）**：
   - 现有的 `.pb` 描述集管理仅能检查依赖拓扑闭包和方法重名冲突（ADR-10），无法检测微服务升级时的 **Breaking Changes**（如删除 Service/Method、篡改方法签名、篡改 Tag 编号、更改字段类型、未声明数值区间 `reserved` 即删除字段/枚举等）；
   - 破坏性契约容易蒙混过关流入测试环境，直到运行时解码或下游反序列化报错才暴露；同时在紧急故障回滚旧版 `.pb` 时又需要受审计管控的管理员逃生舱机制。
4. **研发生命周期断层（缺乏 CI/CD 流水线强阻断门禁与并发租约排队协调）**：
   - 现有一致性核验与重放 Diff 依赖研发人员在 UI 界面人工发起和肉眼对比，且纯重放模式下尚未回传实测出站调用边以同步校验架构 `Violation`；
   - 缺乏面向流水线（`code-pipeline` / GitLab CI / Jenkins / GitHub Actions）的自动化门禁 API、机器 Token 鉴权以及应对单机排他租约争抢的排队等待机制，无法在代码合并或发版阶段对“架构漂移”或“契约回归 FAILED”实现高可靠自动化强阻断。

---

## 2. 架构决策细则 (Decision Details)

### 2.1 决策一：契约治理全景大盘 (`/dashboard`)、分步菜单演进与三维封顶健康评分模型 (`F-24`)

1. **分阶段无死链信息架构重组与默认入口迁移**：
   - 在 `menu.ts` 中新增置顶分组「全局概览」，将 `/dashboard` 设为全局第一项与默认落地页（`DEFAULT_ENTRY_PATH = /traefik/dashboard`）；
   - 将原「流量作业」升维重组为「契约核验与回归」，按里程碑分两步平滑交付，**严禁在任何交付态暴露空壳页面菜单**：
     - **M14 交付态（4 组 9 项）**：置顶「全局概览」(`治理大盘 /dashboard`) + 「契约核验与回归」(`契约采集 /record`、`事务分析 /debug`、`防劣化回归 /replay`) + 「架构与契约」(`架构拓扑 /topology`、`架构元素 /architecture`、`Proto 契约 /schemas`) + 「环境与治理」(`Agent 与进程 /agents`、`模块参数配置 /admin/settings`)；
     - **M15 交付态（4 组 10 项）**：在「契约核验与回归」末尾正式挂载 `黄金用例库` (`/golden-suites`)；
     - 持续保留 `/traffic` 等历史旧深链的无损带参重定向。
2. **公私域严格隔离与三维封顶健康指数 (`Health Score`, 0~100 分)**：
   - **统计口径防污染红线**：全局健康分**仅统计 `SHARED` 公共录制批次与权威回归任务（`trigger_source IN ('GOLDEN_SUITE', 'CI_GATE')`）**，**严禁纳入研发个人 `PRIVATE` 录制素材或 `/debug` 临时单包试错重放 (`SINGLE_DEBUG`)**，避免个人联调噪音拉低平台评分；
   - **去重最新态口径**：架构漂移边按每个标准架构进程 (`ArchProcess`) 最近一次已完成 `SHARED` 核验（或近 30 天活跃去重违规边 `(src_module_id, service_name)`）统计，消除同一违规反复抓包导致的重复累加扣分；
   - **三支柱子分封顶公式**（根治大规模系统下单一维度恶化导致总分恒定归零失真的问题）：
     $$\text{Score} = S_{\text{arch}} + S_{\text{asset}} + S_{\text{reg}}$$
     - **架构防漂移子分 ($S_{\text{arch}} \in [0, 40]$)**：$S_{\text{arch}} = \max\left(0, 40 - \text{ActiveViolationEdges} \times 10\right)$；
     - **契约资产认领子分 ($S_{\text{asset}} \in [0, 30]$)**：$S_{\text{asset}} = \text{round}\left(30 \times \frac{\text{ClaimedActiveServices}}{\text{TotalActiveServices}}\right)$（不含 `0` Service 的 `BASE_LIB`；当系统尚无生效 Service 时默认给满分 `30`）；
     - **防劣化回归子分 ($S_{\text{reg}} \in [0, 30]$)**：$S_{\text{reg}} = \max\left(0, 30 - \text{UnhealthySuiteProcesses}_{7\text{d}} \times 10\right)$（统计近 7 天最新一次黄金套件/CI 门禁回归结果非 `PASS` 的标准架构进程个数）；
   - **四级健康评级**（与兄弟模块 `code-pipeline` 合规等级心智对齐）：`🟢 优秀 (90~100)`、`🟡 良好 (75~89)`、`🟠 亚健康 (60~74)`、`🔴 高危 (<60)`，并按失分最多的子维度自动生成结构化治理行动指南。
3. **四大核心视窗聚合设计**：
   - **健康指数卡片**：展示总分、三支柱子分拆解进度条及环比变化；
   - **架构漂移雷达**：汇总去重后实测 `Verified` vs `Violation` 边数及违规进程 TOP 5，支持一键穿透 `/topology`；
   - **契约资产清零看板**：实时透视生效 Service 总数、架构认领率与未认领“孤儿契约”清单，支持一键带参跳转 `/schemas?tab=catalog&claimed_status=UNCLAIMED`；
   - **防劣化回归防线**：展示近 30 天黄金套件与 CI 门禁拦截次数、破坏性 Diff 分布与进程回归稳定性。

---

### 2.2 决策二：黄金契约套件自包含快照建模 (`traefik_golden_suites` / `traefik_golden_suite_items`) 与分区生命周期解耦 (`F-25`)

1. **免疫 30 天分区 `DROP TABLE` 的双层自包含数据模型**：
   - **`traefik_golden_suites`（套件主表，表 16）**：
     - 维护 `id`、`code`（全局唯一）、`name`、`arch_process_id`（绑定的标准架构进程蓝图，`ON DELETE RESTRICT`）、`version`（如 `v1.0.0`）、`status`（`ACTIVE | ARCHIVED`）、`extra_ignore_paths`（套件级默认忽略字段路径 `jsonb`）、`description`、`created_by` 等元数据；
   - **`traefik_golden_suite_items`（套件自包含用例明细表，表 17）**：
     - **完全自包含深拷贝（Self-Contained Snapshot）**：由于 `traefik_traffic_records` 按日分区超过 30 天会执行物理 `DROP TABLE`，明细表**严禁在运行时回查源分区表读取报文**！沉淀时必须完整深拷贝固化：
       - 标识与溯源：`suite_id`（`ON DELETE CASCADE`）、`order_no`（严格执行序）、`name`、`source_session_id`（可空弱引用）、`source_record_id`（可空弱引用，仅作审计溯源）；
       - 协议与基线快照：`service_name`、`operation`、`baseline_status` (`U64Numeric`)、`rsp_seg_count`、`raw_req_packet` (`bytea` 完整入站请求帧)、`raw_rsp_packet` (`bytea` 完整预期应答帧)；
       - 下游因果闭包快照：`scoped_mocks` (`jsonb`，序列化保存该事务专属的 `[]agentproto.MockRuleDownstream{ReqPacket, RspPackets}` 成对二进制规则) 与 `egress_endpoints` (`jsonb`，历史下游 `IP:Port` 列表)；
       - 断言清洗规则：`mask_rules_json` (`jsonb`，用例级动态忽略字段路径与掩码策略，如忽略动态 token、流水号）。
2. **`ReplayTask` / `ReplayItem` 双源重放解耦**：
   - 将 `traefik_replay_tasks.session_id` 由非空改为可空 `*uint64`（`ON DELETE SET NULL`），新增 `golden_suite_id *uint64`（`ON DELETE SET NULL`）与 `trigger_source`（`MANUAL | SINGLE_DEBUG | GOLDEN_SUITE | CI_GATE`）；
   - `traefik_replay_items` 将 `record_id` 调整为兼容套件重放（新增 `suite_item_id *uint64`），确保即使原始 `RecordingSession` 被删除或底层流量分区被物理销毁，黄金套件依然 **100% 永久独立可回放与重算 Diff**。
3. **从 `/debug` 事务树一键固化与 `/golden-suites` 脱机回归**：
   - 在 `/debug` 事务树行操作栏增加 `[🌟 沉淀为黄金契约]`，滑出 680px 固化抽屉，支持勾选单笔或带前置状态依赖链的多笔根事务，自动打包各自的 `ScopedEgressMocks` 闭包写入套件；
   - `/golden-suites` 资产台账支持选定目标环境 Agent 一键触发套件全量脱机回归，作为 `F-27` CI/CD 门禁的唯一权威基线输入源。

---

### 2.3 决策三：设计态契约破坏性变更阻断雷达与管理员回滚逃生舱 (`Breaking Change Radar`, `F-26`)

1. **Protobuf AST 全维度深度比对规则矩阵**：
   在激活候选 `.pb`（`POST /api/proto-schemas/:id/activate`、`batch-activate` 或 `auto_activate=true` 上传）时，在事务咨询锁内对比候选版本与同名当前生效版本的 `FileDescriptorSet`（纯上传未激活草稿版本仅产出 `breaking_warnings` 提示而不拦截入库，当同名制品尚无生效版本时直接放行）：

   | 违规类型 | 规则定义（基于 `FileDescriptorProto` AST） | 阻断等级 |
   | :--- | :--- | :---: |
   | **SERVICE_OR_METHOD_REMOVED** | 已生效的 `Service` 或其中的 RPC `Method` 在新版中被直接删除 | **FATAL** |
   | **METHOD_SIGNATURE_CHANGED** | RPC `Method` 的入参/出参 Message 全名变更，或 `client_streaming` / `server_streaming` 模式变更 | **FATAL** |
   | **TAG_MUTATION** | 既有 Message 字段的数字 Tag (`Number`) 发生变更，或原 Tag 被其他字段名复用 | **FATAL** |
   | **TYPE_INCOMPATIBLE** | 字段类型或基数标签发生不兼容变异（如 `int32` ➔ `string`、单值 `optional` 变更为 `repeated`、`map<K,V>` 键值类型变更、`oneof` 拆并冲突） | **FATAL** |
   | **REMOVED_WITHOUT_RESERVE** | 删除了既有 Message 字段，但原数字 Tag **未落入新版 `DescriptorProto.GetReservedRange()` 闭区间内**（注：仅声明 `reserved "field_name"` 未保留数字 Tag 仍视为违规） | **FATAL** |
   | **ENUM_BREAKING_CHANGE** | 既有枚举项的数值索引被篡改，或删除了枚举项但原数值未落入 `EnumDescriptorProto.GetReservedRange()` | **FATAL** |
   | **BACKWARD_COMPATIBLE_ADD** | 新增 Service/Method、新增 `optional`/`repeated` 字段或枚举项且 Tag 唯一未被保留 | **PASS** |

2. **强阻断门禁与管理员审计逃生舱 (`allow_breaking`)**：
   - 默认（`allow_breaking != true`）检出任意 `FATAL` 改动时，硬性回滚激活并返回 HTTP `409 Conflict`：
     ```json
     {
       "error": "检测到破坏性 Protobuf 契约变更，已阻断激活",
       "code": "BREAKING_CHANGE_DETECTED",
       "breaking_rules": [
         {
           "rule": "TAG_MUTATION",
           "proto_file": "account/v1/account.proto",
           "symbol": "account.v1.AccountReq.user_id",
           "message": "字段 'user_id' 的 Tag 从 1 变更为 2",
           "service_name": "account.v1.AccountService"
         }
       ],
       "affected_dependents": [
         {
           "module_id": 14,
           "module_code": "ORDER_CORE",
           "module_name": "订单核心模块",
           "arch_process_code": "sut-order",
           "owner_name": "李四"
         }
       ]
     }
     ```
   - **故障回滚与架构重构逃生舱**：考虑到生产故障紧急将 `.pb` 从 `v2` 回滚至 `v1`（反向对比会触发 `REMOVED_WITHOUT_RESERVE`）或经架构评审批准的大版本重构，`traefik_admin` 可在激活请求中显式携带 **`allow_breaking: true` 且必填 `breaking_reason`（长度 `8..500` 字符）** 强制放行；后端校验通过后放行激活，并强制写入 `P1` 级高危操作审计日志（记录完整 `breaking_rules` 与授权原因）。
3. **下游消费者影响面血缘推导**：
   - 服务端递归扫描引用了受损 Message 的自有 Service，再关联 `traefik_module_services` 与 `traefik_module_dependencies`，精准提取所有声明了 `DEPEND` 的下游功能模块、所属标准进程及模块负责人，在拦截抽屉中醒目示警。

---

### 2.4 决策四：CI/CD 自动化强阻断门禁、重放实测边核验与排他锁排队 (`POST /api/gates/evaluate`, `F-27`)

1. **门禁接口契约、安全鉴权与有界排队参数**：
   - 路由：`POST /api/gates/evaluate`；
   - **双模安全鉴权 (`CIOrAdminGuard`)**：
     - 支持平台管理员 `Authorization: Bearer <JWT>`，或专用请求头 `X-Traefik-CI-Token: <TOKEN>`（**严禁在 URL Query 中传递 Token，严禁源码硬编码默认密钥，严禁日志打印明文 Token**）；
     - CI 机器 Token 由管理员通过控制台生成/轮换，服务端仅在 `traefik_settings`（键 `ci_gate_token_sha256`）持久化 SHA-256 哈希值，鉴权时使用 `crypto/subtle.ConstantTimeCompare` 常量时间比对，并挂载滑动窗口速率限制；
   - 请求体参数：
     ```json
     {
       "target_arch_process_id": 12,
       "env_agent_id": 3,
       "golden_suite_id": 5,
       "timeout_seconds": 60,
       "wait_lease_seconds": 60
     }
     ```
     - 若未传入 `golden_suite_id`，自动选取 `target_arch_process_id` 下最新处于 `ACTIVE` 状态的黄金套件（若该进程尚无有效黄金套件则返回 `404 SUITE_NOT_FOUND`）；
     - **`wait_lease_seconds`（默认 `60`，范围 `0..300`）**：当多个 MR 流水线并发请求同一共享测试环境 `env_agent_id` 遇到短期 `REPLAY` 排他租约占用时，服务端在窗口内执行指数退避排队重试获取租约，彻底消除并发 CI 互相踩踏导致的偶发 `409` 构建失败。
2. **数据面实测出站边回传与双重强阻断判定（严格区分 `422` 代码缺陷 vs `503` 环境异常）**：
   - **主从协议补齐 (`ObservedEgressCalls`)**：Agent 在执行重放期间（零 `.pb` 依赖），由 `EgressProxy` / `MockServer` 基于 `pkg/crosrpc` 解析被测进程实际发出的所有下游请求头，在 `ReplayTaskResultResp` 中回传去重聚合后的 `ObservedEgressCalls []ObservedEgressCall{ServiceName, Operation, DstAddr, CallCount, MatchStatus}`（含 `SCOPED_HIT`、`GLOBAL_FALLBACK_HIT` 与 `MOCK_MISS`）；
   - **环境前置异常 (`503 Service Unavailable`)**：若目标 Agent 离线、排他锁等待超 `wait_lease_seconds` 仍繁忙（`GATED_ENV_BUSY`）、或目标进程处于 `WAITING_PROC / NEED_RECONNECT`（`GATED_ENV_NOT_READY`），返回 HTTP `503`，提示流水线自动重试环境而非误判代码违规；
   - **第一关：架构防漂移强阻断 (`422 GATED_VIOLATION`)**：主控调用 `topology.VerifyProcessObservedCalls` 将 `ObservedEgressCalls` 与 `target_arch_process_id` 装载模块的静态 `DEPEND` 契约集比对；凡发现被测进程调用了未经报备的下游 `ServiceName`，立即判定为 `GATED_VIOLATION`，返回 HTTP `422` 阻断合并；
   - **第二关：黄金套件防劣化强阻断 (`422 GATED_BREAKING_DIFF`)**：若重放断言结果非 `PASS`（含协议状态码非零、未命中忽略/掩码路径的 Protobuf 字段级 `FAILED`、`MOCK_MISS`、`TIMEOUT` 或 `ERROR`），判定为 `GATED_BREAKING_DIFF`，返回 HTTP `422` 阻断合并；
   - **准出通过 (`200 PASSED`)**：当且仅当 `ViolationCount == 0` 且套件全部用例 `ResultStatus == PASS` 时，返回 HTTP `200`。
3. **结构化 JSON + 原生 Markdown PR/MR 门禁凭据响应**：
   - 接口返回结构化 JSON（含 `gate_status`、`task_id`、`violations`、`failed_items` 与 `markdown_report`），流水线可直接提取 `markdown_report` 回贴至 GitLab MR / CodeArts MR / GitHub PR 评论区：
     ```markdown
     ### 🛡️ Code-Traefik 契约门禁报告: ❌ 阻断 (GATED_BREAKING_DIFF)
     - **被测标准进程**: `sut-order` (ID: 12) | **验证环境**: `SIT-Agent-01` | **黄金套件**: `订单核心黄金基线 v1.0.0`
     - **架构一致性核验**: ✅ 0 Violation（实测 4 条下游依赖边全部合规）
     - **黄金契约回归**: ❌ 1 / 8 笔事务发生破坏性变更
       | 接口契约 | 字段路径 | 基线期望值 | 实测返回值 |
       | :--- | :--- | :--- | :--- |
       | `order.v1.OrderService.CreateOrder` | `rsp.status` | `SUCCESS` | `PENDING` |
     > 🔗 [点击直达 Code-Traefik 事务分析台排查根因](http://192.168.56.18:8000/traefik/debug?replay_task_id=88)
     ```

---

## 3. 影响与后续行动 (Consequences & Action Items)

### 3.1 正向收益
* **架构自洽性**：黄金套件与 30 天流量物理分区彻底解耦，单次重放同时闭环“架构漂移 `Violation` 核验 + 双层语义 `Diff` 断言”，多流水线并发共享环境平稳排队零误报；
* **治理刚柔并济**：常态化死守 AST Breaking 与 CI 门禁红线，紧急回滚与架构大版本重构具备受 P1 审计监管的管理员逃生通道。

### 3.2 任务作战卡规划
- **M14**（[`15-m14-governance-dashboard-and-ia-restructuring.md`](../04-tasks/15-m14-governance-dashboard-and-ia-restructuring.md)）：4 组 9 项信息架构升维、`/dashboard` 治理大盘与三维封顶健康分引擎；
- **M15**（[`16-m15-golden-contract-suites-and-curation-workbench.md`](../04-tasks/16-m15-golden-contract-suites-and-curation-workbench.md)）：黄金契约套件自包含快照建模（17 表升级）、事务固化抽屉、双源重放解耦与 `/golden-suites` 工作台（补齐第 10 项菜单）；
- **M16**（[`17-m16-design-time-breaking-change-radar.md`](../04-tasks/17-m16-design-time-breaking-change-radar.md)）：Protobuf AST 全维度破坏性变更检测引擎、下游消费者血缘推导与 `allow_breaking` 审计逃生舱；
- **M17**（[`18-m17-ci-cd-quality-gate-and-automated-enforcement.md`](../04-tasks/18-m17-ci-cd-quality-gate-and-automated-enforcement.md)）：Agent 重放实测出站边回传、`X-Traefik-CI-Token` 机器鉴权、排他锁有界排队与 `POST /api/gates/evaluate` 自动化门禁交付。
