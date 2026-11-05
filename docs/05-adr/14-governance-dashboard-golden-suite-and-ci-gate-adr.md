# ADR-14：契约治理全景大盘、黄金用例套件与 CI/CD 发布强阻断门禁架构决策

- **状态**：已批准 (Accepted)
- **日期**：2026-11-05
- **决策者**：CodeBench 架构委员会 / `code-traefik` 核心研发组
- **文档版本**：v1.0.0
- **关联基线**：[`01-feature-matrix.md`](../01-features/01-feature-matrix.md) (v2.12.0)、[`01-system-architecture.md`](../02-design/01-system-architecture.md) (v1.5.0)、[`03-architecture-elements-and-topology.md`](../02-design/03-architecture-elements-and-topology.md)、[`04-control-plane-and-web-console.md`](../02-design/04-control-plane-and-web-console.md)、[`05-database-schema.md`](../02-design/05-database-schema.md)、[`12-packet-causality-call-tree-and-sequence-ux-adr.md`](./12-packet-causality-call-tree-and-sequence-ux-adr.md) (ADR-12 v1.0.0)

---

## 1. 背景与问题陈述 (Context & Problem Statement)

随着系统战略定位全面升维为**「基于真实流量的活体契约与微服务架构治理平台」**，原以“报文抓包与回放”为核心的产品架构暴露出四个层次的断层，限制了其在企业级架构治理和质量门禁中的价值发挥：

1. **信息架构与产品心智断层（缺少宏观治理全景）**：
   - 系统左侧菜单与默认路由落地于 `/record`（流量录制），给用户带来“这只是一个底层网络抓包器”的错觉；
   - 架构师和研发主管缺乏一个自顶向下的**平台级治理大盘 (Governance Dashboard)**，无法直观掌握全系统的契约覆盖率、架构漂移违规率（`Violation` 数量趋势）、未认领孤儿契约数量以及整体微服务健康评分。
2. **测试资产沉淀断层（原始录制与权威基线脱节）**：
   - 现有的 `RecordingSession`（录制批次）包含大量非业务噪音、心跳轮询和偶发性异常数据，无法长期作为稳定的“黄金契约”资产；
   - 缺乏**黄金契约套件 (Golden Contract Suite)** 概念，无法将典型核心业务事务经过清洗、掩码和编排后固化为受版本管控的权威回归基线。
3. **设计态防线断层（缺乏 Protobuf AST 破坏性变更检测）**：
   - 现有的 `.pb` 描述集管理仅能检查依赖拓扑闭包和方法重名冲突（ADR-10），无法检测微服务升级时的 **Breaking Changes**（如篡改 Tag 编号、更改字段类型、未声明 `reserved` 即删除字段、更改枚举索引等）；
   - 破坏性契约容易蒙混过关流入测试环境，直到运行时解码或下游反序列化报错才暴露。
4. **研发生命周期断层（缺乏 CI/CD 流水线强阻断门禁）**：
   - 现有一致性核验与重放 Diff 依赖研发人员在 UI 界面人工发起和人工肉眼对比；
   - 缺乏面向流水线（GitLab CI / Jenkins / GitHub Actions）的自动化门禁 API，无法在代码合并或发版阶段对“架构漂移”或“契约回归 FAILED”实现自动化强阻断。

---

## 2. 架构决策细则 (Decision Details)

### 2.1 决策一：契约治理全景大盘 (`/dashboard`) 作为置顶默认首页与健康评分模型 (`F-24`)

1. **信息架构重组与默认入口迁移**：
   - 在 `menu.ts` 中新增置顶分组「全局概览」，将 `/dashboard` 设为全局第一项与默认落地页（`DEFAULT_ENTRY_PATH = /traefik/dashboard`）；
   - 将原「流量作业」升维重组为「契约核验与回归」，子项更名为：
     - `契约采集` (`/record`)：突出活体契约动态抽样心智；
     - `事务分析` (`/debug`)：突出全链路因果树与阶段耗时分析；
     - `防劣化回归` (`/replay`)：突出无下游因果自愈与双层断言；
     - `黄金用例库` (`/golden-suites`)：权威契约测试资产库。
