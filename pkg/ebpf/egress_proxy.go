package ebpf

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"code-traefik/pkg/agentproto"
	"code-traefik/pkg/crosrpc"
	"code-traefik/pkg/replay"
)

// ProxyMode EgressProxy 运行模式
type ProxyMode int32

const (
	// ModePassthrough 待命透传模式（默认）：零应用层协议解析，零额外缓冲区申请，停录不断连
	ModePassthrough ProxyMode = 0
	// ModeCapturing 会话录制模式：分帧采集入信封流，有序排空
	ModeCapturing ProxyMode = 1
	// ModeReplayMock 回放 Mock 接管模式：命中 Mock 下游端点主动阻断真实后端连接 (0 字节外泄)
	ModeReplayMock ProxyMode = 2
)

func (m ProxyMode) String() string {
	switch m {
	case ModePassthrough:
		return "PASSTHROUGH"
	case ModeCapturing:
		return "CAPTURING"
	case ModeReplayMock:
		return "REPLAY_MOCK"
	default:
		return "UNKNOWN"
	}
}

// NatLookupFunc 查找客户端临时端口对应的原始目的地
type NatLookupFunc func(clientPort uint16) (processID uint64, pid uint32, origIP net.IP, origPort uint16, ok bool)

// FrameSinkFunc 接收代理采集到的出站 CROS RPC 信封帧
type FrameSinkFunc func(envelopeBytes []byte)

var proxyBufPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 32768)
		return &b
	},
}

// EgressProxy 透明 TCP 出站代理（三态隔离与零泄漏不断连机制）
type EgressProxy struct {
	mu            sync.RWMutex
	listener      net.Listener
	proxyPort     uint16
	natLookup     NatLookupFunc
	frameSink     FrameSinkFunc
	activeConns   sync.Map // net.Conn -> bool
	connSeqGen    uint32
	closed        chan struct{}
	statCaptured  int64
	mode          int32 // ProxyMode (atomic)
	mockMatcher   *replay.ScopedMockMatcher
	mockEndpoints map[string]bool // "IP:Port" -> true
	draining      int32           // atomic flag: 1 = graceful draining
}

// NewEgressProxy 创建出站代理，默认为 ModePassthrough 模式
func NewEgressProxy(natLookup NatLookupFunc, frameSink FrameSinkFunc) (*EgressProxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("绑定本地出站代理端口失败: %w", err)
	}

	p := &EgressProxy{
		listener:      ln,
		proxyPort:     uint16(ln.Addr().(*net.TCPAddr).Port),
		natLookup:     natLookup,
		frameSink:     frameSink,
		closed:        make(chan struct{}),
		mode:          int32(ModePassthrough),
		mockEndpoints: make(map[string]bool),
	}

	go p.acceptLoop()
	return p, nil
}

// ProxyPort 获取当前监听端口
func (p *EgressProxy) ProxyPort() uint16 {
	return p.proxyPort
}

// Mode 获取当前代理模式
func (p *EgressProxy) Mode() ProxyMode {
	return ProxyMode(atomic.LoadInt32(&p.mode))
}

// SetMode 设置当前代理模式
func (p *EgressProxy) SetMode(mode ProxyMode) {
	atomic.StoreInt32(&p.mode, int32(mode))
}

// SetMockMatcher 注入回放 Mock 匹配器及 Mock 下游端点清单 (ADR-09)
func (p *EgressProxy) SetMockMatcher(matcher *replay.ScopedMockMatcher, mockEndpoints []agentproto.DownstreamEndpoint) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.mockMatcher = matcher
	p.mockEndpoints = make(map[string]bool)
	for _, ep := range mockEndpoints {
		p.mockEndpoints[fmt.Sprintf("%s:%d", ep.IP, ep.Port)] = true
	}
}

// DrainAndSetPassthrough 会话停止时等待排空在途帧并平滑切回透传待命模式 (停录不断连)
func (p *EgressProxy) DrainAndSetPassthrough(drainDuration time.Duration) {
	if drainDuration <= 0 {
		drainDuration = 200 * time.Millisecond
	}
	time.Sleep(drainDuration)
	p.SetMode(ModePassthrough)
}

