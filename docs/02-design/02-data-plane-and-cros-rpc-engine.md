# 架构与活体契约治理 (`code-traefik`) 数据面引擎与 CROS RPC 协议栈设计

- **文档编号**：`02-data-plane-and-cros-rpc-engine.md`
- **文档版本**：v1.5.0
- **更新日期**：2026-11-04
- **归属阶段**：阶段二：系统架构与数据模型设计 (`02-design`)
- **状态**：已批准待实施 (Approved for M13)
- **上位基线**：[`01-feature-matrix.md`](../01-features/01-feature-matrix.md) (v2.10.0)、[`01-cros-rpc-protocol-spec.md`](../07-references/01-cros-rpc-protocol-spec.md) (v1.0.0)、[`01-system-architecture.md`](./01-system-architecture.md)、[`09-arch-process-ssot-and-ebpf-premount-standby-adr.md`](../05-adr/09-arch-process-ssot-and-ebpf-premount-standby-adr.md) (ADR-09 v1.1.0)、[`11-single-process-bidirectional-capture-and-recording-ux-adr.md`](../05-adr/11-single-process-bidirectional-capture-and-recording-ux-adr.md) (ADR-11 v1.0.0)、[`12-packet-causality-call-tree-and-sequence-ux-adr.md`](../05-adr/12-packet-causality-call-tree-and-sequence-ux-adr.md) (ADR-12 v1.0.0)

---

## 1. 共享协议栈 `pkg/crosrpc` 设计（零 `.pb` 依赖）

`pkg/crosrpc` 为控制面（`cmd/server`）与数据面（`cmd/agent`）共用的底层纯 Go 协议解析库，严格遵循 DOPHI `cros_rpccore.ph` 网络大端序与 `#pragma pack(4)` 内存布局规范，**不引入任何 Protobuf 描述集或反射库**。

### 1.1 常量契约与 24B 公共头 (`RPC_COMM_HEAD_S`) 校验规则

```go
package crosrpc

const (
	MagicPrefix          = "CSE.TCP"               // 7B 固定魔术字 (0x43 0x53 0x45 0x2E 0x54 0x43 0x50)
	CommHeadLen          = 24                      // sizeof(RPC_COMM_HEAD_S)
	ExtendCommHeadLen    = 4                       // sizeof(RPC_EXTEND_COMM_HEAD_S)
	ServiceNameMaxLen    = 64                      // RPC_SERVICE_NAME_LEN
	OperationMaxLen      = 64                      // RPC_OPERATION_LEN
	ProcNameMaxLen       = 64                      // RPC_PROC_NAME_LEN
	TraceBuffLen         = 16                      // RPC_MSG_TRACE_BYTE_NUM
	MaxPayloadLen        = 32*1024*1024 - 4096     // RPC_PAYLOAD_MAX_LEN = 33,550,336 Bytes
	MaxExtendHeadType    = 63                      // RPC_EXTEND_HEAD_TYPE_BUTT

	TransportDefault     = uint8(0)                // RPC_TRANSPORT_DEFAULT
	TransportTCP         = uint8(1)                // RPC_TRANSPORT_TCP (本系统主要覆盖底座)

	MsgTypeReq           = uint8(2)                // RPC_MSG_TYPE_REQ
	MsgTypeRsp           = uint8(3)                // RPC_MSG_TYPE_RSP
	MsgTypeRspSeg        = uint8(4)                // RPC_MSG_TYPE_RSP_SEG
	MsgTypeMng           = uint8(5)                // RPC_MSG_TYPE_MNG
	MsgTypeMngReq        = uint8(6)                // RPC_MSG_TYPE_MNG_REQ
	MsgTypeUUIDReq       = uint8(7)                // RPC_MSG_TYPE_UUID_REQ
	MsgTypeButt          = uint8(15)               // RPC_MSG_TYPE_MNG_BUTT 合法上界(不含)

	ReqTypeSync          = uint8(1)                // 同步请求
	ReqTypeAsync         = uint8(2)                // 异步请求
	ReqTypeOneway        = uint8(3)                // 单向通知 (不期待应答)

	ExtTypeTraceKey      = uint8(0)                // RPC_EXTEND_HEAD_TYPE_TRACE_KEY
	ExtTypeTimeStamp     = uint8(1)                // RPC_EXTEND_HEAD_TYPE_TIME_STAMP
)
```

公共消息头线性偏移（Wire Offset `0..23`）合法性判定谓词 `ValidateCommHead(h CommHead)` 必须同时满足：
1. **魔术字与传输协议匹配**：`bytes.Equal(raw[0:7], []byte("CSE.TCP"))` 且 `h.TransportProto <= 1`（严格限定 TCP 明文底座 `0x01` 或缺省 `0x00`，杜绝未纳管非 TCP 杂散报文伪同步）；
2. **消息类型合法**：`2 <= h.MsgType && h.MsgType < 15`；
3. **最小会话头下界**：
   - `MsgType == 2 (REQ)`：`h.HeadLen >= 160`（`RPC_REQ_HEAD_V1_S` 最小长度）；
   - `MsgType == 3, 4 (RSP/RSP_SEG)`：`h.HeadLen >= 156`（`RPC_RSP_HEAD_V1_S` 最小长度）；
   - `MsgType == 5, 6, 7 (MNG)`：`h.HeadLen >= 88`（`RPC_MNG_HEAD_S` 定长 88B）；
4. **总长度与防溢出上界**：`uint32(h.HeadLen) <= h.Length` 且 `h.Length <= MaxPayloadLen + uint32(h.HeadLen)`。

### 1.2 流式分帧与滑动重同步状态机 (`StreamFramer`)

TCP 流式采集（尤其是连接中途介入或异常断包）会面临**前序脏字节、半包（Fragmentation）、粘包（Coalescing）与超采集阈值大包**四类场景。`StreamFramer` 为每条单向字节流（`(ConnKey, Direction)`）独立维护如下状态机：

```mermaid
stateDiagram-v2
    [*] --> SyncMagic
    SyncMagic --> SyncMagic: 首字节起不足 8B 或未匹配 "CSE.TCP"，滑窗逐字节扫描 (DirtyBytes++)
    SyncMagic --> ReadCommHead: 定位到 "CSE.TCP" + ucProtocol
    ReadCommHead --> ReadCommHead: 缓冲可读字节 < 24B (等待后续 TCP 分片)
    ReadCommHead --> ValidateHead: 凑齐 24B 公共头
    ValidateHead --> SyncMagic: ucMsgType / usHeadLen / uiLength 违背协议契约 (丢弃首字节并重置同步)
    ValidateHead --> ReadFullBody: 24 + uiLength <= MaxCaptureBytes (默认 4MB)
    ValidateHead --> DrainOversize: 24 + uiLength > MaxCaptureBytes (合法超大包截断模式)
    ReadFullBody --> ReadFullBody: 缓冲可读字节 < 24 + uiLength (半包等待)
    ReadFullBody --> EmitFrame: 切出完整帧 [0 : 24+uiLength]
    DrainOversize --> DrainOversize: 仅保留前 MaxCaptureBytes 字节，流式排空剩余 (24+uiLength-MaxCaptureBytes) 字节
    DrainOversize --> EmitFrame: 剩余字节排空完毕 (标记 IsTruncated=true, IsReplayable=false)
    EmitFrame --> SyncMagic: 若环形缓冲仍有剩余字节(粘包)，立即进入下一轮循环
```

