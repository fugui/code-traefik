package replay

import (
	"encoding/binary"
	"errors"

	"code-traefik/pkg/crosrpc"
)

var (
	ErrFrameTooShort = errors.New("crosrpc frame length below minimum header")
)

// HealSingleResponseFrame 基于当前实时下游请求帧 liveReq，对单帧录制应答帧 mockRsp 执行表驱动原地头部自愈覆写
func HealSingleResponseFrame(liveReq []byte, mockRsp []byte) ([]byte, error) {
	if len(liveReq) < crosrpc.CommHeadLen || len(mockRsp) < crosrpc.CommHeadLen {
		return nil, ErrFrameTooShort
	}
	healed := make([]byte, len(mockRsp))
	copy(healed, mockRsp)

	// 1. 覆写公共头偏移 8..15 的 8B 大端请求序列号 ullSNO
	copy(healed[8:16], liveReq[8:16])

	// 读取双方会话头实际长度
	// 偏移 20..21: CommHead.HeadLen
	reqSessionLen := int(binary.BigEndian.Uint16(liveReq[20:22]))
	rspSessionLen := int(binary.BigEndian.Uint16(healed[20:22]))
	if reqSessionLen == 0 && len(liveReq) >= 28 {
		reqSessionLen = int(binary.BigEndian.Uint16(liveReq[26:28]))
	}
	if rspSessionLen == 0 && len(healed) >= 28 {
		rspSessionLen = int(binary.BigEndian.Uint16(healed[26:28]))
	}

	if reqSessionLen > len(liveReq)-crosrpc.CommHeadLen {
		reqSessionLen = len(liveReq) - crosrpc.CommHeadLen
	}
	if rspSessionLen > len(healed)-crosrpc.CommHeadLen {
		rspSessionLen = len(healed) - crosrpc.CommHeadLen
	}

	reqSession := liveReq[crosrpc.CommHeadLen : crosrpc.CommHeadLen+reqSessionLen]
	rspSession := healed[crosrpc.CommHeadLen : crosrpc.CommHeadLen+rspSessionLen]

	// 2. 依据 usRspHeadLen 检索权威字段偏移布局表 (V1~V4)
	layout, err := crosrpc.LookupRspLayout(rspSessionLen)
	if err != nil {
		return nil, err
	}

	// 3. 覆写 Trace 缓冲回显: RPC_RSP_HEAD_S[layout.TraceBuffOffset] <- RPC_REQ_HEAD_S[144..160] (aucTraceBuff[16])
	if reqSessionLen >= 160 && layout.TraceBuffOffset+layout.TraceBuffLen <= rspSessionLen {
		copy(rspSession[layout.TraceBuffOffset:layout.TraceBuffOffset+layout.TraceBuffLen], reqSession[144:160])
	}

	// 4. 覆写客户端发送时间戳回显 (V3+ 应答头):
	//    若实时请求为 V5+ (reqSessionLen >= 256)，回填 RPC_REQ_HEAD_S[248..256] 的 ullSendTimeStamp；
	//    若实时请求低于 V5 无发送时间戳，则清零该 8B
	if layout.ClientSendTimeOffset > 0 && layout.ClientSendTimeOffset+8 <= rspSessionLen {
		if reqSessionLen >= 256 {
			copy(rspSession[layout.ClientSendTimeOffset:layout.ClientSendTimeOffset+8], reqSession[248:256])
		} else {
			clear(rspSession[layout.ClientSendTimeOffset : layout.ClientSendTimeOffset+8])
		}
	}

	// 5. 校验并同步服务名与方法名回显
	if reqSessionLen >= 136 && layout.ServiceNameOffset+crosrpc.ServiceNameMaxLen <= rspSessionLen && layout.OperationOffset+crosrpc.OperationMaxLen <= rspSessionLen {
		copy(rspSession[layout.ServiceNameOffset:layout.ServiceNameOffset+crosrpc.ServiceNameMaxLen], reqSession[8:72])
		copy(rspSession[layout.OperationOffset:layout.OperationOffset+crosrpc.OperationMaxLen], reqSession[72:136])
	}

	return healed, nil
}

// HealResponseFrames 针对多段分片应答 (RSP_SEG) 连续切片自愈保障
func HealResponseFrames(liveReq []byte, mockRsps [][]byte) ([][]byte, error) {
	result := make([][]byte, len(mockRsps))
	for i, seg := range mockRsps {
		healed, err := HealSingleResponseFrame(liveReq, seg)
		if err != nil {
			return nil, err
		}
		result[i] = healed
	}
	return result, nil
}
