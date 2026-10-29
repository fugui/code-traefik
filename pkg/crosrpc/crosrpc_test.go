package crosrpc

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"testing"
)

// Helper to construct a valid CommHead byte slice (24 bytes)
func buildCommHeadBytes(msgType uint8, length uint32, headLen uint16) []byte {
	h := CommHead{
		TransportProto: TransportTCP,
		SNO:            1001,
		Length:         length,
		HeadLen:        headLen,
		MsgType:        msgType,
		IsPacked:       1,
	}
	copy(h.Magic[:], []byte(MagicPrefix))
	return h.Marshal()
}

func TestValidateCommHeadBoundaries(t *testing.T) {
	// 1. 合法头
	reqHead := CommHead{
		TransportProto: TransportTCP,
		SNO:            1,
		Length:         200,
		HeadLen:        160,
		MsgType:        MsgTypeReq,
		IsPacked:       1,
	}
	copy(reqHead.Magic[:], []byte(MagicPrefix))
	if err := ValidateCommHead(reqHead); err != nil {
		t.Fatalf("预期合法 REQ 头校验通过，实际失败: %v", err)
	}

	rspHead := CommHead{
		TransportProto: TransportDefault,
		SNO:            2,
		Length:         156,
		HeadLen:        156,
		MsgType:        MsgTypeRsp,
		IsPacked:       1,
	}
	copy(rspHead.Magic[:], []byte(MagicPrefix))
	if err := ValidateCommHead(rspHead); err != nil {
		t.Fatalf("预期合法 RSP 头校验通过，实际失败: %v", err)
	}

	mngHead := CommHead{
		TransportProto: TransportTCP,
		SNO:            3,
		Length:         88,
		HeadLen:        88,
		MsgType:        MsgTypeMng,
		IsPacked:       1,
	}
	copy(mngHead.Magic[:], []byte(MagicPrefix))
	if err := ValidateCommHead(mngHead); err != nil {
		t.Fatalf("预期合法 MNG 头校验通过，实际失败: %v", err)
	}

	// 2. 非法魔术字
	badMagic := reqHead
	copy(badMagic.Magic[:], []byte("BAD.TCP"))
	if err := ValidateCommHead(badMagic); err != ErrInvalidMagic {
		t.Fatalf("预期 ErrInvalidMagic，实际: %v", err)
	}

	// 3. 非法传输协议 (> 1)
	badTransport := reqHead
	badTransport.TransportProto = 2
	if err := ValidateCommHead(badTransport); err != ErrInvalidTransport {
		t.Fatalf("预期 ErrInvalidTransport，实际: %v", err)
	}

	// 4. 非法消息类型
	badMsgLow := reqHead
	badMsgLow.MsgType = 1
	if err := ValidateCommHead(badMsgLow); err != ErrInvalidMsgType {
		t.Fatalf("预期 ErrInvalidMsgType (1)，实际: %v", err)
	}
	badMsgHigh := reqHead
	badMsgHigh.MsgType = 15
	if err := ValidateCommHead(badMsgHigh); err != ErrInvalidMsgType {
		t.Fatalf("预期 ErrInvalidMsgType (15)，实际: %v", err)
	}

	// 5. 最小会话头下界检查
	badReqLen := reqHead
	badReqLen.HeadLen = 159
	if err := ValidateCommHead(badReqLen); err != ErrHeadLenTooSmall {
		t.Fatalf("预期 REQ ErrHeadLenTooSmall，实际: %v", err)
	}

	badRspLen := rspHead
	badRspLen.HeadLen = 155
	if err := ValidateCommHead(badRspLen); err != ErrHeadLenTooSmall {
		t.Fatalf("预期 RSP ErrHeadLenTooSmall，实际: %v", err)
	}

	badRspSegLen := rspHead
	badRspSegLen.MsgType = MsgTypeRspSeg
	badRspSegLen.HeadLen = 155
	if err := ValidateCommHead(badRspSegLen); err != ErrHeadLenTooSmall {
		t.Fatalf("预期 RSP_SEG ErrHeadLenTooSmall，实际: %v", err)
	}

	badMngLen := mngHead
	badMngLen.HeadLen = 87
	if err := ValidateCommHead(badMngLen); err != ErrHeadLenTooSmall {
		t.Fatalf("预期 MNG ErrHeadLenTooSmall，实际: %v", err)
	}

	// 6. usHeadLen > uiLength
	badHeadGtLen := reqHead
	badHeadGtLen.HeadLen = 200
	badHeadGtLen.Length = 199
	if err := ValidateCommHead(badHeadGtLen); err != ErrHeadLenGreaterThanLength {
		t.Fatalf("预期 ErrHeadLenGreaterThanLength，实际: %v", err)
	}

	// 7. uiLength 超出最大载荷防溢出
	badOversize := reqHead
	badOversize.HeadLen = 160
	badOversize.Length = MaxPayloadLen + 160 + 1
	if err := ValidateCommHead(badOversize); err != ErrLengthTooLarge {
		t.Fatalf("预期 ErrLengthTooLarge，实际: %v", err)
	}
}