* **帧首字节到达定戳契约 (`FirstByteCapturedAtNs`, [`ADR-12`](../05-adr/12-packet-causality-call-tree-and-sequence-ux-adr.md))**：
  当大报文跨越多个 TCP 分片（`ReadFullBody` 多次半包等待）时，若以末字节凑齐时刻作为采集时间戳，可能导致父 `INGRESS REQ` 因分片较长而在纳秒时间戳上晚于子 `EGRESS REQ`。因此 `StreamFramer` 提供带时间戳输入入口 `WriteAt(chunk []byte, arrivalNs int64) []StampedFrame`（`Write` 保留兼容测试入口）：在 `SyncMagic -> ReadCommHead` 定位到帧首字节瞬间锁存 `frameStartNs = arrivalNs`（粘包场景下后续紧邻帧继承当前分片 `arrivalNs`），`EmitFrame` 输出 `StampedFrame{Raw, CapturedAtNs: frameStartNs, IsTruncated}`，严格捍卫单机单调时钟下的因果不等式 `Parent.ReqNs <= Child.ReqNs <= Child.RspNs <= Parent.RspNs`。

> [!TIP]
> **合法超大包流式排空 (`DrainOversize`) 与中途挂载双重重同步防伪造机制**：
> 1. 当 `uiLength` 符合 CROS RPC 规范（`<= 32MB`）但超过单帧落盘阈值（默认 `4MB`）时，绝不能直接清空缓冲重扫魔术字。`DrainOversize` 精确按 `remainingToDiscard` 计数丢弃尾部字节，保证内存峰值不超过 `4MB` 的同时，使下一条报文完美对齐偏移 `0`；
> 2. 当在既有活跃长连接中途开启录制闸门（`PASSTHROUGH -> CAPTURING`）时，首片到达字节可能位于上一帧的半包尾部。`StreamFramer` 在扫描到 `"CSE.TCP"` 后**必须继续通过 `ValidateCommHead` 的全套协议头不变量核验**（`TransportProto <= 1`、`MsgType ∈ [2, 14]`、各消息类型的最小 `HeadLen` 下界以及 `HeadLen <= Length <= MaxPayloadLen + HeadLen`），若任一谓词不满足则仅滑窗前进 1 字节继续重同步，彻底杜绝业务二进制载荷偶现 `"CSE.TCP"` 字面量导致的伪分帧。

### 1.3 多版本会话头（`#pragma pack(4)`）与扩展头长度驱动解析器

自 CROS RPC 2023.2 规范起，接收端采用**头长度驱动解析**代替硬编码版本号分支。解析器以 `frame[24 : 24+commHead.HeadLen]` 作为头部总切片：

#### 1.3.1 请求会话头 `RPC_REQ_HEAD_S` 精确字节布局（相对于偏移 `24`）

| 会话头内偏移 | 线上绝对偏移 | 长度 | 字段名 | C 类型 / 字节序 | 引入版本与累计结构体长度 |
| :---: | :---: | :---: | :--- | :--- | :--- |
| `0..1` | `24..25` | 2B | `usMsgStructVer` | `UINT16` (BE) | V00/V01 |
| `2..3` | `26..27` | 2B | `usReqHeadLen` | `UINT16` (BE) | V00/V01（指示本 Req 会话头实际字节数） |
| `4..7` | `28..31` | 4B | `uiDst2ndAddrId` | `UINT32` (BE) | V00/V01 |
| `8..71` | `32..95` | 64B | `ascServiceName` | `CHAR[64]` (`\0` 结尾) | V00/V01（格式 `{package}.{serviceName}`） |
| `72..135` | `96..159` | 64B | `ascOperation` | `CHAR[64]` (`\0` 结尾) | V00/V01（与 Protobuf Method 名相等） |
| `136` | `160` | 1B | `ucReqType` | `UINT8` | V00/V01（`1=Sync, 2=Async, 3=Oneway`） |
| `137` | `161` | 1B | `ucSubjectType` | `UINT8` | V00/V01 |
| `138..139` | `162..163` | 2B | `usRpcVer` | `UINT16` (BE) | V00/V01 |
| `140..143` | `164..167` | 4B | `uiSessionId` | `UINT32` (BE) | V00/V01 |
| **`144..159`** | **`168..183`** | **16B** | **`aucTraceBuff`** | `UCHAR[16]` | **V00/V01 终点 (`sizeof = 160B`)** |
| `160..167` | `184..191` | 8B | `ullIdempotentSeq` | `UINT64` (BE) | **V02 终点 (`sizeof = 168B`)** |
| `168..231` | `192..255` | 64B | `unSubjectName` | `CHAR[64]` / `UINT64` | **V03 终点 (`sizeof = 232B`)** |
| `232..239` | `256..263` | 8B | `ullClientUuid` | `UINT64` (BE) | V04 |
| `240..241` | `264..265` | 2B | `usUuidTimeoutS` | `UINT16` (BE) | V04 |
| `242..243` | `266..267` | 2B | `ucReserved[2]` | `UINT8[2]` | **V04 终点 (`sizeof = 244B`，4B 对齐无填充)** |
| `244..247` | `268..271` | 4B | `uiTimeOutSecondLimit` | `UINT32` (BE) | V05 |
| **`248..255`** | **`272..279`** | **8B** | **`ullSendTimeStamp`** | `UINT64` (BE) | **V05 终点 (`sizeof = 256B`)** |

#### 1.3.2 应答会话头 `RPC_RSP_HEAD_S` 表驱动布局规范（相对于会话头起始偏移）

为彻底消除解析端与重放自愈端因硬编码偏移导致的逻辑分歧，`pkg/crosrpc` 提供**权威表驱动布局结构体 (`RspLayout`)**：

| 会话头长度 (`usRspHeadLen`) | 结构体版本 | `aucTraceBuff` 范围 | `ullClientSendTime` 范围 | `ullServerRecvTime` / `ullServerSendTime` 范围 | `ascServiceName` 范围 | `ascOperation` 范围 | `ullStatusCode` / `usRspMsgNo` 范围 |
| :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: |
| **`156 B`** | **V01 (兼容降级包)** | `[8..24]` | *无* | *无* | `[24..88]` (64B) | `[88..152]` (64B) | `usRspMsgNo`: `[152..154]` |
| **`164 B`** | **V02 (服务实例过渡包)** | `[8..24]` | *无* | *无* | `[32..96]` (64B) | `[96..160]` (64B) | `usRspMsgNo`: `[160..162]` |
| **`188 B`** | **V03 (定位性增强包)** | `[8..24]` | `[32..40]` (8B BE) | `Recv: [40..48]`, `Send: [48..56]` (各 8B BE) | `[56..120]` (64B) | `[120..184]` (64B) | `usRspMsgNo`: `[184..186]` |
| **`200 B`** | **V04 (现代标准结构)** | `[8..24]` | `[32..40]` (8B BE) | `Recv: [40..48]`, `Send: [48..56]` (各 8B BE) | `[56..120]` (64B) | `[120..184]` (64B) | `ullStatusCode`: `[184..192]`, `usRspMsgNo`: `[192..194]` |

`pkg/crosrpc` 统一导出 `LookupRspLayout(headLen int) (*RspLayout, error)`。若遇到未注册的非标长度，显式报错 `ErrUnknownRspLayout`，严禁使用盲目模糊推断。

#### 1.3.3 管理会话头 `RPC_MNG_HEAD_S`（`ucMsgType = 5/6/7`，定长 `88B`）

