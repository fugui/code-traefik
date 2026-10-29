package crosframe

import (
	"context"
	"net"
	"testing"
	"time"

	"code-traefik/pkg/crosrpc"
)

func TestBuildPacketValidation(t *testing.T) {
	trace := &crosrpc.TraceKeyExt{
		TraceID:      0x1122334455667788,
		SpanID:       0x0102030405060708,
		FatherSpanID: 0,
		RecordSwitch: 1,
	}
	payload := []byte("hello cros rpc payload")

	// 1. 测试请求包构建
	reqPkt := BuildReqPacket(2001, "demo.order-OrderService", "CreateOrder", trace, payload)
	parsedReq, err := crosrpc.ParsePacket(reqPkt)
	if err != nil {
		t.Fatalf("ParsePacket for Req failed: %v", err)
	}
	if parsedReq.Comm.SNO != 2001 {
		t.Fatalf("SNO expected 2001, got %d", parsedReq.Comm.SNO)
	}
	if parsedReq.Req == nil || parsedReq.Req.ServiceName != "demo.order-OrderService" || parsedReq.Req.Operation != "CreateOrder" {
		t.Fatalf("Req header mismatch: %+v", parsedReq.Req)
	}
	if parsedReq.Trace == nil || parsedReq.Trace.TraceID != trace.TraceID || parsedReq.Trace.SpanID != trace.SpanID {
		t.Fatalf("TraceKey header mismatch: %+v", parsedReq.Trace)
	}
	extractedReqPayload := reqPkt[parsedReq.PayloadOffset:]
	if string(extractedReqPayload) != string(payload) {
		t.Fatalf("Payload mismatch: expected %s, got %s", payload, extractedReqPayload)
	}

	// 2. 测试应答包构建
	rspPkt := BuildRspPacket(2001, "demo.order-OrderService", "CreateOrder", 0, trace, payload)
	parsedRsp, err := crosrpc.ParsePacket(rspPkt)
	if err != nil {
		t.Fatalf("ParsePacket for Rsp failed: %v", err)
	}
	if parsedRsp.Comm.SNO != 2001 {
		t.Fatalf("SNO expected 2001, got %d", parsedRsp.Comm.SNO)
	}
	if parsedRsp.Rsp == nil || parsedRsp.Rsp.ServiceName != "demo.order-OrderService" || parsedRsp.Rsp.StatusCode != 0 {
		t.Fatalf("Rsp header mismatch: %+v", parsedRsp.Rsp)
	}
	if parsedRsp.Trace == nil || parsedRsp.Trace.TraceID != trace.TraceID {
		t.Fatalf("TraceKey in Rsp mismatch: %+v", parsedRsp.Trace)
	}
	extractedRspPayload := rspPkt[parsedRsp.PayloadOffset:]
	if string(extractedRspPayload) != string(payload) {
		t.Fatalf("Payload mismatch: expected %s, got %s", payload, extractedRspPayload)
	}
}

func TestServerAndClientEndToEnd(t *testing.T) {
	// 动态申请本地空闲端口
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	srv := NewServer(addr, "TestEchoServer")
	srv.Register("demo.test-EchoService", "Echo", func(ctx context.Context, packet *crosrpc.ParsedPacket, reqPayload []byte) (uint32, []byte, error) {
		// 返回前缀 + reqPayload
		rsp := append([]byte("ECHO:"), reqPayload...)
		return 0, rsp, nil
	})

	if err := srv.Start(); err != nil {
		t.Fatalf("Server start failed: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	client := NewClient(addr, 2*time.Second)
	defer func() { _ = client.Close() }()

	trace := &crosrpc.TraceKeyExt{
		TraceID: 0x9999,
		SpanID:  0x1,
	}

	rspPkt, rspPayload, err := client.Invoke(context.Background(), "demo.test-EchoService", "Echo", trace, []byte("Ping"))
	if err != nil {
		t.Fatalf("Client.Invoke failed: %v", err)
	}

	if rspPkt.Rsp.StatusCode != 0 {
		t.Fatalf("Expected status code 0, got %d", rspPkt.Rsp.StatusCode)
	}
	if string(rspPayload) != "ECHO:Ping" {
		t.Fatalf("Expected ECHO:Ping, got %s", string(rspPayload))
	}
	if rspPkt.Trace == nil || rspPkt.Trace.TraceID != 0x9999 {
		t.Fatalf("Expected trace 0x9999, got %+v", rspPkt.Trace)
	}
}
