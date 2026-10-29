package crosframe

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"

	"code-traefik/pkg/crosrpc"
)

// HandlerFunc 处理接收到的 CROS RPC 请求并返回业务状态码与应答载荷
type HandlerFunc func(ctx context.Context, packet *crosrpc.ParsedPacket, reqPayload []byte) (statusCode uint32, rspPayload []byte, err error)

// Server 基于 DOPHI CROS RPC 规范的 TCP 业务服务容器
type Server struct {
	addr      string
	listener  net.Listener
	handlers  map[string]map[string]HandlerFunc
	mu        sync.RWMutex
	running   int32
	ctx       context.Context
	cancel    context.CancelFunc
	activeWg  sync.WaitGroup
	LogPrefix string
}

// NewServer 实例化 CROS RPC 服务端
func NewServer(addr string, logPrefix string) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		addr:      addr,
		handlers:  make(map[string]map[string]HandlerFunc),
		ctx:       ctx,
		cancel:    cancel,
		LogPrefix: logPrefix,
	}
}

// Register 注册业务 Handler，绑定 (serviceName, operation)
func (s *Server) Register(serviceName, operation string, h HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.handlers[serviceName]; !ok {
		s.handlers[serviceName] = make(map[string]HandlerFunc)
	}
	s.handlers[serviceName][operation] = h
}

// Start 启动 TCP 监听并异步分发处理连接
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("crosframe.Server: listen %s failed: %w", s.addr, err)
	}
	s.listener = ln
	atomic.StoreInt32(&s.running, 1)

	s.activeWg.Add(1)
	go s.acceptLoop()

	if s.LogPrefix != "" {
		log.Printf("[%s] CROS RPC TCP Server started on %s", s.LogPrefix, s.addr)
	}
	return nil
}

// Addr 返回监听地址
func (s *Server) Addr() net.Addr {
	if s.listener != nil {
		return s.listener.Addr()
	}
	return nil
}

func (s *Server) acceptLoop() {
	defer s.activeWg.Done()
	for atomic.LoadInt32(&s.running) == 1 {
		conn, err := s.listener.Accept()
		if err != nil {
			if atomic.LoadInt32(&s.running) == 0 {
				return
			}
			log.Printf("[%s] accept error: %v", s.LogPrefix, err)
			continue
		}

		s.activeWg.Add(1)
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer s.activeWg.Done()
	defer conn.Close()

	framer := crosrpc.NewStreamFramer(4 * 1024 * 1024)
	readBuf := make([]byte, 8192)

	for atomic.LoadInt32(&s.running) == 1 {
		n, err := conn.Read(readBuf)
		if err != nil {
			if err != io.EOF && atomic.LoadInt32(&s.running) == 1 {
				// 客户端断开连接或网络故障
			}
			return
		}

		frames, err := framer.Feed(readBuf[:n])
		if err != nil {
			log.Printf("[%s] framer feed error: %v", s.LogPrefix, err)
			return
		}

		for _, frame := range frames {
			s.dispatchFrame(conn, frame.Data)
		}
	}
}

func (s *Server) dispatchFrame(conn net.Conn, frameData []byte) {
	packet, err := crosrpc.ParsePacket(frameData)
	if err != nil {
		log.Printf("[%s] ParsePacket failed: %v", s.LogPrefix, err)
		return
	}

	if packet.Comm.MsgType != crosrpc.MsgTypeReq || packet.Req == nil {
		// 仅响应 REQ 报文
		return
	}

	serviceName := packet.Req.ServiceName
	operation := packet.Req.Operation

	s.mu.RLock()
	var handler HandlerFunc
	if ops, ok := s.handlers[serviceName]; ok {
		handler = ops[operation]
	}
	s.mu.RUnlock()

	var reqPayload []byte
	if packet.PayloadOffset < len(frameData) {
		reqPayload = frameData[packet.PayloadOffset:]
	}

	var statusCode uint32
	var rspPayload []byte
	if handler == nil {
		statusCode = 404 // 接口未找到
	} else {
		code, out, err := handler(s.ctx, packet, reqPayload)
		if err != nil {
			log.Printf("[%s] handler error for (%s, %s): %v", s.LogPrefix, serviceName, operation, err)
			statusCode = 500
		} else {
			statusCode = code
			rspPayload = out
		}
	}

	// 拼装 RSP 报文帧回传
	rspFrame := BuildRspPacket(packet.Comm.SNO, serviceName, operation, statusCode, packet.Trace, rspPayload)
	if _, err := conn.Write(rspFrame); err != nil {
		log.Printf("[%s] conn.Write response error: %v", s.LogPrefix, err)
	}
}

// Stop 停止监听与连接处理
func (s *Server) Stop() error {
	if !atomic.CompareAndSwapInt32(&s.running, 1, 0) {
		return nil
	}
	s.cancel()
	if s.listener != nil {
		_ = s.listener.Close()
	}
	s.activeWg.Wait()
	return nil
}
