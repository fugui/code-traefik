# Milestone 3 作战卡：数据面 Agent 受控信道、嵌入式 `cilium/ebpf` 采集与零 `.pb` 二进制自愈重放

- **文档编号**：`04-m3-agent-dataplane-ebpf-and-replay.md`
- **文档版本**：v1.0.0
- **更新日期**：2026-10-29
- **归属阶段**：阶段三/四：M3 TDD 任务拆解与验收标准 (`04-tasks`)
- **覆盖特性与场景**：`F-07`、`F-08`、`F-09`、`F-13`、`SC-03`(Agent 侧)、`SC-05`(采集侧)、`SC-07`(自愈重放侧)、`SC-08`(Mock 未命中零外泄侧)
- **设计参考基线**：[`01-system-architecture.md` §2.1 & §4](../02-design/01-system-architecture.md)、[`02-data-plane-and-cros-rpc-engine.md` §2~§3](../02-design/02-data-plane-and-cros-rpc-engine.md)、[`02-ebpf-hook-and-dual-subsystem-adr.md`](../05-adr/02-ebpf-hook-and-dual-subsystem-adr.md)、[`01-http-api-contract.md` §3](../03-api/01-http-api-contract.md)

---

## 1. 交付包与文件清单 (Deliverable Packages & Files)

| 操作类型 | 包 / 文件路径 | 职责与实现范围（详细算法与 BPF 钩子见设计文档链接） |
| :---: | :--- | :--- |
| **NEW** | `pkg/ebpf/bpf/cros_probe.c`<br>`pkg/ebpf/gen.go` | CO-RE BPF C 程序与 `bpf2go` 生成指令：入站 `accept` + `read/write` 族 Tracepoints + `ing_ringbuf`；出站 `cgroup/connect4`（含 `agent_pid` 自环豁免与重放 `MODE_REPLAY` Fail-Closed `EPERM`）+ `sock_ops` Cookie 原目的还原；`sched_process_exec` 进程重启热捕获（见 [`02-data-plane-and-cros-rpc-engine.md` §2.1~§2.4](../02-design/02-data-plane-and-cros-rpc-engine.md)） |
| **NEW** | `pkg/ebpf/proc_scanner.go` | 纯 Go 标准库只读解析 `/proc`（严禁 `os/exec`）：按 `listen_port` / `cmd_pattern` 反查实时 PID、候选 LISTEN 进程探查、监控前存量 `ESTABLISHED` 长连接诊断 (A-01)、同机入站连接 Socket inode 对端进程反查 |
| **NEW** | `pkg/ebpf/manager.go`<br>`pkg/ebpf/ingress_reader.go`<br>`pkg/ebpf/egress_proxy.go` | Effective Capability 校验（`CAP_BPF` + `CAP_NET_ADMIN` + `CAP_PERFMON` + `CAP_SYS_PTRACE`）、基于 `bpf_link` 零残留挂载/卸载（不 pin bpffs）、多 PID / 下游规则热增删、`Gap-Aware StreamFramer`、`GlobalFramerBudgetPool` (64MB) 与出站透明 TCP 代理 |
| **NEW** | `pkg/ebpf/ebpf_unit_test.go`<br>`pkg/ebpf/ebpf_linux_test.go` | 非特权用户态逻辑单测（`/proc` 模拟解析、规则编译、断流重同步）与 `//go:build linux && ebpf` 内核态特权隔离集成测试 |
| **NEW** | `pkg/replay/matcher.go`<br>`pkg/replay/healer.go`<br>`pkg/replay/executor.go`<br>`pkg/replay/replay_test.go` | 零 `.pb` `ScopedMockMatcher`（局部精确 Payload SHA256 -> 局部次序 -> 全局兜底 -> `MOCK_MISS` 静默零外泄、`Oneway` 免应答、`MNG` 同类回放）、`HealSingleResponseFrame` / `HealResponseFrames` 表驱动头部偏移自愈（V1~V4 `RSP` 及多段 `RSP_SEG`）与 TCP 请求注入器（见 [`02-data-plane-and-cros-rpc-engine.md` §3](../02-design/02-data-plane-and-cros-rpc-engine.md)） |
| **NEW** | `internal/agent/server/http_server.go`<br>`internal/agent/server/buffer.go`<br>`internal/agent/server/server_test.go` | 源 IP 白名单中间件（`[]netip.Prefix`，忽略 `X-Forwarded-For`）、`crypto/rand` 一次性 `ClaimCode`、`SingleTenantGuard`、`BoundedSessionBuffer` (64MB `Drop-Newest` 背压)、`GracePeriodWatchdog` (5m 失联自动卸载探针)、9 个下行 REST 接口与 `/agent/v1/stream` 56B 二进制帧推送 |
| **NEW** | `cmd/agent/main.go` | `code-traefik-agent` 独立守护进程入口（参数解析、信号拦截 `SIGTERM`/`SIGINT` 强制调用 `PurgeAllMonitors()` 释放全部 `bpf_link` 与本地监听端口） |

