package ebpf

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"code-traefik/pkg/agentproto"
	"code-traefik/pkg/crosrpc"
	"code-traefik/pkg/replay"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
)

// CapabilityChecker 权限探测器接口 (便于单测注入 Mock)
type CapabilityChecker interface {
	CheckCapabilities() (hasCaps bool, missing []string, detail string)
}

// LinuxCapabilityChecker 默认只读解析 /proc/self/status CapEff 位图
type LinuxCapabilityChecker struct{}

// Linux capability 常量
const (
	CapNetAdmin  = 12
	CapSysPtrace = 19
	CapPerfmon   = 38
	CapBPF       = 39
)

func (c *LinuxCapabilityChecker) CheckCapabilities() (bool, []string, string) {
	// 若是 root 用户 (EUID == 0)，通常具备所有 Capabilities
	if os.Geteuid() == 0 {
		return true, nil, "EUID=0 (Full root privileges)"
	}

	f, err := os.Open("/proc/self/status")
	if err != nil {
		return false, []string{"CAP_BPF", "CAP_NET_ADMIN"}, fmt.Sprintf("无法读取 /proc/self/status: %v", err)
	}
	defer f.Close()

	var capEffStr string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "CapEff:") {
			capEffStr = strings.TrimSpace(strings.TrimPrefix(line, "CapEff:"))
			break
		}
	}

	if capEffStr == "" {
		return false, []string{"CAP_BPF", "CAP_NET_ADMIN"}, "未在 /proc/self/status 中找到 CapEff"
	}

	capEff, err := strconv.ParseUint(capEffStr, 16, 64)
	if err != nil {
		return false, []string{"CAP_BPF", "CAP_NET_ADMIN"}, fmt.Sprintf("解析 CapEff 失败: %v", err)
	}

	var missing []string
	if capEff&(1<<CapNetAdmin) == 0 {
		missing = append(missing, "CAP_NET_ADMIN")
	}
	if capEff&(1<<CapSysPtrace) == 0 {
		missing = append(missing, "CAP_SYS_PTRACE")
	}
	if capEff&(1<<CapPerfmon) == 0 {
		missing = append(missing, "CAP_PERFMON")
	}
	if capEff&(1<<CapBPF) == 0 {
		missing = append(missing, "CAP_BPF")
	}

	if len(missing) > 0 {
		return false, missing, fmt.Sprintf("缺少必须的特权集合: %s (CapEff: 0x%x)", strings.Join(missing, ", "), capEff)
	}

	return true, nil, fmt.Sprintf("特权完整 (CapEff: 0x%x)", capEff)
}

// LifecycleEventSink 进程生命周期或连接变更事件回调
type LifecycleEventSink func(eventType uint8, eventData []byte)

// ManagerConfig eBPF 管理器启动配置
type ManagerConfig struct {
	AgentPID           uint32
	CgroupPath         string
	ProcRoot           string
	MaxCaptureBytes    int
	MaxFramerBudget    int
	CapabilityChecker  CapabilityChecker
	FrameSink          FrameSinkFunc
	LifecycleEventSink LifecycleEventSink
}

// MonitoredProc 内部维护的目标进程状态
type MonitoredProc struct {
	Spec             agentproto.ProcessMonitorSpec
	CurrentPID       uint32
	DiagStatus       string
	EstablishedConns int
	NeedReconnect    bool
}

// Manager eBPF 探针装载、规则热更新与生命周期自洁中枢
type Manager struct {
	mu            sync.Mutex
	cfg           ManagerConfig
	capChecker    CapabilityChecker
	scanner       *ProcScanner
	ingressReader *GapAwareIngressReader
	egressProxy   *EgressProxy
	objs          *cros_probeObjects
	links         []link.Link
	rwLinks       []link.Link
	ringbufReader *ringbuf.Reader
	attached      bool
	stopChan      chan struct{}
	sessionID     uint64

	monitoredProcs map[uint64]*MonitoredProc // processID -> MonitoredProc
	statBypassed   int64
	statDrops      int64
	statRedirected int64
	connSeqGen     uint32
	mockPort       uint16
}

