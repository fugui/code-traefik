# Milestone 5 作战卡：微前端五大工作台、四段报文解剖抽屉与全链路交付验收

- **文档编号**：`06-m5-microfrontend-and-e2e-delivery.md`
- **文档版本**：v1.0.0
- **更新日期**：2026-10-29
- **归属阶段**：阶段三/四：M5 TDD 任务拆解与验收标准 (`04-tasks`)
- **覆盖特性与场景**：`F-04`(拓扑画布)、`F-15`、`F-16`(前端权限守卫)、`SC-01`~`SC-08` 全链路端到端回归
- **设计参考基线**：[`01-system-architecture.md` §1 & §6](../02-design/01-system-architecture.md)、[`04-control-plane-and-web-console.md` §4~§5](../02-design/04-control-plane-and-web-console.md)、[`01-http-api-contract.md` §2](../03-api/01-http-api-contract.md)

---

## 1. 交付包与文件清单 (Deliverable Packages & Files)

| 操作类型 | 包 / 文件路径 | 职责与实现范围（组件分层与 DTO 契约见设计文档链接） |
| :---: | :--- | :--- |
| **DELETE** | `frontend/src/pages/DashboardPage.tsx`<br>`frontend/src/pages/ItemListPage.tsx`<br>`main.go` (根目录旧入口) | 删除脚手架示例页面；将根入口迁移为 `cmd/server/main.go` 与 `cmd/agent/main.go` 同仓双入口 |
| **UPDATE** | `frontend/src/menu.ts`<br>`frontend/src/App.tsx`<br>`frontend/src/constants.ts`<br>`frontend/src/index.css` | 暴露 `./App` 与 `./menu` 联邦契约，注册 `/architecture`、`/traffic`、`/replay`、`/schemas`、`/agents`、`/admin/settings` 路由，兼容 `isEmbedded` 门户嵌入与 `StandaloneShell` 独立调试双模，全局采用 `--color-*` 语义 Token 与 Flat BEM 类名（见 [`04-control-plane-and-web-console.md` §4.1](../02-design/04-control-plane-and-web-console.md)） |
| **NEW** | `frontend/src/types/arch.ts`<br>`frontend/src/types/agent.ts`<br>`frontend/src/types/schema.ts`<br>`frontend/src/types/traffic.ts`<br>`frontend/src/types/replay.ts` | 完整 TypeScript 强类型接口定义（含 `PacketAnatomyDrawerDTO` 四段结构，64 位无符号整数统一定义为 `string`，全仓零 `any`，见 [`04-control-plane-and-web-console.md` §4.3](../02-design/04-control-plane-and-web-console.md)） |
| **NEW** | `frontend/src/api/sse.ts`<br>`frontend/src/api/{arch,agent,schema,traffic,replay}.ts` | 基于 `fetch` + `ReadableStream` 的 `/api/events` SSE 订阅器（通过 `Authorization: Bearer` Header 传参，严禁 URL Query 传 Token）及各领域 REST API 客户端 |
| **NEW** | `frontend/src/pages/ArchitectureWorkbenchPage/*`<br>`frontend/src/pages/AgentManagementPage/*`<br>`frontend/src/pages/ProtoSchemaWorkbenchPage/*` | 1. 架构中心（三级元素树、模块接口表、逻辑/物理静态拓扑画布、六分类一致性验证叠加层）；<br>2. 公私域 Agent 看板（`SHARED`/`PRIVATE` 标签页、排他租约徽章与强制解锁、进程探查与公共模块挂载器）；<br>3. `.pb` 描述集工作台（上传、版本切换、`{package}-{serviceName}` 映射检索与忽略路径编辑） |
| **NEW** | `frontend/src/pages/TrafficMonitorWorkbenchPage/*`<br>`frontend/src/pages/ReplayWorkbenchPage/*` | 4. 协同录制与流量解剖台（批次控制栏、A-01 重连提示、多维 URL 筛选器、右侧 `PacketAnatomyDrawer` 四段解剖抽屉：`CommHeadBitmapCard` / `SessionHeadDetailCard` / `ExtendHeadTraceCard` / `PayloadJsonHexSplitView`）；<br>5. 跨 Agent 重放与回归工作台（同构目标进程选择器、任务列表、`ReplayReportDrawer` 双层 Diff 与 Mock 命中时间线，见 [`04-control-plane-and-web-console.md` §4.2](../02-design/04-control-plane-and-web-console.md)） |
| **NEW** | `frontend/src/__tests__/PacketAnatomyDrawer.test.tsx`<br>`frontend/src/__tests__/ThemeAndPagination.test.tsx`<br>`frontend/src/__tests__/WorkbenchRBAC.test.tsx` | 前端四段报文解剖渲染、URL 5 页滑动窗口分页、亮/暗主题 Token 合规与普通用户/管理员视图隔离组件测试 |
| **UPDATE** | `cmd/server/main.go`<br>`Makefile`<br>`e2e_scenarios_test.go`<br>`docs/06-ops/01-verification-and-deployment-guide.md` | 内嵌 `frontend/dist`、挂载 CSP/Nosniff/Frame-Options 安全响应头与 `ServerRecoveryPipeline`；`Makefile` 支持同时构建 `code-traefik-server` 与 `code-traefik-agent`；编写覆盖 `SC-01`~`SC-08` 的端到端回归测试套件并同步运维手册 |

---

## 2. TDD 先行测试矩阵 (Test-First Matrix)