// isMockTarget 判断目标端点是否属于被 Mock 拦截的服务
func (p *EgressProxy) isMockTarget(ip net.IP, port uint16) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	key := fmt.Sprintf("%s:%d", ip.String(), port)
	if p.mockEndpoints[key] {
		return true
	}
	// 兼容 127.0.0.1 本地回环
	keyLocal := fmt.Sprintf("127.0.0.1:%d", port)
	return p.mockEndpoints[keyLocal]
}

// Close 强制关闭代理服务及现有连接
func (p *EgressProxy) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	select {
	case <-p.closed:
		return nil
	default:
		close(p.closed)
	}

	err := p.listener.Close()
	p.activeConns.Range(func(key, value interface{}) bool {
		if c, ok := key.(io.Closer); ok {
			_ = c.Close()
		}
		return true
	})
	return err
}

// GracefulDrain 优雅排空：关闭监听器禁止新连接，存量连接允许在超时内优雅自然退出
func (p *EgressProxy) GracefulDrain(timeout time.Duration) error {
	if !atomic.CompareAndSwapInt32(&p.draining, 0, 1) {
		return nil
	}

	p.mu.Lock()
	select {
	case <-p.closed:
		p.mu.Unlock()
		return nil
	default:
		close(p.closed)
	}
	err := p.listener.Close()
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		hasActive := false
		p.activeConns.Range(func(_, _ interface{}) bool {
			hasActive = true
			return false
		})

		if !hasActive {
			break
		}

		select {
		case <-ctx.Done():
			// 超时后强制释放
			p.activeConns.Range(func(key, _ interface{}) bool {
				if c, ok := key.(io.Closer); ok {
					_ = c.Close()
				}
				return true
			})
			return nil
		case <-ticker.C:
		}
	}

	return err
}

func (p *EgressProxy) acceptLoop() {
	for {
		clientConn, err := p.listener.Accept()
		if err != nil {
			select {
			case <-p.closed:
				return
			default:
				time.Sleep(10 * time.Millisecond)
				continue
			}
		}

		go p.handleConnection(clientConn)
	}
}