相对于偏移 `24`：`usRole` (`0..1`)、`usType` (`2..3`)、`bRspResult` (`4..7`, 4B `BOOL`)、`acProcName[64]` (`8..71`)、`uiProcRet` (`72..75`)、`bSvrAuthorize` (`76..79`, 4B `BOOL`)、`ucEnCryption` (`80`)、`ucEnIntegrity` (`81`)、`ucCipherSuite` (`82`)、`aucReserved[5]` (`83..87`)。

#### 1.3.4 扩展头链式解析器 (`24 + sessionHeadLen .. 24 + commHead.HeadLen`)

当 `commHead.HeadLen > sessionHeadLen` 时，对剩余头部切片循环读取：
1. 读取 4B `RPC_EXTEND_COMM_HEAD_S`：`extLen = BigEndian.Uint16(b[0:2])`，`extType = b[2]`；
2. 边界校验：`4 + int(extLen) <= len(remaining)` 且 `extType < 63` 且 `extLen % 8 == 0`（8 字节对齐约束）；若越界则终止扩展头解析并标记扩展头告警，不影响 Payload 起始偏移 `24 + commHead.HeadLen`；
3. 按 `extType` 提取已知扩展头（未知 `extType` 保留原始 Hex 前向兼容跳过）：
   - `extType == 0 (TRACE_KEY, extLen >= 32)`：大端读取 `TraceID = b[4:12]`、`SpanID = b[12:20]`、`FatherSpanID = b[20:28]`、`RecordSwitch = b[28]`；
   - `extType == 1 (TIME_STAMP, extLen >= 8)`：大端读取 `ServerEnqueueTime = b[4:12]`（回归 Diff 固定豁免）。

### 1.4 `pkg/crosrpc` 核心导出数据结构

```go
package crosrpc

// RspLayout 表驱动应答会话头字段区间
type RspLayout struct {
	Version              string
	HeadLen              int
	TraceBuffOffset      int
	TraceBuffLen         int
	ClientSendTimeOffset int // 0 表示当前版本无此字段 (V03/V04 为 32)
	ServerRecvTimeOffset int // 0 表示当前版本无此字段 (V03/V04 为 40, 8B UINT64 BE)
	ServerSendTimeOffset int // 0 表示当前版本无此字段 (V03/V04 为 48, 8B UINT64 BE)
	ServiceNameOffset    int
	OperationOffset      int
	StatusCode64Offset   int // 0 表示当前版本仅有 32 位状态码
	RspMsgNoOffset       int
}

// LookupRspLayout 依据会话头实际字节数检索权威字段偏移映射
func LookupRspLayout(headLen int) (*RspLayout, error)

// ParsedPacket 表示单帧 CROS RPC 协议头完整解析结果（零 Payload 反序列化开销）
type ParsedPacket struct {
	Comm          CommHead       `json:"comm"`
	Req           *ReqHead       `json:"req,omitempty"`
	Rsp           *RspHead       `json:"rsp,omitempty"`
	Mng           *MngHead       `json:"mng,omitempty"`
	ExtendHeads   []ExtendHead   `json:"extend_heads,omitempty"`
	Trace         *TraceKeyExt   `json:"trace,omitempty"`
	ServerEnqueue *uint64        `json:"server_enqueue_time,omitempty"`
	PayloadOffset int            `json:"payload_offset"` // 恒等于 24 + int(Comm.HeadLen)
	PayloadLen    int            `json:"payload_len"`    // 实际保留帧中的 Payload 字节长度
	IsTruncated   bool           `json:"is_truncated"`
}

// SplitFrames 按 CROS RPC 24B 公共头 ("CSE.TCP" + 24 + uiLength) 无损切分连续拼接的多段帧切片
func SplitFrames(concatenated []byte) ([][]byte, error)
```

* **`StreamFramer` SIMD 滑窗扫描与原地缓冲紧缩（[`ADR-11`](../05-adr/11-single-process-bidirectional-capture-and-recording-ux-adr.md)）**：
  在全出站透明代理模式下，`StreamFramer` 可能旁路扫描完全不含 CROS RPC 报文的非目标 TCP 流（如 PostgreSQL/MySQL/Redis/HTTP）。`Write` 状态机采用 `bytes.Index(f.buf, magicPrefixBytes)` SIMD 加速定位 `"CSE.TCP"`：若未命中，仅保留末尾最多 `6` 字节潜在前缀片段并调用 `copy(f.buf, tail); f.buf = f.buf[:len(tail)]` 原地紧缩（当底层 `cap(f.buf) > 8192` 且 `len(f.buf) <= 6` 时重新收敛回 `8KB` 切片，彻底杜绝 `f.buf = f.buf[1:]` 逐字节递进导致的底层数组钉死与 CPU 空转）。

---

## 2. Agent 嵌入式 `cilium/ebpf` 采集引擎 (`pkg/ebpf`)

Agent 在进程内通过 `github.com/cilium/ebpf` 加载预编译的 CO-RE BPF 字节码（`bpf2go` 生成），单套探针同时服务宿主机上全部被纳管的目标业务进程（`1 Agent : N Processes`）。目标环境为现代 Linux 内核（Ubuntu/SUSE 等主流发行版，Linux Kernel `>= 5.8`，支持 `BPF_MAP_TYPE_RINGBUF`、`cgroup v2` 与 `bpf_link`）。

### 2.1 内核钩子分级按需挂载矩阵与 BPF Maps 设计

（钩子选型权衡详见 [`docs/05-adr/02-ebpf-hook-and-dual-subsystem-adr.md`](../05-adr/02-ebpf-hook-and-dual-subsystem-adr.md) 与 [`ADR-09`](../05-adr/09-arch-process-ssot-and-ebpf-premount-standby-adr.md)）

> [!IMPORTANT]
> **待命态与采集态内核钩子物理分离（全机待命零 `read/write` 系统调用损耗）**：
> - **`ARMED` 预挂待命期（常驻低频建连钩子）**：仅挂载 `cgroup/connect4`、`cgroup/sock_ops` 与 `tracepoint/sched/sched_process_{exec,fork,exit}`，**完全不挂载全局 `sys_enter/exit_read/write/recv*/send*` 系统调用跟踪点**，确保宿主机上所有进程（含待命目标进程与同机其他高吞吐服务）在日常待命期享受 **100% 零读写 syscall 追踪损耗**；
> - **`CAPTURING` 活跃录制期（按需动态挂载入站钩子）**：因为入站旁路复制无需在建连前拦截握手，Agent 仅在开启录制（`StartCapture`）时才动态 `Attach` 入站 `sys_exit_accept4` / `sys_exit_accept`、`sys_enter_close` 与 `sys_enter/exit_{read,write,recvfrom,sendto}`，并在停止录制（`StopCapture`）瞬间立即 `Detach`。

