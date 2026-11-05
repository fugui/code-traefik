# ADR-13：独立流量调试台因果树元数据水合缺陷与链路展示降级修复决策

- **状态**：提议中 / 待评审 (Proposed / Under Review)
- **日期**：2026-11-05
- **决策者**：CodeBench 架构委员会 / `code-traefik` 核心研发组
- **文档版本**：v1.0.0
- **关联基线**：[`12-packet-causality-call-tree-and-sequence-ux-adr.md`](./12-packet-causality-call-tree-and-sequence-ux-adr.md) (ADR-12)、[`04-control-plane-and-web-console.md`](../02-design/04-control-plane-and-web-console.md)、[`14-m13-packet-causality-call-tree-and-sequence-workbench.md`](../04-tasks/14-m13-packet-causality-call-tree-and-sequence-workbench.md)

---

## 1. 背景与问题陈述 (Context & Problem Statement)

在 `M13` 里程碑落地「可折叠事务因果树与内联甘特瀑布」后，研发与测试人员在 `独立流量调试工作台` (`/debug`) 体验真实录制批次（如 `Create Order New (REC_20261104172559_788)`）时，反馈前两列表达信息晦涩，尤其是**「架构链路」列通篇呈现为 `- ➔ -`**，完全丧失了服务调用拓扑的指导价值：

```text
[层级 / 发起序 / 连接]          [架构链路]      [服务契约 & 接口方法]
▼ #1 Conn#1 • 1001              - ➔ -          GetCompositeQuote (demo.order.OrderService)
  • #2 Conn#2 • 1001            - ➔ -          GetItemStock (demo.inventory.InventoryService)
  • #3 Conn#3 • 1001            - ➔ -          GetBalance (demo.account.AccountService)
▼ #4 Conn#1 • 1002              - ➔ -          CreateOrder (demo.order.OrderService)
  • #5 Conn#2 • 1002            - ➔ -          GetItemStock (demo.inventory.InventoryService)
  • #6 Conn#2 • 1003            - ➔ -          LockStock (demo.inventory.InventoryService)
```

经深入核查数据库物理落盘数据、控制面水合算法以及录制编排器，定位到此现象是由 **3 处明确的代码缺陷** 与 **1 项采集维度的边界场景** 叠加所致：

### 1.1 缺陷 1：树构建水合漏传数据库实例，导致模块字典被完全跳过（核心缺陷）
- **现象**：虽然底层数据表 `traefik_traffic_records` 中准确落盘了 `src_module_id = 1` 与 `dst_module_id = 3`，且 `traefik_modules` 表中完整定义了名称（`1: 订单交易核心模块`、`2: 账户资产核心模块`、`3: 仓储库存履约模块`），但因果树接口返回的 `src_module_name` 与 `dst_module_name` 恒为空字符串 `""`。
- **根因**：在平铺列表接口中正常传入了 `db`，但在 `internal/server/callgraph/tree.go:24` 的 `BuildTransactionTrees` 内部，硬编码传入了 `HydrateRecords(nil, session, records)`（`db` 恒为 `nil`）。`HydrationCache.LoadFromDB` 触发 `if db == nil { return }` 空指针防御，直接跳过了从数据库加载模块字典与架构蓝图进程名的逻辑。

### 1.2 缺陷 2：录制编排固化快照时 Go 循环「值拷贝」失效
- **现象**：数据库会话表 `traefik_recording_sessions.process_snapshots` 固化的快照数据为 `[{"code": "", "name": "", "module_ids": [1], "process_id": 1, ...}]`，进程名与编码均为空。
- **根因**：在 `internal/server/recorder/orchestrator.go:116` 中使用了 `for _, p := range procs { p.FillFromArchProcess() }`。在 Go 语言中循环变量 `p` 是结构体值拷贝副本，修改并未写回原 `procs` 切片；后续从 `procs` 遍历组装 `ProcessSnapshotItem` 时读取到的 `p.Name` 依旧是初始空值。导致快照无法在 DB 查不到时作为二级保底。

