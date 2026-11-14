# M24 实施任务卡：可配置静态规范引擎、原生领域词汇表与三护栏 AST 重复结构检测 (`F-30`, `F-31`)

> **文档版本**：`v1.2.0`
> **创建日期**：`2026-11-13`
> **最后更新**：`2026-11-13`
> **所属阶段**：阶段四 — 研发实施与 TDD 门禁 (Implementation & TDD Gate)
> **里程碑编号**：`M24 — 安全底座、静态规范引擎、原生领域词库 (`BUILTIN_SEED`)、旁路 LLM 与三护栏 AST 重复消息检测`
> **覆盖需求范围**：`F-30`（可配置命名规则、原生领域词汇表与 `BUILTIN_SEED` 初始化、`3s` 旁路熔断 LLM、三护栏 AST 重复消息公共抽取建议）、`F-31`（规则/词库治理前端交互底座）、`SC-15`
> **前置依赖**：`M22`（`protocompile`）、`M23`（`SourceCodeInfo`）、[`../05-adr/18-proto-full-lifecycle-and-git-mr-governance-adr.md`](../05-adr/18-proto-full-lifecycle-and-git-mr-governance-adr.md) (`v1.3.0`)
> **当前状态**：✅ **已交付 (Delivered)**

### 修订记录

| 版本 | 日期 | 作者 | 变更说明 |
| :--- | :--- | :--- | :--- |
| `v1.0.0` | `2026-11-13` | Architecture Team | 初版发布：依据 ADR-18 (`v1.1.0`) 拆解 M24 交付计划 |
| `v1.1.0` | `2026-11-13` | Architecture Team | 依据 ADR-18 (`v1.2.0`) 检视修订：统一主密钥 env **`TRAEFIK_CRYPTO_KEY`**；`SafeDialer` 新增 `outbound_trusted_cidrs` 放行内网受信任服务及跨机 `code-notifier` / LLM；`handlers/settings.go` 强制过滤与 `***REDACTED***` 审计掩码；冻结 `CheckRule.suggestion_template`、`Jaccard >= 0.85` 与统一 `DuplicateGroupDTO` |
| `v1.2.0` | `2026-11-13` | Architecture Team | 依据 ADR-18 (`v1.3.0`) 清除外部旧库迁移设计：鉴于 `code-proto` 与 `protohub` 均未开发完成且从未运行过，彻底移除 `scripts/migrate_protohub_vocab`、`modernc.org/sqlite` 与 `PROTOHUB_IMPORT`；由 `models/db.go` 在启动时幂等预置通用基础词根（`EnsureBuiltinVocabularyTerms`, `BUILTIN_SEED`）与内置规范规则，M24 精简收敛为 P1~P3 三个清晰子阶段 |

---

## 1. 里程碑目标与三阶段交付拆解 (Phased Execution Plan)

M24 将静态设计规范、领域术语统一与跨服务重复结构体识别内聚收口至 `code-traefik` 控制面（因 `code-proto` 与 `protohub` 均未开发完成且从未真正运行过，无历史数据需迁移，全部底座直接原生构建、全仓零 SQLite）：
1. 在数据库中原生新增 4 张核心表（`traefik_check_rules`、`traefik_vocabulary_terms`、`traefik_check_reports`、`traefik_check_violations`），在 `models.SetupWithDB` 启动时幂等初始化兼容 `dophi.order.v1` 多段式包名与 `COMMENT_REQUIRED = WARNING` 的内置规则及高频通用缩写基础词根（`BUILTIN_SEED`）；
2. 构建 **`internal/server/security`**（`TRAEFIK_CRYPTO_KEY` AES-256-GCM 加解密与 `SafeDialer` SSRF 防护拨号器）与 **`internal/server/linter`**（正则表达式规则校验 + 词根切分查库 + `3s` 硬超时本地 TTL 缓存可选 LLM 降级 + 带三重防误报护栏的 AST 重复 Message 探测）；
3. 提供 `/api/check-rules`、`/api/vocabulary-terms`、`/api/check-reports{/scan,,/:id}` 全套接口，以及 `/schemas` 规则/词库抽屉与 `/admin/settings` 出站白名单/LLM 脱敏配置区。

本里程碑按 **3 个递进子阶段（P1 ~ P3）** 推进（每个阶段严格遵循 `Red -> Green -> Refactor` TDD 循环）：

