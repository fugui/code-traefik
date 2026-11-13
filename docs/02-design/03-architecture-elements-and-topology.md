# 架构与活体契约治理 (`code-traefik`) 架构元素建模与拓扑一致性验证设计

- **文档编号**：`03-architecture-elements-and-topology.md`
- **文档版本**：v1.6.0
- **更新日期**：2026-11-11
- **归属阶段**：阶段二：系统架构与数据模型设计 (`02-design`)
- **状态**：已批准（待 M20 实施）(Approved for M20)
- **上位基线**：[`01-feature-matrix.md`](../01-features/01-feature-matrix.md) (v2.15.0)、[`01-system-architecture.md`](./01-system-architecture.md)、[`09-arch-process-ssot-and-ebpf-premount-standby-adr.md`](../05-adr/09-arch-process-ssot-and-ebpf-premount-standby-adr.md) (ADR-09 v1.1.0)、[`15-scalable-topology-focus-and-layout-persistence-adr.md`](../05-adr/15-scalable-topology-focus-and-layout-persistence-adr.md) (ADR-15 v1.1.0)、[`16-multi-implementation-service-contract-adr.md`](../05-adr/16-multi-implementation-service-contract-adr.md) (ADR-16 v1.0.0)
- **关联工程作战卡**：[`docs/04-tasks/21-m20-multi-implementation-service-contract.md`](../04-tasks/21-m20-multi-implementation-service-contract.md) (M20)

### 修订记录

| 版本 | 日期 | 修订说明 |
| :--- | :--- | :--- |
| v1.5.1 | 2026-11-09 | 依据 ADR-15 (v1.1.0) 增补 §6 大规模拓扑五段式渲染管线、作用域裁剪、有向 N-Hop 邻域、Hub 插头折叠与位置记忆双层持久化设计 |
| v1.6.0 | 2026-11-11 | 依据 ADR-16 (v1.0.0 / M20) 同步多实现服务契约建模与拓扑验证设计：1. §1.1/§1.3 补充 `module_kind ∈ {BUSINESS, CONTRACT_LIB}` 与库模块/契约删除与改类型保护；2. §2 修订 `A-05` 为分模式唯一并新增 §2.2 多实现服务契约（`traefik_service_contracts`、`impl_key`、`A-09` 进程互斥双入口守卫、分发依赖 `DISPATCH` 与存量迁移）；3. §3 补充 `A-03` 对 `CONTRACT_LIB` 的豁免、分发依赖下游推导、`endpoint_snapshots` 端点字典快照及重放兼容校验；4. §4 增补 `«interface»` 库卡、`REALIZATION` 边图层、逻辑视图默认按接口聚合 (`topo_contract_mode=AGG\|EXPAND`)、物理视图库模块豁免与 DTO 增量；5. §5 升级 `ResolveDestinationModule` v2 按目的进程消歧定位，将一致性比对从七分类扩展为八分类（新增第八类 `IMPL_UNRESOLVED` 实现待定），并补充双视图叠加挂载规则 |

---

## 1. 三级逻辑架构元素模型与树形治理 (`internal/server/topology`)

`code-traefik` 作为架构元素与运行映射的**唯一真实源（SSOT）**，采用「**子系统 (`Subsystem`) → 功能组多级树 (`FunctionalGroup`) → 功能模块 (`Module`)**」三级逻辑层次结构。

### 1.1 物化路径 (`Materialized Path`) 树编码模型

功能组支持在同一子系统内最大 **8 级**（`level ∈ [1, 8]`）自嵌套，功能模块可挂载在任意层级的功能组下：

```text
Subsystem [ID=1, Code="TRADE_SYS"]
 ├── FunctionalGroup [ID=10, Parent=nil, Level=1, Path="/10/", Code="ORDER_GRP"]
 │    ├── Module [ID=101, GroupID=10, SubsystemID=1, Code="MOD_ORDER_ENTRY"]   (直接挂在 1 级功能组)
 │    └── FunctionalGroup [ID=12, Parent=10, Level=2, Path="/10/12/", Code="PAY_SUBGRP"]
 │         └── Module [ID=102, GroupID=12, SubsystemID=1, Code="MOD_PAY_SETTLE"] (挂在 2 级功能组)
```

* **设计选型（主键 ID 物化路径 `"/<id1>/<id2>/"` 与模块类型 `module_kind`）**：
  1. 首尾强制带斜杠 `/`，使得子树前缀查询 `WHERE subsystem_id = ? AND path LIKE '/10/12/%'` 绝对精确，彻底消除 `'/10/12'` 误匹配 `'/10/120/'` 的部分前缀越界缺陷；
  2. 修改功能组名称或业务编码 `code` 时无需重写子孙节点的 `path`；
  3. 功能模块冗余维护 `subsystem_id`（始终强制等于其挂载功能组的 `subsystem_id`），使流量列表按子系统筛选无需多层 `JOIN`；
  4. **模块类型区分 (`module_kind ∈ {'BUSINESS', 'CONTRACT_LIB'}`，[`ADR-16`](../05-adr/16-multi-implementation-service-contract-adr.md))**：`traefik_modules` 通过 `module_kind`（默认 `'BUSINESS'`）区分「运行期业务模块 (`BUSINESS`)」与「编译期接口库模块 (`CONTRACT_LIB`)」。`CONTRACT_LIB` 模块留在三级逻辑树中维护所属子系统/功能组与负责人，作为多实现服务契约（`traefik_service_contracts`）的定义属主，**禁止绑定任何运行期 `ArchProcess`**（`CHECK (module_kind = 'BUSINESS' OR arch_process_id IS NULL)`），亦禁止直接创建运行期 `traefik_module_services` 或 `traefik_module_dependencies` 行（返回 `400 Bad Request`）。

### 1.2 功能组移动防成环校验与级联更新算法 (`MoveFunctionalGroup`)

当管理员将功能组 `G` 移动至同子系统内的新父节点 `NewParent`（`NewParent == nil` 表示提升为一级根功能组）时，为防止并发移动导致树形破损或幽灵环路，必须在事务首行获取子系统级排他咨询锁 `SELECT pg_advisory_xact_lock(subsystem_id)`，并在单个事务内执行校验与单条批量 SQL 原子重算：

```go
package topology

const MaxGroupTreeDepth = 8

// MoveFunctionalGroup 在单事务内串行化防成环与最大深度约束，并单条 SQL 原子级联更新子树 path 与 level
func MoveFunctionalGroup(tx *gorm.DB, groupID uint64, newParentID *uint64) error {
	var group models.FunctionalGroup
	if err := tx.First(&group, groupID).Error; err != nil {
		return ErrGroupNotFound // 404
	}

	// 1. 获取子系统粒度事务咨询锁，彻底消除同子系统并发移动成环与脏读
	if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", group.SubsystemID).Error; err != nil {
		return err
	}

	// 2. 禁止移动到自身
	if newParentID != nil && *newParentID == group.ID {
		return ErrCycleMoveToSelf // 400 Bad Request
	}

	newLevel := 1
	newParentPath := "/"
	if newParentID != nil {
		var parent models.FunctionalGroup
		if err := tx.First(&parent, *newParentID).Error; err != nil {
			return ErrParentGroupNotFound // 404
		}
		// 3. 跨子系统移动拦截（功能组归属固定子系统）
		if parent.SubsystemID != group.SubsystemID {
			return ErrCrossSubsystemMove // 400 Bad Request
		}
		// 4. 递归防成环检测：若目标父节点的物化路径以当前节点路径为前缀，说明目标父节点是当前节点的子孙！
		if strings.HasPrefix(parent.Path, group.Path) {
			return ErrCycleMoveToDescendant // 400 Bad Request
		}
		newLevel = parent.Level + 1
		newParentPath = parent.Path
	}

	// 5. 子树最大相对深度计算与 8 级上限门禁校验
	var maxDescendantLevel int
	if err := tx.Model(&models.FunctionalGroup{}).
		Where("subsystem_id = ? AND path LIKE ?", group.SubsystemID, group.Path+"%").
		Select("COALESCE(MAX(level), ?)", group.Level).
		Scan(&maxDescendantLevel).Error; err != nil {
		return err
	}
	subtreeHeight := maxDescendantLevel - group.Level // 自身为 0，含一层子节点为 1
	if newLevel+subtreeHeight > MaxGroupTreeDepth {
		return ErrExceedMaxTreeDepth // 400 Bad Request (超过最大 8 级深度)
	}

	// 6. 原子更新当前节点父指针与整棵子树的物化路径与层级
	oldPrefix := group.Path
	newPrefix := fmt.Sprintf("%s%d/", newParentPath, group.ID)
	levelDelta := newLevel - group.Level

	if err := tx.Model(&models.FunctionalGroup{}).Where("id = ?", group.ID).
		Update("parent_id", newParentID).Error; err != nil {
		return err
	}

	// 利用数据库原生字符串替换与层级加算，单条 SQL 批量完成子树重算，杜绝逐行 Save 脏数据
	return tx.Exec(`
		UPDATE traefik_functional_groups 
		SET path = ? || SUBSTRING(path FROM ?),
		    level = level + ?
		WHERE subsystem_id = ? AND path LIKE ?`,
		newPrefix, len(oldPrefix)+1, levelDelta, group.SubsystemID, oldPrefix+"%",
	).Error
}
```

### 1.3 删除保护冲突矩阵 (`409 Conflict`)

- 删除子系统：若存在关联功能组或功能模块，拒绝删除并返回 `409`；
- 删除功能组：若存在子功能组（`parent_id = id`）或挂载的功能模块（`group_id = id`），拒绝删除并返回 `409`；
- 删除功能模块：若业务模块（`BUSINESS`）已声明被其他模块引用的 `PROVIDE` 服务契约或当前仍装载于某个架构标准进程（`arch_process_id IS NOT NULL`），或接口库模块（`CONTRACT_LIB`）名下已登记有多实现服务契约（`traefik_service_contracts.definer_module_id = id`），拒绝删除并返回 `409`；
- 修改功能模块类型（`module_kind`）：若接口库模块（`CONTRACT_LIB`）名下已有被登记的多实现服务契约（`traefik_service_contracts`），禁止改回 `BUSINESS`（返回 `409`）；若业务模块（`BUSINESS`）已绑定 `ArchProcess` 或已声明 `PROVIDE`/`DEPEND` 行，禁止改为 `CONTRACT_LIB`（返回 `409`）；
- 删除多实现服务契约（`traefik_service_contracts`）：若该契约仍被任何实现模块引用（`traefik_module_services.contract_id = id`）或仍被任何消费模块声明为分发依赖（`traefik_module_dependencies.contract_id = id`），通过外键 `ON DELETE RESTRICT` 与应用层门禁拒绝删除并返回 `409`（附带关联的实现模块与分发调用方列表）；
- 删除物理进程组 (`ArchProcessGroup`)：若组下仍存在标准架构进程（`ArchProcess`），拒绝删除并返回 `409`；
- 删除标准架构进程 (`ArchProcess`)：若当前已被任何宿主机 Agent 挂载实例化（`traefik_processes` 中存在引用）或被历史重放任务 `ReplayTask` 引用，拒绝删除并返回 `409`（附带已挂载环境 Agent 名称列表）。

---

## 2. 模块级服务供给与依赖契约 (Module Service Contracts)