// NewManager 实例化 eBPF 管理器
func NewManager(cfg ManagerConfig) *Manager {
	if cfg.AgentPID == 0 {
		cfg.AgentPID = uint32(os.Getpid())
	}
	if cfg.CgroupPath == "" {
		cfg.CgroupPath = "/sys/fs/cgroup"
	}
	if cfg.MaxCaptureBytes <= 0 {
		cfg.MaxCaptureBytes = crosrpc.DefaultMaxCaptureBytes
	}
	capChecker := cfg.CapabilityChecker
	if capChecker == nil {
		capChecker = &LinuxCapabilityChecker{}
	}

	pool := NewGlobalFramerBudgetPool(cfg.MaxFramerBudget)
	reader := NewGapAwareIngressReader(pool, cfg.MaxCaptureBytes)

	return &Manager{
		cfg:            cfg,
		capChecker:     capChecker,
		scanner:        NewProcScanner(cfg.ProcRoot),
		ingressReader:  reader,
		monitoredProcs: make(map[uint64]*MonitoredProc),
		stopChan:       make(chan struct{}),
	}
}

// CheckPrivileges 检查宿主机 eBPF 所需特权
func (m *Manager) CheckPrivileges() (bool, []string, string) {
	return m.capChecker.CheckCapabilities()
}

// IsAttached 返回内核探针当前是否处于挂载状态
func (m *Manager) IsAttached() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.attached
}

// StartIngressReaderLoop 启动 RingBuf 消费循环
func (m *Manager) startRingbufConsumer(rb *ringbuf.Reader) {
	go func() {
		for {
			record, err := rb.Read()
			if err != nil {
				if errors.Is(err, ringbuf.ErrClosed) {
					return
				}
				continue
			}

			// 解析 ing_data_event
			if len(record.RawSample) < 48 {
				continue
			}
			b := record.RawSample

			sessionID := binary.LittleEndian.Uint64(b[0:8])
			processID := binary.LittleEndian.Uint64(b[8:16])
			pid := binary.LittleEndian.Uint32(b[16:20])
			direction := b[20]
			srcIP := net.IPv4(b[24], b[25], b[26], b[27])
			dstIP := net.IPv4(b[28], b[29], b[30], b[31])
			srcPort := binary.BigEndian.Uint16(b[32:34])
			dstPort := binary.BigEndian.Uint16(b[34:36])
			connSeq := binary.LittleEndian.Uint32(b[36:40])
			streamOff := binary.LittleEndian.Uint64(b[40:48])
			dataLen := binary.LittleEndian.Uint32(b[48:52])

			var data []byte
			if len(b) >= 52+int(dataLen) {
				data = b[52 : 52+dataLen]
			}

			chunk := IngressChunk{
				SessionID: sessionID,
				ProcessID: processID,
				PID:       pid,
				Direction: direction,
				SrcIP:     srcIP,
				DstIP:     dstIP,
				SrcPort:   srcPort,
				DstPort:   dstPort,
				ConnSeq:   connSeq,
				StreamOff: streamOff,
				Data:      data,
			}

			res, _ := m.ingressReader.FeedChunk(chunk)
			if m.cfg.FrameSink != nil {
				for _, env := range res.Frames {
					m.cfg.FrameSink(env)
				}
			}
		}
	}()
}