| BPF Map 名称 | Map 类型 | Key 结构 | Value 结构 | 容量上限 | 核心用途與字节序契约 (`ADR-11`) |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `agent_cfg_map` | `ARRAY` | `uint32 (0)` | `struct agent_cfg { uint32 agent_pid; __be16 proxy_port; __be16 mock_port; }` | `1` | 存储 Agent 自身 PID（自环豁免）与本地代理/Mock 监听端口（端口统一以 `htons(port)` 网络字节序 `__be16` 存储，可直接赋值给 `ctx->user_port`） |
| `target_pids_map` | `HASH` | `uint32 pid` | `struct target_proc { uint64 session_id; uint64 process_id; uint16 listen_port; uint8 mode; }` | `1024` | `O(1)` 判定目标进程与模式：`session_id == 0` 为 `ARMED` 待命，`session_id > 0` 为 `CAPTURING` 录制，`mode == MODE_REPLAY` 为离线重放；`listen_port` 为主机序 `uint16` |
| `active_rw_args_map` | `HASH` | `uint64 tgid_pid` | `struct rw_args { void *buf; int32 fd; uint8 is_read; }` | `16384` | 仅在 `CAPTURING` 期间使用：暂存目标进程入站 Socket `sys_enter_*` 缓冲区指针与 FD |
| `ing_conn_map` | `LRU_HASH` | `struct pid_fd { uint32 pid; int32 fd; }` | `struct conn_tuple { __be32 src_ip; __be32 dst_ip; uint16 src_port; uint16 dst_port; uint32 conn_seq; uint64 stream_off_rx; uint64 stream_off_tx; }` | `65536` | **严格白名单**：仅记录目标进程监听端口 `listen_port` 上的入站 TCP Socket FD 四元组与流内累计偏移（`SyncMonitors` 开录时通过 `/proc/<pid>/fd` 预热存量已建立入站连接，严禁非监听 Socket FD 混入） |
| `downstream_rules_map` | `HASH` | `struct ds_key { uint64 process_id; __be32 dst_ip; __be16 dst_port; uint16 pad; }` | `struct ds_target { __be16 redirect_port; uint8 is_replay; uint8 pad; }` | `4096` | 记录已声明下游参考集（`dst_ip` 为 `__be32` 内存四字节序 `ipv4ToBPFBe32`，`dst_port` 为 `htons(port)` 网络字节序 `__be16`），供录制期判定未声明连接观测事件 |
| `cookie_orig_dst_map` | `LRU_HASH` | `uint64 sock_cookie` | `struct orig_dst { uint64 process_id; uint32 pid; __be32 orig_ip; __be16 orig_port; uint16 pad; }` | `65536` | `connect4` 阶段暂存 Socket Cookie 对应的原始下游目的地址（`orig_ip`/`orig_port` 保持内核网络字节序 `__be32`/`__be16`） |
| `nat_tuple_map` | `LRU_HASH` | `struct client_addr { __be32 ip; uint16 port; uint16 pad; }` | `struct orig_dst` | `65536` | `sock_ops` 握手完成时按客户端地址（`ip` 为 `skops->local_ip4` `__be32`，`port` 为主机序 `uint16(skops->local_port)`）归档，供用户态 `EgressProxy` `Accept` 反查并通过 `bpfBe32ToIPv4` / `ntohs` 还原 |
| `ing_ringbuf` | `RINGBUF` | - | 变长 `struct ing_data_event`（含 `rw_dir: 1=RX_READ, 2=TX_WRITE` 与 `stream_off` 累计偏移，单分片最大 32KB） | `16 MB` | `CAPTURING` 期间内核向用户态零拷贝异步投递入站收发字节流 |
| `connect_observe_ringbuf` | `RINGBUF` | - | `struct conn_observe_event { uint64 process_id; uint32 pid; __be32 dst_ip; __be16 dst_port; }` | `1 MB` | `CAPTURING` 期间投递目标进程发起的未命中 `downstream_rules_map` 的出站连接观测事件（支撑 `VIOLATION` 检测） |
| `lifecycle_ringbuf` | `RINGBUF` | - | `struct proc_lifecycle_event { uint32 pid; uint32 ppid; uint8 kind; }` | `256 KB` | 投递 `sched_process_exec` / `exit` 生命周期事件，驱动用户态即时匹配命令行与清理死 PID |
| `ebpf_stats_map` | `PERCPU_ARRAY` | `uint32 stat_idx` | `uint64 counter` | `8` | 统计 `stat_bypassed_self`、`stat_ringbuf_drops`、`stat_redirected` |

### 2.2 入站被动旁路复制机制 (`INGRESS Passive Copy`) 与监听 Socket 严格白名单

入站采集**完全不经过用户态代理**，对业务 TCP 链路零修改、零附加网络跳数，并严格遵循「按需动态挂载 + 监听端口 Socket FD 白名单」两道防线：

1. **严禁任意 `fd > 2` 惰性污染，仅追踪 `listen_port` 入站 TCP 连接**：
   - **架构铁律**：BPF `handle_rw_enter` **严禁**对未在 `ing_conn_map` 中登记的任意 `fd > 2` 惰性创建默认连接条目（否则会把目标进程的本地磁盘日志文件、配置文件、管道、`eventfd` 以及出站客户端 Socket 全部误当作入站流量打入 `ing_ringbuf` 造成严重污染与丢包）；
   - **开录瞬间存量入站连接预热**：执行 `StartCapture`（`PUT /agent/v1/monitors/sync`）瞬间，Agent 在用户态扫描目标进程 `/proc/<pid>/fd` 与 `/proc/<pid>/net/tcp`，**仅将已处于 `TCP_ESTABLISHED` 且本地端口严格等于 `target_proc.listen_port`（`> 0`）的入站服务端 `(pid, fd) -> conn_tuple`** 预写入 `ing_conn_map`，确保既有上游长连接无需重连即可立即捕获；
   - **录制期增量入站连接与关闭清理**：在 `CAPTURING` 期间动态挂载 `tracepoint/syscalls/sys_exit_accept4`、`sys_exit_accept`（以及可选 `kretprobe/inet_csk_accept` 补齐四元组）与 `tracepoint/syscalls/sys_enter_close`；新 `accept` 返回的 `ret_fd >= 0` 写入 `ing_conn_map`，`close(fd)` 时精确从 `ing_conn_map` 移除 `(pid, fd)` 并上报 `EVENT_CONN_CLOSED`。
2. **按需动态挂载读写钩子与精确旁路复制**：
   - 仅当存在活跃录制会话（`session_id > 0`）时，Agent 才挂载 `sys_enter/exit_read`、`sys_enter/exit_recvfrom`（`INGRESS REQ`）与 `sys_enter/exit_write`、`sys_enter/exit_sendto`（`INGRESS RSP`）；停止录制（`StopCapture`）时立即 `Close()` 卸载上述读写 `bpf_link`，使待命期保持 **0 读写钩子挂载**；
   - **进入钩子 (`sys_enter_*`)**：校验 `tp = bpf_map_lookup_elem(&target_pids_map, &pid)` 非空且 `tp->session_id > 0`，并进一步查表确认 `(pid, fd)` **已存在于 `ing_conn_map`** 后，才将缓冲区指针写入 `active_rw_args_map[tgid_pid]`；
   - **退出钩子 (`sys_exit_*`)**：从 `active_rw_args_map` 取出暂存参数并清理条目；若返回值 `ret_bytes > 0`，提取流内累计偏移 `stream_off`（原子加 `ret_bytes`），按最大 `32KB` 块调用 `bpf_probe_read_user` 拷贝数据并写入 `ing_ringbuf`。
3. **丢包感知、读写半流隔离与恒定 `DirectionIngress` 信封封装 (`Gap-Aware StreamFramer`, [`ADR-11`](../05-adr/11-single-process-bidirectional-capture-and-recording-ux-adr.md))**：
   - `struct ing_data_event` 明确包含 `uint8 rw_dir`（`1 = RX_READ` 上游请求流、`2 = TX_WRITE` 本地应答流）、`uint64 stream_off` 与 `uint32 data_len`；
   - 用户态 `ebpf.IngressReader` 按 `(pid, conn_seq, rw_dir)` 严格隔离收发两个方向的 `StreamFramer` 与 `expected_off`，但在输出 `agentproto.EnvelopeHeader` 时**统一设置 `Direction = agentproto.DirectionIngress (1)`**（将四元组固定为 `Src = 远端客户端, Dst = 本机监听端口`），确保入站 `REQ` 与入站 `RSP / RSP_SEG` 在慢轨 `ExchangeAssembler` 中落入同一分片键并按 `ullSNO` 准确配对；
   - 中途开启 `CAPTURING` 的首片数据或底层 RingBuf 满导致断流时，分帧器自动依靠 `"CSE.TCP"` + `ValidateCommHead` 对齐下一完整帧边界；发生中途断流（`HAS_GAP`）的连接当前未完成往返记录落盘标记 `is_replayable = false`。