功能模块是接口契约声明的最小业务单元，遵循 [`ADR-05`](../05-adr/05-hierarchical-topology-canvas-redesign.md)、[`ADR-06`](../05-adr/06-proto-schema-repository-and-field-inspection-adr.md) 与 [`ADR-16`](../05-adr/16-multi-implementation-service-contract-adr.md) 服务级（Service-Level）治理粒度，通过 `traefik_service_contracts`（多实现契约定义）、`traefik_module_services`（服务供给 / 契约实现）与 `traefik_module_dependencies`（服务依赖 / 分发依赖）三张表协同维护契约：
- **多实现契约定义（`traefik_service_contracts`，仅多实现契约需登记）**：由接口库模块（`module_kind = 'CONTRACT_LIB'`）定义会被多个进程模块共同实现的 CROS RPC 服务契约（`service_name`）；
- **`PROVIDE`（供给服务 / 实现契约，`traefik_module_services`，仅 `BUSINESS` 模块可声明）**：本业务功能模块对外实现的 CROS RPC 服务（`contract_id IS NULL` 为默认独占供给 `EXCLUSIVE`；`contract_id IS NOT NULL` 为实现某多实现契约 `MULTI_IMPL` 并携带业务身份 `impl_key`）；
- **`DEPEND`（依赖服务 / 分发依赖，`traefik_module_dependencies`，仅 `BUSINESS` 模块可声明）**：本业务功能模块运行期需要调用的下游内部具体服务实现（关联 `service_id`）、多实现契约分发调用（关联 `contract_id`，`DISPATCH`）或外部异构系统（`is_external = true`）。

### 2.1 `{package}.{serviceName}` 规范化命名契约 (A-07) 与分模式唯一性红线 (修订 A-05)

依据 DOPHI CROS RPC 框架运行时规范，线上报文 `RPC_REQ_HEAD_S` 中的 `ascServiceName` 与 `ascOperation` 严格满足以下映射公式：

$$\text{ascServiceName} = \begin{cases} \text{package} + \text{"."} + \text{serviceName}, & \text{package} \neq \text{""} \\ \text{serviceName}, & \text{package} = \text{""} \end{cases}, \quad \text{ascOperation} = \text{methodName}$$

| Protobuf 定义示例 | `package` 声明 | `service` 名称 | `rpc` 方法名 | 规范化 `service_name` (`ascServiceName`) | 规范化 `operation` (`ascOperation`) |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `dophi/order/v1/trade.proto` | `dophi.order.v1` | `TradeService` | `CreateOrder` | `dophi.order.v1.TradeService` | `CreateOrder` |
| `cros/auth/session.proto` | `cros.auth` | `SessionMgr` | `VerifyTicket` | `cros.auth.SessionMgr` | `VerifyTicket` |

* **强校验规则**：
  1. `package` 与 `serviceName` **统一遵循点分连接规范**（`.`，与 `internal/server/decoder/registry.go` 动态解码注册表完全一致；若未声明 `package` 则直接采用 `serviceName`）；
  2. 受限于 CROS RPC 协议头 `CHAR[64]`（含末尾 `\0` 终止符），`service_name` 与 `operation` 字节长度均强制校验为 **`1..63` 位可见 ASCII 字符**（正则：`^[A-Za-z0-9._]{1,63}$` 与 `^[A-Za-z0-9_]{1,63}$`）。
  3. **唯一性红线（由 [`ADR-16`](../05-adr/16-multi-implementation-service-contract-adr.md) v2.0 修订 A-05）**：
     - **独占契约与自定义发布名实现全局唯一供给（`contract_id IS NULL OR is_custom_service_name = true`）**：对于独占服务，以及实现多实现契约但自定义了不同于契约原名的运行期发布服务名（如 `DH_SyncService`）的服务，其 `service_name` 在全库部分唯一索引 `uk_traefik_mod_svc_name_excl ON (service_name) WHERE contract_id IS NULL OR is_custom_service_name = true` 保护下保持全局唯一，且自定义发布名不得与任意 `ServiceContract.service_name` 冲突；
     - **多实现契约按 `(contract_id, impl_key)` 与 `(contract_id, module_id)` 唯一（`MULTI_IMPL`，`contract_id IS NOT NULL`）**：契约本身在 `traefik_service_contracts` 中按 `.pb` 原始 `service_name` 全局唯一（`uk_traefik_svc_contract_name`）；其下各实现行通过部分唯一索引 `uk_traefik_mod_svc_contract_impl` 与 `uk_traefik_mod_svc_contract_mod` 保证同一契约下每个 `impl_key` 唯一且同一模块对同一契约至多实现一次；
     - **消费依赖唯一性**：同一消费模块对同一内部具体服务实现唯一（`UNIQUE(consumer_module_id, service_id)`），对同一多实现分发契约唯一（`uk_traefik_dep_consumer_contract ON (consumer_module_id, contract_id) WHERE contract_id IS NOT NULL`），且同一消费方对同一契约不得同时声明分发依赖与具体实现依赖（`409 Conflict`）。

### 2.2 多实现服务契约、接口库建模与分发依赖 (`F-02.1`, [`ADR-16`](../05-adr/16-multi-implementation-service-contract-adr.md) v2.0)

面向「接口库模块 `FM A` 定义 Protobuf 服务 `pkg.IfaceSvc` ➔ 多个业务模块 `FM B / C / D` 各自实现该服务（可**沿用契约原名**在不同进程中运行，也可**自行定义唯一运行期发布名**如 `DH_SyncService`） ➔ 独立调度中心模块 `X` 分发调用」的生产形态，系统建立三层解耦契约模型：

1. **三方角色与表结构建模**：
   - **接口库模块 (`traefik_modules.module_kind = 'CONTRACT_LIB'`)**：作为设计期契约的定义属主，自身不是运行期部署单元。数据库层施加 `CHECK (module_kind = 'BUSINESS' OR arch_process_id IS NULL)`；调用 `BindModuleProcess` 或为其新增 `PROVIDE`/`DEPEND` 均返回 `400 Bad Request`；
   - **多实现服务契约表 (`traefik_service_contracts`，第 19 张核心业务表)**：
     - `service_name varchar(128) NOT NULL`（`uk_traefik_svc_contract_name`，对应 `.pb` 契约原名，格式校验同 A-07）；
     - `definer_module_id bigint NOT NULL`（FK `traefik_modules(id) ON DELETE RESTRICT`，应用层强制校验目标模块 `module_kind == 'CONTRACT_LIB'`，否则返回 `400`）；
     - `provide_mode varchar(16) NOT NULL DEFAULT 'MULTI_IMPL'`（`CHECK (provide_mode IN ('MULTI_IMPL'))`）；
     - `impl_key_label varchar(64) NOT NULL DEFAULT ''`（实现身份的业务标签说明，如「业务类型」、「渠道编码」）。
   - **服务实现表增量 (`traefik_module_services`)**：
     - 新增 `contract_id bigint NULL`（FK `traefik_service_contracts(id) ON DELETE RESTRICT`，`NULL` 表示独占契约 `EXCLUSIVE`）、`impl_key varchar(64) NOT NULL DEFAULT ''`（多实现必填，格式 `^[A-Za-z0-9._-]{1,64}$`）与 `is_custom_service_name boolean NOT NULL DEFAULT false`（标记是否自定义了不同于 `contract.service_name` 的运行期发布名）；
     - `service_name` 表示运行期线上实际发布的 `ascServiceName`（默认等于 `contract.service_name`，用户可自由修改为 `DH_SyncService` 等任意合法唯一名）；
     - 施加 `CHECK ((contract_id IS NULL AND impl_key = '') OR (contract_id IS NOT NULL AND impl_key <> ''))` 与 `CHECK (contract_id IS NOT NULL OR is_custom_service_name = false)`。
2. **跨表串行化不变量、解码别名与存量平滑迁移**：
   - 所有涉及 `service_name` 的契约创建与 `PROVIDE` 声明写事务，首行统一获取 `SELECT pg_advisory_xact_lock(hashtext(service_name))`：
     - ① 创建多实现契约时，若 `traefik_module_services` 中已存在同名独占行或同名自定义发布名实现，拒绝并返回 `409 Conflict`；
     - ② 创建实现时（传入 `contract_id`）：若未指定自定义 `service_name` 则默认等于 `contract.service_name`（`is_custom_service_name = false`）；若传入不同于 `contract.service_name` 的自定义发布名（`is_custom_service_name = true`），则校验其全系统全局唯一且不与任何 `ServiceContract.service_name` 冲突；
     - ③ **单份 `.pb` 解码别名映射 (`decoder.Registry`)**：内存编译池维护 `ServiceAliases`（自定义发布名 ➔ `.pb` 契约名）与独立 `ServiceIgnores`，实现单份 LIB `.pb` 透明解码所有自定义运行期发布名；
     - ④ **存量独占转多实现 / 挂接到既有契约**：提供「转换为多实现契约」（`POST /api/arch/service-contracts/convert`）与「将存量独占服务挂接到既有契约」（`POST /api/arch/service-contracts/:id/attach`，保留其原 `service_name` 如 `DH_SyncService` 与 `service_id`）。
3. **`A-09` 实现进程互斥守卫 (`AssertContractImplProcessDisjoint`)**：
   - 仅针对**同一契约下沿用契约原名（`is_custom_service_name = false`）**的同名共享实现模块，强制要求两两分属不同的 `ArchProcess`（从而使运行期「目的进程 ➔ 唯一实现模块」成为确定性映射）；
   - **自定义唯一发布名实现（`is_custom_service_name = true`）豁免 A-09**，允许部署在同一标准架构进程内；
   - 在**新增/编辑实现**与**绑定模块到标准进程**两个写入口执行 `AssertContractImplProcessDisjoint`，违反返回 `409 Conflict`。
4. **分发依赖 (`DISPATCH`) 建模 (`traefik_module_dependencies`)**：
   - 新增可空外键 `contract_id bigint NULL`（FK `traefik_service_contracts(id) ON DELETE RESTRICT`），语义为「调度方运行期可调用该多实现契约下的任意已登记实现」，新增或下线实现模块时无需修改调度方 `X` 的依赖声明；
   - 施加三选一排他约束：`CHECK (num_nonnulls(service_id, contract_id) + (is_external::int) = 1)`（内部具体实现 `service_id`、多实现契约分发 `contract_id`、外部系统 `is_external` 严格三选一）；
   - 同一消费方对同一契约同时声明分发依赖（`contract_id`）与具体实现依赖（指向该契约下某 `service_id`）时返回 `409 Conflict`（语义重复）。

---

## 3. 架构物理进程蓝图 SSOT、多环境 `1:1` 挂载覆盖与同构重放校验

### 3.1 全局物理进程蓝图 (`ArchProcessGroup -> ArchProcess`) 与多环境 `1:1` 挂载模型 (`1 台 Agent = 1 套独立环境`)

依据 [`ADR-09`](../05-adr/09-arch-process-ssot-and-ebpf-premount-standby-adr.md)，系统将**「标准业务进程蓝图（全局架构 SSOT）」**与**「环境宿主机进程挂载实例（Agent 运行态）」**彻底解耦为两层正交模型：
- 所有逻辑架构元素（`Subsystem`、`FunctionalGroup`、`Module`、`ModuleService`、`ModuleDependency`）与**物理进程蓝图（`ArchProcessGroup`、`ArchProcess` 及 `ArchProcess <-> Module` 组装关系）**均为全团队共享的**公共唯一真实源（SSOT）**，统一在 `/architecture`（`[逻辑架构 | 物理进程]`）中由 `traefik_admin` 维护，零 Agent 机器耦合；
- **1 台 `Agent` 代表 1 套独立的运行环境**（包含多套团队公共测试环境如 `SHARED Agent_SIT`、`SHARED Agent_UAT`，以及每位开发者个人的 `PRIVATE Agent_Dev`）。任意一套环境 `Agent` 均从已定义的 `ArchProcess` 树中 **`1:1` 勾选挂载进程**（`traefik_processes`），并按需施加上下文局部的本地覆盖配置：

