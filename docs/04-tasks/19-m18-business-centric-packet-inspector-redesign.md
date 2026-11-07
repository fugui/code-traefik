# M18 作战卡：面向业务视角的 RPC 报文调用透视与双模解剖抽屉重构

- **里程碑编号**：`M18`
- **状态**：✅ 已交付 (Completed)
- **前置依赖**：`M13`、`M17` 已交付
- **关联特性**：`F-15.2` (业务视角升维)
- **关联设计规范**：[`04-control-plane-and-web-console.md` §4.7](../02-design/04-control-plane-and-web-console.md)

---

## 1. 里程碑背景与设计哲学

在微服务协同录制与流量调试中，原有的「CROS RPC 深度报文四段解剖抽屉」深度聚焦于底层二进制网络封包（24B 公共头位图、字段字节偏移、十六进制 Raw Hex），导致**排查业务 Bug、联调接口的业务开发人员**面临极高的认知负荷：
1. **核心诉求倒置**：业务人员最关心的“传了什么入参、返回了什么结果、报错原因与耗时在哪”，被埋在最底部的第 4 段；
2. **请求与应答割裂**：必须在 REQ 和 RSP 之间来回切换 Tab，无法同屏比对入参与出参；
3. **噪音过多**：大量 C/C++ 内存对齐结构体变量（如 `usVersion`, `usHeadLen`, 偏移范围 `24..25`）干扰业务阅读。

本里程碑通过 **SDD (规范驱动) + TDD (测试驱动)** 研发流水线，彻底重构抽屉交互与视觉架构：
- **业务第一**：默认突出服务/方法全称、执行状态徽标（成功绿色/业务错误显眼标红）、TraceID 一键复制、架构链路与往返总耗时；
- **双栏孪生对比 (Split View)**：请求入参 (Request) 与 返回结果 (Response) 左右并排同屏展示，带 JSON 树高亮与一键复制；
- **微秒级耗时甘特拆解 (Waterfall Bar)**：直观拆解网络传输、队列排队与服务端业务执行；
- **底层协议专家模式 (渐进式折叠)**：将 24B CommHead 位图、SessionHead 字段表、ExtendHead 原始段与 16B Hex 对照完整收纳在默认折叠区，点击可无损展开，保持底层排错能力 100% 完整与测试向后兼容。

---

## 2. 交付组件清单与架构分层

| 目录/文件 | 职责与技术规范 |
| :--- | :--- |
| `frontend/src/components/PacketAnatomyDrawer/PacketAnatomyDrawer.tsx` | 抽屉外壳与无模态嵌入容器，支持模式切换与单包重放动作插槽 |
| `frontend/src/components/PacketAnatomyDrawer/BusinessSummaryBar.tsx` | 顶部业务调用概览条（点分服务/方法大标题、状态码胶囊、TraceID 一键复制、往返耗时与链路） |
| `frontend/src/components/PacketAnatomyDrawer/BusinessPayloadCompareView.tsx` | 核心业务孪生视窗：请求入参 vs 返回结果左右双栏并排（JSON 高亮、格式化、折叠、独立复制） |
| `frontend/src/components/PacketAnatomyDrawer/RpcLatencyWaterfallBar.tsx` | 微秒级阶段耗时彩色甘特条（传输 / 排队 / 服务端处理比例计算与诊断说明） |
| `frontend/src/components/PacketAnatomyDrawer/AdvancedProtocolAccordion.tsx` | 底层协议与十六进制专家排错手风琴折叠区（默认折叠，解析异常自动展开，内嵌原有 4 段组件） |
| `frontend/src/components/PacketAnatomyDrawer/CommHeadBitmapCard.tsx` | ① 24B 公共头位图与校验（维持原有能力） |
| `frontend/src/components/PacketAnatomyDrawer/SessionHeadDetailCard.tsx` | ② 会话头结构与字段表（维持原有能力） |
| `frontend/src/components/PacketAnatomyDrawer/ExtendHeadTraceCard.tsx` | ③ 扩展头链路与排队耗时（维持原有能力） |
| `frontend/src/components/PacketAnatomyDrawer/PayloadJsonHexSplitView.tsx` | ④ 载荷解析与 16B 对齐十六进制对照（维持原有能力） |
| `frontend/src/index.css` | 针对业务视图组件补充全新 Design Tokens 样式类（严格使用 `--color-*` 语义 Token） |

---

## 3. TDD 测试先行清单 (Test Cases)

在 `frontend/src/__tests__/PacketAnatomyDrawer.test.tsx` 中编写并通过以下测试断言：

1. **TC-INSPECT-01 业务调用概览渲染**：
   - 验证渲染点分 Service 与 Operation 大标题（如 `demo.inventory.InventoryService / GetItemStock`）；
   - 验证成功状态显示绿色徽章，业务错误（`uiStatusCode !== 0`）醒目渲染红色错误胶囊并展示错误码与原因；
   - 验证往返耗时（μs/ms）与架构链路节点显示正确；
   - 验证 DOTR TraceID 存在时提供复制按钮与联动筛选入口。
2. **TC-INSPECT-02 业务请求入参与返回结果并排对比**：
   - 验证左栏包含请求入参 JSON，右栏包含返回结果 JSON，无需点击 Tab 即可同时可见；
   - 验证 `ONEWAY` 单向通知或 `REQ_ONLY` 未收到应答时右栏显示优雅空态占位。
3. **TC-INSPECT-03 微秒阶段耗时甘特拆解**：
   - 验证根据 `timingBreakdownUs` 正确计算客户端传输、队列排队与服务端处理耗时占比并渲染彩色条形。
4. **TC-INSPECT-04 底层协议专家折叠区与向后兼容**：
   - 验证折叠区组件包含原有 ① 24B 公共头位图、② 会话头结构、③ 扩展头 Trace、④ 16B Hex 对照视窗，保持既有测试断言 100% 通过；
   - 当 `decodeStatus === 'NO_SCHEMA_RAW_HEX'` 时折叠区自动展开。
5. **TC-INSPECT-05 安全转义与 XSS 防御**：
   - 包含 `<script>alert(1)</script>` 恶意的服务名、方法名或 JSON 键值在业务视图与折叠区均被安全转义为 HTML 实体，严禁执行。
6. **TC-INSPECT-06 深浅主题适配与 Design Tokens 校验**：
   - 组件样式全部采用 `--color-*` 语义变量，无内联硬编码色值。

---

## 4. 准出质量门禁命令 (Exit Gate)

```bash
cd frontend && npm test
make lint && make build && make clean
```

---

## 5. 交付验证结果 (Verification Summary)

1. **前端自动化测试 (`vitest run`)**：全量 22 个测试套件、76 个用例 100% 全部通过；`PacketAnatomyDrawer.test.tsx` 涵盖业务概览、入参出参同屏双栏对比、非零错误高亮、微秒级阶段耗时拆解、底层协议折叠展开及 XSS 实体转义防注入；
2. **TypeScript 类型安全与 ESLint 门禁 (`make lint`)**：`tsc --noEmit && eslint` 零警告通过，全量遵循零 `any` 契约；
3. **前端与单二进制构建 (`make build`)**：Vite 5.4 生产打包成功，Go `cmd/server` 单二进制打包成功；
4. **主题规范与 Design Tokens 审查**：100% 使用 `--color-*` 语义变量，亮暗模式完美契合，无内联硬编码绝对颜色。