### 1.3 缺陷 3：前端架构链路缺少外部调用与网络端点的兜底降级
- **现象**：前端在渲染无缝合链路时，直接写死为 `{src_module_name || peer_proc_name || '-'} ➔ {dst_module_name || process_name || '-'}`。
- **根因**：根事务（外部调用打入微服务）属于外部客户端调用，外部客户端不是拓扑中已注册的微服务模块，且未走探针内部管理握手（无 `peer_proc_name`）。此时左侧直接显示 `-`，缺乏对“外部客户端/调用源 IP”的语义化降级，最终形成生硬的 `- ➔ -`。

### 1.4 采集边界：单进程录制模式下的客观物理限制
- **现象**：未产生带有 `🔗` 标识与网络跳耗时高亮的跨进程缝合（`stitched_ingress_record` 为空）。
- **说明**：跨跳缝合算法（`StitchCrossHops`）需要同一逻辑调用的双端（上游 `EGRESS` + 下游 `INGRESS`）被同时录制到。该批次配置为 `target_process_ids = [1]`，仅勾选了订单服务单进程，下游库存/账户服务当时未被录制，数据集中没有对应的 `INGRESS` 帧，单端数据自然无法缝合。

---

## 2. 架构设计与修复决策细则 (Decision Details)

### 2.1 决策一：后端补齐 `db` 注入，彻底激活调用树元数据水合

1. **改造 `BuildTransactionTrees` 签名并透传数据库实例**：
   - 修改 `internal/server/callgraph/tree.go`：
     ```go
     // BuildTransactionTrees 构建调用树全流程（接收 db 实例用于水合模块与进程字典）
     func BuildTransactionTrees(
         db *gorm.DB,
         records []models.TrafficRecord,
         session *models.RecordingSession,
         filter *TreeFilterOptions,
     ) []TransactionTreeGroupDTO {
         if len(records) == 0 {
             return []TransactionTreeGroupDTO{}
         }
         // 1. 批量水合（传入真实 db 实例，允许按需补齐缺失的模块与进程信息）
         hydratedList := HydrateRecords(db, session, records)
         ...
     ```
2. **联动更新上层 Controller 与调用点**：
   - `handlers/traffic_handler.go:377`：将 Handler 持有的 `db` 传入 `callgraph.BuildTransactionTrees(db, records, &session, filter)`；
   - `internal/server/callgraph/sequence.go:30`：同步补齐 `BuildTransactionTrees(db, records, &session, nil)`；
   - `internal/server/callgraph/closure.go:56`：同步补齐 `BuildTransactionTrees(db, sessionRecords, &session, nil)`；
   - 保持兼容性：在单测（`callgraph_test.go`）中允许传入 `nil`，确保纯内存测试用例不受阻碍。

**决策收益**：**零数据迁移成本**！无需重新录制流量，所有历史录制素材在前端打开时，立刻能根据已有的 `src_module_id` / `dst_module_id` 从数据库自动查出真实的中文模块名。

---

### 2.2 决策二：修复录制编排器切片修改，固化真实蓝图进程快照

1. **重构 `orchestrator.go` 中的前置属性填充**：
   - 在 `internal/server/recorder/orchestrator.go:116` 将值拷贝循环修改为索引赋值：
     ```go
     // 修复前:
     // for _, p := range procs { p.FillFromArchProcess() }

     // 修复后: 使用索引遍历，直接修改 procs 切片中的结构体实体
     for i := range procs {
         procs[i].FillFromArchProcess()
         if procs[i].Agent == nil {
             return nil, fmt.Errorf("400 Bad Request: 进程 %s 未关联有效 Agent", procs[i].Name)
         }
         ...
     }
     ```
2. **固化快照质量闭环**：
   - 随后在组装 `models.ProcessSnapshotItem` 时，`p.Code` 和 `p.Name` 将取得蓝图中的有效名称（如 `order-service`、`订单交易服务`），并正确存入 `traefik_recording_sessions.process_snapshots` JSONB 字段。

**决策收益**：确保录制快照具备完全的架构自包含性，即便物理进程被卸载或更名，历史快照依然精准还原录制当时的主机与进程属性。

---

### 2.3 决策三：前端架构链路引入多级优雅降级策略，彻底消灭 `- ➔ -`

在 `TransactionCallTreeTable.tsx` 与 `TrafficRecordTable.tsx` 中抽象统一的格式化辅助函数，遵循自顶向下的层级兜底策略：