func buildValidReqPacket(service, op string, payload []byte) []byte {
	sessionHead := make([]byte, 160)
	binary.BigEndian.PutUint16(sessionHead[0:2], 1)
	binary.BigEndian.PutUint16(sessionHead[2:4], 160)
	copy(sessionHead[8:72], []byte(service))
	copy(sessionHead[72:136], []byte(op))
	sessionHead[136] = ReqTypeSync

	comm := buildCommHeadBytes(MsgTypeReq, uint32(160+len(payload)), 160)
	var pkt []byte
	pkt = append(pkt, comm...)
	pkt = append(pkt, sessionHead...)
	pkt = append(pkt, payload...)
	return pkt
}

func TestStreamFramerDirtyResyncFragmentAndOversize(t *testing.T) {
	// 场景 1: 头部前含 17B 随机脏字节
	framer1 := NewStreamFramer(4 * 1024 * 1024)
	dirty := make([]byte, 17)
	_, _ = rand.Read(dirty)
	// 确保脏字节不碰巧包含魔术字
	copy(dirty[:7], []byte("BADDIRT"))

	validPkt1 := buildValidReqPacket("com.test-Service1", "Method1", []byte("hello"))
	input1 := append(dirty, validPkt1...)

	frames1, err := framer1.Write(input1)
	if err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	if len(frames1) != 1 {
		t.Fatalf("期望切出 1 帧，实际得到 %d 帧", len(frames1))
	}
	if framer1.DirtyBytes() != 17 {
		t.Fatalf("期望脏字节数为 17，实际得到: %d", framer1.DirtyBytes())
	}
	if !bytes.Equal(frames1[0].Data, validPkt1) {
		t.Fatalf("切出的帧内容与原始报文不一致")
	}

	// 场景 2: 每次 Write 1 字节的极端碎片流
	framer2 := NewStreamFramer(4 * 1024 * 1024)
	validPkt2 := buildValidReqPacket("com.test-Service2", "Method2", []byte("fragment-payload"))
	var frames2 []Frame
	for i := 0; i < len(validPkt2); i++ {
		out, err := framer2.Write(validPkt2[i : i+1])
		if err != nil {
			t.Fatalf("碎片写入失败: %v", err)
		}
		frames2 = append(frames2, out...)
	}
	if len(frames2) != 1 {
		t.Fatalf("期望凑齐后切出 1 帧，实际得到 %d 帧", len(frames2))
	}
	if !bytes.Equal(frames2[0].Data, validPkt2) {
		t.Fatalf("碎片凑齐后帧内容与原始报文不匹配")
	}

	// 场景 3: 3 个完整帧拼接的粘包流
	framer3 := NewStreamFramer(4 * 1024 * 1024)
	p1 := buildValidReqPacket("com.test-SvcA", "OpA", []byte("111"))
	p2 := buildValidReqPacket("com.test-SvcB", "OpB", []byte("222"))
	p3 := buildValidReqPacket("com.test-SvcC", "OpC", []byte("333"))
	coalesced := append(append(p1, p2...), p3...)

	frames3, err := framer3.Write(coalesced)
	if err != nil {
		t.Fatalf("粘包写入失败: %v", err)
	}
	if len(frames3) != 3 {
		t.Fatalf("期望一次切出 3 帧，实际得到 %d 帧", len(frames3))
	}
	if !bytes.Equal(frames3[0].Data, p1) || !bytes.Equal(frames3[1].Data, p2) || !bytes.Equal(frames3[2].Data, p3) {
		t.Fatalf("粘包切分后的帧内容与原始报文不一致")
	}

	// 场景 4: uiLength = 6MB > MaxCaptureBytes(4MB)，后紧随 1 条正常帧
	const maxCap = 4 * 1024 * 1024
	framer4 := NewStreamFramer(maxCap)

	// 构造超大帧 (24B CommHead + 160B ReqHead + 6MB Payload)
	const bigPayloadLen = 6 * 1024 * 1024
	bigComm := buildCommHeadBytes(MsgTypeReq, uint32(160+bigPayloadLen), 160)
	bigReqSession := make([]byte, 160)
	binary.BigEndian.PutUint16(bigReqSession[0:2], 1)
	binary.BigEndian.PutUint16(bigReqSession[2:4], 160)
	copy(bigReqSession[8:72], []byte("com.test-BigSvc"))
	copy(bigReqSession[72:136], []byte("BigOp"))

	bigPayload := make([]byte, bigPayloadLen)
	// 填充特征值
	for i := range bigPayload {
		bigPayload[i] = byte(i % 251)
	}

	normalPkt := buildValidReqPacket("com.test-NormalSvc", "NormalOp", []byte("tail-normal-packet"))

	// 组合数据流
	var stream4 []byte
	stream4 = append(stream4, bigComm...)
	stream4 = append(stream4, bigReqSession...)
	stream4 = append(stream4, bigPayload...)
	stream4 = append(stream4, normalPkt...)

	// 分块写入模拟真实网络
	chunkSize := 1024 * 1024
	var frames4 []Frame
	for offset := 0; offset < len(stream4); offset += chunkSize {
		end := offset + chunkSize
		if end > len(stream4) {
			end = len(stream4)
		}
		out, err := framer4.Write(stream4[offset:end])
		if err != nil {
			t.Fatalf("写入超大流分块失败: %v", err)
		}
		frames4 = append(frames4, out...)
	}

	if len(frames4) != 2 {
		t.Fatalf("期望得到 2 帧 (1 截断帧 + 1 正常帧)，实际得到 %d 帧", len(frames4))
	}

	// 断言 1: 超大帧保留前 4MB 并标记 IsTruncated = true
	if !frames4[0].IsTruncated {
		t.Fatalf("超大帧未被标记为 IsTruncated")
	}
	if len(frames4[0].Data) != maxCap {
		t.Fatalf("超大帧长度期望为 %d，实际得到 %d", maxCap, len(frames4[0].Data))
	}
	expectedBigPrefix := append(bigComm, bigReqSession...)
	expectedBigPrefix = append(expectedBigPrefix, bigPayload[:maxCap-24-160]...)
	if !bytes.Equal(frames4[0].Data, expectedBigPrefix) {
		t.Fatalf("超大帧前 4MB 数据与预期前缀不一致")
	}

	// 断言 2: 尾部 2MB 排空后紧随的正常帧在偏移 0 完美对齐切出 (SC-04)
	if frames4[1].IsTruncated {
		t.Fatalf("紧随的正常帧被错误标记为截断")
	}
	if !bytes.Equal(frames4[1].Data, normalPkt) {
		t.Fatalf("紧随正常帧内容与原始报文不匹配")
	}
}

