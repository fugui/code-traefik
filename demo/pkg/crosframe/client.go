package crosframe

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"code-traefik/pkg/crosrpc"
)

// Client 遵循 DOPHI CROS RPC 规范的 TCP 客户端
type Client struct {
	targetAddr string
	timeout    time.Duration
	snoSeq     uint64
	mu         sync.Mutex
	conn       net.Conn
	framer     *crosrpc.StreamFramer
}

// NewClient 实例化 CROS RPC 客户端
func NewClient(targetAddr string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Client{
		targetAddr: targetAddr,
		timeout:    timeout,
		snoSeq:     1000,
	}
}

// NextSNO 获取下一个自增 SNO
func (c *Client) NextSNO() uint64 {
	return atomic.AddUint64(&c.snoSeq, 1)
}

func (c *Client) getOrCreateConn() (net.Conn, error) {
	if c.conn != nil {
		return c.conn, nil
	}
	conn, err := net.DialTimeout("tcp", c.targetAddr, c.timeout)
	if err != nil {
		return nil, fmt.Errorf("crosframe.Client: connect %s failed: %w", c.targetAddr, err)
	}
	c.conn = conn
	c.framer = crosrpc.NewStreamFramer(4 * 1024 * 1024)
	return c.conn, nil
}

// Invoke 发起同步 CROS RPC 调用，写入请求帧并等待应答返回
func (c *Client) Invoke(ctx context.Context, serviceName, operation string, trace *crosrpc.TraceKeyExt, reqPayload []byte) (*crosrpc.ParsedPacket, []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	conn, err := c.getOrCreateConn()
	if err != nil {
		return nil, nil, err
	}

	sno := c.NextSNO()
	reqFrame := BuildReqPacket(sno, serviceName, operation, trace, reqPayload)

	deadline := time.Now().Add(c.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)

	if _, err := conn.Write(reqFrame); err != nil {
		_ = conn.Close()
		c.conn = nil
		return nil, nil, fmt.Errorf("crosframe.Client: write request failed: %w", err)
	}

	readBuf := make([]byte, 8192)
	for {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		default:
		}

		n, err := conn.Read(readBuf)
		if err != nil {
			_ = conn.Close()
			c.conn = nil
			if err == io.EOF {
				return nil, nil, fmt.Errorf("crosframe.Client: server closed connection")
			}
			return nil, nil, fmt.Errorf("crosframe.Client: read response failed: %w", err)
		}

		frames, err := c.framer.Feed(readBuf[:n])
		if err != nil {
			_ = conn.Close()
			c.conn = nil
			return nil, nil, fmt.Errorf("crosframe.Client: framer feed error: %w", err)
		}

		for _, frame := range frames {
			pkt, err := crosrpc.ParsePacket(frame.Data)
			if err != nil {
				continue
			}
			if pkt.Comm.MsgType == crosrpc.MsgTypeRsp || pkt.Comm.MsgType == crosrpc.MsgTypeRspSeg {
				if pkt.Comm.SNO == sno {
					var rspPayload []byte
					if pkt.PayloadOffset < len(frame.Data) {
						rspPayload = frame.Data[pkt.PayloadOffset:]
					}
					return pkt, rspPayload, nil
				}
			}
		}
	}
}

// Close 关闭底层连接
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}
