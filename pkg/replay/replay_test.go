package replay

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"code-traefik/pkg/agentproto"
	"code-traefik/pkg/crosrpc"
)

// helper to build a CROS RPC request frame
func buildTestReqPacket(sno uint64, service, operation string, reqType uint8, traceBuff [16]byte, sendTimestamp uint64, payload []byte) []byte {
	// HeadLen for V05 = 256
	headLen := uint16(256)
	bodyLen := uint32(headLen) + uint32(len(payload))
	totalLen := 24 + bodyLen

	buf := make([]byte, totalLen)
	// CommHead (24 bytes)
	copy(buf[0:7], []byte("CSE.TCP"))
	buf[7] = 1 // TransportTCP
	binary.BigEndian.PutUint64(buf[8:16], sno)
	binary.BigEndian.PutUint32(buf[16:20], bodyLen)
	binary.BigEndian.PutUint16(buf[20:22], headLen)
	buf[22] = crosrpc.MsgTypeReq
	buf[23] = 1 // IsPacked

	// ReqHead starting at offset 24
	reqSession := buf[24 : 24+headLen]
	binary.BigEndian.PutUint16(reqSession[0:2], 5)       // MsgStructVer
	binary.BigEndian.PutUint16(reqSession[2:4], headLen) // ReqHeadLen
	binary.BigEndian.PutUint32(reqSession[4:8], 100)     // Dst2ndAddrId
	copy(reqSession[8:72], []byte(service))
	copy(reqSession[72:136], []byte(operation))
	reqSession[136] = reqType
	reqSession[137] = 1 // SubjectType
	binary.BigEndian.PutUint16(reqSession[138:140], 1)
	binary.BigEndian.PutUint32(reqSession[140:144], 42) // SessionId
	copy(reqSession[144:160], traceBuff[:])

	// V05 fields: send timestamp at 248..256
	binary.BigEndian.PutUint64(reqSession[248:256], sendTimestamp)

	// Payload
	copy(buf[24+headLen:], payload)
	return buf
}

// helper to build a CROS RPC response frame
func buildTestRspPacket(sno uint64, msgType uint8, headLen uint16, service, operation string, rspMsgNo uint16, payload []byte) []byte {
	bodyLen := uint32(headLen) + uint32(len(payload))
	totalLen := 24 + bodyLen
	buf := make([]byte, totalLen)

	// CommHead
	copy(buf[0:7], []byte("CSE.TCP"))
	buf[7] = 1 // TransportTCP
	binary.BigEndian.PutUint64(buf[8:16], sno)
	binary.BigEndian.PutUint32(buf[16:20], bodyLen)
	binary.BigEndian.PutUint16(buf[20:22], headLen)
	buf[22] = msgType
	buf[23] = 1

	// RspHead starting at offset 24
	rspSession := buf[24 : 24+headLen]
	binary.BigEndian.PutUint16(rspSession[0:2], 1)
	binary.BigEndian.PutUint16(rspSession[2:4], headLen)

	layout, err := crosrpc.LookupRspLayout(int(headLen))
	if err == nil {
		copy(rspSession[layout.ServiceNameOffset:layout.ServiceNameOffset+len(service)], []byte(service))
		copy(rspSession[layout.OperationOffset:layout.OperationOffset+len(operation)], []byte(operation))
		binary.BigEndian.PutUint16(rspSession[layout.RspMsgNoOffset:layout.RspMsgNoOffset+2], rspMsgNo)
	}

	copy(buf[24+headLen:], payload)
	return buf
}

// helper to build a CROS RPC MNG frame
func buildTestMngPacket(sno uint64, msgType uint8, procName string, payload []byte) []byte {
	headLen := uint16(88)
	bodyLen := uint32(headLen) + uint32(len(payload))
	totalLen := 24 + bodyLen
	buf := make([]byte, totalLen)

	copy(buf[0:7], []byte("CSE.TCP"))
	buf[7] = 1
	binary.BigEndian.PutUint64(buf[8:16], sno)
	binary.BigEndian.PutUint32(buf[16:20], bodyLen)
	binary.BigEndian.PutUint16(buf[20:22], headLen)
	buf[22] = msgType
	buf[23] = 1

	mngSession := buf[24 : 24+headLen]
	copy(mngSession[8:72], []byte(procName))
	copy(buf[24+headLen:], payload)
	return buf
}

