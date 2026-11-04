package crosrpc

import (
	"bytes"
	"time"
)

// Frame 表示分帧器切出的单条报文帧
type Frame struct {
	Data        []byte // 包含 24B 公共头在内的完整报文切片
	IsTruncated bool   // 是否因超过 MaxCaptureBytes 被截断
}

// StampedFrame 封装分帧器切出的单条报文帧及首字节到达时刻 (纳秒)
type StampedFrame struct {
	Frame
	CapturedAtNs int64
}

type chunkSpan struct {
	length int
	ns     int64
}

// StreamFramer 流式分帧与滑动重同步状态机
type StreamFramer struct {
	maxCaptureBytes  int
	buf              []byte
	spans            []chunkSpan
	dirtyBytes       uint64
	draining         bool
	drainingStartNs  int64
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

func (f *StreamFramer) discard(n int) {
	if n <= 0 {
		return
	}
	if n >= len(f.buf) {
		f.buf = f.buf[:0]
		f.spans = nil
		return
	}
	copy(f.buf, f.buf[n:])
	f.buf = f.buf[:len(f.buf)-n]

	rem := n
	for len(f.spans) > 0 && rem > 0 {
		if f.spans[0].length <= rem {
			rem -= f.spans[0].length
			f.spans = f.spans[1:]
		} else {
			f.spans[0].length -= rem
			rem = 0
		}
	}
}

func (f *StreamFramer) headNs(defaultNs int64) int64 {
	if len(f.spans) > 0 {
		return f.spans[0].ns
	}
	return defaultNs
}

var magicPrefixBytes = []byte(MagicPrefix)

// WriteAt 接收流式字节分片与到达纳秒时刻，并在定位到帧首字节时锁存到达定戳
func (f *StreamFramer) WriteAt(p []byte, arrivalNs int64) []StampedFrame {
	if len(p) > 0 {
		f.buf = append(f.buf, p...)
		f.spans = append(f.spans, chunkSpan{length: len(p), ns: arrivalNs})
	}

	var frames []StampedFrame

	for len(f.buf) > 0 || (f.draining && f.remainingToDrain == 0 && len(f.truncatedFrame) > 0) {
		// 1. 若当前处于超大帧流式排空模式
		if f.draining {
			if len(f.truncatedFrame) < f.maxCaptureBytes && len(f.buf) > 0 {
				needed := f.maxCaptureBytes - len(f.truncatedFrame)
				take := min(needed, len(f.buf))
				f.truncatedFrame = append(f.truncatedFrame, f.buf[:take]...)
				f.discard(take)
			}

			if len(f.truncatedFrame) == f.maxCaptureBytes && f.remainingToDrain > 0 && len(f.buf) > 0 {
				discardBytes := min(f.remainingToDrain, len(f.buf))
				f.discard(discardBytes)
				f.remainingToDrain -= discardBytes
			}

			if f.remainingToDrain == 0 && len(f.truncatedFrame) > 0 {
				frames = append(frames, StampedFrame{
					Frame: Frame{
						Data:        f.truncatedFrame,
						IsTruncated: true,
					},
					CapturedAtNs: f.drainingStartNs,
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
				discardCount := len(f.buf) - keep
				if discardCount > 0 {
					f.dirtyBytes += uint64(discardCount)
					f.discard(discardCount)
				}
				break
			}

			// 命中魔术字，丢弃前面所有脏字节并原地前移
			if idx > 0 {
				f.dirtyBytes += uint64(idx)
				f.discard(idx)
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
			f.discard(1)
		}

		// 3. 检查是否凑齐 24B 公共头
		if len(f.buf) < CommHeadLen {
			break
		}

		comm, err := ParseCommHead(f.buf[:CommHeadLen])
		if err != nil || ValidateCommHead(comm) != nil {
			// 头校验失败，丢弃首字节重新滑窗
			f.dirtyBytes++
			f.discard(1)
			continue
		}

		totalLen := CommHeadLen + int(comm.Length)

		// 4. 判断是否进入合法超大包截断排空模式
		if totalLen > f.maxCaptureBytes {
			f.draining = true
			f.drainingStartNs = f.headNs(arrivalNs)
			f.remainingToDrain = totalLen - f.maxCaptureBytes
			take := min(len(f.buf), f.maxCaptureBytes)
			f.truncatedFrame = make([]byte, take, f.maxCaptureBytes)
			copy(f.truncatedFrame, f.buf[:take])
			f.discard(take)
			continue
		}

		// 5. 正常帧半包等待
		if len(f.buf) < totalLen {
			break
		}

		// 6. 切出完整帧并原地紧缩释放
		frameStartNs := f.headNs(arrivalNs)
		frameBytes := make([]byte, totalLen)
		copy(frameBytes, f.buf[:totalLen])
		f.discard(totalLen)
		frames = append(frames, StampedFrame{
			Frame: Frame{
				Data:        frameBytes,
				IsTruncated: false,
			},
			CapturedAtNs: frameStartNs,
		})
	}

	return frames
}

// Write 接收输入切片并推进状态机 (向后兼容)
func (f *StreamFramer) Write(p []byte) ([]Frame, error) {
	stamped := f.WriteAt(p, time.Now().UnixNano())
	frames := make([]Frame, len(stamped))
	for i, sf := range stamped {
		frames[i] = sf.Frame
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