// SyncMonitors 声明式下发并同步监控规格
func (m *Manager) SyncMonitors(sessionID uint64, specs []agentproto.ProcessMonitorSpec) ([]agentproto.ProcessDiagStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	hasCaps, missing, detail := m.capChecker.CheckCapabilities()
	if !hasCaps {
		var diags []agentproto.ProcessDiagStatus
		for _, sp := range specs {
			diags = append(diags, agentproto.ProcessDiagStatus{
				ProcessID: sp.ProcessID,
				Status:    "CAP_DENIED",
				Message:   fmt.Sprintf("缺少特权: %s (%s)", strings.Join(missing, ", "), detail),
			})
		}
		return diags, errors.New("缺少必要的 eBPF 特权")
	}

	m.sessionID = sessionID

	// 1. 若尚未挂载探针，执行内核探针挂载
	if !m.attached {
		if err := m.attachLocked(); err != nil {
			return nil, fmt.Errorf("挂载 eBPF 探针失败: %w", err)
		}
	}

	// 录制阶段：动态按需挂载读写 Tracepoints，并将 EgressProxy 置为 CAPTURING 模式并原子注入 sessionID
	m.attachCaptureTracepointsLocked()
	if m.egressProxy != nil {
		m.egressProxy.StartCapture(sessionID)
	}

	// 2. 逐一诊断并登记目标进程
	var diags []agentproto.ProcessDiagStatus
	activeProcMap := make(map[uint64]*MonitoredProc)

	proxyPort := uint16(0)
	if m.egressProxy != nil {
		proxyPort = m.egressProxy.ProxyPort()
	}

	for _, spec := range specs {
		pid, err := m.scanner.FindPIDByPortAndCmd(spec.ListenPort, spec.CmdPattern)
		status := "MONITORING"
		msg := ""
		estConns := 0
		needRec := false

		if err != nil || pid == 0 {
			status = "NOT_FOUND"
			if spec.ListenPort == 0 {
				msg = fmt.Sprintf("未在特征 [%s] 下检测到运行中的客户端进程", spec.CmdPattern)
			} else {
				msg = fmt.Sprintf("未在端口 %d 检测到符合特征的业务进程", spec.ListenPort)
			}
		} else {
			var diagErr error
			estConns, needRec, diagErr = m.scanner.DiagnoseEstablishedConns(pid, spec.Downstreams, spec.ListenPort, proxyPort)
			if diagErr != nil {
				log.Printf("[eBPF] 诊断存量连接 (pid=%d) 警告: %v", pid, diagErr)
			}
			if needRec {
				msg = fmt.Sprintf("检测到 %d 条存量已建立长连接，需重启业务进程生效 (A-01)", estConns)
			}

			// 更新 BPF Map
			if m.objs != nil {
				tp := cros_probeTargetProc{
					SessionId:  sessionID,
					ProcessId:  spec.ProcessID,
					ListenPort: spec.ListenPort,
					Mode:       0, // RECORDING
				}
				if err := m.objs.TargetPidsMap.Put(pid, tp); err != nil {
					log.Printf("[eBPF] TargetPidsMap.Put (pid=%d) 失败: %v", pid, err)
				}

				// 存量入站 FD 预热：将 local_port == spec.ListenPort 的已建立入站 Socket 批量写入 ing_conn_map (F-08, ADR-11)
				if spec.ListenPort > 0 {
					inboundSocks, sErr := m.scanner.ScanProcessEstablishedSockets(pid)
					if sErr == nil {
						for _, isock := range inboundSocks {
							if isock.LocalPort == spec.ListenPort {
								pfd := cros_probePidFd{
									Pid: pid,
									Fd:  isock.FD,
								}
								conn := cros_probeConnTuple{
									SrcIp:   ipv4ToBPFBe32(net.ParseIP(isock.RemIP)),
									DstIp:   ipv4ToBPFBe32(net.ParseIP(isock.LocalIP)),
									SrcPort: htons(isock.RemPort),
									DstPort: htons(isock.LocalPort),
									ConnSeq: atomic.AddUint32(&m.connSeqGen, 1),
								}
								if pErr := m.objs.IngConnMap.Put(pfd, conn); pErr != nil {
									log.Printf("[eBPF] 预热入站连接 (pid=%d, fd=%d) 警告: %v", pid, isock.FD, pErr)
								}
							}
						}
					}
				}

				// 更新出站重定向参考规则 (downstream_rules_map)
				for _, ds := range spec.Downstreams {
					ipParsed := net.ParseIP(ds.IP)
					if ipParsed == nil {
						ipParsed = net.IPv4(127, 0, 0, 1)
					}
					key := cros_probeDsKey{
						ProcessId: spec.ProcessID,
						DstIp:     ipv4ToBPFBe32(ipParsed),
						DstPort:   htons(ds.Port),
					}
					target := cros_probeDsTarget{
						RedirectPort: htons(proxyPort),
						IsReplay:     0,
					}
					if err := m.objs.DownstreamRulesMap.Put(key, target); err != nil {
						log.Printf("[eBPF] DownstreamRulesMap.Put 失败: %v", err)
					}
				}
			}
		}

		diag := agentproto.ProcessDiagStatus{
			ProcessID:        spec.ProcessID,
			Status:           status,
			ResolvedPID:      pid,
			EstablishedConns: estConns,
			NeedReconnect:    needRec,
			Message:          msg,
		}
		diags = append(diags, diag)

		activeProcMap[spec.ProcessID] = &MonitoredProc{
			Spec:             spec,
			CurrentPID:       pid,
			DiagStatus:       status,
			EstablishedConns: estConns,
			NeedReconnect:    needRec,
		}
	}

	m.monitoredProcs = activeProcMap
	return diags, nil
}

