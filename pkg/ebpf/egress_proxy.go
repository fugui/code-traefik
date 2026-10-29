package ebpf

import (
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"code-traefik/pkg/agentproto"
	"code-traefik/pkg/crosrpc"
)

// NatLookupFunc 查找客户端临时端口对应的原始目的地
type NatLookupFunc func(clientPort uint16) (processID uint64, pid uint32, origIP net.IP, origPort uint16, ok bool)

// FrameSinkFunc 接收代理采集到的出站 CROS RPC 信封帧
type FrameSinkFunc func(envelopeBytes []byte)

// EgressProxy 透明 TCP 出站代理
type EgressProxy struct {
	mu           sync.Mutex
	listener     net.Listener
	proxyPort    uint16
	natLookup    NatLookupFunc
	frameSink    FrameSinkFunc
	activeConns  sync.Map
	connSeqGen   uint32
	closed       chan struct{}
	statCaptured int64
}

// NewEgressProxy 创建出站代理
func NewEgressProxy(natLookup NatLookupFunc, frameSink FrameSinkFunc) (*EgressProxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("绑定本地出站代理端口失败: %w", err)
	}

	p := &EgressProxy{
		listener:  ln,
		proxyPort: uint16(ln.Addr().(*net.TCPAddr).Port),
		natLookup: natLookup,
		frameSink: frameSink,
		closed:    make(chan struct{}),
	}

	go p.acceptLoop()
	return p, nil
}

// ProxyPort 获取当前监听端口
func (p *EgressProxy) ProxyPort() uint16 {
	return p.proxyPort
}

// Close 关闭代理服务
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
		if c, ok := value.(io.Closer); ok {
			_ = c.Close()
		}
		return true
	})
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

	// 连接真实下游服务 (此发起连接的进程是 Agent 本身，受 agent_pid 自环豁免保护)
	targetAddr := net.JoinHostPort(origIP.String(), strconv.Itoa(int(origPort)))
	targetConn, err := net.DialTimeout("tcp", targetAddr, 3*time.Second)
	if err != nil {
		return
	}
	defer targetConn.Close()

	p.activeConns.Store(clientConn, true)
	defer p.activeConns.Delete(clientConn)

	var wg sync.WaitGroup
	wg.Add(2)

	// 1. Client -> Target (出站请求 EGRESS REQ)
	go func() {
		defer wg.Done()
		p.relayAndCapture(clientConn, targetConn, processID, pid, clientAddr.IP, origIP, clientPort, origPort, connSeq, agentproto.DirectionEgress)
	}()

	// 2. Target -> Client (出站应答 EGRESS RSP)
	go func() {
		defer wg.Done()
		p.relayAndCapture(targetConn, clientConn, processID, pid, origIP, clientAddr.IP, origPort, clientPort, connSeq, agentproto.DirectionIngress)
	}()

	wg.Wait()
}

func (p *EgressProxy) relayAndCapture(
	src, dst net.Conn,
	processID uint64,
	pid uint32,
	srcIP, dstIP net.IP,
	srcPort, dstPort uint16,
	connSeq uint32,
	dir uint8,
) {
	framer := crosrpc.NewStreamFramer(crosrpc.DefaultMaxCaptureBytes)
	buf := make([]byte, 32768)

	for {
		select {
		case <-p.closed:
			return
		default:
		}

		n, err := src.Read(buf)
		if n > 0 {
			// 透明转发
			if _, wErr := dst.Write(buf[:n]); wErr != nil {
				return
			}

			// 分帧采集
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
