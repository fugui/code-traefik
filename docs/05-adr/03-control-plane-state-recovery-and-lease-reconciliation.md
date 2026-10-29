# ADR-03: 控制面状态自愈恢复、Agent 双向租约对账与孤儿探针防御

- **文档编号**：`03-control-plane-state-recovery-and-lease-reconciliation.md`
- **日期**：2026-10-29
- **状态**：已采纳 (Accepted)
- **关联设计**：[`01-system-architecture.md`](../02-design/01-system-architecture.md)、[`02-data-plane-and-cros-rpc-engine.md`](../02-design/02-data-plane-and-cros-rpc-engine.md)、[`04-control-plane-and-web-console.md`](../02-design/04-control-plane-and-web-console.md)、[`05-database-schema.md`](../02-design/05-database-schema.md)

---

## 1. 背景与决策问题 (Context & Problem Statement)

在分布式流量录制与重放系统中，控制面（`code-traefik-server`）与多台宿主机数据面 Agent（`code-traefik-agent`）通过内网通信。生产环境中不可避免面临以下异常场景：
1. **控制面偶发崩溃或热重启**：主控重启后，进行中的录制批次或重放任务在内存中丢失，数据库中 Agent 仍标记为被占用，导致后续用户遭遇 `409 Conflict` 租约死锁；
2. **Agent 宿主机意外断电重启**：系统重新启动后 BPF Map 与内核探针自动重置，但数据库中该 Agent 仍记录为 `ONLINE` 且持有租约；
3. **网络抖动与分区**：主控与 Agent 之间短时失联或长时失联，若 Agent 不主动安全清场，残留的 eBPF `cgroup/connect4` 重定向探针将导致业务出站流量永久黑洞或假阻断；
4. **多机租约争用死锁**：协同录制任务涉及跨多台宿主机的并发加锁，若缺乏严格的锁序与超时兜底，可能引发分布式死锁。

---

## 2. 方案权衡与最终决策 (Options & Decisions)

### 决策 1：主控单实例部署约束与启动自愈流水线 (Startup Reconciliation Pipeline)

- **主控单实例部署约束**：`code-traefik-server` 定位为研发测试工作台的轻量控制面，明确采用单实例模式运行（杜绝多主分布式写冲突与复杂选主成本）。
- **启动自愈流水线**：
  1. **扫描未决任务**：查询所有处于 `RECORDING` / `STOPPING` 态的 `RecordingSession`，以及处于 `PENDING` / `ARMED` / `INJECTING` 态的 `ReplayTask`；
  2. **下发终止指令**：对受影响批次锁定的全部 Agent 发送 `POST /agent/v1/sessions/drain` 或 `POST /agent/v1/replays/abort`；若 Agent 在线，则立即回收内核探针并清理内存；若 Agent 离线，则记录警告日志；
  3. **终态收敛**：将未决任务统一原子更新为 `FAILED`，并回写 `error_message = "Server restarted during task execution"`；
  4. **全量解锁 Agent**：清空所有失联或残留租约（`lease_type = ''`, `lease_ref_id = NULL`, `lease_holder_id = NULL`, `lease_expires_at = NULL`），使整个系统在启动后 3 秒内恢复完全可用状态。

### 决策 2：双向期望租约心跳对账协议 (Bidirectional Lease Reconciliation)

Agent 每 5 秒向主控发送一次心跳请求（`POST /api/v1/traefik/agents/heartbeat`），双方通过双向声明达成租约一致：
1. **Agent 上报自身事实状态**：上报字段包含 `boot_id`（宿主机 `/proc/sys/kernel/random/boot_id`）、当前租约状态 `current_lease: {type, ref_id}`、内存队列水位与丢帧数；
2. **主控下发期望状态响应头**：心跳响应头携带 `X-Expected-Lease-Type` 与 `X-Expected-Lease-Ref-ID`；
3. **分歧自愈处理**：
   - 若主控发现数据库中该 Agent 的 `boot_id` 发生变更（说明宿主机发生过重启），即使此前有活跃租约，主控也立刻判定历史租约失效，自动更新数据库为最新 `boot_id` 并释放租约；
   - 若 Agent 发现主控返回的期望租约为无（`NONE`），而自身内存中处于 `RECORDING` 或 `REPLAY`，Agent 立即触发本地清场逻辑（调用 `UnmountAllProbes()`），消除孤儿任务；
   - 若主控记录中无租约，但 Agent 上报持有租约，主控在响应中明确指令 Agent 回滚并清理。

### 决策 3：Agent 宿主机看门狗与 5 分钟失联安全清场 (Fail-Closed Safety)

- **失联宽限期**：Agent 内部维持失联看门狗定时器。若连续 5 分钟（`300s`）未能与主控成功建立心跳交互，Agent 将被判定为进入“网络孤岛”状态。
- **强制脱网安全自愈**：
  1. 立即停止一切被动旁路复制；
  2. 将 `cgroup/connect4`、`sys_enter_*` Tracepoints 及 `sock_ops` 钩子自内核中彻底卸载（销毁 `bpf_link`）；
  3. 彻底释放透明代理与 Mock 监听端口；
  4. 恢复所有出站规则，确保即使在完全脱管状态下，宿主机上的业务进程亦绝不受任何干扰（保持业务透明）。

### 决策 4：严格升序加锁与惰性租约过期安全网

- **确定性加锁顺序**：无论录制批次还是重放任务，涉及多台 Agent 租用时，主控必须首先对目标 Agent ID 数组进行严格自然升序排序（`sort.Slice(agentIDs, ...)`），并在单个事务中依序执行 `SELECT * FROM traefik_agents WHERE id IN (...) ORDER BY id ASC FOR UPDATE`，从根本上杜绝死锁。
- **软硬双重租约超时**：租约获取时设定硬性过期时间 `lease_expires_at = NOW() + INTERVAL '35 minutes'`（稍大于单次录制上限 30 分钟）。任何后续调度前置校验时，若检测到 `lease_expires_at < NOW()`，即使此前任务异常崩溃未回写数据库，系统也自动执行惰性回收解锁，杜绝死锁驻留。

---

## 3. 架构收益与合规约束 (Consequences & Constraints)

- **确定性自愈**：无需人工登录后台或数据库执行修数 SQL，主控与 Agent 任意一方故障恢复后均能在 5~10 秒内自动完成状态对齐；
- **业务零残留风险**：失联 5 分钟自动卸载探针，从物理层面杜绝了遗留 eBPF 规则拦截业务生产通信的可能；
- **排他互斥性守护**：有效保障共享环境（`SHARED` Agent）重放时单时刻单租户，多人并发争抢确定性返回 `409 Conflict`。