// ArmStandby 预挂载并进入 ARMED 待命状态 (零大流量 Tracepoint 损耗)
func (m *Manager) ArmStandby(specs []agentproto.ProcessMonitorSpec) ([]agentproto.ProcessDiagStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	hasCaps, missing, detail := m.capChecker.CheckCapabilities()
	if !hasCaps {
		var diags []agentproto.ProcessDiagStatus
		for _, sp := range specs {
			diags = append(diags, agentproto.ProcessDiagStatus{
				ProcessID: sp.ProcessID,
				Status:    "CAP_DENIED",
				Message:   fmt.Sprintf("缺少特权: %s (%s)", strings.Join(missing, ", "), detail),
			})
		}
		return diags, errors.New("缺少必要的 eBPF 特权")
	}

	if !m.attached {
		if err := m.attachLocked(); err != nil {
			return nil, fmt.Errorf("挂载 eBPF 探针失败: %w", err)
		}
	}

	// 待命状态脱落大流量 Tracepoints，并置为 PASSTHROUGH 模式
	m.detachCaptureTracepointsLocked()
	if m.egressProxy != nil {
		m.egressProxy.SetMode(ModePassthrough)
	}

	var diags []agentproto.ProcessDiagStatus
	activeProcMap := make(map[uint64]*MonitoredProc)

	proxyPort := uint16(0)
	if m.egressProxy != nil {
		proxyPort = m.egressProxy.ProxyPort()
	}

	for _, spec := range specs {
		pid, err := m.scanner.FindPIDByPortAndCmd(spec.ListenPort, spec.CmdPattern)
		status := "MONITORING"
		msg := ""
		estConns := 0
		needRec := false

		if err != nil || pid == 0 {
			status = "NOT_FOUND"
			if spec.ListenPort == 0 {
				msg = fmt.Sprintf("未在特征 [%s] 下检测到运行中的客户端进程", spec.CmdPattern)
			} else {
				msg = fmt.Sprintf("未在端口 %d 检测到符合特征的业务进程", spec.ListenPort)
			}
		} else {
			var diagErr error
			estConns, needRec, diagErr = m.scanner.DiagnoseEstablishedConns(pid, spec.Downstreams, spec.ListenPort, proxyPort)
			if diagErr != nil {
				log.Printf("[eBPF] ArmStandby 诊断存量连接 (pid=%d) 警告: %v", pid, diagErr)
			}
			if needRec {
				msg = fmt.Sprintf("检测到 %d 条存量已建立长连接，需重启业务进程生效 (A-01)", estConns)
			}

			// 预设 BPF Map
			if m.objs != nil {
				tp := cros_probeTargetProc{
					SessionId:  0,
					ProcessId:  spec.ProcessID,
					ListenPort: spec.ListenPort,
					Mode:       0,
				}
				if err := m.objs.TargetPidsMap.Put(pid, tp); err != nil {
					log.Printf("[eBPF] ArmStandby TargetPidsMap.Put (pid=%d) 失败: %v", pid, err)
				}

				for _, ds := range spec.Downstreams {
					ipParsed := net.ParseIP(ds.IP)
					if ipParsed == nil {
						ipParsed = net.IPv4(127, 0, 0, 1)
					}
					key := cros_probeDsKey{
						ProcessId: spec.ProcessID,
						DstIp:     ipv4ToBPFBe32(ipParsed),
						DstPort:   htons(ds.Port),
					}
					target := cros_probeDsTarget{
						RedirectPort: htons(proxyPort),
						IsReplay:     0,
					}
					if err := m.objs.DownstreamRulesMap.Put(key, target); err != nil {
						log.Printf("[eBPF] ArmStandby DownstreamRulesMap.Put 失败: %v", err)
					}
				}
			}
		}

		diag := agentproto.ProcessDiagStatus{
			ProcessID:        spec.ProcessID,
			Status:           status,
			ResolvedPID:      pid,
			EstablishedConns: estConns,
			NeedReconnect:    needRec,
			Message:          msg,
		}
		diags = append(diags, diag)

		activeProcMap[spec.ProcessID] = &MonitoredProc{
			Spec:             spec,
			CurrentPID:       pid,
			DiagStatus:       status,
			EstablishedConns: estConns,
			NeedReconnect:    needRec,
		}
	}

	m.monitoredProcs = activeProcMap
	return diags, nil
}