2. **多维契约健康指数 (Health Score, 0~100 分) 加权模型**：
   $$Score = \max\left(0, 100 - (\text{ViolationCount} \times 15) - (\text{OrphanServices} \times 5) - (\text{RecentReplayFailures} \times 10)\right)$$
   - **架构漂移扣分**：每个未经报备的实测调用边（`Violation`）扣除 15 分；
   - **孤儿契约扣分**：每个未被架构功能模块认领的 Proto Service 扣除 5 分；
   - **回归防线扣分**：近 7 天内处于 `FAILED` 状态的重放任务扣除 10 分；
   - 根据分值划分评级：`🟢 优秀 (90~100)`、`🟡 良好 (75~89)`、`🟠 亚健康 (60~74)`、`🔴 高危 (<60)`，并生成结构化治理行动指南。
3. **四大核心视窗聚合设计**：
   - **健康指数卡片**：展示加权健康分、各扣分因子及环比趋势；
   - **架构漂移雷达**：汇总实测比对 `Verified` vs `Violation` 数量及违规进程 TOP 5，支持一键穿透 `/topology`；
   - **契约资产清零看板**：实时透视已纳管 Service 总数、架构认领率与未认领“孤儿契约”清单，支持一键跳转 `/schemas`；
   - **防劣化回归防线**：展示近 30 天门禁拦截次数、破坏性 Diff 分布与微服务回归稳定性。

---

### 2.2 决策二：黄金契约套件资产库 (`traefik_golden_suites` / `traefik_golden_suite_items`) 物理建模与事务资产化 (`F-25`)

1. **双层数据模型设计**：
   - `traefik_golden_suites`（套件主表）：
     - 维护 `id`、`code`（全局唯一）、`name`、`arch_process_id`（绑定的架构标准进程）、`version`（语义版本如 `v1.0.0`）、`status`（`ACTIVE | ARCHIVED`）、`created_by` 等元数据；
   - `traefik_golden_suite_items`（套件用例明细表）：
     - 关联 `suite_id` 与源 `traffic_record_id`，提取固化其专属的 `scoped_mocks_json`；
     - 增加 `mask_rules_json`（动态字段掩码规则，如忽略 token、动态时间戳断言）；
     - 维护 `order_no` 保证调用链或并发用例的严格执行序列。
2. **从 `/debug` 事务树一键固化流转**：
   - 在事务树行操作栏增加 `[🌟 沉淀为黄金契约]`，滑出固化抽屉；
   - 支持加入已有套件或创建新套件，自动打包该事务及其依赖的全部下游 `ScopedEgressMocks` 闭包，彻底剥离原始抓包会话的瞬时状态。
3. **套件脱机回归一键触发**：
   - 资产台账 `/golden-suites` 支持指定目标 Agent 环境，一键发起全量套件回归并生成标准报告，作为 CI/CD 自动化门禁的权威输入源。

---

### 2.3 决策三：设计态契约破坏性变更阻断雷达 (`Breaking Change Radar`, `F-26`)

1. **Protobuf AST 深度比对规则矩阵**：
   在上传新版 `.pb` 或触发 `batch-activate` 时，对比候选版本与当前生效版本的 `FileDescriptorProto`：

   | 违规类型 | 规则定义 | 阻断等级 |
   | :--- | :--- | :---: |
   | **TAG_MUTATION** | 既有字段的 Tag (Number) 发生变更，或原 Tag 被新命名字段复用 | **FATAL** |
   | **TYPE_INCOMPATIBLE** | 字段标量类型变更（如 `int32` ➔ `string`）、单值变更为 `repeated` 等 | **FATAL** |
   | **REMOVED_WITHOUT_RESERVE** | 删除了既有字段，但未在对应 Message 中声明 `reserved <tag>` | **FATAL** |
   | **ENUM_VALUE_ALTERED** | 枚举项的数值索引被修改，破坏枚举线缆序 | **FATAL** |
   | **OPTIONAL_ADDED** | 仅新增 optional/repeated 字段且 Tag 唯一未被保留 | **PASS** (向前兼容) |