### 2.3 目标 PID 默认全出站透明重定向与 `EgressProxy` 三态热切换引擎 (`PASSTHROUGH | CAPTURING | REPLAY_MOCK`, [`ADR-11`](../05-adr/11-single-process-bidirectional-capture-and-recording-ux-adr.md))

```mermaid
flowchart LR
    subgraph SameHost["单进程 / 多进程默认双向采集与无死锁链路"]
        ProcA["目标进程 PID_A (如仅挂载 sut-order 1个进程)"]
        Hook["cgroup/connect4 钩子"]
        Proxy["Agent 本地出站代理 EgressProxy (agent_pid :proxy_port)<br/>[PASSTHROUGH 透传 | CAPTURING 双录 | REPLAY_MOCK 本地自愈]"]
        MockSrv["本地重放 MockServer (:mock_port)<br/>(MODE_REPLAY 新建连接物理零外拨)"]
        ProcB["任意下游服务 (同机 PID_B :9002 / 跨机 RPC / DB :5432)"]
        IngCopy["Ingress RingBuf 旁路复制 (仅 CAPTURING 挂载)"]
    end

    ProcA -- "1. 任意出站 connect(DstIP:DstPort)" --> Hook
    Hook -- "2a. MODE_RECORDING (含 ARMED): 默认重定向 127.0.0.1:proxy_port" --> Proxy
    Hook -- "2b. MODE_REPLAY: 统一重定向 127.0.0.1:mock_port (0外拨)" --> MockSrv
    Proxy -- "3a. PASSTHROUGH / CAPTURING: 查 nat_tuple_map 以 agent_pid connect(orig_dst)" --> Hook
    Proxy -. "3b. REPLAY_MOCK (既有长连接): 切断 targetConn 或写前 CSE.TCP 守卫拦截" .-> ProcA
    Hook -- "4. 识别 pid == agent_pid! stat_bypassed_self++ 原样放行" --> ProcB
    ProcB -. "5. 若下游亦为同机纳管进程 PID_B: CAPTURING 期旁路复制入站帧" .-> IngCopy
```

1. **内核态默认目标 PID 出站重定向与重放零外泄算法（BPF C 核心逻辑）**：
   ```c
   SEC("cgroup/connect4")
   int cros_connect4(struct bpf_sock_addr *ctx) {
       if (ctx->family != AF_INET || (ctx->protocol != IPPROTO_TCP && ctx->protocol != 0))
           return 1;
       __u32 pid = bpf_get_current_pid_tgid() >> 32;
       struct agent_cfg *cfg = bpf_map_lookup_elem(&agent_cfg_map, &zero);
       if (!cfg) return 1;

       // 核心红线 1：Agent 自身进程发起的一切出站连接直接放行，杜绝同机代理回源死锁
       if (pid == cfg->agent_pid) {
           incr_stat(STAT_BYPASSED_SELF);
           return 1;
       }
       struct target_proc *tp = bpf_map_lookup_elem(&target_pids_map, &pid);
       if (!tp) return 1; // 非纳管目标进程，100% 原样放行 (Non-Goal 6)

       // 核心红线 2：豁免指向本地代理/Mock 端口及进程自身监听端口的回环自连，防极端自环
       if (ctx->user_ip4 == bpf_htonl(0x7f000001)) {
           if (ctx->user_port == cfg->proxy_port || ctx->user_port == cfg->mock_port)
               return 1;
           if (tp->listen_port > 0 && ctx->user_port == bpf_htons(tp->listen_port))
               return 1;
       }

       // 活跃录制期：若未命中已声明 downstream_rules_map，异步投递未声明连接观测事件 (支撑 F-05 VIOLATION)
       if (tp->mode == MODE_RECORDING && tp->session_id != 0) {
           struct ds_key key = { .process_id = tp->process_id, .dst_ip = ctx->user_ip4, .dst_port = ctx->user_port };
           struct ds_target *dst = bpf_map_lookup_elem(&downstream_rules_map, &key);
           if (!dst) {
               key.dst_ip = bpf_htonl(0x7f000001);
               dst = bpf_map_lookup_elem(&downstream_rules_map, &key);
           }
           if (!dst) {
               struct conn_observe_event obs = {
                   .process_id = tp->process_id, .pid = pid,
                   .dst_ip = ctx->user_ip4, .dst_port = ctx->user_port,
               };
               bpf_ringbuf_output(&connect_observe_ringbuf, &obs, sizeof(obs), 0);
           }
       }

       __u64 cookie = bpf_get_socket_cookie(ctx);
       struct orig_dst od = {
           .process_id = tp->process_id, .pid = pid,
           .orig_ip = ctx->user_ip4, .orig_port = ctx->user_port,
       };
       bpf_map_update_elem(&cookie_orig_dst_map, &cookie, &od, BPF_ANY);

       // 重放模式导向物理零外拨的本地 MockServer；待命/录制模式默认全量导向透明 EgressProxy
       __be16 target_port = (tp->mode == MODE_REPLAY) ? cfg->mock_port : cfg->proxy_port;
       if (target_port == 0) return (tp->mode == MODE_REPLAY) ? 0 : 1;

       ctx->user_ip4 = bpf_htonl(0x7f000001);
       ctx->user_port = target_port;
       incr_stat(STAT_REDIRECTED);
       return 1;
   }
   ```