```mermaid
flowchart TB
    subgraph PublicSSOT["全局架构 SSOT (/architecture: 逻辑架构 + 物理进程蓝图, traefik_admin 维护)"]
        PGrp1["ArchProcessGroup: 核心交易组 (CORE_TRADE)"]
        AP1["ArchProcess: sut-order (default_port=9001, default_cmd='./sut-order')"]
        AP2["ArchProcess: backend-stock (default_port=9002, default_cmd='./backend-stock')"]
        Mod1["Module_Order (PROVIDE: dophi.order.OrderSvc)"]
        Mod2["Module_Risk (PROVIDE: dophi.risk.RiskSvc)"]
        Mod3["Module_Stock (PROVIDE: dophi.stock.StockSvc)"]

        PGrp1 --> AP1
        PGrp1 --> AP2
        AP1 -- "1:N 模块组装 (Module.ArchProcessID)" --> Mod1
        AP1 -- "1:N 模块组装" --> Mod2
        AP2 -- "1:N 模块组装" --> Mod3
    end

    subgraph SharedSIT["公共测试环境 1: SHARED Agent_SIT (192.168.56.19)"]
        SProc1["Process Mount #1 (arch=sut-order, 继承默认 :9001, probe='ARMED')"]
        SProc2["Process Mount #2 (arch=backend-stock, 继承默认 :9002, probe='ARMED')"]
    end

    subgraph SharedUAT["公共测试环境 2: SHARED Agent_UAT (192.168.56.20)"]
        UProc1["Process Mount #3 (arch=sut-order, 继承默认 :9001, probe='ARMED')"]
        UProc2["Process Mount #4 (arch=backend-stock, 继承默认 :9002, probe='ARMED')"]
    end

    subgraph PrivateEnv["个人联调环境: PRIVATE Agent_Dev (owner=Alice, fallback=Agent_SIT)"]
        PProc1["Process Mount #5 (arch=sut-order, override_port=19001, probe='ARMED')"]
    end

    SProc1 -- "1:1 实例化" --> AP1
    SProc2 -- "1:1 实例化" --> AP2
    UProc1 -- "1:1 实例化 (多套 SHARED 环境并存)" --> AP1
    UProc2 -- "1:1 实例化" --> AP2
    PProc1 -. "1:1 实例化 + 本地端口覆盖 (未挂载下游兜底指向 Agent_SIT)" .-> AP1
```

核心领域不变量：
1. **单环境（单机）严格 `1:1` 单实例约束 (`UNIQUE(agent_id, arch_process_id)`)**：
   - 同一标准架构进程在单套环境（单个 `Agent`）上至多部署 1 个实例，数据库层以 `uk_traefik_proc_agent_arch` 强制保证不重复挂载；
   - **禁止自造野生进程与篡改模块组装**：Agent 挂载界面不提供自由新建进程名或自由增删 `FM` 模块入口；缺失的标准进程必须回到 `/architecture?tree=physical` 定义。
2. **有效运行参数继承、匹配语法契约与蓝图端口演进自动固化 (A-02)**：
   - 挂载实例的有效属性遵循**「本地覆盖优先 ➔ 架构蓝图默认兜底」**确定性公式：
     $$\text{EffectiveListenPort} = \text{OverrideListenPort} \neq \text{nil} \ ? \ *\text{OverrideListenPort} : \text{ArchProcess.DefaultListenPort}$$
     $$\text{EffectiveCmdPattern} = \text{OverrideCmdPattern} \neq \text{""} \ ? \ \text{OverrideCmdPattern} : \text{ArchProcess.DefaultCmdPattern}$$
     $$\text{EffectiveIP} = \text{AdvertiseIP} \neq \text{""} \ ? \ \text{AdvertiseIP} : \text{HostIP(Agent.Endpoint)}$$
     $$\text{EffectiveExtraDownstreams} = \text{Dedupe}(\text{ArchProcess.DefaultExtraDownstreams} \cup \text{Process.ExtraDownstreams})$$
   - **`cmd_pattern` 匹配语法与防 ReDoS 校验**：长度强制 `<= 256` 字符；默认按**精确子串包含**（`strings.Contains(cmdline, pattern)`）匹配 `/proc/<pid>/cmdline`（`\0` 替换为空格后）；若以显式前缀 `re:` 开头，则按 **Go `regexp` (RE2 线性时间引擎)** 编译匹配，主控在保存 `ArchProcess` 或 `Process` 覆盖参数时强制预执行 `regexp.Compile` 语法校验（非法正则返回 `400 Bad Request`）；
   - **同机非零端口互斥与蓝图变更自动固化**：同一 `agent_id` 下所有非零有效端口（`effective_listen_port > 0`）通过部分唯一索引 `uk_traefik_proc_agent_port` 严格互斥（同时允许并存多个 `0` 端口纯客户端进程）。当管理员在 `/architecture` 修改 `ArchProcess.DefaultListenPort`（由 $P_{\text{old}} \to P_{\text{new}}$）时，主控在单事务内自动检测各已挂载该蓝图的 Agent：若某挂载实例未设置覆盖端口（`override_listen_port IS NULL`）且 $P_{\text{new}} > 0$ 与该 Agent 同机其他已挂载进程的有效端口冲突，**自动将该冲突实例的 `override_listen_port` 固化为原有效端口 $P_{\text{old}}$**（并在响应 `auto_pinned_overrides` 中返回提示），既绝对不破坏个人/特定环境既有挂载，又保证全局蓝图自由演进。
3. **模块单归属、接口库 `CONTRACT_LIB` 豁免与多环境公私双域并存 (A-03 & A-09)**：
   - **架构层（A-03 对 `CONTRACT_LIB` 的豁免与 A-09 进程互斥）**：
     - **业务模块 (`module_kind = 'BUSINESS'`)**：1 个 `ArchProcess` 可组装承载 $N \ge 0$ 个 `BUSINESS` 模块（记录于 `traefik_modules.arch_process_id`），每个 `BUSINESS` 模块全局至多归属 **1 个** `ArchProcess`（尝试将已分配给进程 A 的模块重复绑给进程 B 时返回 `409 Conflict`）；且同一多实现契约的各个实现模块严禁绑定至同一 `ArchProcess`（遵循 `A-09`，见 §2.2）；
     - **接口库模块 (`module_kind = 'CONTRACT_LIB'`) 豁免 `A-03` 物理进程归属**：`CONTRACT_LIB` 是被编译链接进多处的纯契约定义载体，不是运行期部署单元。数据库层以 `CHECK (module_kind = 'BUSINESS' OR arch_process_id IS NULL)` 强制保证其 `arch_process_id` 恒为 `NULL`（尝试绑定进程返回 `400 Bad Request`），且在物理部署视图中不渲染（亦不归入「未部署」容器）；
   - **公共测试环境域 (`SHARED` Agent)**：`1 台 SHARED Agent = 1 套独立公共测试环境`（如 `SIT`、`UAT`、`准生产回归`），**支持多台不同的 `SHARED` Agent 各自 `1:1` 挂载同一个 `ArchProcess`**，互不排斥；
   - **个人私有域 (`PRIVATE` Agent)**：每位开发者本人的 `PRIVATE` Agent 作为独立隔离沙箱，同样自由 `1:1` 勾选挂载任意 `ArchProcess`，并可通过 `Agent.FallbackAgentID` 可选绑定某台 `SHARED` 公共环境 Agent 作为本地未启动下游的跨机连接兜底。
4. **动态 PID 非权威与统一七态预挂就绪快照**：操作系统 `PID` 随业务重启动态变化，不作为静态权威配置，由 Agent 在 `ARMED` 预挂待命及活跃录制期通过 `(EffectiveListenPort, EffectiveCmdPattern)` 实时扫描 `/proc` 获取，并将统一七态就绪诊断（`READY` / `NEED_RECONNECT` / `WAITING_PROC` / `MONITORING` / `DISARMED` / `OFFLINE` / `CAP_DENIED` 或 `ERROR`）回写至 `traefik_processes` 快照列。

### 3.2 多环境混合下游 `IP:Port` 自动推导算法 (`CompileProcessDownstreams` — 同机优先 + 绑定基线环境兜底) 与端点字典快照 (`endpoint_snapshots`)

当主控向 Agent 下发某挂载进程 `P`（所属环境 `Agent_X`，对应蓝图 `ArchProcess_Src`）的预挂载待命（`/monitors/arm`）、录制（`/monitors/sync`）或重放规格 `ProcessMonitorSpec` 时，自动聚合以下三层下游网络端点集合：

1. **已声明架构依赖混合端点（同环境同机优先 ➔ `FallbackAgentID` 绑定公共环境兜底）**：
   - 读取 `ArchProcess_Src` 承载的全部模块集合 $M(\text{ArchProcess\_Src})$ 及其内部依赖 `ModuleDependency`，定位目标供给模块所属的标准架构进程 `ArchProcess_Dst`（若依赖为 `contract_id != nil` 的分发依赖 `DISPATCH`，则自动展开为该契约当前已登记的全部实现模块并逐一提取各自所属的 `ArchProcess_Dst`）；
   - 若 `ArchProcess_Dst != ArchProcess_Src`：
     - **优先级 ①（同环境同机挂载实例优先）**：检查当前这台 `Agent_X` 上是否挂载了 `ArchProcess_Dst`（记为 `P_local`）；若存在且 `P_local.EffectiveListenPort > 0`，加入 `(EffectiveIP(P_local), P_local.EffectiveListenPort)` 及同机回环别名 `("127.0.0.1", P_local.EffectiveListenPort)`；
     - **优先级 ②（绑定公共基线环境兜底，严禁多环境盲目串绑）**：当同机未挂载 `ArchProcess_Dst`（如开发者仅在个人 `PRIVATE` Agent 启动单个待测微服务，或某套分片环境依赖公共底座 Agent）时：
       - 若 `Agent_X.FallbackAgentID != nil`，精确查找该绑定公共 `SHARED` 环境 Agent 上的挂载实例 `P_fallback`，若 `P_fallback.EffectiveListenPort > 0` 则加入 `(EffectiveIP(P_fallback), P_fallback.EffectiveListenPort)`；
       - 若 `Agent_X.FallbackAgentID == nil` 且全库所有 `SHARED` Agent 中**恰好仅有唯一 1 台**挂载了 `ArchProcess_Dst`，自动回退纳入该唯一公共实例端点；若存在 $\ge 2$ 套 `SHARED` 环境同时挂载了 `ArchProcess_Dst` 且未显式指定 `FallbackAgentID`，**绝不跨环境盲目合并其他 `SHARED` 环境 IP:Port**，彻底杜绝 `SIT` 与 `UAT` 跨环境串流！
2. **同机已挂载进程监听端口全集（`PeerListenEndpoints` & `ArmedDownstreams` 超集不变量）**：
   - **核心物理保障（解决未声明 `VIOLATION` 调用拦截盲区）**：在编译预挂待命规格与录制规格时，统一将同机 `Agent_X` 上**全部已挂载进程的非零有效监听端口**（含 `(EffectiveIP, Port)` 与 `("127.0.0.1", Port)`，剔除 `P` 自身端口）悉数并入 `Downstreams`，严格保持**预挂超集不变量**：
     $$\text{ArmedDownstreams}(P) \supseteq \text{ActiveRecordingDownstreams}(P, S)$$
   - 由此，无论模块 $A$ 源码是否声明了对同机模块 $B$ 的依赖，只要进程在 `ARMED` 预挂后建立连接，出站长连接在待命期即已 100% 接入 `EgressProxy` 纯透传，开启录制时无需任何重连即可 100% 捕获未声明跨进程 `VIOLATION` 调用！
3. **架构默认补充端点 + 本地覆盖补充端点 + 离线重放历史端点**：
   - 合并 `ArchProcess_Src.DefaultExtraDownstreams` 与 `P.ExtraDownstreams`；若为离线重放（`CompileReplayDownstreams`），额外并入源录制批次观测到的全部 `EGRESS` 原始 `dst_addr`（`IP:Port`）；最终按 `IP:Port` 稳定排序去重输出。
4. **录制时刻端点字典快照固化 (`RecordingSession.endpoint_snapshots`，[`ADR-16`](../05-adr/16-multi-implementation-service-contract-adr.md) §3.6)**：
   - 在创建录制批次、编译下游时，将上述第 1、2 步中**能唯一确定 `ArchProcess`** 的「网络端点 `IP:Port`（含同机 `127.0.0.1` 别名）➔ `ArchProcessID`」一次性固化至 `traefik_recording_sessions.endpoint_snapshots`（`jsonb NOT NULL DEFAULT '[]'`，匿名 `extra_downstreams` 不入字典），供 §5.1 多实现契约目的模块定位使用，确保事后环境挂载变更不会导致历史批次归属漂移：