// TestHealResponseFramesV1ToV4AndMultiSeg 校验表驱动响应头自愈覆写
func TestHealResponseFramesV1ToV4AndMultiSeg(t *testing.T) {
	liveSNO := uint64(0x000000010009A2B3)
	var liveTrace [16]byte
	if _, err := rand.Read(liveTrace[:]); err != nil {
		t.Fatalf("rand.Read failed: %v", err)
	}
	liveTimestamp := uint64(time.Now().UnixNano())

	liveReq := buildTestReqPacket(liveSNO, "com.demo.PaymentService", "PayOrder", crosrpc.ReqTypeSync, liveTrace, liveTimestamp, []byte("req_payload_data"))

	// 测试各种应答会话头长度: V01 (156B), V02 (164B), V03 (188B), V04 (200B)
	testHeadLens := []uint16{156, 164, 188, 200}
	for _, hl := range testHeadLens {
		recordedSNO := uint64(9999)
		oldPayload := []byte("original_response_payload_12345")
		mockRsp := buildTestRspPacket(recordedSNO, crosrpc.MsgTypeRsp, hl, "old.service", "oldOp", 0, oldPayload)

		healed, err := HealSingleResponseFrame(liveReq, mockRsp)
		if err != nil {
			t.Fatalf("HealSingleResponseFrame failed for headLen %d: %v", hl, err)
		}

		// 1. 验证 SNO 被覆写为 liveSNO
		gotSNO := binary.BigEndian.Uint64(healed[8:16])
		if gotSNO != liveSNO {
			t.Errorf("headLen %d: expected SNO %x, got %x", hl, liveSNO, gotSNO)
		}

		// 2. 验证 TraceBuff 被覆写
		layout, err := crosrpc.LookupRspLayout(int(hl))
		if err != nil {
			t.Fatalf("LookupRspLayout failed: %v", err)
		}
		rspSession := healed[24 : 24+hl]
		gotTrace := rspSession[layout.TraceBuffOffset : layout.TraceBuffOffset+layout.TraceBuffLen]
		if !bytes.Equal(gotTrace, liveTrace[:]) {
			t.Errorf("headLen %d: TraceBuff was not healed correctly", hl)
		}

		// 3. 验证 ClientSendTime (V03/V04)
		if layout.ClientSendTimeOffset > 0 {
			gotTime := binary.BigEndian.Uint64(rspSession[layout.ClientSendTimeOffset : layout.ClientSendTimeOffset+8])
			if gotTime != liveTimestamp {
				t.Errorf("headLen %d: ClientSendTime expected %d, got %d", hl, liveTimestamp, gotTime)
			}
		}

		// 4. 验证服务名与方法名
		gotSvc := string(bytes.TrimRight(rspSession[layout.ServiceNameOffset:layout.ServiceNameOffset+crosrpc.ServiceNameMaxLen], "\x00"))
		if gotSvc != "com.demo.PaymentService" {
			t.Errorf("headLen %d: serviceName expected com.demo.PaymentService, got %s", hl, gotSvc)
		}
		gotOp := string(bytes.TrimRight(rspSession[layout.OperationOffset:layout.OperationOffset+crosrpc.OperationMaxLen], "\x00"))
		if gotOp != "PayOrder" {
			t.Errorf("headLen %d: operation expected PayOrder, got %s", hl, gotOp)
		}

		// 5. 验证 Payload 分毫不差
		gotPayload := healed[24+hl:]
		if !bytes.Equal(gotPayload, oldPayload) {
			t.Errorf("headLen %d: payload altered during heal! expected %s, got %s", hl, oldPayload, gotPayload)
		}
	}

	// 测试 3 段连续 RSP_SEG + RSP 分片应答
	seg0 := buildTestRspPacket(111, crosrpc.MsgTypeRspSeg, 200, "old.svc", "oldOp", 0, []byte("part_0"))
	seg1 := buildTestRspPacket(111, crosrpc.MsgTypeRspSeg, 200, "old.svc", "oldOp", 1, []byte("part_1"))
	seg2 := buildTestRspPacket(111, crosrpc.MsgTypeRsp, 200, "old.svc", "oldOp", 2, []byte("part_2"))

	healedList, err := HealResponseFrames(liveReq, [][]byte{seg0, seg1, seg2})
	if err != nil {
		t.Fatalf("HealResponseFrames multi-seg failed: %v", err)
	}
	if len(healedList) != 3 {
		t.Fatalf("expected 3 healed segments, got %d", len(healedList))
	}
	for i, healedSeg := range healedList {
		gotSNO := binary.BigEndian.Uint64(healedSeg[8:16])
		if gotSNO != liveSNO {
			t.Errorf("seg %d: expected SNO %x, got %x", i, liveSNO, gotSNO)
		}
	}
}