2. **`EgressProxy` 三态闸门热切换、会话 ID 注入与方向不变量（`PASSTHROUGH | CAPTURING | REPLAY_MOCK`，[`ADR-09`](../05-adr/09-arch-process-ssot-and-ebpf-premount-standby-adr.md) & [`ADR-11`](../05-adr/11-single-process-bidirectional-capture-and-recording-ux-adr.md)）**：
   - **状态一 `PASSTHROUGH`（`ARMED` 待命期 & 停录无损回退）**：
     - 目标进程新建的任意下游 TCP 连接接入 `EgressProxy` 后，代理通过 `nat_tuple_map` 反查 `orig_dst` 建立 `targetConn` 并登记于 `activeConns[connID]`；
     - 处于 `PASSTHROUGH` 时，双向转发协程直接执行高吞吐纯字节块搬运（零 `StreamFramer` 分配、零头部解析、零 WebSocket 信封构造，对 MySQL/PostgreSQL/Redis/HTTP 完全透明），**停止录制后不关闭 `clientConn` 与 `targetConn`，继续无缝转发业务数据**。
   - **状态二 `CAPTURING`（有序闸门 0ms 零重启开录、会话归属注入与 `DirectionEgress` 恒定不变量）**：
     - **有序开录序列 (`StartCapture(sessionID)`)**：① 先将 `EgressProxy.activeSessionID` 原子置为 `sessionID` 并将所选 `process_id` 切换为 `CAPTURING`（动态挂载 SIMD `StreamFramer`） ➔ ② 通过 `/proc/<pid>/fd` 预热存量入站 `TCP_ESTABLISHED` FD 至 `ing_conn_map`、动态 `Attach` 入站读写钩子并将 `target_pids_map[pid].session_id` 置为 `sessionID` ➔ ③ 扫描该进程自有 FD 中启动前残留的未代理直连出站 `IP:Port` 补报初始连接观测事件；
     - **出站信封不变量**：`Client -> Target`（出站 `REQ`）与 `Target -> Client`（出站 `RSP / RSP_SEG`）切出的全部 CROS RPC 帧，统一填充 **`SessionOrTaskID = activeSessionID`（非零）、`Direction = agentproto.DirectionEgress (2)`、`Src = clientIP:clientPort`、`Dst = origDstIP:origDstPort`**，保证出站往返在慢轨 `ExchangeAssembler` 中 100% 聚合为同一行 `EGRESS` 记录；非 CROS RPC 连接因不含 `"CSE.TCP"` 魔术字而仅原样透传、零伪帧上报；
     - **有序停录序列 (`StopCapture`)**：① 先将 `target_pids_map[pid].session_id` 拨回 `0` 并 `Detach` 入站读写钩子 ➔ ② 维持 `EgressProxy` `200ms` 极短在途应答排空窗（使停录前刚发出的在途 `EGRESS REQ` 能收齐对应 `RSP`） ➔ ③ 将 `EgressProxy` 原子拨回 `PASSTHROUGH`、清零 `activeSessionID` 并卸下 `StreamFramer` 归还预算池（**底层 TCP 长连接全程保持不断开**） ➔ ④ 刷空有界缓冲并发送 `EVENT_SESSION_DRAINED`。
   - **状态三 `REPLAY_MOCK`（既有代理长连接零重启接管 + 写前 `"CSE.TCP"` 守卫，真实下游 100% 零外泄）**：
     - **重放启动前置硬门禁**：重放前基于 `/proc/<pid>/fd` 自有 Socket Inode 实时核验目标进程：若进程未运行（`WAITING_PROC`）或存在指向重放 Mock 端点集（`CompileProcessDownstreams ∪ 历史 EGRESS DstAddr`）的未代理直连长连接（`NEED_RECONNECT`, `established_conns > 0`），**立即拒绝启动重放并返回 `409 Conflict`**；
     - **零外泄本地 Mock 接管与写前魔术字守卫**：当目标进程已处于 `🟢 READY` 时无需重启进程——① `EgressProxy` 中目的地址命中 Mock 端点集的既有长连接立即关闭/物理阻断 `targetConn` 切入 `REPLAY_MOCK`，直接由本地 `ScopedMockMatcher` + `HealResponseFrames` 应答；② 同进程其余未命中 Mock 端点集的既有代理连接（如测试环境常驻 DB 连接）启用**写前 `"CSE.TCP"` 魔术字守卫**：客户端上行字节块在写入 `targetConn` 前若检出 `"CSE.TCP"` 合法公共头，立即切断 `targetConn` 转入 `REPLAY_MOCK` 交由 `ScopedMockMatcher` 记录 `MOCK_MISS`（确保代码突变新增调用闲置 RPC 连接时同样 0 字节外泄，同时不中断测试库 SQL 访问）；③ `target_pids_map[pid].mode` 置为 `MODE_REPLAY`，新建连接统一由内核导向物理零外拨的本地 `MockServer`；
     - **重放终态干净恢复**：重放完成或取消时，主动关闭在重放期已脱离真实下游状态机的被接管连接，将规则恢复为 `ARMED` `PASSTHROUGH` 并释放本地租约。
3. **原始目的地址还原、进程自有 Socket Inode 诊断与同机 INGRESS 对端反查**：
   - 客户端建立连接时，在 `BPF_SOCK_OPS_ACTIVE_ESTABLISHED_CB` 取出 `orig_dst` 写入 `nat_tuple_map[{local_ip, local_port}]`；
   - **基于 `/proc/<pid>/fd` 自有 Inode 的存量连接精准诊断 (`ProcScanner.DiagnoseEstablishedConns`)**：由于 Linux `/proc/<pid>/net/tcp` 返回整个网络命名空间的全部 Socket，扫描器强制先读取 `/proc/<pid>/fd` 收集目标 `pid` 亲身持有的 `socket:[inode]` 集合，排除 `local_port == listen_port` 的入站连接以及 `rem_addr == 127.0.0.1:proxy_port` 的已代理连接后，精准统计绕过 `EgressProxy` 的直连出站 `TCP_ESTABLISHED` 连接（根治同机 `agent_pid` 回源连接造成的假阳性 `NEED_RECONNECT`）；
   - **同机跨进程入站反查机制**：当目标进程 `B` 收到入站连接时，其客户端 IP 为本机 IP（或 `127.0.0.1`）；Agent 用户态读取宿主机 `/proc/net/tcp`，凭对端客户端临时端口查询对应 Socket inode 反查所属进程 PID（若对端为本地 `EgressProxy` 回源端口，则从 `EgressProxy` 活跃连接映射表直接还原上游发起进程 `A.ID`），精确绑定 `SrcProcessID = A.ID`。

### 2.4 进程生命周期追踪、平滑排空 (`Graceful Drain`) 与内存预算

1. **进程启动/派生/退出闭环追踪 (`sched_process_fork / exec / exit`)**：
   - **`sched_process_fork`（内核态零竞态继承）**：若父进程 `parent_pid` 已存在于 `target_pids_map`，BPF 钩子在内核态同步将 `child_pid` 以相同 `target_proc` 配置写入 `target_pids_map`，确保多进程派生（fork worker）的首条连接 0 漏网；
   - **`sched_process_exec`（事件驱动即时匹配 + 诚实对账）**：新进程执行 `execve` 时通过 `lifecycle_ringbuf` 唤醒用户态高优先级协程，即时读取 `/proc/<pid>/cmdline` 匹配已预挂进程的 `(EffectiveListenPort, EffectiveCmdPattern)` 并写入 `target_pids_map`；2s 间隔的 `ProcWatcher` 持续执行全量 `/proc` 对账，若某进程在微秒级用户态匹配窗口内抢先建立了极少数直连连接，`ProcWatcher` 如实将其状态置为 `🟡 NEED_RECONNECT`（`established_conns > 0`），绝不产生虚假的 `🟢 READY` 误导；
   - **`sched_process_exit`（防 PID 回绕污染）**：目标进程退出时内核态立即从 `target_pids_map` 删除该 `pid` 并通知用户态刷新为 `🔵 WAITING_PROC`，彻底消除 Linux PID 回绕复用后误拦截无关系统进程的风险。
2. **完整特权集、手动暂停 (`DISARMED`) 与 5 分钟失联平滑排空 (`Graceful Drain`)**：
   - Agent 启动时校验 Effective Capability 集合必须同时包含：**`CAP_BPF`、`CAP_NET_ADMIN`、`CAP_PERFMON` 与 `CAP_SYS_PTRACE`**（用于只读解析宿主机目标进程 `/proc/<pid>/fd`）；
   - **`DISARMED` 与 5 分钟心跳失联的平滑排空（不闪断在途业务）**：
     - 当用户在空闲期将某进程或整机置为 `DISARMED`，或主控与 Agent 网络失联达到 **5 分钟宽限期**（`GracePeriodWatchdog`）时，Agent **立即从 BPF Map 移除规则 / 调用 `link.Close()` 卸载全部 `bpf_link` 并关闭 `EgressProxy` 监听端口 `listener.Close()`**，使此后一切新建 TCP 连接 100% 恢复内核原生直连；
     - 对于此时仍在 `EgressProxy` 上传输业务数据的存量连接，转入 **`Draining` 平滑排空态**：停止一切统计与采集，继续维持纯字节透传直至连接自然关闭或达到最大排空时限（`--drain-timeout=60s`），避免管控面网络抖动直接掐断测试环境正在运行的业务长连接；
   - **进程退出与紧急清场硬关闭**：当 Agent 收到操作系统 `SIGINT/SIGTERM` 退出信号或收到主控 `POST /agent/v1/purge` 强制清场指令时，立即硬关闭全部代理/Mock 连接并卸载全部 `bpf_link`；严禁将任何 BPF Map 或 Program pin 到 `bpffs`，确保即使 Agent 进程崩溃/OOM 内核亦自动回收全部探针 FD。