```go
// EndpointSnapshotItem 录制时刻「网络端点 ➔ 标准架构进程」字典项 (固化于 RecordingSession.EndpointSnapshots)
type EndpointSnapshotItem struct {
	Endpoint      string `json:"endpoint"`        // "IP:Port"，含同机 127.0.0.1 别名
	ArchProcessID uint64 `json:"arch_process_id"`
	Source        string `json:"source"`          // LOCAL | FALLBACK | SHARED_UNIQUE
}
```

### 3.3 基于录制快照解耦的跨 Agent 同构进程重放兼容性校验 (`ValidateReplayProcessCompatibility`)

为确保某台环境 Agent 日后卸载 `Process` 挂载实例（`DELETE /api/processes/:id`）后，历史录制批次依然可以随时跨环境重放，录制批次创建时在 `RecordingSession.ProcessSnapshots` 中固化参与进程的蓝图快照 `ProcessSnapshotItem{ProcessID, ArchProcessID, Code, Name, ModuleIDs, ListenPort}`。兼容性校验优先读取批次快照，彻底解除对源 `traefik_processes` 存活状态的硬依赖；对于多实现契约的入站记录（[`ADR-16`](../05-adr/16-multi-implementation-service-contract-adr.md) §7.1），所需模块取**源进程快照中实现该契约的模块**（$\text{impls}(\text{contract}) \cap \text{sourceSnap.ModuleIDs}$），而非全局单一 `provideMap[svc]`：

```go
package topology

// ProcessSnapshotItem 固化在 RecordingSession.ProcessSnapshots 中的录制时刻进程蓝图快照
type ProcessSnapshotItem struct {
	ProcessID     uint64   `json:"process_id"`
	ArchProcessID uint64   `json:"arch_process_id"`
	Code          string   `json:"code"`
	Name          string   `json:"name"`
	ModuleIDs     []uint64 `json:"module_ids"`
	ListenPort    uint16   `json:"listen_port"`
}

// ValidateReplayProcessCompatibility 校验录制源进程选定报文能否安全重放到目标挂载进程 targetProc
func ValidateReplayProcessCompatibility(
	callerID uint,
	isAdmin bool,
	session *models.RecordingSession,
	selectedRecords []models.TrafficRecord,
	sourceSnap *ProcessSnapshotItem, // 优先从 session.ProcessSnapshots 按 source_process_id 提取（兼容回退现存 sourceProc）
	targetProc *models.Process,
	targetAgent *models.Agent,
	archProcModules map[uint64][]uint64, // key: ArchProcessID -> []ModuleID
	provideMap map[string]uint64,        // key: "serviceName" -> DstModuleID (EXCLUSIVE 独占契约)
	contractImplMods map[string][]uint64, // key: "serviceName" -> []ModuleID (MULTI_IMPL 多实现契约全体实现模块)
) error {
	// 1. 录制素材与目标 Agent 的公私域归属防越权校验 (Anti-IDOR)
	if session.Scope == models.ScopePrivate && session.OwnerID != callerID && !isAdmin {
		return ErrForbiddenPrivateSession // 403 Forbidden
	}
	if targetAgent.Scope == models.ScopePrivate && (targetAgent.OwnerID == nil || *targetAgent.OwnerID != callerID) && !isAdmin {
		return ErrForbiddenPrivateAgent // 403 Forbidden
	}
	if sourceSnap == nil {
		return ErrSourceProcessSnapshotNotFound // 400 Bad Request
	}

	// 2. 目标挂载进程必须具备有效监听端口 (> 0，纯客户端进程无法接收重放注入)
	if targetProc.EffectiveListenPort <= 0 {
		return ErrClientOnlyReplayTarget // 400 Bad Request
	}

	// 3. 原地同挂载实例 或 挂载同一标准架构进程蓝图 (ArchProcessID 相等) 天然 100% 同构兼容！
	if sourceSnap.ProcessID == targetProc.ID || sourceSnap.ArchProcessID == targetProc.ArchProcessID {
		return nil
	}

	// 4. 跨架构进程重构演进场景（如模块拆分迁移至新标准进程）：校验目标进程必须完整承载选定请求命中的全部公共架构模块
	targetModSet := toUint64Set(archProcModules[targetProc.ArchProcessID])
	sourceMods := toUint64Set(sourceSnap.ModuleIDs)
	if len(sourceMods) == 0 {
		sourceMods = toUint64Set(archProcModules[sourceSnap.ArchProcessID])
	}
	requiredMods := make(map[uint64]struct{})
	hasUnregisteredIface := false

	for _, rec := range selectedRecords {
		if implMods, isMulti := contractImplMods[rec.ServiceName]; isMulti {
			// 多实现契约 (ADR-16 §7.1): 优先取已定位的 rec.DstModuleID，否则取源进程快照中实现该契约的模块 (A-09 保证至多 1 个)
			matched := false
			if rec.DstModuleID != nil {
				requiredMods[*rec.DstModuleID] = struct{}{}
				matched = true
			} else {
				for _, mID := range implMods {
					if _, inSrc := sourceMods[mID]; inSrc {
						requiredMods[mID] = struct{}{}
						matched = true
					}
				}
			}
			if !matched {
				hasUnregisteredIface = true
			}
		} else if dstModID, ok := provideMap[rec.ServiceName]; ok {
			requiredMods[dstModID] = struct{}{}
		} else {
			hasUnregisteredIface = true
		}
	}

	if hasUnregisteredIface || len(requiredMods) == 0 {
		if len(sourceMods) == 0 || !isSubset(sourceMods, targetModSet) {
			return ErrIncompatibleReplayTargetModule // 400 Bad Request
		}
		return nil
	}

	if !isSubset(requiredMods, targetModSet) {
		return ErrIncompatibleReplayTargetModule // 400 Bad Request (附带缺失的公共模块编码明细)
	}
	return nil
}
```

---

## 4. 全系统分层复合架构拓扑推导与边汇总引擎 (`BuildTopologyGraph`)

依据 [`ADR-05`](../05-adr/05-hierarchical-topology-canvas-redesign.md) 与 [`ADR-16`](../05-adr/16-multi-implementation-service-contract-adr.md)，架构拓扑引擎从局部的单子系统扁平图全面升级为**全系统分层复合有向图（Hierarchical Compound Graph）**，统一支持**逻辑架构视角 (`LOGICAL`)** 与**物理部署视角 (`PHYSICAL`)**。后端单次输出全量层级节点（顶层 `depth=0` 直接为子系统或进程组，不下发冗余外层 `SYSTEM` 巨框）与原子叶子边，前端通过纯函数执行实时边汇总与按需下钻展开。

### 4.1 双重视角分层树模型（彻底去 Agent 化 & 多实现契约双模表达）

#### 1. 逻辑架构视角 (`view=LOGICAL`，`F-04.4` / [`ADR-16`](../05-adr/16-multi-implementation-service-contract-adr.md) §6.1)
```text
(画布根视口 Viewport)
 ├── Subsystem (depth=0 / L0: 业务子系统容器，如 TRADE_SYS, RISK_SYS)
 │    └── FunctionalGroup L1 (depth=1 / L1: 一级功能组容器)
 │         ├── FunctionalGroup L2...L8 (depth=2..8: 多级功能组自嵌套容器)
 │         │    ├── Module [BUSINESS] (叶子卡片: 最小运行期契约模块，实现多实现契约时附带 implements 元数据)
 │         │    └── Module [CONTRACT_LIB] («interface» 虚线边框库卡片: 定义多实现服务契约，展示契约数与实现数)
 │         └── Module (挂载于一级功能组下的模块)
 ├── ExternalContainer (depth=0 / L0: 外部依赖系统容器，仅存在外部依赖时生成)
 │    ├── ExternalSystem_A (按 external_system 名称实体化，如 WeChatPay)
 │    └── ExternalSystem_B (如 UnionPay)
 └── UnregisteredInterface (动态虚拟节点: 仅在一致性叠加层检出调用未登记接口时由前端按需注入)
```
> **外键对齐说明**：`traefik_modules.group_id` 为 `NOT NULL` 外键，合法模块均挂载于某级 `FunctionalGroup` 下；前端 `hierarchy.ts` 对异常缺失父功能组的数据保留回退挂载至 `sub_<SubsystemID>` 的防御性兜底。

* **逻辑视图多实现契约表达规则 (`F-04.4`)**：
  1. **接口库卡片 (`CONTRACT_LIB`)**：作为 `MODULE` 节点（`meta.module_kind = "CONTRACT_LIB"`），渲染为虚线边框 + `«interface»` 角标卡片，卡片内显示定义的契约数与已登记实现总数；
  2. **实现关系边图层 (`Kind = "REALIZATION"`)**：由各实现模块指向定义契约的库模块（`B / C / D ⇢ A`），采用 UML 实现线（虚线 + 空心三角）渲染，作为**独立可开关图层**；**不参与 Tarjan 强连通分量环路检测、不参与一致性状态叠加、与运行期调用边分开上卷**；
  3. **分发调用双模表达与默认聚合 (`topo_contract_mode=AGG|EXPAND`)**：
     - **默认模式「按接口聚合 (`topo_contract_mode=AGG`)」**：将调度方 `X` 对多实现契约的分发调用收敛为单条指向库模块 `A` 的虚拟调用边 `X ➔ A`，附带 `×N 实现` 徽章，一致性叠加时边状态取各实现边（及 `IMPL_UNRESOLVED`）中的最严重者，直观表达设计意图并抑制高扇出连线；
     - **切换模式「按实现展开 (`topo_contract_mode=EXPAND`)」**：工具栏支持一键切换，将分发调用展开为 `X ➔ B / C / D` 三条调用边，边标签展示 `pkg.IfaceSvc [b]`。

#### 2. 物理部署视角 (`view=PHYSICAL`，100% 源自架构物理进程蓝图 SSOT、彻底去 Agent 化、不渲染接口库)
> [!IMPORTANT]
> **物理拓扑去 Agent 化、库模块豁免与多环境可选端点叠加铁律（[`ADR-05`](../05-adr/05-hierarchical-topology-canvas-redesign.md)、[`ADR-09`](../05-adr/09-arch-process-ssot-and-ebpf-premount-standby-adr.md) & [`ADR-16`](../05-adr/16-multi-implementation-service-contract-adr.md)）**：`Agent` 仅作为环境宿主机上无状态的字节流采集探测器与 eBPF 探针载体，不是系统业务架构的构成元素。物理部署视图节点与连线骨架 **100% 直接由 `ArchProcessGroup -> ArchProcess -> Module(BUSINESS)` 架构物理进程蓝图推导生成**（即使尚未纳管任何 Agent 机器也能完整构建物理全景图！），**画布绝对不出现 `AGENT` 节点，且 `meta` 中严禁透出 `agent_id`**：
> - **接口库模块 (`CONTRACT_LIB`) 物理视图不渲染**：`CONTRACT_LIB` 本就不是运行期可部署实体，在物理视图中**完全不输出节点**，**亦严禁放入 `UndeployedContainer`（未部署容器）**；实现模块照常位于各自 `ArchProcess` 容器内，卡片附加标签「实现 `pkg.IfaceSvc [b]`」；分发调用在物理视图中**恒按实现展开**为真实的进程间连线 `PX ➔ PB / PC / PD`；
> - **默认纯蓝图态（未指定 `env_agent_id`）**：`meta.endpoint` 确定性输出 `:<default_listen_port>`（纯客户端为 `"client-only"`），并附带 `meta.mounted_shared_env_count`（已挂载该蓝图的公共环境数），避免多套 `SHARED` 环境并存时随机选取某台机器 IP 引发歧义；
> - **指定环境叠加态（传入可选查询参数 `env_agent_id`，调用方需对该 `SHARED` 或本人 `PRIVATE` 环境具备可见权，越权返 `403`）**：后端按该环境上的挂载实例富化填入 `EffectiveIP:EffectiveListenPort`（该环境未挂载时标记 `meta.env_mounted = false` 并回退显示 `:<default_listen_port>`），同时合并该环境挂载实例的 `extra_downstreams` 展示外部网络端点：