func (p *EgressProxy) handleConnection(clientConn net.Conn) {
	defer clientConn.Close()

	clientAddr, ok := clientConn.RemoteAddr().(*net.TCPAddr)
	if !ok {
		return
	}
	clientPort := uint16(clientAddr.Port)

	// 查 NAT 还原原始目的地址
	var (
		processID uint64
		pid       uint32
		origIP    net.IP
		origPort  uint16
	)

	if p.natLookup != nil {
		processID, pid, origIP, origPort, ok = p.natLookup(clientPort)
	}
	if !ok || origPort == 0 {
		return // 未在 NAT 表找到合法条目，静默关闭
	}

	connSeq := atomic.AddUint32(&p.connSeqGen, 1)

	// 检查当前模式是否为 REPLAY_MOCK
	if p.Mode() == ModeReplayMock && p.isMockTarget(origIP, origPort) {
		// 核心红线：回放 Mock 接管时，绝对禁止向真实 targetConn 发起任何 Dial！
		// 确保 0 字节外泄到真实网络，直接将请求导入用户态 MockMatcher 并执行头部自愈
		p.activeConns.Store(clientConn, true)
		defer p.activeConns.Delete(clientConn)

		p.handleMockConnection(clientConn, origIP, origPort)
		return
	}

	// PASSTHROUGH 或 CAPTURING 模式：连接真实下游服务
	targetAddr := net.JoinHostPort(origIP.String(), strconv.Itoa(int(origPort)))
	targetConn, err := net.DialTimeout("tcp", targetAddr, 3*time.Second)
	if err != nil {
		return
	}
	defer targetConn.Close()

	p.activeConns.Store(clientConn, true)
	p.activeConns.Store(targetConn, true)
	defer func() {
		p.activeConns.Delete(clientConn)
		p.activeConns.Delete(targetConn)
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	// 1. Client -> Target (出站请求)
	go func() {
		defer wg.Done()
		defer targetConn.Close()
		p.relayBidirectional(clientConn, targetConn, processID, pid, clientAddr.IP, origIP, clientPort, origPort, connSeq, agentproto.DirectionEgress)
	}()

	// 2. Target -> Client (出站应答)
	go func() {
		defer wg.Done()
		defer clientConn.Close()
		p.relayBidirectional(targetConn, clientConn, processID, pid, origIP, clientAddr.IP, origPort, clientPort, connSeq, agentproto.DirectionIngress)
	}()

	wg.Wait()
}

// relayBidirectional 双向转发与按需抓包循环 (停录不断连核心实现)
func (p *EgressProxy) relayBidirectional(
	src, dst net.Conn,
	processID uint64,
	pid uint32,
	srcIP, dstIP net.IP,
	srcPort, dstPort uint16,
	connSeq uint32,
	dir uint8,
) {
	bufPtr := proxyBufPool.Get().(*[]byte)
	defer proxyBufPool.Put(bufPtr)
	buf := *bufPtr

	var framer *crosrpc.StreamFramer

	for {
		select {
		case <-p.closed:
			return
		default:
		}

		n, err := src.Read(buf)
		if n > 0 {
			// 透明转发真实数据
			if _, wErr := dst.Write(buf[:n]); wErr != nil {
				return
			}

			// 若处于 CAPTURING 录制模式，按需解析并投递信封流
			curMode := p.Mode()
			if curMode == ModeCapturing {
				if framer == nil {
					framer = crosrpc.NewStreamFramer(crosrpc.DefaultMaxCaptureBytes)
				}
				frames, fErr := framer.Feed(buf[:n])
				if fErr == nil && len(frames) > 0 && p.frameSink != nil {
					nowNs := time.Now().UnixNano()
					for _, f := range frames {
						flags := uint8(0)
						if f.IsTruncated {
							flags |= agentproto.FlagIsTruncated
						}
						header := agentproto.EnvelopeHeader{
							Magic:        agentproto.EnvelopeMagic,
							ProtoVer:     agentproto.EnvelopeProtoVer,
							EventType:    agentproto.EventTrafficFrame,
							ProcessID:    processID,
							CapturedPID:  pid,
							Direction:    dir,
							Flags:        flags,
							CapturedAtNs: nowNs,
							SrcPort:      srcPort,
							DstPort:      dstPort,
							ConnSeq:      connSeq,
						}
						header.SetSrcIP(srcIP)
						header.SetDstIP(dstIP)

						env := agentproto.MarshalEnvelope(header, f.Data)
						p.frameSink(env)
						atomic.AddInt64(&p.statCaptured, 1)
					}
				}
			} else {
				// PASSTHROUGH 待命模式：释放 framer 缓存，零额外内存开销
				if framer != nil {
					framer = nil
				}
			}
		}

		if err != nil {
			return
		}
	}
}

// handleMockConnection 在回放 Mock 模式下接管客户端长连接 (真实后端 0 连接 0 字节外泄)
func (p *EgressProxy) handleMockConnection(clientConn net.Conn, origIP net.IP, origPort uint16) {
	bufPtr := proxyBufPool.Get().(*[]byte)
	defer proxyBufPool.Put(bufPtr)
	buf := *bufPtr

	framer := crosrpc.NewStreamFramer(crosrpc.DefaultMaxCaptureBytes)

	for {
		select {
		case <-p.closed:
			return
		default:
		}

		n, err := clientConn.Read(buf)
		if n > 0 {
			frames, fErr := framer.Feed(buf[:n])
			if fErr == nil && len(frames) > 0 {
				p.mu.RLock()
				matcher := p.mockMatcher
				p.mu.RUnlock()

				if matcher != nil {
					for _, f := range frames {
						matchRes, mErr := matcher.MatchAndHeal(f.Data)
						if mErr == nil && matchRes != nil && len(matchRes.ResponseFrames) > 0 {
							for _, rsp := range matchRes.ResponseFrames {
								if _, wErr := clientConn.Write(rsp); wErr != nil {
									return
								}
							}
						}
					}
				}
			}
		}

		if err != nil {
			return
		}
	}
}

// StatCaptured 获取累计捕获帧数
func (p *EgressProxy) StatCaptured() int64 {
	return atomic.LoadInt64(&p.statCaptured)
}