3. **Framer 全局内存预算池 (`GlobalFramerBudgetPool`)**：
   - Agent 单机所有活跃采集连接的 `StreamFramer` 共享一个最大为 `64MB` 的内存预算池（`PASSTHROUGH` 待命透传连接不占用此池）；
   - 当遇到上千条长连接且并发分帧时，若全局使用量超过上限，按照 LRU 策略淘汰长时间无数据传输连接的分帧缓存，绝不突破 Agent 物理内存红线。

---

## 3. 零 `.pb` 纯二进制 Mock 匹配与自愈引擎 (`pkg/replay`)

在离线重放模式下，下游真实 CROS RPC 服务已关闭或网络隔离（A-06）。Agent 在本地独立完成**入站请求注入**与**出站下游二进制 Mock 自愈应答**，全流程无需知道 Payload 内部任何业务字段含义，天然支持将公共（`SHARED`）或个人（`PRIVATE`）录制素材投递到任意承载相同公共架构模块的目标进程（含开发者本人 `PRIVATE` Agent 上的本地测试进程）执行重放。

### 3.1 重放执行拓扑、前置就绪门禁与请求注入器 (`ReplayExecutor`)

1. **前置就绪硬门禁、既有代理连接零外泄接管与 Fail-Closed 闭环**：
   - **直连零容忍门禁 (`409 Conflict`)**：重放启动前实时核验目标进程：若进程未启动（`WAITING_PROC`）或存在任何绕过 `EgressProxy` 的预挂前直连长连接（`NEED_RECONNECT`, `established_conns > 0`），立即拒绝启动并返回 `409 Conflict`，杜绝未受控直连导致重放请求打穿真实下游；
   - **既有 `EgressProxy` 连接原地切入 `REPLAY_MOCK` + 新建连接本地 Mock 闭环（[`ADR-11`](../05-adr/11-single-process-bidirectional-capture-and-recording-ux-adr.md)）**：目标进程处于 `🟢 READY` 时无需手动重启！主控取「目标进程自身推导/登记的下游端点集合」与「源录制批次观测到的全部 `EGRESS` 原始 `dst_addr`（`IP:Port`）集合」的**并集**下发 Agent；Agent 一方面将 `EgressProxy` 中命中该并集的既有长连接原子切入 `REPLAY_MOCK`（立即关闭/物理阻断 `targetConn`，0 字节发往真实下游，直接由本地 `ScopedMockMatcher` 应答），并对同进程其余既有代理连接启用写前 `"CSE.TCP"` 魔术字守卫（检出 CROS RPC 帧即切断 `targetConn` 转入 `ScopedMockMatcher`），另一方面将 `target_pids_map[pid].mode` 置为 `MODE_REPLAY`，使目标进程一切新建出站连接统一由内核 `connect4` 重定向至物理零外拨的本地 `MockServer`（既保证 100% 零外泄，又能在 `SC-08` 多调未录制下游时精确提取服务与方法名记录 `MOCK_MISS`）；
   - **Mock 资产合格性门禁**：主控下发前严格剔除 `is_truncated == true`（超限截断帧）与 `exchange_type == 'REQ_ONLY'`（录制期挂起未等到应答帧）的记录，确保下发的下游 Mock 均为协议合规且分片完整的二进制数据。
2. **连接注入、按 `RecordID` 切换局部 Mock 池与多连接会话还原 (`ADR-12`)**：
   - Agent 注入器通过 `/proc/net/tcp` 探知目标进程绑定的实际监听地址（如特定网卡 IP 或 `127.0.0.1`）及 `ListenPort` 建立 TCP 注入连接（注入协程 `pid == agent_pid`，不受重定向影响）；
   - 若录制会话中存在前置 `MNG` 管理消息帧（A-08），注入器按原连接时序先发送入站 MNG 帧；
   - 按 `ExecuteReplayReq.IngressItems` 顺序逐条执行注入（若旧调用方仅传入平铺 `IngressRequests` 则自动包装为虚拟递增 `RecordID` 保持 100% 向后兼容）：**每发送一条 `IngressItem` 前，立即调用 `matcher.SetActiveRecordID(item.RecordID)` 将局部 Mock 匹配焦点切换至当前入口请求的因果闭包**，随后写入 `item.RawReq` 并在连接 `StreamFramer` 中等待相同 `ullSNO` 的应答帧（若收到 `ucMsgType == 4 (RSP_SEG)` 则持续收集各分片直至收到终结帧，单请求最长等待 `TimeoutMs` 默认 `30s`）。

### 3.2 分层作用域与双级无 Schema 二进制 Mock 匹配算法 (`ScopedMockMatcher`)

为杜绝单批次多请求因全局顺序兜底导致的“张冠李戴”（例如第 10 次请求因为带时间戳无法精确哈希命中，却意外弹出了第 1 次请求录制的旧应答），`pkg/agentproto` 与 `pkg/replay` 端到端打通**「`RecordID` 因果闭包局部优先 + 全局顺序兜底」**的分层匹配契约（零 DB、零 `.pb` 依赖）：

```go
package agentproto

// ReplayIngressItem 单条待重放入站请求及其不透明记录标识（供 Agent 切换 ScopedMocks 作用域）
type ReplayIngressItem struct {
	RecordID uint64 `json:"record_id"` // 对应主控 traefik_traffic_records.id
	RawReq   []byte `json:"raw_req"`   // 原始入站 CROS RPC 请求帧
}

// MockRuleDownstream 下游出站 Mock 规则（成对请求与应答，纯二进制切片，零 .pb 依赖）
type MockRuleDownstream struct {
	ReqPacket  []byte   `json:"req_packet"`  // 录制时的出站请求帧（用于提取 Service/Operation 并计算 ReqPayload SHA256 精确匹配）
	RspPackets [][]byte `json:"rsp_packets"` // 录制时的出站应答帧组（可含拼接 RSP_SEG+RSP，用于自愈并回包）
}

// ExecuteReplayReq 主控向 Agent 下发的离线隔离重放执行载荷（纯二进制切片，零 .pb 依赖）
type ExecuteReplayReq struct {
	TaskID              uint64                          `json:"task_id"`
	ProcessSpec         ProcessMonitorSpec              `json:"process_spec"`
	TimeoutMs           int                             `json:"timeout_ms"`
	IngressItems        []ReplayIngressItem             `json:"ingress_items,omitempty"`         // 按发起序排列的待注入请求列表 (M13 主路径)
	ScopedEgressMocks   map[uint64][]MockRuleDownstream `json:"scoped_egress_mocks,omitempty"`   // key = RecordID -> 专属下游成对 Mock 规则列表
	GlobalFallbackMocks []MockRuleDownstream            `json:"global_fallback_mocks,omitempty"` // 未归属孤儿下游 Mock 规则兜底池
	IngressRequests     [][]byte                        `json:"ingress_requests,omitempty"`      // 兼容旧平铺注入列表
	EgressMocks         [][]byte                        `json:"egress_mocks,omitempty"`          // 兼容旧平铺下游 Mock 池
	MngMocks            [][]byte                        `json:"mng_mocks,omitempty"`
}
```