// TestScopedMockMatcherExactAndFallback 测试分层局部优先与兜底匹配
func TestScopedMockMatcherExactAndFallback(t *testing.T) {
	matcher := NewScopedMockMatcher()

	// 预置录制素材
	mockPayload1 := []byte("payload_for_user_1")
	mockRsp1 := buildTestRspPacket(101, crosrpc.MsgTypeRsp, 200, "com.demo.UserService", "GetUser", 0, []byte("rsp_user_1"))

	mockPayload2 := []byte("payload_for_user_2")
	mockRsp2 := buildTestRspPacket(102, crosrpc.MsgTypeRsp, 200, "com.demo.UserService", "GetUser", 0, []byte("rsp_user_2"))

	activeRecordID := uint64(5001)

	// 录入专属局部池
	matcher.AddScopedMock(activeRecordID, "com.demo.UserService", "GetUser", mockPayload1, [][]byte{mockRsp1})
	matcher.AddScopedMock(activeRecordID, "com.demo.UserService", "GetUser", mockPayload2, [][]byte{mockRsp2})

	// 录入全局兜底池
	globalRsp := buildTestRspPacket(999, crosrpc.MsgTypeRsp, 200, "com.demo.ConfigService", "GetConfig", 0, []byte("global_config"))
	matcher.AddGlobalFallback("com.demo.ConfigService", "GetConfig", []byte("req_config"), [][]byte{globalRsp})

	// 录入 MNG 消息
	mngRsp := buildTestMngPacket(888, crosrpc.MsgTypeMng, "proc_node", []byte("mng_rsp"))
	matcher.AddMngMock(crosrpc.MsgTypeMng, mngRsp)

	matcher.SetActiveRecordID(activeRecordID)

	// 1. 实时请求 1: 全新随机 SNO, Trace, Timestamp, 但 Payload 相同 -> SCOPED_HIT 精确命中
	liveReq1 := buildTestReqPacket(0x1234, "com.demo.UserService", "GetUser", crosrpc.ReqTypeSync, [16]byte{1, 2, 3}, 1000000, mockPayload1)
	res1, err := matcher.MatchAndHeal(liveReq1)
	if err != nil {
		t.Fatalf("res1 MatchAndHeal failed: %v", err)
	}
	if res1.Result != MatchHitScoped {
		t.Errorf("res1: expected MatchHitScoped, got %v", res1.Result)
	}
	if len(res1.ResponseFrames) != 1 {
		t.Fatalf("res1: expected 1 response frame, got %d", len(res1.ResponseFrames))
	}
	if binary.BigEndian.Uint64(res1.ResponseFrames[0][8:16]) != 0x1234 {
		t.Errorf("res1: SNO not healed")
	}

	// 2. 实时请求 2: Payload 携带动态随机数导致 Hash 未命中 -> 触发同接口顺序队列兜底
	randomPayload := []byte("payload_with_random_token_99999")
	liveReq2 := buildTestReqPacket(0x5678, "com.demo.UserService", "GetUser", crosrpc.ReqTypeSync, [16]byte{4, 5, 6}, 2000000, randomPayload)
	res2, err := matcher.MatchAndHeal(liveReq2)
	if err != nil {
		t.Fatalf("res2 MatchAndHeal failed: %v", err)
	}
	if res2.Result != MatchHitScopedSeq && res2.Result != MatchHitScoped {
		t.Errorf("res2: expected scoped fallback hit, got %v", res2.Result)
	}
	if len(res2.ResponseFrames) != 1 {
		t.Fatalf("res2: expected 1 response frame, got %d", len(res2.ResponseFrames))
	}
	if binary.BigEndian.Uint64(res2.ResponseFrames[0][8:16]) != 0x5678 {
		t.Errorf("res2: SNO not healed")
	}

	// 3. 实时请求 3: 下游多发起第 3 次未录制的调用 -> 产生 MOCK_MISS, 响应切片为空 (静默等待超时)
	liveReq3 := buildTestReqPacket(0x9999, "com.demo.UserService", "GetUser", crosrpc.ReqTypeSync, [16]byte{7, 8, 9}, 3000000, []byte("third_call"))
	res3, err := matcher.MatchAndHeal(liveReq3)
	if err != nil {
		t.Fatalf("res3 MatchAndHeal error: %v", err)
	}
	if res3.Result != MatchMiss {
		t.Errorf("res3: expected MatchMiss, got %v", res3.Result)
	}
	if len(res3.ResponseFrames) != 0 {
		t.Errorf("res3: MatchMiss must return 0 frames (silent timeout), got %d frames", len(res3.ResponseFrames))
	}
	if matcher.MockMissCount() != 1 {
		t.Errorf("expected 1 mock miss count, got %d", matcher.MockMissCount())
	}

	// 4. 发送 Oneway (ucReqType=3) -> 仅记录, 不回包
	liveReqOneway := buildTestReqPacket(0xAAAA, "com.demo.NotifyService", "SendNotice", crosrpc.ReqTypeOneway, [16]byte{}, 4000000, []byte("notice"))
	resOneway, err := matcher.MatchAndHeal(liveReqOneway)
	if err != nil {
		t.Fatalf("resOneway MatchAndHeal error: %v", err)
	}
	if resOneway.Result != MatchOneway {
		t.Errorf("resOneway: expected MatchOneway, got %v", resOneway.Result)
	}
	if len(resOneway.ResponseFrames) != 0 {
		t.Errorf("resOneway: must return 0 frames, got %d", len(resOneway.ResponseFrames))
	}

	// 5. 发送 MNG 消息 -> 同类原样回放并覆写 SNO
	liveMng := buildTestMngPacket(0xBBBB, crosrpc.MsgTypeMng, "proc_node", []byte("mng_req"))
	resMng, err := matcher.MatchAndHeal(liveMng)
	if err != nil {
		t.Fatalf("resMng MatchAndHeal error: %v", err)
	}
	if resMng.Result != MatchHitMng {
		t.Errorf("resMng: expected MatchHitMng, got %v", resMng.Result)
	}
	if len(resMng.ResponseFrames) != 1 {
		t.Fatalf("resMng: expected 1 response frame, got %d", len(resMng.ResponseFrames))
	}
	if binary.BigEndian.Uint64(resMng.ResponseFrames[0][8:16]) != 0xBBBB {
		t.Errorf("resMng: SNO not healed, expected 0xBBBB, got %x", binary.BigEndian.Uint64(resMng.ResponseFrames[0][8:16]))
	}
}