```mermaid
flowchart LR
    P1["M24-P1: 4张静态检查表 + 内置规则/BUILTIN_SEED词库\n+ security (AES-GCM & SafeDialer) + 设置脱敏"] ==> P2["M24-P2: internal/server/linter\n多段包名/3s缓存LLM/3护栏AST重复 + HTTP API"]
    P2 ==> P3["M24-P3: LintRuleAndVocabDrawer + AdminSettings\n脱敏配置与端到端 Vitest 验收"]
```

| 子阶段 | 阶段名称 | 核心交付内容 | 估算改动规模 |
| :--- | :--- | :--- | :---: |
| **M24-P1** | **静态检查四表、内置规则/`BUILTIN_SEED` 词库与安全底座 (`security` + `settings.go`)** | 新增 `models/linter.go`（表 #25 `CheckRule` 含 `SuggestionTemplate`、表 #26 `VocabularyTerm` 含 `Aliases`/`DescriptionEn`/`SimilarTerms`/`Domain` 且 `Source ∈ {MANUAL, BUILTIN_SEED, LLM_SUGGESTED}`、表 #27 `CheckReport`、表 #28 `CheckViolation`）并接入 `models/db.go` 幂等迁移；实现 `EnsureBuiltinCheckRules`（`ON CONFLICT (rule_code) DO NOTHING` 预置 `PKG_NAMING` 支持 `^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*(\.v[0-9]+)?$` 多段兼容包名、`MSG_PASCAL_CASE`、`FIELD_SNAKE_CASE`、`ENUM_UPPER_SNAKE_WITH_PREFIX`、`COMMENT_REQUIRED` 默认 `WARNING` 不误杀、`VOCAB_STANDARD_TERM`、`DUPLICATE_MESSAGE` 默认 `WARNING`）与 `EnsureBuiltinVocabularyTerms`（`ON CONFLICT (term) DO NOTHING` 幂等预置通用标准缩写词根 `amt`/`ts`/`uid` 等，`source='BUILTIN_SEED'`、`is_approved=true`）；新建 `internal/server/security`（基于 env **`TRAEFIK_CRYPTO_KEY`** 的 AES-256-GCM 密文封装 `EncryptSecret`/`DecryptSecret`、**永久硬阻断云元数据 `169.254.0.0/16` / `100.100.100.200/32` 与组播/0.0.0.0/8**、默认拦截未授权回环/私网但支持管理员配置 **`traefik_settings.outbound_trusted_cidrs`** 白名单放行内网受信任主机含同内网跨机 `code-notifier`/GitLab/LLM、Dial `Control` IP 重检防 DNS 重绑定与禁止跨信任重定向的 `SafeDialer`、`ConstantTimeCompare` 辅助）；升级 `handlers/settings.go` 对保留密钥字段（`llm_api_key_enc`、`code_notifier_token_enc`、`smtp_password_enc`、`ci_gate_token_sha256`）在 `GET /api/admin/settings` 中强制过滤（仅返 `*_configured` 布尔标识），放宽 `outbound_trusted_cidrs` 长度至 `4096`，且向 `commonAudit` 写入 `***REDACTED***` 掩码绝不记录明文或密文；守卫 `cmd/server` 与 `cmd/agent` 全仓零 SQLite 依赖。 | ~700 LOC |
| **M24-P2** | **`linter` 引擎（规范、词汇、`3s` 缓存 LLM、三护栏 AST 重复）与 HTTP API** | 新建 `internal/server/linter`：① `rules_eval.go`：对 `protoreflect.FileDescriptor`（兼容 M22/M23 编译包与结构化文本解析）逐层执行正则/结构/注释校验并基于 `CheckRule.SuggestionTemplate` 输出精确 `(file_path, line, column, rule_code, severity, suggestion)`；② `vocab_llm.go`：按 `_` 与 camelCase 拆分标识符为词元检查 `traefik_vocabulary_terms`，命中 `aliases` 即建议标准词根；未命中生僻缩写先查进程内 `(token, vocab_hash)` TTL 缓存，开启 `llm_enabled` 时通过 `SafeDialer` 发起 `context.WithTimeout(..., 3*time.Second)` 请求并可选落库 `is_approved=false, source='LLM_SUGGESTED'` 待审词根；**LLM 超时/异常/未开启均自动降级记录 `TIMEOUT_FALLBACK`/`ERROR_FALLBACK`/`SKIPPED` 零阻塞**；③ `duplicate_ast.go`：读取真实 AST 字段类型与标准化字段名构成规范结构指纹，严格实施**三重防误报护栏**（仅检测 `field_count >= 3`；跳过同包跨版本演进如 `dophi.order.v1` vs `dophi.order.v2` 与已入 `BASE_LIB` 结构体；先按规范化类型签名倒排桶预筛候选对再计算 **Jaccard >= 0.85** 相似度并冻结输出统一 `[]DuplicateGroupDTO`）；新建 `handlers/lint_vocab_handler.go` 交付 `/api/check-rules*`、`/api/vocabulary-terms*`、`/api/check-reports{/scan,,/:id}`（对接 `commonAudit` `traefik_linter:*` 动作）。 | ~880 LOC |
| **M24-P3** | **规则/词库工作台抽屉、LLM 与出站白名单脱敏设置及 Vitest** | 扩展 `frontend/src/api/schema.ts`、`api/settings.ts`、`types/schema.ts`：对齐冻结版 `DuplicateGroupDTO` 与零 `any` TS 类型；新增 `frontend/src/pages/ProtoSchemaWorkbenchPage/LintRuleAndVocabDrawer.tsx`（规则矩阵启用/禁用/调级、内置规则防误删提示、标准词根与同义词/`description_en`/`domain` CRUD 维护、`LLM_SUGGESTED` 一键审核转正 `[✓ 纳为标准]`、手动全量/单仓扫描与重复 Message 抽提簇对比卡片）；更新 `frontend/src/pages/AdminSettingsPage.tsx` 挂载 `outbound_trusted_cidrs` 出站信任白名单（含跨机 GitLab / LLM / `code-notifier` 说明）与 LLM 开关/Endpoint/Model/只写不回显密钥占位；完善全量 Vitest（100% `--color-*` Token 与 `15/25/50/100` 默认 `25` URL 分页）。 | ~640 LOC |