func TestParseReqRspMngAndExtendHeads(t *testing.T) {
	// 1. 测试 V01(160B) 与 V05(256B) Req 头
	v01Req := make([]byte, 160)
	binary.BigEndian.PutUint16(v01Req[0:2], 1)
	binary.BigEndian.PutUint16(v01Req[2:4], 160)
	copy(v01Req[8:72], []byte("pkg.sub-OrderService"))
	copy(v01Req[72:136], []byte("CreateOrder"))
	v01Req[136] = ReqTypeSync
	copy(v01Req[144:160], []byte("1234567890abcdef"))

	pkt01 := append(buildCommHeadBytes(MsgTypeReq, 160, 160), v01Req...)
	p01, err := ParsePacket(pkt01)
	if err != nil {
		t.Fatalf("ParsePacket V01 Req 失败: %v", err)
	}
	if p01.Req == nil || p01.Req.ServiceName != "pkg.sub-OrderService" || p01.Req.Operation != "CreateOrder" {
		t.Fatalf("V01 Req 解析字段不符: %+v", p01.Req)
	}

	// V05 Req (256B)
	v05Req := make([]byte, 256)
	binary.BigEndian.PutUint16(v05Req[0:2], 5)
	binary.BigEndian.PutUint16(v05Req[2:4], 256)
	copy(v05Req[8:72], []byte("pkg.sub-OrderService"))
	copy(v05Req[72:136], []byte("CreateOrder"))
	binary.BigEndian.PutUint32(v05Req[244:248], 5000)
	binary.BigEndian.PutUint64(v05Req[248:256], 1700000000123)

	pkt05 := append(buildCommHeadBytes(MsgTypeReq, 256, 256), v05Req...)
	p05, err := ParsePacket(pkt05)
	if err != nil {
		t.Fatalf("ParsePacket V05 Req 失败: %v", err)
	}
	if p05.Req.SendTimeStamp == nil || *p05.Req.SendTimeStamp != 1700000000123 {
		t.Fatalf("V05 Req SendTimeStamp 解析错误: %+v", p05.Req)
	}

	// 2. 表驱动应答头测试 V01(156B), V02(164B), V03(188B), V04(200B)
	for _, tc := range []struct {
		headLen   int
		ver       string
		hasSendT  bool
		is64BitSt bool
	}{
		{156, "V01", false, false},
		{164, "V02", false, false},
		{188, "V03", true, false},
		{200, "V04", true, true},
	} {
		layout, err := LookupRspLayout(tc.headLen)
		if err != nil {
			t.Fatalf("LookupRspLayout %d 失败: %v", tc.headLen, err)
		}
		rawRsp := make([]byte, tc.headLen)
		binary.BigEndian.PutUint16(rawRsp[0:2], 1)
		binary.BigEndian.PutUint16(rawRsp[2:4], uint16(tc.headLen))
		copy(rawRsp[layout.ServiceNameOffset:layout.ServiceNameOffset+64], []byte("pkg.sub-OrderService"))
		copy(rawRsp[layout.OperationOffset:layout.OperationOffset+64], []byte("CreateOrder"))
		binary.BigEndian.PutUint16(rawRsp[layout.RspMsgNoOffset:layout.RspMsgNoOffset+2], 7)

		if tc.hasSendT {
			binary.BigEndian.PutUint64(rawRsp[layout.ClientSendTimeOffset:layout.ClientSendTimeOffset+8], 99887766)
		}
		if tc.is64BitSt {
			binary.BigEndian.PutUint64(rawRsp[layout.StatusCode64Offset:layout.StatusCode64Offset+8], 18446744073709551610)
		} else {
			binary.BigEndian.PutUint32(rawRsp[4:8], 200)
		}

		comm := buildCommHeadBytes(MsgTypeRsp, uint32(tc.headLen), uint16(tc.headLen))
		parsed, err := ParsePacket(append(comm, rawRsp...))
		if err != nil {
			t.Fatalf("解析 Rsp %s 失败: %v", tc.ver, err)
		}
		if parsed.Rsp.ServiceName != "pkg.sub-OrderService" || parsed.Rsp.Operation != "CreateOrder" || parsed.Rsp.RspMsgNo != 7 {
			t.Fatalf("Rsp %s 字段解析不匹配: %+v", tc.ver, parsed.Rsp)
		}
		if tc.is64BitSt && parsed.Rsp.StatusCode != 18446744073709551610 {
			t.Fatalf("Rsp V04 64位状态码解析不匹配: %d", parsed.Rsp.StatusCode)
		}
		if !tc.is64BitSt && parsed.Rsp.StatusCode != 200 {
			t.Fatalf("Rsp %s 32位状态码解析不匹配: %d", tc.ver, parsed.Rsp.StatusCode)
		}
	}

	// 非标 Rsp 长度返回 ErrUnknownRspLayout
	if _, err := LookupRspLayout(170); err != ErrUnknownRspLayout {
		t.Fatalf("非标长度 170 期望 ErrUnknownRspLayout，实际得到: %v", err)
	}

	// 3. 管理消息头 MngHead (88B)
	rawMng := make([]byte, 88)
	binary.BigEndian.PutUint16(rawMng[0:2], 10) // role
	binary.BigEndian.PutUint16(rawMng[2:4], 20) // type
	binary.BigEndian.PutUint32(rawMng[4:8], 1)  // rspResult = true
	copy(rawMng[8:72], []byte("ProcWorker-1"))
	binary.BigEndian.PutUint32(rawMng[72:76], 0) // procRet = 0
	binary.BigEndian.PutUint32(rawMng[76:80], 1) // svrAuthorize = true
	rawMng[80] = 1                               // encryption
	rawMng[81] = 2                               // integrity
	rawMng[82] = 3                               // cipherSuite

	commMng := buildCommHeadBytes(MsgTypeMng, 88, 88)
	parsedMng, err := ParsePacket(append(commMng, rawMng...))
	if err != nil {
		t.Fatalf("ParsePacket Mng 失败: %v", err)
	}
	if parsedMng.Mng.ProcName != "ProcWorker-1" || !parsedMng.Mng.RspResult || !parsedMng.Mng.SvrAuthorize {
		t.Fatalf("Mng 字段不匹配: %+v", parsedMng.Mng)
	}

	// 4. 扩展头 TRACE_KEY 与 TIME_STAMP
	// 构造 160B Req 会话头 + 36B TraceKey 扩展头 (4B comm + 32B data) + 12B TimeStamp 扩展头 (4B comm + 8B data)
	extTrace := make([]byte, 36)
	binary.BigEndian.PutUint16(extTrace[0:2], 32)
	extTrace[2] = ExtTypeTraceKey
	binary.BigEndian.PutUint64(extTrace[4:12], 10001)  // TraceID
	binary.BigEndian.PutUint64(extTrace[12:20], 20002) // SpanID
	binary.BigEndian.PutUint64(extTrace[20:28], 30003) // FatherSpanID
	extTrace[28] = 1                                   // RecordSwitch

	extTime := make([]byte, 12)
	binary.BigEndian.PutUint16(extTime[0:2], 8)
	extTime[2] = ExtTypeTimeStamp
	binary.BigEndian.PutUint64(extTime[4:12], 17111222333) // ServerEnqueueTime

	headLenWithExt := uint16(160 + len(extTrace) + len(extTime))
	commWithExt := buildCommHeadBytes(MsgTypeReq, uint32(headLenWithExt), headLenWithExt)

	var fullPkt []byte
	fullPkt = append(fullPkt, commWithExt...)
	fullPkt = append(fullPkt, v01Req...)
	fullPkt = append(fullPkt, extTrace...)
	fullPkt = append(fullPkt, extTime...)

	parsedExt, err := ParsePacket(fullPkt)
	if err != nil {
		t.Fatalf("带扩展头的报文解析失败: %v", err)
	}
	if parsedExt.Trace == nil || parsedExt.Trace.TraceID != 10001 || parsedExt.Trace.SpanID != 20002 || parsedExt.Trace.FatherSpanID != 30003 || parsedExt.Trace.RecordSwitch != 1 {
		t.Fatalf("TraceKey 扩展头解析不匹配: %+v", parsedExt.Trace)
	}
	if parsedExt.ServerEnqueue == nil || *parsedExt.ServerEnqueue != 17111222333 {
		t.Fatalf("TimeStamp 扩展头解析不匹配: %+v", parsedExt.ServerEnqueue)
	}
	if len(parsedExt.ExtendHeads) != 2 {
		t.Fatalf("期望解析到 2 段扩展头，实际得到: %d", len(parsedExt.ExtendHeads))
	}
}