// TestReplayExecutorEndToEnd 测试 ReplayExecutor 真实注入与响应收集
func TestReplayExecutorEndToEnd(t *testing.T) {
	// 启动模拟目标服务
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on 127.0.0.1: %v", err)
	}
	defer ln.Close()

	targetPort := uint16(ln.Addr().(*net.TCPAddr).Port)

	// 模拟目标服务应答逻辑
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				framer := crosrpc.NewStreamFramer(crosrpc.MaxPayloadLen)
				buf := make([]byte, 4096)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					frames, err := framer.Feed(buf[:n])
					if err != nil {
						continue
					}
					for _, f := range frames {
						if len(f.Data) >= 24 && f.Data[22] == crosrpc.MsgTypeReq {
							sno := binary.BigEndian.Uint64(f.Data[8:16])
							// 回写对应 SNO 的合法响应
							rsp := buildTestRspPacket(sno, crosrpc.MsgTypeRsp, 200, "com.demo.Test", "Op", 0, []byte("echo_ok"))
							_, _ = c.Write(rsp)
						}
					}
				}
			}(conn)
		}
	}()

	executor := NewReplayExecutor()
	reqPacket1 := buildTestReqPacket(1001, "com.demo.Test", "Op", crosrpc.ReqTypeSync, [16]byte{}, 0, []byte("req1"))
	reqPacket2 := buildTestReqPacket(1002, "com.demo.Test", "Op", crosrpc.ReqTypeSync, [16]byte{}, 0, []byte("req2"))

	execReq := agentproto.ExecuteReplayReq{
		TaskID: 9001,
		ProcessSpec: agentproto.ProcessMonitorSpec{
			ProcessID:  1,
			ListenPort: targetPort,
		},
		TimeoutMs:       2000,
		IngressRequests: [][]byte{reqPacket1, reqPacket2},
	}

	result, err := executor.Execute(context.Background(), execReq)
	if err != nil {
		t.Fatalf("executor.Execute failed: %v", err)
	}
	if result.Status != "PASS" {
		t.Errorf("expected status PASS, got %s, details: %s", result.Status, result.Details)
	}
	if result.TotalRequests != 2 || result.PassedRequests != 2 {
		t.Errorf("expected 2/2 passed, got %d/%d", result.PassedRequests, result.TotalRequests)
	}
}
