package ebpf

import (
	"bytes"
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
	mu              sync.RWMutex
	listener        net.Listener
	proxyPort       uint16
	natLookup       NatLookupFunc
	frameSink       FrameSinkFunc
	activeConns     sync.Map // net.Conn -> bool
	connSeqGen      uint32
	closed          chan struct{}
	statCaptured    int64
	mode            int32 // ProxyMode (atomic)
	activeSessionID atomic.Uint64
	mockMatcher     *replay.ScopedMockMatcher
	mockEndpoints   map[string]bool // "IP:Port" -> true
	draining        int32           // atomic flag: 1 = graceful draining
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
	if mode == ModeCapturing && p.activeSessionID.Load() == 0 {
		p.activeSessionID.Store(1)
	}
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

// StartCapture 开启录制并原子注入 sessionID (F-09, ADR-11)
func (p *EgressProxy) StartCapture(sessionID uint64) {
	p.activeSessionID.Store(sessionID)
	p.SetMode(ModeCapturing)
}

// ActiveSessionID 获取当前录制会话 ID
func (p *EgressProxy) ActiveSessionID() uint64 {
	return p.activeSessionID.Load()
}

// DrainAndSetPassthrough 会话停止时等待排空在途帧并平滑切回透传待命模式 (停录不断连)
func (p *EgressProxy) DrainAndSetPassthrough(drainDuration time.Duration) {
	if drainDuration <= 0 {
		drainDuration = 200 * time.Millisecond
	}
	time.Sleep(drainDuration)
	p.SetMode(ModePassthrough)
	p.activeSessionID.Store(0)
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
		p.relayBidirectional(clientConn, targetConn, processID, pid, clientAddr.IP, origIP, clientPort, origPort, connSeq, true)
	}()

	// 2. Target -> Client (出站应答)
	go func() {
		defer wg.Done()
		p.relayBidirectional(targetConn, clientConn, processID, pid, clientAddr.IP, origIP, clientPort, origPort, connSeq, false)
		if p.Mode() != ModeReplayMock {
			_ = clientConn.Close()
		}
	}()

	wg.Wait()
}

// relayBidirectional 双向转发与按需抓包循环 (停录不断连与写前魔术字守卫核心实现)
func (p *EgressProxy) relayBidirectional(
	src, dst net.Conn,
	processID uint64,
	pid uint32,
	clientIP, origIP net.IP,
	clientPort, origPort uint16,
	connSeq uint32,
	isClientToTarget bool,
) {
	bufPtr := proxyBufPool.Get().(*[]byte)
	defer proxyBufPool.Put(bufPtr)
	buf := *bufPtr

	var framer *crosrpc.StreamFramer
	magicPrefixBytes := []byte(crosrpc.MagicPrefix)

	for {
		select {
		case <-p.closed:
			return
		default:
		}

		n, err := src.Read(buf)
		if n > 0 {
			curMode := p.Mode()

			// 核心防御：REPLAY_MOCK 模式防外泄与自愈处理
			if curMode == ModeReplayMock {
				isMockTarget := p.isMockTarget(origIP, origPort)
				hasCrosMagic := isClientToTarget && bytes.Contains(buf[:n], magicPrefixBytes)

				if isMockTarget || hasCrosMagic {
					if !isClientToTarget {
						// 真实下游发来的数据，在 Mock 接管时直接丢弃并切断 targetConn
						_ = src.Close()
						return
					}

					// 物理切断向真实下游的连接，严守 0 字节外泄
					_ = dst.Close()

					p.mu.RLock()
					matcher := p.mockMatcher
					p.mu.RUnlock()

					if matcher != nil {
						if framer == nil {
							framer = crosrpc.NewStreamFramer(crosrpc.DefaultMaxCaptureBytes)
						}
						origAddr := net.JoinHostPort(origIP.String(), strconv.Itoa(int(origPort)))
						frames, fErr := framer.Feed(buf[:n])
						if fErr == nil && len(frames) > 0 {
							for _, f := range frames {
								matchRes, mErr := matcher.MatchAndHealWithDst(f.Data, origAddr)
								if mErr == nil && matchRes != nil && len(matchRes.ResponseFrames) > 0 {
									for _, rsp := range matchRes.ResponseFrames {
										if _, wErr := src.Write(rsp); wErr != nil {
											return
										}
									}
								}
							}
						}
					}
					// 已接管出站请求，切勿向 targetConn 写入，继续在 clientConn 循环中响应后续请求
					continue
				}
			}

			// 透明转发真实数据
			if _, wErr := dst.Write(buf[:n]); wErr != nil {
				return
			}

			// 若处于 CAPTURING 录制模式且已注入有效会话，按需解析并投递信封流
			if curMode == ModeCapturing {
				sessID := p.activeSessionID.Load()
				if sessID > 0 {
					if framer == nil {
						framer = crosrpc.NewStreamFramer(crosrpc.DefaultMaxCaptureBytes)
					}
					arrivalNs := time.Now().UnixNano()
					frames := framer.WriteAt(buf[:n], arrivalNs)
					if len(frames) > 0 && p.frameSink != nil {
						for _, f := range frames {
							flags := uint8(0)
							if f.IsTruncated {
								flags |= agentproto.FlagIsTruncated
							}
							header := agentproto.EnvelopeHeader{
								Magic:           agentproto.EnvelopeMagic,
								ProtoVer:        agentproto.EnvelopeProtoVer,
								EventType:       agentproto.EventTrafficFrame,
								SessionOrTaskID: sessID,
								ProcessID:       processID,
								CapturedPID:     pid,
								Direction:       agentproto.DirectionEgress, // 恒定为 DirectionEgress (2)
								Flags:           flags,
								CapturedAtNs:    f.CapturedAtNs,
								SrcPort:         clientPort,
								DstPort:         origPort,
								ConnSeq:         connSeq,
							}
							header.SetSrcIP(clientIP)
							header.SetDstIP(origIP)

							env := agentproto.MarshalEnvelope(header, f.Data)
							p.frameSink(env)
							atomic.AddInt64(&p.statCaptured, 1)
						}
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
					origAddr := net.JoinHostPort(origIP.String(), strconv.Itoa(int(origPort)))
					for _, f := range frames {
						matchRes, mErr := matcher.MatchAndHealWithDst(f.Data, origAddr)
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
