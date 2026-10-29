# 业务流量监控与重放 (`code-traefik`) 文档知识库 (Docs Hub)

本项目严格采用 **SDD (Spec-Driven Development，规范驱动开发) + TDD (Test-Driven Development，测试驱动开发)** 模式，所有提示词资产、需求边界、系统架构、数据表模型、API 契约、TDD 任务拆解与运维验证手册均在此目录中进行统一版本化管理。

---

## 目录分层架构

```text
docs/
├── 00-prompt/       # 提示词库：记录各阶段 SDD/TDD Prompt 资产与上下文演进指令
│   ├── 01-init-features-prompt.md         # 阶段一：特性矩阵 (Feature Matrix) 与需求边界生成提示词
│   ├── 02-design-prompt.md                # 阶段二：系统架构、GORM 模型与微前端交互设计提示词
│   ├── 03-api-and-tasks-prompt.md         # 阶段三：API 契约冻结与 TDD Milestone 任务拆解提示词
│   └── 04-tdd-implementation-prompt.md    # 阶段四：TDD 红-绿-重构编码实施与全链路交付验证提示词
├── 01-features/     # 特性与需求边界：特性清单矩阵 (Feature Matrix)、P0~P2 优先级与 Non-Goals
│   └── 01-feature-matrix.md               # 阶段一：需求范围、核心特性矩阵与验收场景定义
├── 02-design/       # 系统技术架构 (SDD)：双子系统边界、数据面引擎、业务拓扑与数据模型
│   ├── 01-system-architecture.md               # 【总览篇】系统总体架构、双模拓扑、主从信道与全链路时序
│   ├── 02-data-plane-and-cros-rpc-engine.md    # 【数据面篇】CROS RPC 协议栈、Agent eBPF 采集与自愈重放引擎
│   ├── 03-architecture-elements-and-topology.md # 【业务篇】架构元素建模 (SSOT)、模块接口契约与拓扑验证
│   ├── 04-control-plane-and-web-console.md     # 【管理面篇】Server 控制面、微前端控制台、Protobuf 编解码与回归 Diff
│   └── 05-database-schema.md                   # 【数据模型篇】11 张 traefik_* 物理表 GORM 字典与索引设计
├── 03-api/          # 接口与数据契约：RESTful API 签名、请求/响应 Schema、RBAC 权限与审计规范
│   └── 01-http-api-contract.md            # 【API 契约】HTTP 接口字典、角色守卫 (traefik_admin) 与状态码规范
├── 04-tasks/        # TDD 实施规划与验收：Milestone 拆解、任务作战卡与自动化验收测试标准 (AC)
│   ├── 01-roadmap.md                      # 【工程路线图】Milestone 1~4 依赖拓扑图与 TDD 质量门禁
│   └── 02-m1-domain-model-and-api.md      # 【M1 作战卡】核心领域模型、API 与物理隔离单元测试
├── 05-adr/          # 架构决策记录 (ADR)：重大技术选型与权衡记录 (Architecture Decision Records)
│   └── 01-microfrontend-and-table-prefix-isolation.md # ADR-01：微前端集成、共享库表前缀隔离与 TDD 测试策略
├── 06-ops/          # 交付与验证：双模联调、物理隔离数据库测试与生产环境单二进制部署手册
│   └── 01-verification-and-deployment-guide.md # 联调验证、自动化回归测试与生产部署操作指南
└── 07-references/   # 外部参考与上位契约基准：协议包规范、字节字典与领域标准文档
    └── 01-cros-rpc-protocol-spec.md            # DOPHI CROS RPC 协议二进制报文格式与字段级规范说明
```

---

## 目录治理规则

1. **全链路双位编号递进**：
   - 一级子目录采用阶段双位数字前缀（`00-` ~ `06-`）；
   - 各阶段目录下的**所有 Markdown 文档必须带双位数字编号前缀**（如 `01-feature-matrix.md`、`01-system-architecture.md`、`01-http-api-contract.md`、`01-roadmap.md`），保证在文件树、IDE 及文档中心中保持严格时序与逻辑有序性。
2. **规范先行与测试驱动 (SDD + TDD)**：
   - 任何业务代码实施前，必须先在 `01-features/`、`02-design/`、`03-api/` 与 `04-tasks/` 中冻结设计契约与验收标准 (AC)；
   - 实施阶段遵循 **TDD（先写测试 -> 运行失败 Red -> 编写实现通过 Green -> 重构优化 Refactor）**，所有数据库单测必须通过 `testdb.SetupIsolatedDB` 实现物理隔离。
3. **决策可溯**：凡涉及关键架构选型、数据模型拆分、跨模块交互或性能权衡，须在 `05-adr/` 中以标准 ADR 模板记录背景与决策理由。