---

### 1.1 交付物文件清单 (Deliverables Inventory)

| 端侧 | 动作 | 文件路径 | 职责说明 |
| :--- | :--- | :--- | :--- |
| **后端安全** | 新增 | `internal/server/security/crypto.go`、`ssrf.go` | AES-256-GCM 凭据加密、`SafeDialer` 出站 SSRF/DNS 重绑定校验拨号器、恒定时间比较封装 |
| **后端模型** | 新增/修改 | `models/linter.go` (新增)、`models/db.go`、`models/setting.go` | 表 #25~#28 (`CheckRule`, `VocabularyTerm`, `CheckReport`, `CheckViolation`) 及内置规则 / `BUILTIN_SEED` 词根初始化种子 |
| **后端引擎** | 新增 | `internal/server/linter/rules_eval.go`、`vocab_llm.go`、`duplicate_ast.go`、`scanner.go` | 多段命名/注释正则引擎、词元分词+缓存+3s 超时可选 LLM、三护栏倒排桶 AST 重复结构体探测器 |
| **后端接口** | 新增/修改 | `handlers/lint_vocab_handler.go` (新增)、`handlers/settings.go`、`handlers/routes.go` | `/api/check-rules*`、`/api/vocabulary-terms*`、`/api/check-reports*` 与 `GET/PUT /api/admin/settings` 密钥脱敏改造 |
| **后端测试** | 新增/修改 | `models/linter_seed_test.go`、`internal/server/security/crypto_ssrf_test.go`、`internal/server/linter/linter_test.go`、`duplicate_ast_test.go`、`handlers/lint_vocab_handler_test.go` | 覆盖 `SC-15`、内置种子幂等性、规则验证、三护栏去重、3s LLM 降级、`SafeDialer` 黑洞与全仓零 SQLite 断言 |
| **前端工作台** | 新增/修改 | `frontend/src/types/schema.ts`、`api/schema.ts`、`pages/ProtoSchemaWorkbenchPage/LintRuleAndVocabDrawer.tsx` (新增)、`pages/AdminSettingsPage.tsx` | 静态规则与词汇抽屉、重复消息簇对比抽提视图、脱敏 LLM 与出站白名单配置区 |
| **前端测试** | 新增 | `frontend/src/pages/ProtoSchemaWorkbenchPage/__tests__/LintRuleAndVocabDrawer.test.tsx` | 验证规则开关与 `WARNING` 提示、词汇 aliases 增删、LLM 待审词根转正与重复结构抽提卡片渲染 |

