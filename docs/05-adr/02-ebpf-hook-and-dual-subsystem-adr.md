# ADR-02: eBPF 入站旁路复制钩子选型、Cookie 原始目的地址还原与无 Schema 数据面解耦

- **文档编号**：`02-ebpf-hook-and-dual-subsystem-adr.md`
- **日期**：2026-10-29
- **状态**：已采纳 (Accepted)
- **关联设计**：[`01-system-architecture.md`](../02-design/01-system-architecture.md)、[`02-data-plane-and-cros-rpc-engine.md`](../02-design/02-data-plane-and-cros-rpc-engine.md)、[`04-control-plane-and-web-console.md`](../02-design/04-control-plane-and-web-console.md)、[`05-database-schema.md`](../02-design/05-database-schema.md)

---

## 1. 背景与决策问题 (Context & Problem Statement)

阶段一特性矩阵（[`01-feature-matrix.md`](../01-features/01-feature-matrix.md) v2.6.0 §2.2）明确留待阶段二定稿以下四个底层架构选型：
1. **入站被动旁路复制钩子选型**：如何在不插入用户态 TCP 代理、不污染非目标进程的前提下，按「目标 PID + 监听端口」零延迟复制入站请求与应答字节？
2. **出站 `cgroup/connect4` 原始目的地址传递**：`cgroup/connect4` 在 TCP 连接建立前直接修改 `user_ip4/user_port`，不经过 Netfilter `conntrack`，无法通过传统 `SO_ORIGINAL_DST` 获取原始下游 `IP:Port`，用户态透明代理如何精确反查？
3. **Agent 与 `.pb` 描述集解耦**：如何在每台业务机器不分发 `.pb` 文件的条件下，实现含动态序列号与时间戳的 CROS RPC 二进制 Mock 匹配与自愈？
4. **多人并发租约粒度与流量持久化物理格式**：多人并发使用公共/个人 Agent 时如何消除内核规则冲突与串包？`traefik_traffic_records` 如何在「逐帧一行」、「冷热双表」与「单次往返一行双模存储」之间权衡选型？

---

## 2. 方案权衡与最终决策 (Options & Decisions)

### 决策 1：入站采用「`inet_csk_accept` + `/proc` 预热维护 `(pid, fd)` 表 + 系统调用 `read/write` 族 Tracepoints + `BPF_MAP_TYPE_RINGBUF`」

| 候选方案 | 优点 | 淘汰/采纳原因 |
| :--- | :--- | :--- |
| 方案 A：`XDP` / `tc ingress/egress` | 网络层最早介入 | **淘汰**：`XDP`/`tc` 接收路径运行在软中断（softirq）上下文，`bpf_get_current_pid_tgid()` 无法获取目标业务进程 PID，违背按目标 PID `O(1)` 隔离约束（Non-Goal 6）。 |
| 方案 B：`kprobe/tcp_sendmsg` + `kretprobe/tcp_recvmsg` | 直接拿到 `struct sock *` | **淘汰**：Linux 5.x 至 6.x 内核中 `struct msghdr -> iov_iter` 内存布局发生重大变更（如 Linux 6.0 引入 `ITER_UBUF`），跨内核版本提取用户态缓冲容易兼容性断裂。 |
| **方案 C（采纳）：Socket Accept 跟踪 + Syscall I/O Tracepoints + RingBuf** | 进程上下文精确 `PID` 过滤、Tracepoint ABI 跨内核稳定、零链路代理 | **采纳**：通过 `kretprobe/inet_csk_accept` 与启动时 `/proc/<pid>/fd` 扫描锁定 `local_port == listen_port` 的入站 FD，在 `sys_enter/exit_read|recvfrom|recvmsg|readv` 与 `write|sendto|sendmsg|writev` 中以 `< 20ns` 开销过滤非目标进程，异步拷入 `BPF_MAP_TYPE_RINGBUF`。 |

### 决策 2：出站采用「`cgroup/connect4` + `bpf_get_socket_cookie` + `sock_ops`」传递原始目的地址