```text
(画布根视口 Viewport)
 ├── ProcessGroup (depth=0 / L0: 架构进程组容器，源自 traefik_arch_process_groups)
 │    └── Process (depth=1 / L1: 标准业务进程容器，源自 traefik_arch_processes，标识进程名与监听端点)
 │         └── Module [BUSINESS] (depth=2 / L2: 该标准架构进程组装承载的业务模块实例，多实现时展示「实现 pkg.IfaceSvc [b]」)
 ├── UndeployedContainer (depth=0 / L0: 尚未分配至任何标准架构进程的孤立 BUSINESS 模块容器 arch_process_id IS NULL；不含 CONTRACT_LIB)
 │    └── Module [BUSINESS] (depth=1: 未分配进程业务模块叶子卡片)
 ├── ExternalContainer (depth=0 / L0: 外部依赖与网络端点容器，按需生成)
 │    ├── ExternalSystem (承接模块声明的 is_external=true 业务外部系统)
 │    └── ExternalEndpoint (承接 ArchProcess.default_extra_downstreams 及公共挂载 extra_downstreams 的固定 IPv4:Port 端点)
 ├── UnregisteredInterface (动态虚拟节点: 一致性叠加层检出未登记接口时按需注入)
 └── ImplUnresolved (动态虚拟节点: 物理视图下多实现契约命中但目的实现无法定位时按需注入，展示「实现待定: pkg.IfaceSvc」)
```

### 4.2 前端实时边汇总算法 (Edge Rollup) 与复合坐标规则

后端接口一次性返回全系统架构节点与最细粒度的**模块/外部端点级原子叶子边**集合。在前端画布中，无论用户如何展开或折叠容器，有向连线始终动态聚合连接至**当前状态下两端最近的可见代表节点 (Representative Visible Ancestor)**：

```mermaid
flowchart LR
    subgraph SubA["子系统 A (已展开)"]
        subgraph Grp1["功能组 1 (已展开)"]
            Mod1["模块 M1"]
        end
        subgraph Grp2["功能组 2 (已折叠)"]
            Mod2["模块 M2"]
            Mod3["模块 M3"]
        end
    end
    subgraph SubB["子系统 B (已折叠)"]
        Mod4["模块 M4"]
        Mod5["模块 M5"]
    end

    Mod1 -- "单条直连" --> Grp2
    Grp2 == "聚合汇总边 (3 条契约)" ==> SubB
```

#### 边汇总与复合排版核心不变量：
1. **代表节点查找 (`rep(nodeId)`)**：沿当前节点的 `ancestors[]` 路径从顶层根（`depth=0`）向下扫描，首个处于“折叠 (Collapsed)”状态的容器即为其可见代表节点；若其所有祖先均处于展开状态，则该叶子节点自身即为代表节点；
2. **同容器内收敛 (Internal Dependency)**：若源与目标的代表节点相同（$S_{\text{rep}} = T_{\text{rep}}$，即两端落在同一个折叠容器内部）：
   - **逻辑视图**：在折叠容器的标题栏徽章累计为「内部依赖: $N$」；
   - **物理视图**：若两端落在同一折叠 `Process` 容器内，自动收敛为进程卡片右上角的「进程内调用: $N$」徽章（契合 A-04 不经内核 Socket、物理不可观测语义）；
3. **跨容器聚合边 (Rollup Edge) 与实现边分层**：若代表节点不同（$S_{\text{rep}} \neq T_{\text{rep}}$），将底层调用叶子边按有向对偶元组 `(S_rep, T_rep)` 归并为一条汇总边，附带聚合去重后的契约清单，线宽依据 $\log_2(\text{count} + 1)$ 动态缩放；`Kind == "REALIZATION"` 的实现关系边在独立图层内按 `(S_rep, T_rep, "REALIZATION")` 单独上卷，绝不与运行期调用边混并；
4. **环路标红 (Cycle Detection)**：在当前可见代表节点导出的**运行期调用有向图**（剔除 `REALIZATION` 实现关系边）上执行 Tarjan 强连通分量 (SCC) 算法，处于大小 $\ge 2$ 强连通分量内的汇总边标记 `isCycle = true` 并以 `--color-danger` 高亮报警；
5. **React Flow v12 父先于子拓扑序与全局拐点换算**：输出给 React Flow 的可见节点必须按 `depth` 升序排列（确保 `parentId` 父容器先于子节点出现）；ELK `INCLUDE_CHILDREN` 输出的跨层边 `bendPoints` 必须累加所属 `containingNode` 的全局绝对坐标 `(absX, absY)`，且在节点被用户手动拖拽后自动退化为 `SmoothStep` 动态正交路径。

### 4.3 静态拓扑图完整 DTO 契约 (`TopologyGraphDTO`)

```go
package topology

// GraphNodeType 全系统分层拓扑节点类型枚举 (零 Agent)
type GraphNodeType string

const (
	NodeTypeSubsystem             GraphNodeType = "SUBSYSTEM"
	NodeTypeFunctionalGroup       GraphNodeType = "FUNCTIONAL_GROUP"
	NodeTypeModule                GraphNodeType = "MODULE"
	NodeTypeExternalContainer     GraphNodeType = "EXTERNAL_CONTAINER"
	NodeTypeExternalSystem        GraphNodeType = "EXTERNAL_SYSTEM"
	NodeTypeProcessGroup          GraphNodeType = "PROCESS_GROUP"
	NodeTypeProcess               GraphNodeType = "PROCESS"
	NodeTypeUndeployedContainer   GraphNodeType = "UNDEPLOYED_CONTAINER"
	NodeTypeExternalEndpoint      GraphNodeType = "EXTERNAL_ENDPOINT"
	NodeTypeUnregisteredInterface GraphNodeType = "UNREGISTERED_INTERFACE" // 前端一致性叠加态按需动态注入 (未登记接口)
	NodeTypeImplUnresolved        GraphNodeType = "IMPL_UNRESOLVED"        // 物理视图一致性叠加态按需动态注入 (多实现契约实现待定, ADR-16)
)

// ModuleContractImplItem 模块实现的多实现服务契约摘要 (挂载于 GraphNode.Meta["implements"])
type ModuleContractImplItem struct {
	ContractID  uint64 `json:"contract_id"`
	ServiceName string `json:"service_name"`
	ImplKey     string `json:"impl_key"`
}

// GraphNode 复合图节点传输对象
type GraphNode struct {
	ID        string         `json:"id"`                   // 唯一标识: "sub_1", "grp_10", "mod_101", "pgrp_交易核心", "proc_7"
	Type      GraphNodeType  `json:"type"`                 // 节点类型枚举（严禁出现 AGENT）
	ParentID  string         `json:"parent_id,omitempty"`  // 父节点 ID，顶层节点 (depth=0) 为空字符串
	Ancestors []string       `json:"ancestors"`            // 从顶层祖先至直接父节点的有序 ID 数组，加速前端 rep() 计算
	Depth     int            `json:"depth"`                // 所在层级深度 (0: 顶层子系统/进程组/虚拟根容器)
	EntityID  uint64         `json:"entity_id,omitempty"`  // 对应业务数据库主键 (Subsystem.ID, Module.ID, Process.ID)
	Code      string         `json:"code,omitempty"`       // 业务编码 (如 "TRADE_SYS", "MOD_ORDER")
	Name      string         `json:"name"`                 // 展示名称
	SortOrder int            `json:"sort_order"`           // 排序权重，供 ELK considerModelOrder 保持排版稳定
	Meta      map[string]any `json:"meta,omitempty"`       // 视图专有元数据 (如 module_kind, implements: []ModuleContractImplItem, contract_count, impl_count, endpoint, listen_port, provide_count, owner_id, owner_name；SUBSYSTEM 与 MODULE 节点均输出 owner_id 供「我负责的」过滤；严禁含 agent_id)
}

// EdgeServiceItem 底层服务契约明细
type EdgeServiceItem struct {
	ServiceID    *uint64 `json:"service_id,omitempty"`
	ServiceName  string  `json:"service_name"`
	ContractID   *uint64 `json:"contract_id,omitempty"` // 多实现契约 ID (ADR-16)
	ImplKey      string  `json:"impl_key,omitempty"`    // 多实现业务身份标识，如 "b" (ADR-16)
	DependencyID uint64  `json:"dependency_id,omitempty"`
	Description  string  `json:"description,omitempty"`
}

// GraphEdge 原子叶子依赖边传输对象
type GraphEdge struct {
	ID             string            `json:"id"`                   // 如 "edge_m101_m102"、"edge_real_m102_m100" 或 "edge_ext_m101_WeChatPay"
	Kind           string            `json:"kind"`                 // CROSS_PROCESS | INTRA_PROCESS | EXTERNAL_UNREGISTERED | UNDEPLOYED | REALIZATION
	SourceNodeID   string            `json:"source_node_id"`       // 源叶子节点 ID ("mod_101" 或 "proc_7")
	TargetNodeID   string            `json:"target_node_id"`       // 目标叶子节点 ID ("mod_102" / "ext_sys_WeChatPay" / "ext_ep_10.0.0.8:3306")
	SrcModuleID    uint64            `json:"src_module_id,omitempty"`
	DstModuleID    *uint64           `json:"dst_module_id,omitempty"`
	SrcProcessID   *uint64           `json:"src_process_id,omitempty"`
	DstProcessID   *uint64           `json:"dst_process_id,omitempty"`
	ContractID     *uint64           `json:"contract_id,omitempty"` // 多实现服务契约 ID (ADR-16)
	ImplKey        string            `json:"impl_key,omitempty"`    // 目标实现模块的业务身份标识 (ADR-16)
	Services       []EdgeServiceItem `json:"services"`             // 聚合的底层服务调用清单
	ExternalSystem string            `json:"external_system,omitempty"`
}

// TopologyGraphDTO 全量架构拓扑图响应载荷
type TopologyGraphDTO struct {
	View         string      `json:"view"`          // LOGICAL | PHYSICAL
	ModelVersion string      `json:"model_version"` // 基于规范化 nodes+edges 计算的 SHA-256 内容指纹 (如 "v1-LOGICAL-9f86d081884c7d65")，100% 感知硬删除并作为 HTTP ETag
	// StructureVersion 仅对按 id 排序的 (id, parent_id) 树结构计算的 SHA-256 指纹 (如 "s1-LOGICAL-3a7bd3e2360a3d29")，
	// 不受边、module_kind、implements、REALIZATION 图层、topo_contract_mode、名称、描述与 env_agent_id 端点叠加影响，
	// 专供拓扑布局过期判定 (F-04.3, ADR-15 §4.3)，既有持久化布局无需迁移 (ADR-16 §6.3)
	StructureVersion string  `json:"structure_version"`
	Nodes        []GraphNode `json:"nodes"`
	Edges        []GraphEdge `json:"edges"`
}
```

---

## 5. 动态报文穿透定位与架构一致性验证引擎 (`VerifySessionTopology`)

基于一次多进程协同录制批次（`session_id`）采集到的全部业务请求报文（`ucMsgType == 2 (REQ)`），引擎自动穿透还原实际模块级调用图，并与当前静态架构契约执行逐边比对。

为消除慢轨并发连接分片处理时“父入站请求尚未落盘而出站调用已到达”的时序竞态，**来源模块判定在录制批次停止后，由后台离线批量对账引擎 (`OfflineBatchAttribution`) 统一归集全链路报文并执行结算**，保证父子 SpanID 因果关系的 100% 精确闭环。

### 5.1 目的模块穿透定位 (`ResolveDestinationModule` v2 — 独占直查 + 多实现按目的进程消歧)