| 目标测试文件 | 测试函数名 | 输入场景与前置条件 | 预期断言结果 (Assertions) |
| :--- | :--- | :--- | :--- |
| `frontend/src/__tests__/PacketAnatomyDrawer.test.tsx` | `TestPacketAnatomyDrawerFourSections` | 传入预置的 `UNARY` 与 `STREAM_RSP` (3 段分片) `PacketAnatomyDrawerDTO`（含恶意 `<script>alert(1)</script>` 作为 `serviceName` 字符串），切换「请求帧 REQ」与「应答帧 RSP」标签 | 1. 准确渲染四段内容：① 24B 公共头偏移 `0..23` 位图色块、② 会话头版本 `V05 (256B)` 与耗时拆解、③ DOTR `TraceID`/`SpanID`/`FatherSpanID`、④ Protobuf JSON 树与 16B 对齐 Raw Hex 对照；<br>2. 恶意脚本字符串被 JSX 安全转义为纯文本，DOM 中零 `<script>` 节点 (`F-15.2`) |
| `frontend/src/__tests__/ThemeAndPagination.test.tsx` | `TestPaginationWindowAndThemeTokens` | 1. 在流量监控台、重放明细、Agent 列表与 `.pb` 映射表模拟 `total=500, page=20`，切换 `pageSize` 与筛选条件；<br>2. 扫描 `frontend/src/**/*.{tsx,ts,css}` 源码规则 | 1. 分页控件严格按 `[首页] [上一页] [18 19 [20] 21 22] [下一页] [末页] [15/25/50/100 条/页(默认 25)]` 渲染，无 `...` 省略号，且状态实时同步至 URL Search Params；<br>2. 源码零 `#fff`/`#000` 硬编码色值、零 `any` 类型、零 `dangerouslySetInnerHTML`/`innerHTML`、零原生 `alert()`/`confirm()` (`F-15.1`, `F-15.3`) |
| `frontend/src/__tests__/WorkbenchRBAC.test.tsx` | `TestPublicPrivateScopeAndAdminGuards` | 分别以普通认证用户（`is_module_admin: false`）与模块管理员（`is_module_admin: true`）上下文渲染五大工作台 | 普通用户下公共架构元素与 `.pb` 上传/编辑按钮隐藏或禁用，但「我的私有 Agent (PRIVATE)」创建/编辑、空闲 `SHARED` Agent 录制/重放发起按钮正常可用；管理员可见全部治理操作与强制解锁入口 (`F-16.1`) |
| `e2e_scenarios_test.go` | `TestAllScenarios_SC01_To_SC08`<br>*(`testdb.SetupIsolatedDB(t)`，绑定 `127.0.0.1`)* | 串联执行 `SC-01`（探针与公私域 RBAC）-> `SC-02`（三级架构建模、`.pb` 中划线服务名映射与静态拓扑）-> `SC-03`（Agent 纳管、排他租约 `409`、白名单 `403` 与离线自愈）-> `SC-04`（脏字节/碎片/粘包分帧与解码）-> `SC-05`（多进程协同录制与双模分区落盘）-> `SC-06`（六分类架构一致性验证）-> `SC-07`（跨 Agent 同构进程零 `.pb` 重放与 `ullSNO`/`aucTraceBuff`/`ullClientSendTime` 自愈 `PASS`）-> `SC-08`（状态码/字段突变 `FAILED` 与 `MOCK_MISS` 零外泄） | `SC-01` ~ `SC-08` 全部断言 100% 绿灯通过；测试结束后所有临时 `Server`、Mock `Agent` 与模拟业务 TCP 端口完全关闭，无残留后台进程或内核钩子 |

---

## 3. 验收门禁命令 (Acceptance Criteria)

```bash
# AC-M5-01: 前端静态安全红线扫描（零 any、零 dangerouslySetInnerHTML/innerHTML、零原生 alert/confirm）
! grep -rnE ":\s*any\b|as\s+any\b|dangerouslySetInnerHTML|\.innerHTML\s*=|\balert\(|\bconfirm\(" frontend/src/

# AC-M5-02: 前端 TypeScript 类型检查与 ESLint 零警告门禁
make lint

# AC-M5-03: 后端全量格式化、静态检查与 SC-01~SC-08 物理隔离数据库集成回归测试
TEST_DB_DSN="host=127.0.0.1 port=5432 user=postgres password=postgres dbname=postgres sslmode=disable" \
  make test

# AC-M5-04: 双二进制完整打包验证（code-traefik-server 内嵌前端 + code-traefik-agent）并清理产物
make build && make clean

# AC-M5-05: 端口与进程卫生检查（确保 :8091 与 :5181 零残留占用）
! ss -lptn 'sport = :8091 or sport = :5181' | grep -q LISTEN
```

---

## 4. 验收执行结果与交付结论 (Verification Results & Sign-off)

- **执行日期**：2026-10-29
- **验收结果**：
  1. **AC-M5-01** (前端静态安全扫描)：`PASS`，零 `any`、零 `dangerouslySetInnerHTML`/`innerHTML`、零原生 `alert()`/`confirm()`。
  2. **AC-M5-02** (前端 lint & 类型检查)：`PASS`，`tsc --noEmit` 与 `eslint` 100% 零错误零警告。
  3. **AC-M5-03** (`make test` 全量包测试与 SC-01~SC-08 E2E 隔离库测试)：`PASS`，全部 14 个 Go 包及 SC-01~SC-08 全链路端到端场景断言 100% 绿灯。
  4. **AC-M5-04** (双二进制打包与产物清理)：`PASS`，`code-traefik-server`（内嵌 `frontend/dist`）与 `code-traefik-agent` 成功编译构建并通过 `make clean` 清理。
  5. **AC-M5-05** (端口卫生检查)：`PASS`，`:8091` 与 `:5181` 物理端口零泄漏占用。
- **阶段结论**：M5 作战卡准出质量门禁全部通过，工程已具备全链路交付能力。