---

## 2. 核心技术设计细化与红线约束

1. **三护栏 AST 重复结构体检测算法 (`internal/server/linter/duplicate_ast.go`)**：
   - **数据结构**：从真实编译后的 `protoreflect.MessageDescriptor`（禁止使用脆弱文本正则，杜绝把所有类型退化为 `'unknown'` 的虚高误报）提取每个非 `map_entry` Message 的规范特征：
     - `FieldCount = len(msg.Fields())`
     - `PackagePrefix`（如去掉尾部 `.v[0-9]+$` 后的规范命名空间前缀）
     - `SchemaCategory`（所属描述集分类：`BASE_LIB | BUSINESS | GATEWAY`）
     - `Tokens = normalize(CanonicalTypeName + ":" + normalizeFieldName)`（使用真实 `Kind`/`FullName` + 小写归一化字段名，而非 `'unknown'`）。
   - **三重硬过滤护栏**：
     1. `FieldCount < 3` **强制跳过**（过滤掉 `Empty`、`IdReq`、`PageReq`、`CommonRsp` 等高频微小结构体）；
     2. 属于 **`BASE_LIB`** 制品内的公共 Message 之间及自身不重复报错（公共库本来就是被提取复用的目标）；两消息若同属相同归一化包前缀的相邻版本（如 `dophi.order.v1.OrderDTO` 与 `dophi.order.v2.OrderDTO`），**强制豁免**；
     3. 先建立 `token -> []MessageIndex` 倒排桶，仅当两消息共享至少 `max(2, ceil(0.5*minLen))` 个桶命中特征时才进入真实 Jaccard 集合重合度比较，默认统一阈值 **`similarity >= 0.85`**（输出统一结构 `DuplicateGroupDTO{cluster_hash, similarity, suggestion, members}`），单扫描硬设计算上限与 `context.Context` 取消检查。
2. **`TRAEFIK_CRYPTO_KEY`、`SafeDialer` 白名单与旁路 LLM / 配置审计红线**：
   - `TRAEFIK_CRYPTO_KEY` 用于派生 AES-256-GCM 密钥加密敏感凭据（缺省未配置密钥时仅允许非加密普通设置并明确报错拦截加密凭据写入，绝不回退硬编码弱密钥）；
   - `SafeDialer` **永久硬阻断**云元数据与链路本地段（`169.254.0.0/16`、`100.100.100.200/32`、`fe80::/10`）、组播与未指定段（`224.0.0.0/4`、`0.0.0.0/8`）；默认拒绝未列入 `traefik_settings.outbound_trusted_cidrs` 的回环（`127.0.0.0/8`、`::1`）与 RFC1918 私网/ULA（单测可显式注入白名单测试回环 Mock 端点）；同时**显式放行 `outbound_trusted_cidrs` 中声明的企业内网受信任主机/CIDR**（用于同内网 GitLab、内网 LLM 网关及**跨机部署的 `code-notifier` 机器**），并在 Dial `Control` 重检真实解析 IP 防御 DNS 重绑定与阻断跨信任重定向；
   - LLM 请求严禁阻塞 HTTP `/api/check-reports/scan` 或 M26 MR Webhook 主链路超过 `3s`；超时或 5xx 时立即记录 `LLMStatus = "TIMEOUT_FALLBACK" | "ERROR_FALLBACK"` 并完整返回确定性词典与规则结果；
   - `handlers/settings.go` 对 `llm_api_key_enc`、`code_notifier_token_enc`、`smtp_password_enc`、`ci_gate_token_sha256` 等凭据在 `GET /api/admin/settings` 中强制过滤，仅返 `*_configured: true|false`，且向 `commonAudit` 写 `***REDACTED***` 占位。

---

## 3. TDD 先行测试用例矩阵 (TDD Test-First Matrix)