// StopCapture 平滑停止录制：排空在途数据并解绑大流量 Tracepoint，但保留长连接与待命状态
func (m *Manager) StopCapture() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.egressProxy != nil {
		m.egressProxy.DrainAndSetPassthrough(200 * time.Millisecond)
	}
	m.detachCaptureTracepointsLocked()
	m.sessionID = 0

	// 恢复 BPF Map 中目标进程模式为 MODE_RECORDING (0)
	if m.objs != nil {
		for _, proc := range m.monitoredProcs {
			if proc.CurrentPID > 0 {
				tp := cros_probeTargetProc{
					SessionId:  0,
					ProcessId:  proc.Spec.ProcessID,
					ListenPort: proc.Spec.ListenPort,
					Mode:       0, // RECORDING / PASSTHROUGH
				}
				if err := m.objs.TargetPidsMap.Put(proc.CurrentPID, tp); err != nil {
					log.Printf("[eBPF] StopCapture TargetPidsMap.Put (pid=%d) 警告: %v", proc.CurrentPID, err)
				}
			}
		}
	}
}

// SetReplayMock 设置回放 Mock 拦截模式
func (m *Manager) SetReplayMock(taskID uint64, matcher *replay.ScopedMockMatcher, mockEndpoints []agentproto.DownstreamEndpoint) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.detachCaptureTracepointsLocked()
	if m.egressProxy != nil {
		m.egressProxy.SetMockMatcher(matcher, mockEndpoints)
		m.egressProxy.SetMode(ModeReplayMock)
	}

	// 更新 BPF Map 中纳管目标进程为 MODE_REPLAY (1)
	if m.objs != nil {
		for _, proc := range m.monitoredProcs {
			if proc.CurrentPID > 0 {
				tp := cros_probeTargetProc{
					SessionId:  taskID,
					ProcessId:  proc.Spec.ProcessID,
					ListenPort: proc.Spec.ListenPort,
					Mode:       1, // MODE_REPLAY
				}
				if err := m.objs.TargetPidsMap.Put(proc.CurrentPID, tp); err != nil {
					log.Printf("[eBPF] SetReplayMock TargetPidsMap.Put (pid=%d) 警告: %v", proc.CurrentPID, err)
				}
			}
		}
	}
}

