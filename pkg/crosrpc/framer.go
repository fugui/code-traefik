package crosrpc

import (
	"bytes"
)

// Frame 表示分帧器切出的单条报文帧
type Frame struct {
	Data        []byte // 包含 24B 公共头在内的完整报文切片
	IsTruncated bool   // 是否因超过 MaxCaptureBytes 被截断
}

// StreamFramer 流式分帧与滑动重同步状态机
type StreamFramer struct {
	maxCaptureBytes  int
	buf              []byte
	dirtyBytes       uint64
	draining         bool
	truncatedFrame   []byte
	remainingToDrain int
}

// NewStreamFramer 实例化分帧状态机，传入单帧最大落盘捕获阈值
func NewStreamFramer(maxCaptureBytes int) *StreamFramer {
	if maxCaptureBytes <= 0 {
		maxCaptureBytes = DefaultMaxCaptureBytes
	}
	return &StreamFramer{
		maxCaptureBytes: maxCaptureBytes,
		buf:             make([]byte, 0, 8192),
	}
}

// DirtyBytes 返回累计检出并跳过的脏字节数
func (f *StreamFramer) DirtyBytes() uint64 {
	return f.dirtyBytes
}

// Feed 接收流式字节输入并返回切出的帧列表 (Write 的别名)
func (f *StreamFramer) Feed(p []byte) ([]Frame, error) {
	return f.Write(p)
}

var magicPrefixBytes = []byte(MagicPrefix)

// Write 接收输入切片并推进状态机
func (f *StreamFramer) Write(p []byte) ([]Frame, error) {
	if len(p) > 0 {
		f.buf = append(f.buf, p...)
	}

	var frames []Frame

	for len(f.buf) > 0 || (f.draining && f.remainingToDrain == 0 && len(f.truncatedFrame) > 0) {
		// 1. 若当前处于超大帧流式排空模式
		if f.draining {
			if len(f.truncatedFrame) < f.maxCaptureBytes && len(f.buf) > 0 {
				needed := f.maxCaptureBytes - len(f.truncatedFrame)
				take := min(needed, len(f.buf))
				f.truncatedFrame = append(f.truncatedFrame, f.buf[:take]...)
				copy(f.buf, f.buf[take:])
				f.buf = f.buf[:len(f.buf)-take]
			}

			if len(f.truncatedFrame) == f.maxCaptureBytes && f.remainingToDrain > 0 && len(f.buf) > 0 {
				discard := min(f.remainingToDrain, len(f.buf))
				copy(f.buf, f.buf[discard:])
				f.buf = f.buf[:len(f.buf)-discard]
				f.remainingToDrain -= discard
			}

			if f.remainingToDrain == 0 && len(f.truncatedFrame) > 0 {
				frames = append(frames, Frame{
					Data:        f.truncatedFrame,
					IsTruncated: true,
				})
				f.draining = false
				f.truncatedFrame = nil
				continue
			}

			// 缓冲已耗尽，等待后续 TCP 分片继续排空
			break
		}

		// 2. SIMD 魔术字滑窗与原地紧缩重同步扫描
		for len(f.buf) > 0 {
			idx := bytes.Index(f.buf, magicPrefixBytes)
			if idx < 0 {
				// 未命中魔术字：保留尾部至多 6 字节（可能为跨分片魔术字前缀）并原地紧缩
				keep := min(len(f.buf), len(magicPrefixBytes)-1)
				discard := len(f.buf) - keep
				if discard > 0 {
					f.dirtyBytes += uint64(discard)
					copy(f.buf[:keep], f.buf[discard:])
					f.buf = f.buf[:keep]
				}
				break
			}

			// 命中魔术字，丢弃前面所有脏字节并原地前移
			if idx > 0 {
				f.dirtyBytes += uint64(idx)
				copy(f.buf, f.buf[idx:])
				f.buf = f.buf[:len(f.buf)-idx]
			}

			// 检查是否凑齐 ucProtocol (第 8 字节)
			if len(f.buf) < 8 {
				break
			}

			if f.buf[7] <= 1 {
				// 协议版本合法，魔术字对齐成功
				break
			}

			// 假阳性魔术字 (ucProtocol > 1)，跳过首字节继续查找
			f.dirtyBytes++
			copy(f.buf, f.buf[1:])
			f.buf = f.buf[:len(f.buf)-1]
		}

		// 3. 检查是否凑齐 24B 公共头
		if len(f.buf) < CommHeadLen {
			break
		}

		comm, err := ParseCommHead(f.buf[:CommHeadLen])
		if err != nil || ValidateCommHead(comm) != nil {
			// 头校验失败，丢弃首字节重新滑窗
			f.dirtyBytes++
			copy(f.buf, f.buf[1:])
			f.buf = f.buf[:len(f.buf)-1]
			continue
		}

		totalLen := CommHeadLen + int(comm.Length)

		// 4. 判断是否进入合法超大包截断排空模式
		if totalLen > f.maxCaptureBytes {
			f.draining = true
			f.remainingToDrain = totalLen - f.maxCaptureBytes
			take := min(len(f.buf), f.maxCaptureBytes)
			f.truncatedFrame = make([]byte, take, f.maxCaptureBytes)
			copy(f.truncatedFrame, f.buf[:take])
			copy(f.buf, f.buf[take:])
			f.buf = f.buf[:len(f.buf)-take]
			continue
		}

		// 5. 正常帧半包等待
		if len(f.buf) < totalLen {
			break
		}

		// 6. 切出完整帧并原地紧缩释放
		frameBytes := make([]byte, totalLen)
		copy(frameBytes, f.buf[:totalLen])
		copy(f.buf, f.buf[totalLen:])
		f.buf = f.buf[:len(f.buf)-totalLen]
		frames = append(frames, Frame{
			Data:        frameBytes,
			IsTruncated: false,
		})
	}

	return frames, nil
}

// SplitFrames 按 CROS RPC 24B 公共头无损切分连续拼接的多段帧切片
func SplitFrames(concatenated []byte) ([][]byte, error) {
	if len(concatenated) == 0 {
		return nil, nil
	}

	var frames [][]byte
	remaining := concatenated

	for len(remaining) > 0 {
		if len(remaining) < CommHeadLen {
			return nil, ErrBufferTooShort
		}

		comm, err := ParseCommHead(remaining[:CommHeadLen])
		if err != nil {
			return nil, err
		}
		if err := ValidateCommHead(comm); err != nil {
			return nil, err
		}

		totalLen := CommHeadLen + int(comm.Length)
		if len(remaining) < totalLen {
			return nil, ErrBufferTooShort
		}

		frames = append(frames, remaining[:totalLen])
		remaining = remaining[totalLen:]
	}

	return frames, nil
}