依据分模式供给契约（修订后 `A-05` 与 `A-09`，[`ADR-16`](../05-adr/16-multi-implementation-service-contract-adr.md) §4），目的模块定位统一由 `ContractResolver.ResolveDestinationModule` 执行，同时回填 `traefik_traffic_records.dst_module_id` 与 `dst_contract_id`（后台离线批量对账 `OfflineBatchAttribution` 一并计算）：

```mermaid
flowchart TD
    S["输入: 报文 rec (svc, direction, process_id, dst_addr)"] --> K{"svc 已登记多实现契约?"}
    K -- "否 (独占契约 EXCLUSIVE)" --> E["查 provideMap[svc]：命中得 DstModule；未命中归入 VIRTUAL_UNREGISTERED_INTERFACE"]
    K -- "是 (多实现契约 MULTI_IMPL)" --> Dir{"direction"}
    Dir -- "INGRESS" --> I["接收进程 P = rec.process 的 ArchProcess dst = impls(contract) ∩ M(P)"]
    Dir -- "EGRESS" --> G["dst_addr 查 session.endpoint_snapshots ➔ ArchProcess Q ➔ dst = impls(contract) ∩ M(Q)"]
    I --> R{"命中且唯一 (A-09 保证)?"}
    G --> R
    R -- "是" --> OK["DstModule = 实现模块, dst_contract_id = contract.id, impl_key 回填"]
    R -- "否" --> U["DstModule = nil, dst_contract_id = contract.id ➔ 状态 IMPL_UNRESOLVED (实现待定)"]
```

* **分模式定位与消歧铁律**：
  1. **独占契约 (`EXCLUSIVE`，未在 `traefik_service_contracts` 登记)**：无论 `INGRESS` 或 `EGRESS`，直接以 `ascServiceName` 查 `provideMap[serviceName]`——命中则唯一定位 `DstModule`（`dst_contract_id = nil`）；未命中则归属至虚拟异常节点 `VIRTUAL_UNREGISTERED_INTERFACE`（「未登记接口」）；
  2. **多实现契约 (`MULTI_IMPL`，已在 `traefik_service_contracts` 登记)**：按**目的物理进程**消歧：
     - **`INGRESS` 入站报文**：直接取接收进程 `rec.process` 对应的标准架构进程 $P$，求该契约实现模块集合与 $P$ 承载模块集合的交集 $\text{impls}(\text{contract}) \cap M(P)$；
     - **`EGRESS` 出站报文**：以实测 `rec.dst_addr`（`IP:Port`）查找录制批次固化的端点字典 `session.endpoint_snapshots`（见 §3.2）解析出目的标准架构进程 $Q$，再求交集 $\text{impls}(\text{contract}) \cap M(Q)$；
     - **唯一性保障 (`A-09`)**：因同一多实现契约的各实现模块两两分属不同 `ArchProcess`，上述交集元素个数至多为 `1`；若命中唯一实现模块，则确定 `DstModule`、回填 `dst_module_id`、`dst_contract_id` 与对应 `impl_key`；
     - **未命中降级 (`IMPL_UNRESOLVED`)**：若 `dst_addr` 未命中 `endpoint_snapshots`（常见原因为调度中心注册表中的下游地址与挂载实例有效端点不一致，如跨 NAT 或未配置 `advertise_ip`，或对应实现进程未在任何可推导环境中挂载），则置 `DstModule = nil`、`dst_contract_id = &contract.ID`，归入第八类一致性状态 **`IMPL_UNRESOLVED`（实现待定）**；
  3. **`EGRESS` 优先、`INGRESS` 互证**：调度方 `X` 的出站记录仍为跨进程调用的唯一权威统计源（沿用 §5.2）；若实现方 `B` 也在同批次被协同监控，其入站记录按接收进程直接定位，作为双端闭环 (`VERIFIED`) 的互证依据。

### 5.2 来源模块判定与去重权威原则 (`ResolveSourceModule`)

在跨进程调用统计中，**严格以发起方目标进程的 `EGRESS` 出站记录为唯一权威统计源，接收方的 `INGRESS` 仅用于构建父 Span 索引与外部调用方统计，彻底消除同一次往返双重计数**：

对于目标进程 $P$ 发出的出站请求报文 `egressReq`（已知 $P$ 承载的模块集合为 $M(P)$，且依据该批次固化的 `SessionProcessSnapshot` 动态解析；若进程内模块声明了 `contract_id != nil` 的分发依赖，`dependMap` 自动将其展开为对该契约 `service_name` 的依赖声明，故优先级 ② `UNIQUE_DEPEND` 判定逻辑完全不变）：

```mermaid
flowchart TD
    Start["输入: 进程 P 的出站请求 egressReq (svc, op) 与承载模块集合 M(P)"] --> SingleCheck{"|M(P)| == 1 ? (单模块独占进程)"}
    SingleCheck -- "是" --> HitSole["唯一定位 SrcModule = M(P)[0] (SINGLE_MODULE_PROC)"]
    SingleCheck -- "否 (|M(P)| >= 2 多模块共进程)" --> Step1{"优先级 ①: DOTR 扩展头存在非零 TraceID 与 FatherSpanID ?"}
    Step1 -- "存在" --> LookupSpan{"在同批次进程 P 的 INGRESS REQ 中查到 SpanID == FatherSpanID 的父请求?"}
    LookupSpan -- "查到且父请求已定位 DstModule" --> HitTrace["精确归属 SrcModule = ParentIngress.DstModule (TRACE_SPAN)"]
    Step1 -- "无扩展头" --> Step2
    LookupSpan -- "未查到父请求" --> Step2{"优先级 ②: 过滤 M(P) 中声明了 DEPEND(svc, op) 的子集 C"}
    Step2 -- "|C| == 1 (进程内唯一声明方)" --> HitUniqueDep["归属 SrcModule = C[0] (UNIQUE_DEPEND)"]
    Step2 -- "|C| == 0 或 |C| >= 2 (歧义或均未声明)" --> HitProcLevel["优先级 ③: 进程级归属 SrcModule=nil (PROCESS_LEVEL)；附带候选集合 Candidates"]
```

> [!NOTE]
> **单模块进程前置收敛与同机反查**：
> 1. 当进程 $P$ 仅承载 1 个模块（$|M(P)| = 1$）时，即便未携带 Trace 且未声明 `DEPEND`，调用方在物理上也必然是唯一的 $M(P)[0]$。前置收敛保证单模块进程的违规外联被 100% 裁决为 **`VIOLATION`**；
> 2. 同机跨进程互调时，Agent 依据 Socket inode 反查确认发起方 PID 属于纳管进程，从而将对端 INGRESS 关联至发起方进程，避免误判为外部未知调用。

### 5.3 架构一致性八分类比对规则与算法（[`ADR-16`](../05-adr/16-multi-implementation-service-contract-adr.md) §5）

比对算法只针对**“源模块所在进程在本批次被纳管监控”**的静态依赖边进行评估，输出互斥完备的**八类**比对结果（既消除因未监控模块产生的虚假未覆盖噪音，又严密区分双端闭环验证、单端出站证实、来源端进程级待定 `UNATTRIBUTED` 与多实现目的端实现待定 `IMPL_UNRESOLVED`）：

* **多实现契约前置展开与越权判定规则**：
  1. **静态分发边展开**：调度方 `X` 的分发依赖 `X ➔ contract` 在比对前按当前已登记实现集合自动展开为 `X ➔ B`、`X ➔ C`、`X ➔ D` 多条静态边，每条边携带 `(contract_id, impl_key)`，并各自独立套用下表的 `VERIFIED / OUTBOUND_VERIFIED / UNCOVERED / NOT_MONITORED / NOT_OBSERVABLE` 判定；
  2. **实现级越权 (`VIOLATION`)**：若实测定位到实现模块 `C`，但调用方 `X` 既未声明对该契约的分发依赖（`contract_id`）、也未声明对实现 `C` 的具体实现依赖（`service_id`）——例如 `X` 仅声明直接依赖具体实现 `B` 却在运行期调用了 `C`——则 `X ➔ C` 严格裁决为 **`VIOLATION`**；
  3. **契约实现覆盖率**：定义为 $\text{已验证实现数 (VERIFIED + OUTBOUND\_VERIFIED)} / \text{本批次可观测实现数}$，在库模块契约详情与治理大盘中展示。

| 结果分类枚举 | 中文标签 | 严格判定条件 | 架构治理含义与 UI 视觉语义 |
| :--- | :--- | :--- | :--- |
| **`VERIFIED`** | 已验证合规 | ① 内部跨进程依赖（含展开后的多实现静态边）：源端与目标实现端均已纳管监控，且实测归属到 `SrcModule -> DstModule` 的跨进程调用 $\ge 1$；<br>② 外部服务依赖（`is_external=true`）：目标无法安装 Agent，源端实测出站调用 $\ge 1$ 即完成终点证实 | 架构设计与实测运行完全一致且形成闭环（`--color-success` 绿色实线） |
| **`OUTBOUND_VERIFIED`** | 单端已出站 | 静态声明为内部跨进程依赖（含多实现展开边），源端已监控且实测出站调用 $\ge 1$（多实现下 `dst_addr` 已由 `endpoint_snapshots` 定位到目标实现），但**目标供给方物理进程未加入本次监控批次** | 源端确凿发起了契约调用，但目标端处于观测盲区，提示纳管目标端以完成双端闭环（`--color-info` 青色虚线 `6 3`） |
| **`VIOLATION`** | 架构漂移越权 | 实测精确定位到 `SrcModule`，但 `SrcModule` **未声明**对应 `DEPEND`（含调用未登记接口、调用未声明的独占模块、或仅依赖具体实现 `B` 却调用同契约另一实现 `C`） | **高优架构漂移告警**：源码存在未备案的私自跨进程调用（`--color-danger` 红色告警实线） |
| **`UNCOVERED`** | 本次未触发 | 静态声明为跨进程依赖（含多实现展开边），源模块与目标供给/实现模块所在进程**均在本次协同监控集合中**，但本次任务未捕获该调用 | 依赖已就绪但当前业务测试用例未覆盖该调用路径（`--color-text-muted` 灰色虚线 `5 5`） |
| **`NOT_MONITORED`** | 目标未纳管 | 静态声明为跨进程依赖（含多实现展开边），源模块所在进程已被监控，**目标供给/实现模块所在物理进程未加入本次监控批次，且实测未捕获调用** | 物理上不可观测且未产生出站调用，**不计入 `UNCOVERED` 覆盖率分母**（`--color-text-muted` 浅灰点划线 `2 4`） |
| **`NOT_OBSERVABLE`** | 进程内不可观测 | 静态声明为同进程内两模块调用（依据该批次快照 `SrcModule` 与 `DstModule` 绑定在同一 `ProcessID`） | 物理上走函数调用不经过内核网络栈，**不计入 `UNCOVERED` 分母**（`--color-info` 蓝色徽章） |
| **`UNATTRIBUTED`** | 进程级待定归属 | 多模块进程发出的调用缺少父 Trace 且存在重复声明，来源端只能定位于 `Process_P -> DstModule` | 提示在源码中补充 DOTR Trace 扩展头或在架构中心收敛重复契约（`--color-warning` 琥珀色边） |
| **`IMPL_UNRESOLVED`** | 实现待定 | 实测调用的 `ascServiceName` 命中已登记多实现契约（`dst_contract_id IS NOT NULL`），但目的实现模块无法定位（`dst_module_id IS NULL`，如 `dst_addr` 未命中 `endpoint_snapshots` 或对应进程未挂载） | **不计入 `UNCOVERED` 分母**；卡片直接提示「为该环境的对应实现进程设置 `advertise_ip` 或挂载对应进程」（`--color-warning` 琥珀色虚线 `6 3`） |

#### 一致性验证响应 DTO 结构 (`ConsistencyReportDTO`)