// EgressProxy 返回出站代理实例
func (m *Manager) EgressProxy() *EgressProxy {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.egressProxy
}

// MonitoredProcs 返回当前监控进程快照
func (m *Manager) MonitoredProcs() map[uint64]*MonitoredProc {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make(map[uint64]*MonitoredProc, len(m.monitoredProcs))
	for k, v := range m.monitoredProcs {
		cp[k] = v
	}
	return cp
}

// RemoveProcessMonitor 单独移除指定进程的监控
func (m *Manager) RemoveProcessMonitor(processID uint64) (remaining int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	proc, ok := m.monitoredProcs[processID]
	if !ok {
		return len(m.monitoredProcs), nil
	}

	// 从 BPF Map 移除该进程
	if m.objs != nil && proc.CurrentPID > 0 {
		if err := m.objs.TargetPidsMap.Delete(proc.CurrentPID); err != nil {
			log.Printf("[eBPF] TargetPidsMap.Delete (pid=%d) 警告: %v", proc.CurrentPID, err)
		}
		for _, ds := range proc.Spec.Downstreams {
			ipParsed := net.ParseIP(ds.IP)
			if ipParsed == nil {
				ipParsed = net.IPv4(127, 0, 0, 1)
			}
			key := cros_probeDsKey{
				ProcessId: processID,
				DstIp:     ipv4ToBPFBe32(ipParsed),
				DstPort:   htons(ds.Port),
			}
			if err := m.objs.DownstreamRulesMap.Delete(key); err != nil {
				log.Printf("[eBPF] DownstreamRulesMap.Delete 警告: %v", err)
			}
		}
	}

	delete(m.monitoredProcs, processID)
	remaining = len(m.monitoredProcs)

	// 若已无任何监控目标，自动卸载探针
	if remaining == 0 {
		m.purgeLocked()
	}

	return remaining, nil
}

// PurgeAllMonitors 彻底卸载全部探针、清空 Maps、关闭本地代理 (零内核残留)
func (m *Manager) PurgeAllMonitors() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.purgeLocked()
}

func (m *Manager) purgeLocked() {
	m.detachCaptureTracepointsLocked()

	// 1. 关闭 RingBuf Reader
	if m.ringbufReader != nil {
		_ = m.ringbufReader.Close()
		m.ringbufReader = nil
	}

	// 2. 脱落所有 bpf_link (内核零残留)
	for _, l := range m.links {
		_ = l.Close()
	}
	m.links = nil

	// 3. 关闭 eBPF Maps & Programs
	if m.objs != nil {
		_ = m.objs.Close()
		m.objs = nil
	}

	// 4. 关闭出站代理
	if m.egressProxy != nil {
		_ = m.egressProxy.Close()
		m.egressProxy = nil
	}

	m.monitoredProcs = make(map[uint64]*MonitoredProc)
	m.attached = false
	m.sessionID = 0
}