- 在 `cgroup/connect4` 重写目的地址为 `127.0.0.1:<ProxyPort>` 前，提取内核全局唯一 `bpf_get_socket_cookie(ctx)` 存入 `cookie_orig_dst_map`；
- 在三次握手完成的 `sock_ops (BPF_SOCK_OPS_ACTIVE_ESTABLISHED_CB)` 阶段，该客户端 Socket 已分配确定的本地临时端口 `local_port`，凭相同 `socket_cookie` 查出原始下游信息并写入 `nat_tuple_map[{local_ip, local_port}]`；
- Agent 用户态 `Accept()` 后凭 `RemoteAddr` `O(1)` 查 `nat_tuple_map`，零竞态还原 `{process_id, pid, orig_ip, orig_port}`；同时在 `connect4` 首行豁免 `agent_pid` 杜绝同机互调死锁。

### 决策 3：利用 CROS RPC `24 + usHeadLen` 边界实现零 `.pb` 二进制 Mock 与偏移覆写

- CROS RPC 协议中所有随运行变化的头字段（`ullSNO`、`aucTraceBuff`、`ullSendTimeStamp`、DOTR `TraceID`/`SpanID`）均严格位于偏移 `24 + usHeadLen` 之前；
- Agent 仅依赖 `pkg/crosrpc` 提取 `(ascServiceName, ascOperation)` 与 `frame[24+usHeadLen:]` 的 `SHA-256` 即可完成精确匹配（辅以同接口录制次序兜底），命中后对克隆应答切片的固定偏移（公共头 `8..15`、应答头 `8..23`、`32..39`、`56..183`）做原地字节覆写，彻底免除跨主机同步 `.pb` 的运维负担。

### 决策 4：采用「Agent 级动态排他租约」与「单次往返一行 (`Exchange-per-Row`) 无损+结构化双模持久化」

1. **Agent 级动态排他租约（而非进程级共享并发）**：
   - 录制与重放均需动态改写宿主机 eBPF `downstream_rules_map`（分别指向透明代理端口或隔离 Mock 端口）并配合连接重连（A-01）；若允许两名用户在同一台 Agent 上并发执行录制与重放，不仅会产生 BPF Map 规则竞争，还会导致多份 `64MB` 会话缓冲叠加膨胀内存。
   - 因此确立 **同一台 Agent 同一时刻仅允许 1 名用户持有 1 项活跃操作租约**（多机协同录制通过 `ORDER BY id ASC FOR UPDATE` 原子加锁防死锁，辅以普通用户可自建 `PRIVATE` Agent 在个人机器上独立重放公共素材），兼顾物理强隔离与高研发并行度。
2. **单次往返一行双模归档（`raw_req_packet` + `raw_rsp_packet` + `payload_json`）**：
   - **淘汰「逐帧一行 (`Frame-per-Row`)」**：一次标准 RPC 产生 2 行（分片流产生 $K+1$ 行），导致表行数膨胀 2 倍、列表展示与重放提取均需昂贵的跨行自连接 `JOIN`，且需要回表 `UPDATE paired_req_id`。
   - **淘汰「冷热双表拆分」**：引入第 12 张物理表，违背 11 张核心表约束且增加事务与级联清理复杂度；PostgreSQL 原生 **TOAST 机制**已自动将大于 `2KB` 的 `bytea` 大字段压缩外置存储，常规分页列表不 `SELECT raw_*_packet` 时完全不会触发大块二进制 I/O。
   - **采纳「单次往返一行 (`Exchange-per-Row`) 双模存储」**：在 Server 连接分片 Worker 内存滑动窗口内按 `ullSNO` 完成配对，单行同时写入无损 `raw_req_packet` / `raw_rsp_packet`（多段 `RSP_SEG + RSP` 连续拼接，由 `"CSE.TCP"` + `24 + uiLength` 零损耗切分）与结构化 `payload_json: {"req": ..., "rsp": ...}`（配单个 `GIN` 索引），使列表查询、解剖抽屉、明文检索、重放打包与双层回归 Diff 全部收敛为 `O(1)` 单行顺序扫描。

---

## 3. 决策后果与约束 (Consequences)

- **正面收益**：入站业务零代理、零时延损耗；同机多进程互调完全无死锁；Agent 单二进制无状态交付且内存稳定受控（单机恒定至多 1 个 64MB 缓冲）；公私域 Agent 与录制素材灵活跨环境同构重放；数据库单行完备归档且查询零自连接开销。
- **运行约束**：宿主机内核需支持 `BPF_MAP_TYPE_RINGBUF` 与 `cgroup v2`（Linux Kernel `>= 5.8`），Agent 进程需授予 `CAP_BPF` 与 `CAP_NET_ADMIN`；出站重定向仅作用于监控下发后新建的 TCP 连接（已通过存量长连接诊断与重连引导闭环）。