```text
调用方 (SRC) 优先级：
  [1] src_module_name (如: 订单交易核心模块)
  ➔ [2] peer_proc_name (探针握手自报进程名)
  ➔ [3] 若 direction == 'INGRESS' ➔ "外部客户端" (或 "外部客户端 (127.0.0.1)")
  ➔ [4] src_addr (如: 127.0.0.1:45184)
  ➔ [5] "未知源端"

接收方 (DST) 优先级：
  [1] dst_module_name (如: 仓储库存履约模块)
  ➔ [2] process_name (当前承载进程名，如: demo.order.OrderService)
  ➔ [3] dst_addr (如: 127.0.0.1:9001)
  ➔ [4] "未知目标"
```

#### 视觉呈现效果对比：
| 场景 | 当前现状（缺陷） | 修复后呈现效果 |
| :--- | :--- | :--- |
| **外部请求打入订单服务** (`#1`, `#4`) | `- ➔ -` | `外部客户端 ➔ 订单交易核心模块`（次文本显示 `demo.order.OrderService`） |
| **订单服务调用库存查询** (`#2`, `#5`) | `- ➔ -` | `订单交易核心模块 ➔ 仓储库存履约模块` |
| **订单服务调用账户扣减** (`#3`) | `- ➔ -` | `订单交易核心模块 ➔ 账户资产核心模块` |
| **同时录制双端（跨跳缝合）** | `- ➔ -` | `🔗 OrderService ➔ InventoryService`（蓝底高亮 + 悬浮网络跳时延） |

---

### 2.4 决策四：增强第一列「层级 / 发起序 / 连接」的易读性引导

针对用户反馈第一列 `▼ #1 Conn#1 • 1001` 初见难以理解的问题，保持高密度紧凑排版优势的同时，增加交互引导：
1. **表头 Tooltip 指南**：在表头 `层级 / 发起序 / 连接` 增加信息提示图标 `ℹ️`，鼠标悬浮展示清晰图例：
   - `▼ / ▶ / •`：事务层级与子调用折叠；
   - `#seq_no`：按物理捕获时间严格递增的全局发起序；
   - `Conn#k`：网络 Socket 连接紧凑短编号（便于识别多路复用与跨连接）；
   - `sno`：协议请求序列号。
2. **单元格细节气泡**：鼠标悬停在 `Conn#1 · 1001` 上时展示系统提示：`网络连接序号: 1 (长连接)，报文流水号: 1001`。

---

## 3. 验收标准与质量门禁 (Acceptance Criteria)

- [ ] **AC-01（后端树水合单测）**：在 `internal/server/callgraph/callgraph_test.go` 中编写用例，传入带有非空 `db` 的测试上下文，断言 `BuildTransactionTrees` 返回结果中每个节点的 `Record.SrcModuleName` 与 `Record.DstModuleName` 正确填充，不为 `""`。
- [ ] **AC-02（快照值拷贝单测）**：在 `internal/server/recorder/recorder_test.go` 中验证会话创建逻辑，断言固化到 `ProcessSnapshots` 中的每个条目其 `Code` 与 `Name` 必须非空。
- [ ] **AC-03（前端兜底与零 `- ➔ -` 保证）**：在前端单元测试中模拟无 `src_module_name` 的根事务报文，断言界面渲染包含 `外部客户端`，严禁出现 `- ➔ -` 字符串。
- [ ] **AC-04（构建全绿门禁）**：`make test && make lint && make build` 100% 零警告通过。

---

## 4. 评审重点与待确认事项 (Review Checklist & Feedback Requested)

请核心团队与同事在评审本 ADR 时重点确认以下三项：

1. **关于外部客户端的命名**：
   - 根事务 INGRESS 缺少源模块时，建议直接显示为 **`外部客户端`**（或附带 `src_addr`，如 `外部客户端 (127.0.0.1)`）。大家更倾向于简洁的 `外部客户端` 还是带 IP 的 `外部客户端 (IP:Port)`？
2. **是否需要写一次性历史会话快照修复脚本**：
   - 由于决策一实施后，历史批次已经可以通过 DB 实时水合出正确的中文模块名，历史快照中的空名字已被完全遮蔽覆盖。建议**无需执行数据库历史快照更新脚本**，保持历史表数据不可变，新录制批次天然生效。
3. **表头提示交互**：
   - 是否赞同在第一列表头增加 `ℹ️` 图标及悬浮解释 Tooltip？