```go
package topology

type VerificationStatus string

const (
	StatusVerified         VerificationStatus = "VERIFIED"
	StatusOutboundVerified VerificationStatus = "OUTBOUND_VERIFIED"
	StatusViolation        VerificationStatus = "VIOLATION"
	StatusUncovered        VerificationStatus = "UNCOVERED"
	StatusNotMonitored     VerificationStatus = "NOT_MONITORED"
	StatusNotObservable    VerificationStatus = "NOT_OBSERVABLE"
	StatusUnattributed     VerificationStatus = "UNATTRIBUTED"
	StatusImplUnresolved   VerificationStatus = "IMPL_UNRESOLVED" // 第八类一致性状态：多实现契约命中但目的实现待定 (ADR-16)
)

type ConsistencyEdgeResult struct {
	Status              VerificationStatus `json:"status"`
	SrcModuleID         *uint64            `json:"src_module_id,omitempty"`
	SrcProcessID        *uint64            `json:"src_process_id,omitempty"`
	CandidateModuleIDs  []uint64           `json:"candidate_module_ids,omitempty"`
	DstModuleID         *uint64            `json:"dst_module_id,omitempty"` // nil 表示未登记外部接口 或 IMPL_UNRESOLVED 实现待定
	ContractID          *uint64            `json:"contract_id,omitempty"`   // 多实现服务契约 ID (ADR-16)
	ImplKey             string             `json:"impl_key,omitempty"`      // 命中的多实现业务身份标识，如 "b" (ADR-16)
	ServiceName         string             `json:"service_name"`
	Operation           string             `json:"operation"`
	ObservedCallCount   int64              `json:"observed_call_count"`
	AttributionMode     string             `json:"attribution_mode,omitempty"`
	SampleTrafficRecID  *uint64            `json:"sample_traffic_record_id,omitempty"`
}

type ConsistencyReportDTO struct {
	SessionID             uint64                  `json:"session_id"`
	StaticModelVersion    time.Time               `json:"static_model_version"` // 静态架构版本快照时刻，模型变更时缓存自动失效
	VerifiedCount         int                     `json:"verified_count"`
	OutboundVerifiedCount int                     `json:"outbound_verified_count"`
	ViolationCount        int                     `json:"violation_count"`
	UncoveredCount        int                     `json:"uncovered_count"`
	NotMonitoredCount     int                     `json:"not_monitored_count"`
	NotObservableCount    int                     `json:"not_observable_count"`
	UnattributedCount     int                     `json:"unattributed_count"`
	ImplUnresolvedCount   int                     `json:"impl_unresolved_count"` // 第八类 IMPL_UNRESOLVED 边计数 (ADR-16)
	Edges                 []ConsistencyEdgeResult `json:"edges"`
}
```

### 5.4 拓扑画布一致性比对叠加层机制 (Consistency Canvas Overlay)

依据 [`ADR-05`](../05-adr/05-hierarchical-topology-canvas-redesign.md) 与 [`ADR-16`](../05-adr/16-multi-implementation-service-contract-adr.md)，架构一致性验证结果不再孤立于独立二级页签，而是作为静态拓扑画布上的**动态增强图层 (Overlay)**：

1. **静态边归并、动态违规边合成与 `UNATTRIBUTED` / `IMPL_UNRESOLVED` 双视图归属**：
   - **静态已声明边归并**：`VERIFIED / OUTBOUND_VERIFIED / UNCOVERED / NOT_MONITORED / NOT_OBSERVABLE` 按公共架构元素键 `(src_module_id, dst_module_id, service_name)` 精确绑定至静态叶子边（在逻辑视图默认 `topo_contract_mode=AGG` 按接口聚合模式下，同一 `contract_id` 的各实现边收敛绑定至 `X ➔ A` 契约虚拟边）；
   - **`VIOLATION` 动态叶子边合成**：因静态 `traefik_module_dependencies` 表中不存在未声明越权调用，叠加引擎自动将 `status == 'VIOLATION'` 的结果动态合成为临时告警叶子边并并入 `rollup` 输入边集触发 ELK 增量重排；若 `dst_module_id == nil` 且 `contract_id == nil`，自动注入虚拟节点 `VIRTUAL_UNREGISTERED_INTERFACE`（「未登记接口」）作为连线终点；
   - **`UNATTRIBUTED`（来源待定）跨视图归属降级**：因 `src_module_id == nil`，在物理视图 (`PHYSICAL`) 下直接挂载于源进程 `proc_<src_process_id>`（若为 `PRIVATE` 私有进程则映射至 `candidate_module_ids` 所属的公共物理容器）；在逻辑视图 (`LOGICAL`) 下取 `candidate_module_ids` 在逻辑层级树中的**最近公共祖先（LCA，如同一功能组或子系统）**作为源节点参与上卷，若候选模块跨多个顶层子系统无单一容器祖先，则收敛至画布顶部一致性摘要栏的「待定归属 (`N`)」独立徽章抽屉展示；
   - **`IMPL_UNRESOLVED`（目的实现待定）双视图挂载规则（[`ADR-16`](../05-adr/16-multi-implementation-service-contract-adr.md) §5）**：
     - **逻辑视图 (`LOGICAL`)**：挂载于调用方模块指向定义该契约的 `CONTRACT_LIB` 库模块的契约虚拟边 `X ➔ A` 上（在默认 `AGG` 聚合模式下直接合入 `X ➔ A` 虚拟边；在 `EXPAND` 按实现展开模式下，该虚拟边 `X ➔ A` 仅在存在 `IMPL_UNRESOLVED` 时按需出现以承载该告警）；
     - **物理视图 (`PHYSICAL`，不渲染库模块)**：动态注入虚拟节点 `VIRTUAL_IMPL_UNRESOLVED_<contract_id>`（节点类型 `IMPL_UNRESOLVED`，展示标题「实现待定: `pkg.IfaceSvc`」，注入方式与 `UNREGISTERED_INTERFACE` 一致），将 `IMPL_UNRESOLVED` 边挂载于源进程/模块 ➔ 该动态虚拟节点上；
2. **最差状态优先级收敛与实测调用热力**：
   - 当多个底层模块调用被上卷至折叠容器（如子系统间、进程组间）或在逻辑视图 `AGG` 模式下聚合至契约虚拟边 `X ➔ A` 时，汇总边状态严格遵循**最差优先级收敛**：
     $$\text{VIOLATION} \succ \text{UNATTRIBUTED} \succeq \text{IMPL\_UNRESOLVED} \succ \text{UNCOVERED} \succ \text{NOT\_MONITORED} \succ \text{NOT\_OBSERVABLE} \succ \text{OUTBOUND\_VERIFIED} \succ \text{VERIFIED}$$
   - 若某子系统间存在哪怕 1 条未声明的跨子系统调用，整条聚合连线即被渲染为 `--color-danger` 告警红实线并伴随脉冲动效；边标签同步展示实测调用总次数（如 `∑calls: 1,420`）；
3. **闭环穿透至公共四段解剖抽屉**：
   - 用户点击任何一条一致性边，右侧滑出 `EdgeDetailDrawer` 依赖明细抽屉（对 `IMPL_UNRESOLVED` 条目突出显示实测 `dst_addr` 与「为该环境的对应实现进程设置 `advertise_ip` / 挂载对应进程」修复指引）；点击任意包含 `SampleTrafficRecID` 的接口项，直接调用公共组件 `PacketAnatomyDrawer` 检视四段二进制报文（公共头位图、会话头拆解、DOTR Trace、Protobuf JSON 树与 Hex 字节），实现从“宏观架构违规”到“微观网络字节”的完整追溯闭环。

---

## 6. 大规模拓扑渐进式下钻聚焦、多维降噪与位置记忆混合布局引擎 (`F-04.1`~`F-04.4`, [`ADR-15`](../05-adr/15-scalable-topology-focus-and-layout-persistence-adr.md) & [`ADR-16`](../05-adr/16-multi-implementation-service-contract-adr.md))

在 25 个子系统 × 10 个功能组 × 10 个功能模块（约 **2,775 个节点 + 5,000~10,000 条底层边**）的生产规模下，前端拓扑引擎采用「全量下发、前端纯函数裁剪」策略，将渲染链路重排为**五段式纯函数管线**，并新增 **`subgraph.ts` / `noiseReduction.ts`（探索与降噪）** 与 **`layoutMerger.ts` / `layoutPersistence.ts`（位置记忆混合布局与持久化）**；后端由 `traefik_topology_layouts`（第 18 张表，连同 ADR-16 第 19 张表 `traefik_service_contracts` 共 19 张核心业务表）承载位置层（团队基准）与视角层（个人书签）。

> [!IMPORTANT]
> 本节全部阈值、URL 参数、CSS 类名与节点数据字段以 [`ADR-15` §6 统一参数与命名字典](../05-adr/15-scalable-topology-focus-and-layout-persistence-adr.md) 及 [`ADR-16` §6](../05-adr/16-multi-implementation-service-contract-adr.md) 为唯一基准。

### 6.1 五段式渲染管线与作用域裁剪 (`overlay.ts` → `subgraph.ts` → `rollup.ts` → `noiseReduction.ts` → `layoutMerger.ts`)

```mermaid
flowchart LR
    S1["① synthesizeOverlay<br/>(overlay.ts)"] --> S2["② scopeAndFilter<br/>(subgraph.ts)"]
    S2 --> S3["③ aggregateRollup + pruneIsolated<br/>(rollup.ts / subgraph.ts)"]
    S3 --> S4["④ foldHubEdges<br/>(noiseReduction.ts)"]
    S4 --> S5["⑤ planLayout / ELK / resolveOverlapsBottomUp<br/>(layoutMerger.ts)"]
```

1. **① 一致性叠加与多实现契约模式合成 (`synthesizeOverlay`)**：从原 `rollupEdges` 中拆出动态 `VIOLATION / UNATTRIBUTED / IMPL_UNRESOLVED` 边、`VIRTUAL_UNREGISTERED_INTERFACE` / `VIRTUAL_IMPL_UNRESOLVED_<contract_id>` 虚拟节点合成、逻辑视图 `topo_contract_mode=AGG|EXPAND` 分发边聚合/展开转换、`REALIZATION` 图层开关过滤与静态边状态映射；静态边匹配以 `(src_module_id, dst_module_id)` 建立索引，消除 O(报告条数 × 边数) 嵌套扫描。`rollupEdges(...)` 保留为兼容门面（`aggregateRollup(nodeMap, synthesizeOverlay(...), expandState)`）。
2. **② 作用域裁剪 (`scopeAndFilter`)**：**不删除任何节点**（`nodeMap` 始终全量，保证 `rep()` 收敛不丢边），输出：

```typescript
export interface TopologyFilterState {
  focusNodeId: string | null;          // 聚焦根容器 ID (如 "sub_1", "grp_10", "pgrp_3", "proc_7")
  egoNodeId: string | null;            // 邻域追踪中心 ID (模块或容器)
  egoHops: 1 | 2;                      // 有向邻域跳数，默认 1
  subsystemIds: string[];              // 顶层容器多选白名单 (空数组表示不过滤)
  onlyMyScope: boolean;                // 仅看当前登录用户负责的节点
  status: 'ALL' | 'ABNORMAL' | 'VIOLATION_ONLY'; // 未挂载一致性报告时忽略
  hideIsolated: boolean;               // 边上卷后隐藏孤立节点
  hubFold: boolean;                    // 是否开启 Hub 插头折叠
  contractMode?: 'AGG' | 'EXPAND';     // 逻辑视图多实现契约展示模式，默认 'AGG' (URL: topo_contract_mode, ADR-16)
  showRealization?: boolean;           // 逻辑视图是否显示 REALIZATION 实现关系边图层 (ADR-16)
}

export type NeighborDirection = 'UPSTREAM' | 'DOWNSTREAM' | 'BIDIRECTIONAL';

export interface ScopedGraph {
  visibleIds: Set<string>;                     // 显式可见集 (取代“祖先全部展开即可见”规则)
  effectiveExpandState: ExpandStateMap;        // 用户展开态 ⊕ 透明祖先 ⊕ 强制折叠邻居 ⊕ 邻域祖先展开
  relativeDepth: Record<string, number>;       // depth − 聚焦根 depth (全局态等于 depth)
  neighborMeta: Record<string, NeighborDirection>; // 外部邻居卡方向
  breadcrumbs: GraphNode[];                    // 根 → 聚焦根 的面包屑路径
  estimatedVisibleCount: number;               // 供统一守卫使用
}
```