func buildRspFrame(msgType uint8, msgNo uint16, payload []byte) []byte {
	rawRsp := make([]byte, 156)
	binary.BigEndian.PutUint16(rawRsp[0:2], 1)
	binary.BigEndian.PutUint16(rawRsp[2:4], 156)
	copy(rawRsp[24:88], []byte("pkg-ServiceMultiSeg"))
	copy(rawRsp[88:152], []byte("StreamOp"))
	binary.BigEndian.PutUint16(rawRsp[152:154], msgNo)

	comm := buildCommHeadBytes(msgType, uint32(156+len(payload)), 156)
	var pkt []byte
	pkt = append(pkt, comm...)
	pkt = append(pkt, rawRsp...)
	pkt = append(pkt, payload...)
	return pkt
}

func TestSplitFramesMultiSegRsp(t *testing.T) {
	// 2 段 RSP_SEG (ucMsgType=4) 与 1 段终结 RSP (ucMsgType=3)
	seg1 := buildRspFrame(MsgTypeRspSeg, 1, []byte("chunk-1"))
	seg2 := buildRspFrame(MsgTypeRspSeg, 2, []byte("chunk-22"))
	seg3 := buildRspFrame(MsgTypeRsp, 3, []byte("chunk-333"))

	concat := append(append(seg1, seg2...), seg3...)

	slices, err := SplitFrames(concat)
	if err != nil {
		t.Fatalf("SplitFrames 失败: %v", err)
	}
	if len(slices) != 3 {
		t.Fatalf("期望切出 3 段帧，实际得到 %d 段", len(slices))
	}

	for i, frameBytes := range slices {
		pkt, err := ParsePacket(frameBytes)
		if err != nil {
			t.Fatalf("解析切片帧 %d 失败: %v", i, err)
		}
		expectedMsgNo := uint16(i + 1)
		if pkt.Rsp.RspMsgNo != expectedMsgNo {
			t.Fatalf("帧 %d msgNo 期望 %d，实际得到 %d", i, expectedMsgNo, pkt.Rsp.RspMsgNo)
		}
		expectedLen := CommHeadLen + int(pkt.Comm.Length)
		if len(frameBytes) != expectedLen {
			t.Fatalf("帧 %d 长度期望 %d，实际得到 %d", i, expectedLen, len(frameBytes))
		}
	}
}