func (m *Manager) attachLocked() error {
	// 启动出站透明代理
	proxy, err := NewEgressProxy(
		func(clientPort uint16) (uint64, uint32, net.IP, uint16, bool) {
			if m.objs == nil {
				return 0, 0, nil, 0, false
			}
			addr := cros_probeClientAddr{
				Ip:   ipv4ToBPFBe32(net.IPv4(127, 0, 0, 1)),
				Port: clientPort,
			}
			var od cros_probeOrigDst
			if err := m.objs.NatTupleMap.Lookup(addr, &od); err == nil {
				origIP := bpfBe32ToIPv4(od.OrigIp)
				origPort := ntohs(od.OrigPort)
				return od.ProcessId, od.Pid, origIP, origPort, true
			}
			return 0, 0, nil, 0, false
		},
		m.cfg.FrameSink,
	)
	if err != nil {
		return fmt.Errorf("启动 EgressProxy 失败: %w", err)
	}
	m.egressProxy = proxy

	// 加载 BPF 对象
	objs := &cros_probeObjects{}
	if err := loadCros_probeObjects(objs, nil); err != nil {
		_ = proxy.Close()
		return fmt.Errorf("loadCros_probeObjects 失败: %w", err)
	}
	m.objs = objs

	// 配置 agent_cfg_map
	cfg := cros_probeAgentCfg{
		AgentPid:  m.cfg.AgentPID,
		ProxyPort: htons(proxy.ProxyPort()),
		MockPort:  htons(m.mockPort),
	}
	if err := objs.AgentCfgMap.Put(uint32(0), cfg); err != nil {
		_ = proxy.Close()
		return fmt.Errorf("配置 agent_cfg_map 失败: %w", err)
	}

	// 挂载 ARMED 待命常驻钩子 (connect4, sock_ops, sched_process_exec)，零读写 tracepoint 损耗
	var links []link.Link
	cgroupLink, err := link.AttachCgroup(link.CgroupOptions{
		Path:    m.cfg.CgroupPath,
		Attach:  ebpf.AttachCGroupInet4Connect,
		Program: objs.CrosConnect4,
	})
	if err == nil {
		links = append(links, cgroupLink)
	} else {
		log.Printf("[eBPF] AttachCgroup connect4 警告: %v", err)
	}

	sockopsLink, err := link.AttachCgroup(link.CgroupOptions{
		Path:    m.cfg.CgroupPath,
		Attach:  ebpf.AttachCGroupSockOps,
		Program: objs.CrosSockOps,
	})
	if err == nil {
		links = append(links, sockopsLink)
	} else {
		log.Printf("[eBPF] AttachCgroup sock_ops 警告: %v", err)
	}

	// 挂载 sched_process_exec
	tpSchedExec, err := link.Tracepoint("sched", "sched_process_exec", objs.CrosSchedExec, nil)
	if err == nil {
		links = append(links, tpSchedExec)
	} else {
		log.Printf("[eBPF] Tracepoint sched_process_exec 警告: %v", err)
	}

	m.links = links
	log.Printf("[eBPF] 探针待命钩子挂载完成，共挂载 %d 个常驻内核链接", len(links))

	// 启动 RingBuf Reader
	rb, err := ringbuf.NewReader(objs.IngRingbuf)
	if err == nil {
		m.ringbufReader = rb
		m.startRingbufConsumer(rb)
	}

	m.attached = true

	// 启动后台 2s 巡检协程
	go m.backgroundProcessWatcher()

	return nil
}

// attachCaptureTracepointsLocked 录制阶段动态挂载大流量读写 Tracepoint
func (m *Manager) attachCaptureTracepointsLocked() {
	if m.objs == nil || len(m.rwLinks) > 0 {
		return
	}
	var rwLinks []link.Link
	attachTP := func(group, name string, prog *ebpf.Program) {
		l, err := link.Tracepoint(group, name, prog, nil)
		if err == nil {
			rwLinks = append(rwLinks, l)
		} else {
			log.Printf("[eBPF] Tracepoint %s/%s 警告: %v", group, name, err)
		}
	}

	attachTP("syscalls", "sys_enter_read", m.objs.TraceSysEnterRead)
	attachTP("syscalls", "sys_exit_read", m.objs.TraceSysExitRead)
	attachTP("syscalls", "sys_enter_write", m.objs.TraceSysEnterWrite)
	attachTP("syscalls", "sys_exit_write", m.objs.TraceSysExitWrite)
	attachTP("syscalls", "sys_enter_recvfrom", m.objs.TraceSysEnterRecvfrom)
	attachTP("syscalls", "sys_exit_recvfrom", m.objs.TraceSysExitRecvfrom)
	attachTP("syscalls", "sys_enter_sendto", m.objs.TraceSysEnterSendto)
	attachTP("syscalls", "sys_exit_sendto", m.objs.TraceSysExitSendto)
	attachTP("syscalls", "sys_exit_accept4", m.objs.TraceSysExitAccept4)
	attachTP("syscalls", "sys_exit_accept", m.objs.TraceSysExitAccept)
	attachTP("syscalls", "sys_enter_close", m.objs.TraceSysEnterClose)

	m.rwLinks = rwLinks
	log.Printf("[eBPF] 录制阶段动态挂载 %d 个大流量 tracepoints", len(rwLinks))
}