#### 核心裁剪规则
1. **统一可见规模守卫 (`estimateVisibleNodeCount` + `applyVisibilityChange`)**：
   - 页面层所有改变展开态或可见集的入口——层级滑杆、全展、单点展开、滚轮语义展开、搜索定位、邻域追踪、多维过滤、聚焦切换、书签应用——统一调用 `applyVisibilityChange(next)`，对候选状态执行同一裁剪逻辑仅计数；
   - 预估 `> MAX_SAFE_VISIBLE_NODES (150)` 时拦截，**与是否处于聚焦/过滤态无关**；弹出引导（先聚焦 / 先过滤 / 仍要展开），滚轮语义展开改为非模态提示并放弃本次展开；
   - **删除**现有“输入搜索关键字自动展开全部命中节点父链”的副作用。
2. **容器下钻聚焦 (`focusNodeId`)**：
   - 内部集 $V_{\text{in}}$ = 聚焦根 + 其全部后代；聚焦根默认展开 `1` 层，层级滑杆在聚焦态下按 `relativeDepth` 计量；
   - **透明祖先**：聚焦根的祖先在 `effectiveExpandState` 中置为展开但不进入 `visibleIds`，聚焦根即成为画布根（`buildElkGraph` 已按“父节点不可见即为根”处理，无需改写节点 `ancestors`）；
   - **外部邻居代表 (`resolveExternalRep`)**：对一端在 $V_{\text{in}}$、另一端 $u \notin V_{\text{in}}$ 的边（含 ① 合成的动态边），$R_{\text{ext}}(u)$ 取 $u$ 祖先链上 $\text{LCA}(u, \text{focus})$ 的直接子节点，无公共祖先时取 $u$ 的顶层祖先；若结果为 `ext_container`，则 `EXTERNAL_SYSTEM` / `EXTERNAL_ENDPOINT` 叶子单独成卡；`VIRTUAL_UNREGISTERED_INTERFACE` 与 `VIRTUAL_IMPL_UNRESOLVED_*` 单独成卡；
   - $R_{\text{ext}}$ 在 `effectiveExpandState` 中强制折叠、其祖先链置为透明展开，使 `rep(u) = R_ext(u)`；`meta.isExternalNeighbor = true`，`neighborMeta` 记录方向；两端均在 $V_{\text{in}}$ 外的边全部剔除；
   - 点击外部邻居卡 `[↗ 切换聚焦]` 即将 `focusNodeId` 切换至该邻居。
3. **有向 N-Hop 邻域 (`extractEgoNetwork`)**：
   - 在叶子层有向图上，上游沿反向边扩展 `egoHops` 跳、下游沿正向边扩展 `egoHops` 跳后取并集，**严禁混合方向扩展**；容器作为中心时以其全部叶子后代为种子；
   - 命中叶子的祖先链在 `effectiveExpandState` 中自动展开，未命中的兄弟节点不可见。
4. **多维切片过滤 (`applySliceFilters`)**：各维度取交集，命中节点的祖先链（直至当前画布根）自动保留并重新计算折叠徽章计数，严禁产生无父容器的悬空内部节点：
   - **`subsystemIds`**：仅保留所选顶层容器子树及其互调边；
   - **`onlyMyScope`**：命中 `meta.owner_id === currentUserId` 的子系统（整棵子树）与模块（后端 Graph DTO 已为 `MODULE` 节点输出 `owner_id`），并保留命中模块的叶子级 1-Hop 上下游；
   - **`status`**：`ABNORMAL` 保留至少关联 1 条 `VIOLATION / UNATTRIBUTED / IMPL_UNRESOLVED / UNCOVERED` 边的节点，`VIOLATION_ONLY` 仅保留关联 `VIOLATION` 边的节点。
5. **孤立节点裁剪 (`pruneIsolated`)**：在 ③ 边上卷之后执行，隐藏无任何关联汇总边的可见叶子/折叠容器，并级联隐藏被清空的容器。

### 6.2 1-Hop 聚光灯 (`Spotlight`) 与高扇入公共节点插头折叠 (`Hub Pill`)

1. **聚光灯染色 (`computeSpotlightSet`)**：
   - 悬停（`80ms` 防抖）或单击选中节点 $v$（或汇总边 $e$）时，纯函数 `computeSpotlightSet(activeId, rollupEdges, nodeMap)` 计算关联节点集合 $S_{\text{node}}$（含命中节点的祖先容器，容器不淡化）与关联边集合 $S_{\text{edge}}$；
   - 非命中节点附加 `.code-arch-node--dimmed`（`opacity: var(--arch-dim-node-opacity)`，即 `0.2`），非命中边附加 `.code-arch-edge--dimmed`（`0.15`）；单击锁定，点击空白或 Esc 清除；聚光灯为渲染期计算，不触发重排。
2. **高扇入公共节点插头折叠 (`foldHubEdges`)**：
   - 开启 `hubFold` 时，在 ③ 边上卷之后统计每个可见节点的跨顶层入边中**不同调用方顶层容器**数量，`≥ HUB_CALLER_THRESHOLD (8)` 且不在 `expandedHubIds` 中即判定为 Hub；
   - 移除该 Hub 的跨顶层汇总边，**`VIOLATION / UNATTRIBUTED / IMPL_UNRESOLVED` 状态边豁免折叠**；为每个调用方生成插头徽章 `HubPillSummary { hubId, hubName, contractCount, worstStatus }`，渲染为 `.code-arch-hub-pill`（如 `🔗 → 统一鉴权 (3 契约)`），徽章颜色继承被折叠边的最差状态；
   - 点击徽章将该 Hub 加入 `expandedHubIds`，恢复其完整连线。
3. **语义 LOD**：`.code-arch-canvas--lod-macro`（`zoom < 0.45`，简版大字卡片）/ `--lod-medium` / `--lod-detail`（`zoom ≥ 0.85`）。

### 6.3 位置记忆混合布局引擎 (`layoutMerger.ts`)

1. **视口相机锚定定律 (`Viewport Camera Retention`)**：
   - 废除 `ArchCanvas.tsx` 中监听 `layoutNodes.length` 自动触发全局 `rfInstance.fitView()` 的逻辑；
   - **仅在以下场景执行全局 `fitView`**：① 首次加载完成且布局无有效视口；② 切换 `LOGICAL / PHYSICAL` 视图；③ 切换聚焦域；④ 用户点击 `[居中]` 或 `[重新自动排版]`；⑤ 应用个人书签；
   - 单点展开/折叠与拖拽严禁 `fitView`；若被操作节点绝对坐标位移 $(\Delta x, \Delta y)$，按 `viewport -= Δ × zoom` 补偿（本算法下锚点不动，补偿量通常为 0）。
2. **位置记忆与布局模式 (`planLayout`)**：
   - 维护会话内 `positions: Record<string, { x: number; y: number }>`（全部已排布节点相对 `parentId` 父容器的局部坐标，画布根节点相对根原点）与 `pinned: Set<string>`（用户拖拽过的节点，语义为“重新自动排版时保留”）；
   - `GLOBAL_ELK`：首次加载且无任何持久化坐标，或用户点击 `[重新自动排版]`（`pinned` 节点保留坐标）；
   - `LOCAL_ELK`：展开容器 $C$ 且其子节点缺少坐标时，仅对 $C$ 子树执行局部 ELK 得到 $C$ 新尺寸，$C$ 左上锚点与 $C$ 外全部节点原位不动；
   - `FAST_PATH`：全部可见节点均有坐标时免 ELK 直接套用；
   - 折叠容器 $C$ 时 $C$ 收缩为卡片尺寸，兄弟节点不回缩；
   - **拖拽结束 (`onNodeDragStop`)**：坐标钳制于父容器内边距内（越界则撑开父容器），写入 `positions` 与 `pinned`，以被拖节点为锚点执行碰撞消解，**严禁触发 ELK**；`draggedNodeIds` 不再作为 ELK 重排依赖。
3. **自底向上碰撞消解 (`resolveOverlapsBottomUp`)**：
   - 在锚点所在父容器内按距锚点由近及远逐个处理兄弟节点，与已落定集合检测包围盒 $[x, x+w+\text{gap}] \times [y, y+h+\text{gap}]$（`gap = LAYOUT_SIBLING_GAP = 32px`）重叠，重叠时沿最小穿透轴（水平向右或垂直向下）平移；
   - **锚点永不移动；未固定节点优先移动；`pinned` 节点仅在确实重叠时被推开并保持原相对方位**，同步更新其固定坐标；
   - 父容器尺寸重算为子节点包围盒 + 内边距（顶部 `52px`、其余 `16px`，与 ELK `elk.padding` 一致），尺寸变化时以该父容器为新锚点在祖父容器继续消解，直至根或尺寸不变；单调平移 + 每层单次扫描保证终止。
4. **边退化判定 (`markDegradedEdges`)**：凡边的源/目标或其任一祖先相对最近一次 ELK 输出的绝对坐标发生位移（含被推开节点与被移动容器的全部后代），或该汇总边在最近一次 ELK 输出中不存在拐点数据（如局部排版或折叠后新生成的汇总边），标记 `isDraggedDegraded = true` 并由 `<RollupEdge>` 使用 `getSmoothStepPath` 实时绘制；`FAST_PATH` 下全部边退化（ADR-15 决策 #9）。
5. **外部邻居卡排版**：`UPSTREAM` 邻居设置 `elk.layered.layering.layerConstraint = FIRST`，`DOWNSTREAM` 设置 `LAST`，`BIDIRECTIONAL` 不约束；外部邻居卡与聚焦根坐标属于聚焦专属根层，不进入持久化。
6. **架构演进增量 Diff 合并 (`diffLayoutAgainstGraph` + `placeNewNodes`)**：
   - 后端 `TopologyGraphDTO.structure_version` 仅对排序后的 `(id, parent_id)` 计算，与已保存布局的 `structure_version` 相同则跳过 Diff；
   - 不同则：已保存坐标中不存在于当前图的节点静默清理；当前可见、无保存坐标、但父容器下至少有 1 个已保存坐标兄弟的节点视为新增，放置于父容器已排布包围盒右侧/底部网格空位，`data.isNewInLayout = true` 并渲染 `✨ 新增` 徽标；
   - 仅当 `addedCount > 0 || removedCount > 0` 时显示琥珀色提示条，纯依赖边、名称、描述或环境端点变化不提示。

### 6.4 位置层 + 视角层双层持久化 (`layoutPersistence.ts`)

1. **生效坐标叠加**：`composeEffectivePositions` = ELK 结果 ⊕ 团队基准（`SHARED`，按 `view` 唯一，完整坐标）⊕ 个人书签坐标差量（`PRIVATE`）⊕ `localStorage` 草稿差量；坐标为相对父坐标，聚焦视图与全景视图复用同一位置层；
2. **加载优先级 (`resolveInitialLayout`)**：基线 = URL `topo_layout_id` 书签 ≻ 当前 `(view, focus_scope)` 默认书签 ≻ 团队基准 ≻ 无（ELK）；草稿（键 `traefik_topo_draft_v2_${userId}_${view}`，含 `base = { canonical_revision, bookmark_id }`）仅在 `base` 与基线一致时叠加，否则丢弃并提示；
3. **持久化切片 (`extractPersistablePositions`)**：聚焦态仅持久化聚焦根严格后代坐标；保存个人书签时 `diffOverrides` 仅提交相对团队基准变化或被固定的节点；
4. **团队基准发布**：读取最新基准 ➔ 合并可持久化坐标 ➔ 携带 `expected_revision` 整体提交，`409 LAYOUT_REVISION_CONFLICT` 时提示刷新重试；
5. **降级**：书签 `403 / 404` 降级至团队基准并 toast；书签 `focus_scope` 容器已删除时降级至全局。