2. **强阻断门禁机制**：
   - 存在上述任意 `FATAL` 级别改动时，直接拦截上传/激活，返回 HTTP `409 Conflict` 与结构化 JSON：
     ```json
     {
       "error": "BREAKING_CHANGE_DETECTED",
       "breaking_rules": [
         {
           "rule": "TAG_MUTATION",
           "message": "Field 'user_id' tag changed from 1 to 2 in Message 'AccountReq'",
           "service_name": "account.AccountService"
         }
       ],
       "affected_dependents": ["order_service", "billing_service"]
     }
     ```
3. **下游影响面消费者血缘推导**：
   - 服务端自动依据 `traefik_module_dependencies` 匹配出声明了依赖该 Service 的下游功能模块及负责人，在抽屉中以红色徽章清晰示警。

---

### 2.4 决策四：CI/CD 自动化强阻断质量门禁 (`POST /api/gates/evaluate`, `F-27`)

1. **门禁接口契约与鉴权**：
   - 路由：`POST /api/gates/evaluate`；
   - 鉴权：支持专属 CI 机器 Token 或管理员 JWT；
   - 请求体参数：
     ```json
     {
       "target_arch_process_id": 12,
       "env_agent_id": 3,
       "golden_suite_id": 5,
       "timeout_seconds": 60
     }
     ```
     （若未传入 `golden_suite_id`，系统自动取该进程当前处于 `ACTIVE` 状态的最新黄金套件）。
2. **双重强阻断判定逻辑**：
   - **第一关：架构一致性核验**：
     - 执行录制重放后的架构实测边比对，若产生任何 `Violation`（未声明调用），判定为 `GATED_VIOLATION`，立即返回 HTTP 422 阻断构建。
   - **第二关：黄金套件契约回归断言**：
     - 若重放结果状态包含 `FAILED`、`ERROR` 或 `MOCK_MISS`，判定为 `GATED_BREAKING_DIFF`，立即返回 HTTP 422 阻断构建。
   - **通过条件**：`Violation == 0` 且 `ReplayResult == PASS`，返回 HTTP 200 `PASSED`。
3. **格式化 Markdown 门禁凭据生成**：
   - 接口响应体输出原生 Markdown 文本，流水线脚本可直接将其作为 PR/MR Review Comment：
     ```markdown
     ### 🛡️ Code-Traefik 契约门禁报告: ❌ 阻断 (GATED_BREAKING_DIFF)
     - **被测进程**: `sut-order` (ID: 12) | **测试环境**: `SIT-Agent-01`
     - **健康分**: 65 (-35)
     - **架构一致性**: ✅ 0 Violation
     - **契约回归**: ❌ 1 个方法回包字段 Diff 破坏
       | 接口 | 字段路径 | 期望值 | 实际值 |
       | :--- | :--- | :--- | :--- |
       | `order.OrderService.CreateOrder` | `rsp.status` | `SUCCESS` | `PENDING` |
     > 🔗 [点击直达 Code-Traefik 独立调试台排查问题](http://192.168.56.18:8000/traefik/debug?task_id=88)
     ```

---

## 3. 影响与后续行动 (Consequences & Action Items)

### 3.1 正向收益
* 彻底打破“业务流量录制工具”的局限，让产品直接落位到“架构防腐透镜 + 研发流水线守门人”的高价值战略定位；
* 架构师有了全局可视化的治理抓手（大盘评分与漂移雷达），质量团队有了不可逾越的代码合并防线（CI 强阻断门禁）。

### 3.2 任务作战卡规划
- **M14**：信息架构升维、`/dashboard` 治理大盘与健康分引擎；
- **M15**：黄金契约套件物理建模、事务固化抽屉与套件回归工作台；
- **M16**：Protobuf AST 破坏性变更检测引擎与下游影响面推导；
- **M17**：CI/CD 自动化强阻断门禁 OpenAPI 与结构化报告生成。