| 测试文件 | 核心测试函数 (Red -> Green) | 断言覆盖范围与边界条件 |
| :--- | :--- | :--- |
| `models/linter_seed_test.go` | `TestModels_BuiltinRulesAndVocabSeedIdempotent` | 基于 `testdb.SetupIsolatedDB(t)` 连续两次执行 `SetupWithDB` / `EnsureBuiltinCheckRules` + `EnsureBuiltinVocabularyTerms`：验证 4 张表创建完整、多段包名与 `COMMENT_REQUIRED=WARNING` 默认规则就位、`BUILTIN_SEED` 基础词根（`source="BUILTIN_SEED"`, `is_approved=true`）无重复冲突报错；并验证 `VocabularyTerm.Source` 仅允许 `MANUAL \| BUILTIN_SEED \| LLM_SUGGESTED` |
| `internal/server/security/crypto_ssrf_test.go` | `TestTRAEFIKCryptoKey_SafeDialer_TrustedCIDRsAndAuditRedaction` | 验证 `169.254.169.254` 即使配置在白名单仍永久阻断；未授权 `10.0.0.1`/`127.0.0.1` 默认阻断；加入 `outbound_trusted_cidrs` 后可放行指定内网主机/CIDR（支撑跨机 `code-notifier` / GitLab / LLM）且跨信任重定向被拒；`TRAEFIK_CRYPTO_KEY` AES-256-GCM 密文加解密与 `settings.go` `***REDACTED***` 审计掩码验证 |
| `internal/server/linter/linter_test.go` | `TestLinter_MultiSegmentPkg_AndCommentRequiredWarningDefault` | 验证 `dophi.order.v1` 多段包名与带数字后缀命名 100% 通过；缺失字段/枚举注释触发 `COMMENT_REQUIRED` 且严重度为 `WARNING`；`SuggestionTemplate` 正确渲染；`FIELD_SNAKE_CASE` 违规触发 `ERROR` 与精确行号 |
| `internal/server/linter/linter_test.go` | `TestLinter_LLMFallbackOn3sTimeout_AndCacheHit` | 挂载绑定测试白名单 `127.0.0.1` 的慢速 Mock LLM（`Sleep 4s`）：断言扫查在 `<= 3.2s` 返回，主规则+词汇检查完整通过且 `LLMStatus == "TIMEOUT_FALLBACK"`；二次挂载快速 Mock（`200ms`）：命中内存 `(token, vocab_hash)` 缓存后零网络外呼 |
| `internal/server/linter/duplicate_ast_test.go` | `TestDuplicateASTDetector_ThreeGuardrailsZeroFalsePositive` | 构造对比集合：`IdReq` (`1` 字段)、`PageReq` (`2` 字段)、`BASE_LIB` `Money`、`dophi.order.v1.Order` vs `dophi.order.v2.Order`、以及两个不同服务中结构 `Jaccard >= 0.85` 且有 5 个字段的真实 `TradeSummary`/`DealSummary`（并验证不同类型同名字段不误判）；断言前 4 组 **0 误报**，跨服务真实重复组精确聚合为规范 `DuplicateGroupDTO` 且建议迁移至 `BASE_LIB` |
| `handlers/lint_vocab_handler_test.go` | `TestSC15_StaticLinterVocabSeedDuplicateGuardrailsAndLLMTimeout` | **SC-15 集成端到端**：验证内置规则禁止 `DELETE` (`409`) 仅允许 `PUT enabled=false`、`BUILTIN_SEED` 初始化后通过 API 维护词库 `aliases`/`similar_terms`/`description_en`/`domain` 与 `AuditLog` 记录、触发 `POST /api/check-reports/scan`、验证非管理员写规则返回 `403` 及 `/api/admin/settings` 密钥只写不返显（审计含 `***REDACTED***`） |
| `frontend/src/pages/ProtoSchemaWorkbenchPage/__tests__/LintRuleAndVocabDrawer.test.tsx` | `testLintRuleAndVocabDrawer_RuleToggleVocabApproveAndDuplicateClusters` | 验证规则开关调级、词汇新增与待审建议转正、手动扫描后渲染重复 Message 公共抽取建议与 `--color-*` 语义样式类 |

---

## 4. 自动化准出质量门禁与验收命令 (Exit Gate & Acceptance Commands)

```bash
# 1. cmd/server 与 cmd/agent 严禁引入 SQLite 运行时依赖
go list -deps ./cmd/server ./cmd/agent | grep -iE 'sqlite' && exit 1 || echo "OK: zero SQLite runtime in binaries"

# 2. M24 模型、安全模块、linter 引擎与 handler 竞争检测全绿
TEST_DB_DSN="host=127.0.0.1 port=5432 user=postgres password=CodeShield618! dbname=postgres sslmode=disable" \
  go test -race ./internal/server/security/... ./internal/server/linter/... ./handlers/... ./models/...

# 3. 全量标准工程门禁
make test && make lint && make build && make clean
```
