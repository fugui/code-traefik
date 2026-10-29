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
	ringbufReader *ringbuf.Reader
	attached      bool
	stopChan      chan struct{}
	sessionID     uint64

	monitoredProcs map[uint64]*MonitoredProc // processID -> MonitoredProc
	statBypassed   int64
	statDrops      int64
	statRedirected int64
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

	// 2. 逐一诊断并登记目标进程
	var diags []agentproto.ProcessDiagStatus
	activeProcMap := make(map[uint64]*MonitoredProc)

	for _, spec := range specs {
		pid, err := m.scanner.FindPIDByPortAndCmd(spec.ListenPort, spec.CmdPattern)
		status := "MONITORING"
		msg := ""
		estConns := 0
		needRec := false

		if err != nil || pid == 0 {
			status = "NOT_FOUND"
			msg = fmt.Sprintf("未在端口 %d 检测到符合特征的业务进程", spec.ListenPort)
		} else {
			estConns, needRec, _ = m.scanner.DiagnoseEstablishedConns(pid, spec.Downstreams)
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
				_ = m.objs.TargetPidsMap.Put(pid, tp)

				// 更新出站重定向规则
				proxyPort := uint16(0)
				if m.egressProxy != nil {
					proxyPort = m.egressProxy.ProxyPort()
				}
				for _, ds := range spec.Downstreams {
					ipParsed := net.ParseIP(ds.IP).To4()
					if ipParsed == nil {
						ipParsed = net.IPv4(127, 0, 0, 1)
					}
					ipUint := binary.BigEndian.Uint32(ipParsed)
					key := cros_probeDsKey{
						ProcessId: spec.ProcessID,
						DstIp:     ipUint,
						DstPort:   ds.Port,
					}
					target := cros_probeDsTarget{
						RedirectPort: proxyPort,
						IsReplay:     0,
					}
					_ = m.objs.DownstreamRulesMap.Put(key, target)
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
		_ = m.objs.TargetPidsMap.Delete(proc.CurrentPID)
		for _, ds := range proc.Spec.Downstreams {
			ipParsed := net.ParseIP(ds.IP).To4()
			if ipParsed == nil {
				ipParsed = net.IPv4(127, 0, 0, 1)
			}
			ipUint := binary.BigEndian.Uint32(ipParsed)
			key := cros_probeDsKey{
				ProcessId: processID,
				DstIp:     ipUint,
				DstPort:   ds.Port,
			}
			_ = m.objs.DownstreamRulesMap.Delete(key)
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
				Ip:   binary.BigEndian.Uint32(net.IPv4(127, 0, 0, 1)),
				Port: clientPort,
			}
			var od cros_probeOrigDst
			if err := m.objs.NatTupleMap.Lookup(addr, &od); err == nil {
				origIP := net.IPv4(
					byte(od.OrigIp>>24),
					byte(od.OrigIp>>16),
					byte(od.OrigIp>>8),
					byte(od.OrigIp),
				)
				return od.ProcessId, od.Pid, origIP, od.OrigPort, true
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
		ProxyPort: proxy.ProxyPort(),
	}
	_ = objs.AgentCfgMap.Put(uint32(0), cfg)

	// 挂载 cgroup/connect4 钩子
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

	// 挂载 sock_ops 钩子
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

	// 挂载 syscall tracepoints
	tpReadEnter, err := link.Tracepoint("syscalls", "sys_enter_read", objs.TraceSysEnterRead, nil)
	if err == nil {
		links = append(links, tpReadEnter)
	} else {
		log.Printf("[eBPF] Tracepoint sys_enter_read 警告: %v", err)
	}
	tpReadExit, err := link.Tracepoint("syscalls", "sys_exit_read", objs.TraceSysExitRead, nil)
	if err == nil {
		links = append(links, tpReadExit)
	} else {
		log.Printf("[eBPF] Tracepoint sys_exit_read 警告: %v", err)
	}

	tpWriteEnter, err := link.Tracepoint("syscalls", "sys_enter_write", objs.TraceSysEnterWrite, nil)
	if err == nil {
		links = append(links, tpWriteEnter)
	} else {
		log.Printf("[eBPF] Tracepoint sys_enter_write 警告: %v", err)
	}
	tpWriteExit, err := link.Tracepoint("syscalls", "sys_exit_write", objs.TraceSysExitWrite, nil)
	if err == nil {
		links = append(links, tpWriteExit)
	} else {
		log.Printf("[eBPF] Tracepoint sys_exit_write 警告: %v", err)
	}

	tpRecvEnter, err := link.Tracepoint("syscalls", "sys_enter_recvfrom", objs.TraceSysEnterRecvfrom, nil)
	if err == nil {
		links = append(links, tpRecvEnter)
	} else {
		log.Printf("[eBPF] Tracepoint sys_enter_recvfrom 警告: %v", err)
	}
	tpRecvExit, err := link.Tracepoint("syscalls", "sys_exit_recvfrom", objs.TraceSysExitRecvfrom, nil)
	if err == nil {
		links = append(links, tpRecvExit)
	} else {
		log.Printf("[eBPF] Tracepoint sys_exit_recvfrom 警告: %v", err)
	}

	tpSendEnter, err := link.Tracepoint("syscalls", "sys_enter_sendto", objs.TraceSysEnterSendto, nil)
	if err == nil {
		links = append(links, tpSendEnter)
	} else {
		log.Printf("[eBPF] Tracepoint sys_enter_sendto 警告: %v", err)
	}
	tpSendExit, err := link.Tracepoint("syscalls", "sys_exit_sendto", objs.TraceSysExitSendto, nil)
	if err == nil {
		links = append(links, tpSendExit)
	} else {
		log.Printf("[eBPF] Tracepoint sys_exit_sendto 警告: %v", err)
	}

	// 挂载 sched_process_exec
	tpSchedExec, err := link.Tracepoint("sched", "sched_process_exec", objs.CrosSchedExec, nil)
	if err == nil {
		links = append(links, tpSchedExec)
	} else {
		log.Printf("[eBPF] Tracepoint sched_process_exec 警告: %v", err)
	}

	m.links = links
	log.Printf("[eBPF] 探针挂载完成，共挂载 %d 个内核链接", len(links))

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
				_ = m.objs.TargetPidsMap.Delete(oldPID)
			}
			// 登记新 PID
			tp := cros_probeTargetProc{
				SessionId:  m.sessionID,
				ProcessId:  proc.Spec.ProcessID,
				ListenPort: proc.Spec.ListenPort,
				Mode:       0,
			}
			_ = m.objs.TargetPidsMap.Put(newPID, tp)
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