// detachCaptureTracepointsLocked 停录阶段立即卸载大流量读写 Tracepoint (恢复 0 CPU 损耗)
func (m *Manager) detachCaptureTracepointsLocked() {
	for _, l := range m.rwLinks {
		_ = l.Close()
	}
	m.rwLinks = nil
}

func (m *Manager) backgroundProcessWatcher() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			m.checkProcessesRestart()
		}
	}
}

func (m *Manager) checkProcessesRestart() {
	m.mu.Lock()
	if !m.attached || m.objs == nil {
		m.mu.Unlock()
		return
	}
	var eventsToEmit [][]byte
	for _, proc := range m.monitoredProcs {
		newPID, err := m.scanner.FindPIDByPortAndCmd(proc.Spec.ListenPort, proc.Spec.CmdPattern)
		if err == nil && newPID > 0 && newPID != proc.CurrentPID {
			oldPID := proc.CurrentPID
			// 移除旧 PID
			if oldPID > 0 {
				if err := m.objs.TargetPidsMap.Delete(oldPID); err != nil {
					log.Printf("[eBPF] TargetPidsMap.Delete (oldPID=%d) 警告: %v", oldPID, err)
				}
			}
			// 登记新 PID
			tp := cros_probeTargetProc{
				SessionId:  m.sessionID,
				ProcessId:  proc.Spec.ProcessID,
				ListenPort: proc.Spec.ListenPort,
				Mode:       0,
			}
			if err := m.objs.TargetPidsMap.Put(newPID, tp); err != nil {
				log.Printf("[eBPF] TargetPidsMap.Put (newPID=%d) 警告: %v", newPID, err)
			}
			proc.CurrentPID = newPID

			// 组装 PROCESS_RESTARTED 事件
			eventData := []byte(fmt.Sprintf(`{"process_id":%d,"old_pid":%d,"new_pid":%d,"event":"PROCESS_RESTARTED"}`, proc.Spec.ProcessID, oldPID, newPID))
			eventsToEmit = append(eventsToEmit, eventData)
		}
	}
	m.mu.Unlock()

	if m.cfg.LifecycleEventSink != nil {
		for _, evt := range eventsToEmit {
			m.cfg.LifecycleEventSink(agentproto.EventProcessLifecycle, evt)
		}
	}
}

// GetStats 获取内核统计指标
func (m *Manager) GetStats() (bypassedSelf, ringbufDrops, redirected int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.objs == nil {
		return atomic.LoadInt64(&m.statBypassed), atomic.LoadInt64(&m.statDrops), atomic.LoadInt64(&m.statRedirected)
	}

	var values []uint64
	idx0 := uint32(0)
	if err := m.objs.EbpfStatsMap.Lookup(idx0, &values); err == nil {
		for _, v := range values {
			bypassedSelf += int64(v)
		}
	}
	idx1 := uint32(1)
	if err := m.objs.EbpfStatsMap.Lookup(idx1, &values); err == nil {
		for _, v := range values {
			ringbufDrops += int64(v)
		}
	}
	idx2 := uint32(2)
	if err := m.objs.EbpfStatsMap.Lookup(idx2, &values); err == nil {
		for _, v := range values {
			redirected += int64(v)
		}
	}

	return bypassedSelf, ringbufDrops, redirected
}

// IngressReader 获取内部分帧器
func (m *Manager) IngressReader() *GapAwareIngressReader {
	return m.ingressReader
}
