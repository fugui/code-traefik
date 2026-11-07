# 架构与活体契约治理 (`code-traefik`) 文档知识库 (Docs Hub)

> **定位升级**：本项目已从纯技术视角的“业务流量录制工具”战略升维为**「基于真实流量的活体契约、架构一致性与防劣化自愈平台」**。录制与重放作为底层零侵入数据面手段，服务于**接口资产治理、架构防漂移雷达、无依赖自愈防劣化**三大核心业务价值。

本项目严格采用 **SDD (Spec-Driven Development，规范驱动开发) + TDD (Test-Driven Development，测试驱动开发)** 模式，所有提示词资产、需求边界、系统架构、数据表模型、API 契约、TDD 任务拆解与运维验证手册均在此目录中进行统一版本化管理。

---

## 目录分层架构

```text
docs/
├── 00-prompt/       # 提示词库：记录各阶段 SDD/TDD Prompt 资产与上下文演进指令
│   ├── 01-init-features-prompt.md                 # 阶段一：特性矩阵 (Feature Matrix) 与需求边界生成提示词
│   ├── 02-design-prompt.md                        # 阶段二：系统架构、GORM 模型与微前端交互设计提示词
│   ├── 03-api-and-tasks-prompt.md                 # 阶段三：API 契约冻结与 TDD Milestone 任务拆解提示词
│   ├── 04-tdd-implementation-prompt.md            # 阶段四：TDD 红-绿-重构编码实施与全链路交付验证提示词
│   └── 05-e2e-browser-test-and-ux-review-goal-prompt.md # 阶段四/验收：微前端全链路端到端黑盒测试与产品交互体验走查 Goal 提示词
├── 01-features/     # 特性与需求边界：特性清单矩阵 (Feature Matrix)、P0~P2 优先级与 Non-Goals
│   └── 01-feature-matrix.md                       # 阶段一：需求范围、核心特性矩阵 (F-01~F-27 含子特性) 与验收场景 (SC-01~SC-10)
├── 02-design/       # 系统技术架构 (SDD)：双子系统边界、数据面引擎、业务拓扑与数据模型
│   ├── 01-system-architecture.md                  # 【总览篇】系统总体架构、双模拓扑、主从信道与全链路时序
│   ├── 02-data-plane-and-cros-rpc-engine.md       # 【数据面篇】CROS RPC 协议栈、Agent eBPF 采集与自愈重放引擎
│   ├── 03-architecture-elements-and-topology.md   # 【业务篇】架构元素建模 (SSOT)、模块接口契约与拓扑验证
│   ├── 04-control-plane-and-web-console.md        # 【管理面篇】Server 控制面、微前端控制台、Protobuf 编解码与回归 Diff
│   ├── 05-database-schema.md                      # 【数据模型篇】17 张 traefik_* 物理表 GORM 字典、黄金套件快照、分区表与索引设计
│   └── 06-agent-observability-and-self-healing-design.md # 【运维可观测与自愈篇】Agent 探针特权感知、体检诊断与全链路闭环自愈设计
├── 03-api/          # 接口与数据契约：RESTful / SSE API 签名、主从信道契约、RBAC 权限与审计规范
│   └── 01-http-api-contract.md                    # 【API 契约】控制面 REST/SSE 字典、CI 门禁契约、主从 HTTP/WS 契约与 RBAC 守卫
├── 04-tasks/        # TDD 实施规划与验收：Milestone 1~17 依赖拓扑、任务作战卡与自动化验收标准 (AC)
│   ├── 01-roadmap.md                              # 【工程路线图】模板清理策略、M1~M17 DAG 拓扑、F/SC 覆盖矩阵与安全质量门禁
│   ├── 02-m1-crosrpc-and-domain-models.md         # 【M1 作战卡】协议栈 (pkg/crosrpc)、信封协议与 13 张表/分区管理器
│   ├── 03-m2-arch-proto-and-agent-control.md      # 【M2 作战卡】三级架构元素、.pb 动态编解码与公私域 Agent 排他租约管控
│   ├── 04-m3-agent-dataplane-ebpf-and-replay.md   # 【M3 作战卡】数据面 Agent 信道、cilium/ebpf 采集与零 .pb 二进制自愈重放
│   ├── 05-m4-recording-topology-and-diff-engine.md# 【M4 作战卡】多进程协同录制、离线架构一致性验证与双层回归 Diff 引擎
│   ├── 06-m5-microfrontend-and-e2e-delivery.md    # 【M5 作战卡】微前端五大工作台、四段报文解剖抽屉与 SC-01~SC-08 端到端交付
│   ├── 07-m6-hierarchical-topology-canvas.md      # 【M6 作战卡】分层复合架构拓扑与一致性画布（React Flow + ELK Worker）
│   ├── 08-m7-proto-schema-deepening-and-nested-contract-workbench.md # 【M7 作战卡】Protobuf 契约资产化治理、嵌套服务表与消息体字段树
│   ├── 09-m8-console-ia-restructuring-and-debug-workflow.md          # 【M8 作战卡】控制台三层信息架构重组、录制-调试-重放闭环与物理进程单点收口
│   ├── 10-m9-recording-drawer-and-process-tree-selection.md          # 【M9 作战卡】协同录制抽屉重构、作用域卡片解挤、Agent 状态防呆与业务进程树
│   ├── 11-m10-arch-process-blueprint-and-premount-workflow.md        # 【M10 作战卡】架构物理进程蓝图解耦、Agent 1:1 挂载覆盖与 eBPF 默认自动预挂待命工作流
│   ├── 12-m11-enhanced-proto-schema-asset-management.md              # 【M11 作战卡】增强 Protobuf 描述集资产管理、跨集依赖闭包解析与双模契约工作台
│   ├── 13-m12-bidirectional-capture-and-agent-recording-ux.md        # 【M12 作战卡】单进程开箱即用出入站双录、数据面链路修复与双工作台协同录制体验优化
│   ├── 14-m13-packet-causality-call-tree-and-sequence-workbench.md   # 【M13 作战卡】报文时序因果树、内联甘特瀑布、全链路时序泳道与事务闭包重放
│   ├── 15-m14-governance-dashboard-and-ia-restructuring.md           # 【M14 作战卡】平台级契约治理全景大盘、三维封顶健康指数与信息架构升维
│   ├── 16-m15-golden-contract-suites-and-curation-workbench.md       # 【M15 作战卡】黄金契约套件自包含快照库、双源重放解耦与事务固化工作台
│   ├── 17-m16-design-time-breaking-change-radar.md                   # 【M16 作战卡】设计态契约破坏性变更阻断雷达、下游血缘推导与回滚逃生舱
│   └── 18-m17-ci-cd-quality-gate-and-automated-enforcement.md        # 【M17 作战卡】CI/CD 自动化强阻断质量门禁、实测边回传与排他锁有界排队
├── 05-adr/          # 架构决策记录 (ADR)：重大技术选型与权衡记录 (Architecture Decision Records)
│   ├── 01-microfrontend-and-table-prefix-isolation.md                  # ADR-01：微前端集成、共享库表前缀隔离与 TDD 测试策略
│   ├── 02-ebpf-hook-and-dual-subsystem-adr.md                          # ADR-02：eBPF 入站旁路钩子、Cookie 原目的还原与无 Schema 数据面解耦
│   ├── 03-control-plane-state-recovery-and-lease-reconciliation.md     # ADR-03：控制面状态自愈恢复、Agent 双向租约对账与孤儿探针防御
│   ├── 04-traffic-storage-partitioning-and-relational-normalization.md # ADR-04：海量流量物理分区归档与核心关系规范化解耦
│   ├── 05-hierarchical-topology-canvas-redesign.md                     # ADR-05：分层复合架构拓扑画布重设计、去 Agent 物理建模与一致性比对融合
│   ├── 06-proto-schema-repository-and-field-inspection-adr.md          # ADR-06：Protobuf 契约资产化治理、嵌套服务表重构与消息体字段树 Diff 忽略设计
│   ├── 07-console-ia-restructuring-and-workflow-workbench-adr.md       # ADR-07：微前端控制台信息架构重组、“录制-调试-重放”高频工作流闭环与物理进程收口
│   ├── 08-recording-drawer-and-process-tree-selection-adr.md          # ADR-08：协同录制抽屉重构与进程树选择器设计（Drawer 侧边栏、Agent 选择防呆与业务分组树）
│   ├── 09-arch-process-ssot-and-ebpf-premount-standby-adr.md          # ADR-09：架构物理进程蓝图 SSOT 解耦、Agent 1:1 挂载覆盖与 eBPF 默认自动预挂待命架构
│   ├── 10-enhanced-proto-schema-file-management-adr.md                # ADR-10：增强 Protobuf 描述集管理：多 .pb 资产台账、跨集依赖闭包解析与全局单一生效池
│   ├── 11-single-process-bidirectional-capture-and-recording-ux-adr.md # ADR-11：单进程开箱即用出入站双向采集、数据面链路修复与双工作台协同录制体验优化
│   ├── 12-packet-causality-call-tree-and-sequence-ux-adr.md            # ADR-12：报文时序因果树、内联甘特瀑布、全链路时序泳道与事务闭包重放架构决策
│   ├── 13-traffic-debug-tree-metadata-hydration-and-ux-repair-adr.md   # ADR-13：独立流量调试台因果树元数据水合缺陷与链路展示降级修复决策
│   └── 14-governance-dashboard-golden-suite-and-ci-gate-adr.md        # ADR-14：契约治理全景大盘、黄金用例套件与 CI/CD 发布强阻断门禁架构决策
├── 06-ops/          # 交付与验证：双模联调、物理隔离数据库测试与生产环境单二进制部署手册
│   └── 01-verification-and-deployment-guide.md    # 联调验证、自动化回归测试与生产部署操作指南
└── 07-references/   # 外部参考与上位契约基准：协议包规范、字节字典与领域标准文档
    └── 01-cros-rpc-protocol-spec.md               # DOPHI CROS RPC 协议二进制报文格式与字段级规范说明
```

---

## 目录治理规则

1. **全链路双位编号递进**：
   - 一级子目录采用阶段双位数字前缀（`00-` ~ `07-`）；
   - 各阶段目录下的**所有 Markdown 文档必须带双位数字编号前缀**（如 `01-feature-matrix.md`、`01-system-architecture.md`、`01-http-api-contract.md`、`01-roadmap.md` ~ `09-m8-*.md`），保证在文件树、IDE 及文档中心中保持严格时序与逻辑有序性。
2. **规范先行与测试驱动 (SDD + TDD)**：
   - 任何业务代码实施前，必须先在 `01-features/`、`02-design/`、`03-api/` 与 `04-tasks/` 中冻结设计契约与验收标准 (AC)；
   - 实施阶段遵循 **TDD（先写测试 -> 运行失败 Red -> 编写实现通过 Green -> 重构优化 Refactor）**，所有数据库单测必须通过 `testdb.SetupIsolatedDB(t)` 实现物理隔离，eBPF 内核测试通过 `//go:build linux && ebpf` 隔离。
3. **决策可溯**：凡涉及关键架构选型、数据模型拆分、跨模块交互或性能权衡，须在 `05-adr/` 中以标准 ADR 模板记录背景与决策理由。