---

## 2. TDD 先行测试矩阵 (Test-First Matrix)

> **强制约束**：测试内所有 HTTP / WebSocket / 模拟 CROS RPC TCP 服务强制监听 `127.0.0.1:0`；内核 eBPF 挂载用例强制添加文件头 `//go:build linux && ebpf`（无 `CAP_BPF` 特权时自动 `t.Skip` 并输出缺失权限说明）。

| 目标测试文件 | 测试函数名 | 输入场景与前置条件 | 预期断言结果 (Assertions) |
| :--- | :--- | :--- | :--- |
| `internal/agent/server/server_test.go` | `TestAgentZeroSchemaAndZeroDBDeps` | 执行 `go list -deps ./cmd/agent ./pkg/ebpf ./pkg/replay` 并扫描源码 AST 是否引用 `os/exec` | 依赖闭包中零 `gorm.io`、零 `code-common/backend/gormdb`、零 `dynamicpb`、零 `protoregistry`；零 `os/exec` 外部命令拼接 (`F-10.3`) |
| `internal/agent/server/server_test.go` | `TestAgentIPAllowlistAndClaimCode` | 1. 配置 `--allow-ips=127.0.0.1` 的 Agent 收到非白名单源 IP 请求（附带伪造 `X-Forwarded-For: 127.0.0.1`）；<br>2. 白名单 IP 携带错误/正确 `X-Claim-Code` 请求 `/agent/v1/handshake` | 1. 忽略伪造头，非白名单 IP 直接返回 `403 Forbidden` (`F-07.2`, `SC-03`)；<br>2. 错误 `ClaimCode` 返 `403`，正确 `ClaimCode` 返回 `HandshakeResp` (`ProtocolVersion="1.0"`) |
| `internal/agent/server/server_test.go` | `TestBoundedSessionBufferBackpressureAndGracePurge` | 1. 设定会话缓冲上限 `1MB`，暂停 WebSocket 消费端并连续灌入 `2MB` 采集帧；<br>2. 断开主控连接并将失联看门狗宽限期缩短为 `200ms`；<br>3. 心跳下发 `X-Expected-Lease-Type: ""` 而 Agent 本地仍持有活跃租约 | 1. 缓冲驻留字节严格 `<= 1MB`，新帧非阻塞丢弃且 `dropped_frames` 精确累加 (`F-07.3`)；<br>2. 超宽限期后自动触发 `PurgeAllMonitors()`，`Attached == false` 且本地租约清空 (`F-07.4`)；<br>3. 心跳租约失调立即自洁释放 |
| `pkg/ebpf/ebpf_unit_test.go` | `TestProcScannerPIDResolveAndEstablishedConns` | 基于临时伪造 procfs 目录树（及真实 `127.0.0.1` 预建 TCP 长连接）：<br>1. 下发 3 个进程规格（其中 1 个已预建指向下游的长连接，1 个端口不存在）；<br>2. 模拟其中 1 个目标进程重启更换 PID；<br>3. 单独调用 `DELETE` 移除 1 个进程监控 | 1. 2 个存在进程状态为 `MONITORING`（预建连接进程 `EstablishedConns >= 1` 且 `NeedReconnect == true`），不存在端口返回 `NOT_FOUND` (`F-08.3`, `F-08.4`)；<br>2. 2s 巡检捕获新 PID 并输出 `PROCESS_RESTARTED` 事件 (`F-08.2`)；<br>3. 被删进程停止采集，其余进程无中断继续工作 (`F-08.1`) |
| `pkg/ebpf/ebpf_unit_test.go` | `TestGapAwareIngressReaderAndBudgetPool` | 1. 模拟 RingBuf 事件 `stream_off` 发生跳变断流后紧跟新 `CSE.TCP` 报文；<br>2. 模拟 2000 条空闲半包连接超出 `64MB` `GlobalFramerBudgetPool` | 1. 输出 `Flags` 含 `HAS_GAP` 标记，清空残缺半包后准确重同步切出后续完整报文；<br>2. 按 LRU 淘汰冷连接缓存，总分帧内存严格 `<= 64MB` |
| `pkg/ebpf/ebpf_unit_test.go` | `TestEbpfCapabilityCheckWithoutPrivilege` | 注入无 `CAP_BPF` 的 Capability Mock 探测器后调用 `/agent/v1/monitors/sync` | 拒绝挂载并返回 `503` 及缺失的 `CAP_BPF` / `CAP_NET_ADMIN` 明确诊断信息 (`F-09.3`) |
| `pkg/ebpf/ebpf_linux_test.go`<br>*(`//go:build linux && ebpf`)* | `TestEbpfSameHostDualProcessLoopbackBypass` | 在具备 `CAP_BPF` + `CAP_NET_ADMIN` 的 Linux 环境拉起同机 `127.0.0.1` 上的模拟 CROS RPC 进程 `PID_A`、`PID_B`（执行 `外部 -> PID_A -> PID_B` 调用）与未纳管进程 `PID_C`，随后停止监控并发送 `SIGTERM` | 1. `PID_A` 的入站与出站、`PID_B` 的入站报文均完整采集，无自环死锁；<br>2. `stat_bypassed_self >= 1`；`PID_C` 零重定向干扰；<br>3. 停止监控及 `SIGTERM` 退出后 `Attached == false`，内核零残留 `bpf_link` (`F-09.1`, `F-09.2`, `F-09.4`, `SC-05`) |
| `pkg/replay/replay_test.go` | `TestScopedMockMatcherExactAndFallback` | 预置 2 条入站请求的局部 `ScopedMocks` 与全局兜底池：<br>1. 实时下游请求携带全新随机 `ullSNO`、新 `TraceBuff` 与新时间戳，Payload 不变；<br>2. Payload 含动态随机数导致哈希未命中；<br>3. 下游多发起第 3 次未录制的调用；<br>4. 发送 `ucReqType=3 (Oneway)` 与 `ucMsgType=5 (MNG)` 帧 | 1. 精确命中 `SCOPED_HIT`；<br>2. 按同接口录制调用次序兜底命中 (`F-13.2`)；<br>3. 产生 `MOCK_MISS` 明细，Mock 连接保持静默等待目标超时，真实下游监听探针确认收到 `0` 字节连接 (`F-13.3`, `SC-08`)；<br>4. `Oneway` 仅记录不回包，`MNG` 同类原样回放并覆写 `ullSNO` (`F-13.5`, `F-13.6`) |
| `pkg/replay/replay_test.go` | `TestHealResponseFramesV1ToV4AndMultiSeg` | 针对 V01(`156B`)、V02(`164B`)、V03(`188B`)、V04(`200B`) 录制应答及 3 段连续 `RSP_SEG + RSP` 分片应答，输入携带全新 `ullSNO = 0x000000010009A2B3`、新 `aucTraceBuff[16]` 与新 `ullSendTimeStamp` (V05 请求) 的实时 `liveReq` 执行 `HealResponseFrames` | 所有单帧及 3 段 `RSP_SEG` 分片的公共头 `[8..15]` `ullSNO`、会话头 `aucTraceBuff`、V03/V04 `ullClientSendTime` 及 `ascServiceName`/`ascOperation` 均按 `LookupRspLayout` 精确覆写，Payload 字节分毫未改 (`F-13.1`, `F-13.4`, `SC-07`) |

---

## 3. 验收门禁命令 (Acceptance Criteria)

```bash
# AC-M3-01: 编译期强校验 Agent 数据面零 gorm、零 dynamicpb/protoregistry 依赖
! go list -deps ./cmd/agent ./pkg/ebpf ./pkg/replay | grep -E "gorm|dynamicpb|protoregistry"

# AC-M3-02: 运行非特权环境全量 Agent 信道、/proc 扫描与二进制自愈重放竞态单测
go test -v -race ./pkg/ebpf/... ./pkg/replay/... ./internal/agent/... ./cmd/agent/...

# AC-M3-03: 在具备 CAP_BPF + CAP_NET_ADMIN 的 Linux 特权 Runner 上执行内核 eBPF 双进程互调集成测试
go test -v -race -tags=ebpf ./pkg/ebpf/...

# AC-M3-04: 独立编译验证 code-traefik-agent 单二进制并清理临时产物
go build -o code-traefik-agent ./cmd/agent && rm -f code-traefik-agent
```