1. **局部作用域索引与切分**：
   - 主控 `diff.ReplayScheduler` 在下发重放包时，优先依据 DOTR `TraceID` 与 `SpanID / FatherSpanID` 因果子树（无 `TraceID` 时回退录制期请求往返时间窗口 `[req_ns - 500μs, rsp_ns + 500μs]`），将每条选定入站请求所触发的合格下游出站请求与应答帧成对提取为 `MockRuleDownstream`，按发起序划归至 `ScopedEgressMocks[record_id]`；
   - Agent 初始化 `ScopedMockMatcher` 时逐个解析 `rule.ReqPacket` 提取服务与方法名并计算 `ReqPayload` 的 SHA256，将 `rule.RspPackets` 注册至每个局部池内部的 `ExactMap[MethodKey + ReqPayloadSHA256] -> []MockRspFrames`，同时以 `MethodKey` 挂入 `SeqQueue[MethodKey] -> []MockRspFrames` 兜底；
   - 同时将批次内未被认领的合格下游成对规则注册至全局兜底池 `GlobalFallbackMocks`。

```mermaid
flowchart TD
    Recv["收到目标进程出站帧 liveReq"] --> CheckType{"判断 ucMsgType"}
    CheckType -- "MNG (5/6/7)" --> MngMatch["按 ucMsgType 取录制同类 MNG 应答；覆写 [8..15] ullSNO 后返回 (无则忽略)"]
    CheckType -- "REQ (2)" --> CheckOneway{"ucReqType == 3 (Oneway)?"}
    CheckOneway -- "是" --> OnewayHit["记录 Oneway 调用明细，不返回任何应答字节"]
    CheckOneway -- "否" --> CalcKey["提取 MethodKey=(ascServiceName, ascOperation) 并计算 Payload SHA256"]
    CalcKey --> ScopeCheck{"当前活跃 IngressReq 专属 ScopedMocks 存在未消费项?"}
    ScopeCheck -- "命中精确哈希或专属顺序" --> ConsumeScope["从 ScopedMocks 弹出应答组 (SCOPED_HIT)"]
    ScopeCheck -- "专属池未命中/已耗尽" --> GlobalTier{"全局 GlobalFallbackMocks 存在未消费项?"}
    GlobalTier -- "命中" --> ConsumeGlobal["从全局池弹出并标记 GLOBAL_FALLBACK_HIT 警示"]
    GlobalTier -- "已耗尽或未录制" --> Miss["严禁外泄连接真实下游! 记录 MOCK_MISS 明细并保持静默等待目标超时"]
    ConsumeScope --> Heal["执行 HealResponseFrames 表驱动自愈改写全部应答帧/分片"]
    ConsumeGlobal --> Heal
    Heal --> Send["写回 Mock 连接并记录 MockHit 明细"]
```

### 3.3 基于 `RspLayout` 表驱动的原地二进制自愈覆写 (`HealResponseFrames`)

DOPHI CROS RPC 客户端在收到应答帧时，会严格核对公共头序列号 `ullSNO`、会话头回显的 `aucTraceBuff`、发送时间戳 `ullClientSendTime` 以及服务方法名。`pkg/replay` 严禁使用松散的手工偏移硬编码，必须统一调用 `pkg/crosrpc.LookupRspLayout` 表驱动检索并覆写：

```go
package replay

import (
	"encoding/binary"
	"errors"

	"code-traefik/pkg/crosrpc"
)

var (
	ErrFrameTooShort = errors.New("crosrpc frame length below minimum header")
)

// HealSingleResponseFrame 基于当前实时下游请求帧 liveReq，对单帧录制应答帧 mockRsp 执行表驱动原地头部自愈覆写
func HealSingleResponseFrame(liveReq []byte, mockRsp []byte) ([]byte, error) {
	if len(liveReq) < crosrpc.CommHeadLen || len(mockRsp) < crosrpc.CommHeadLen {
		return nil, ErrFrameTooShort
	}
	healed := make([]byte, len(mockRsp))
	copy(healed, mockRsp)

	// 1. 覆写公共头偏移 8..15 的 8B 大端请求序列号 ullSNO
	copy(healed[8:16], liveReq[8:16])

	// 读取双方会话头实际长度 (偏移 26..27: usReqHeadLen / usRspHeadLen, 大端)
	reqSessionLen := int(binary.BigEndian.Uint16(liveReq[26:28]))
	rspSessionLen := int(binary.BigEndian.Uint16(healed[26:28]))
	if reqSessionLen > len(liveReq)-crosrpc.CommHeadLen {
		reqSessionLen = len(liveReq) - crosrpc.CommHeadLen
	}
	if rspSessionLen > len(healed)-crosrpc.CommHeadLen {
		rspSessionLen = len(healed) - crosrpc.CommHeadLen
	}

	reqSession := liveReq[crosrpc.CommHeadLen : crosrpc.CommHeadLen+reqSessionLen]
	rspSession := healed[crosrpc.CommHeadLen : crosrpc.CommHeadLen+rspSessionLen]

	// 2. 依据 usRspHeadLen 检索权威字段偏移布局表 (V1~V4)
	layout, err := crosrpc.LookupRspLayout(rspSessionLen)
	if err != nil {
		return nil, err
	}

	// 3. 覆写 Trace 缓冲回显: RPC_RSP_HEAD_S[layout.TraceBuffOffset] <- RPC_REQ_HEAD_S[144..160] (aucTraceBuff[16])
	if reqSessionLen >= 160 && layout.TraceBuffOffset+layout.TraceBuffLen <= rspSessionLen {
		copy(rspSession[layout.TraceBuffOffset:layout.TraceBuffOffset+layout.TraceBuffLen], reqSession[144:160])
	}

	// 4. 覆写客户端发送时间戳回显 (V3+ 应答头):
	//    若实时请求为 V5+ (reqSessionLen >= 256)，回填 RPC_REQ_HEAD_S[248..256] 的 ullSendTimeStamp；
	//    若实时请求低于 V5 无发送时间戳，则清零该 8B
	if layout.ClientSendTimeOffset > 0 && layout.ClientSendTimeOffset+8 <= rspSessionLen {
		if reqSessionLen >= 256 {
			copy(rspSession[layout.ClientSendTimeOffset:layout.ClientSendTimeOffset+8], reqSession[248:256])
		} else {
			clear(rspSession[layout.ClientSendTimeOffset : layout.ClientSendTimeOffset+8])
		}
	}

	// 5. 校验并同步服务名与方法名回显
	if reqSessionLen >= 136 && layout.ServiceNameOffset+64 <= rspSessionLen && layout.OperationOffset+64 <= rspSessionLen {
		copy(rspSession[layout.ServiceNameOffset:layout.ServiceNameOffset+64], reqSession[8:72])
		copy(rspSession[layout.OperationOffset:layout.OperationOffset+64], reqSession[72:136])
	}

	return healed, nil
}
```

* **多段分片应答 (`RSP_SEG`) 连续切片自愈保障**：当一次下游调用录制有 $K$ 个响应片段（如 `RSP_SEG #0`, `RSP_SEG #1`, `RSP #2`）时，`HealResponseFrames` 按 `usRspMsgNo` 录制原序遍历全部 $K$ 个片段，逐片调用 `HealSingleResponseFrame(liveReq, seg_k)` 后依次写入 Mock Socket，确保分片流在客户端侧无缝重组。
