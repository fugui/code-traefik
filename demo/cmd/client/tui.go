package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"code-traefik/demo/pkg/crosframe"
	"code-traefik/demo/pkg/proto/orderpb"
	"code-traefik/pkg/crosrpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// ANSI 颜色定义
const (
	colorReset   = "\033[0m"
	colorBold    = "\033[1m"
	colorRed     = "\033[31m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"
	colorGray    = "\033[90m"
)

type TUIClient struct {
	targetAddr string
	client     *crosframe.Client
	traceSeq   uint64
	lastOrder  string
}

func NewTUIClient(targetAddr string) *TUIClient {
	return &TUIClient{
		targetAddr: targetAddr,
		client:     crosframe.NewClient(targetAddr, 4*time.Second),
		traceSeq:   uint64(time.Now().Unix()),
	}
}

func (t *TUIClient) nextTrace() *crosrpc.TraceKeyExt {
	t.traceSeq++
	return &crosrpc.TraceKeyExt{
		TraceID:      t.traceSeq,
		SpanID:       0x0001,
		FatherSpanID: 0,
		RecordSwitch: 1,
	}
}

func (t *TUIClient) clearScreen() {
	fmt.Print("\033[H\033[2J")
}

func (t *TUIClient) drawBanner(lastLatency time.Duration, isConnected bool) {
	statusStr := colorGreen + "● CONNECTED" + colorReset
	if !isConnected {
		statusStr = colorRed + "○ DISCONNECTED" + colorReset
	}

	latStr := colorGray + "RTT: - ms" + colorReset
	if lastLatency > 0 {
		latStr = fmt.Sprintf("%sRTT: %.2f ms%s", colorYellow, float64(lastLatency.Microseconds())/1000.0, colorReset)
	}

	fmt.Printf("%s┌────────────────────────────────────────────────────────────────────────────────────────┐%s\n", colorCyan, colorReset)
	fmt.Printf("%s│%s %sCROS RPC Benchmark Client [TUI]%s  Target: %s  %s  %s %s│%s\n",
		colorCyan, colorReset, colorBold, colorReset, t.targetAddr, statusStr, latStr, colorCyan, colorReset)
	fmt.Printf("%s├────────────────────────────────────────────────────────────────────────────────────────┤%s\n", colorCyan, colorReset)
}

func (t *TUIClient) drawMenu() {
	fmt.Println(colorBold + " [快捷业务指令 (Quick Actions)]" + colorReset)
	fmt.Printf("   %s[1]%s 查询买家资产与商品行情     (Query Composite Quote: Alice + SKU-101)\n", colorGreen, colorReset)
	fmt.Printf("   %s[2]%s 正向下单 (Happy Path)        (Alice 购买 1件机械键盘 ￥299.00)\n", colorGreen, colorReset)
	fmt.Printf("   %s[3]%s 购买触发库存不足 (Out of Stock) (Alice 申请购买 99件显示器 ￥1999.00)\n", colorYellow, colorReset)
	fmt.Printf("   %s[4]%s 购买触发余额不足并回滚补偿  (Bob 尝试购买 1件机械键盘 ￥299.00)\n", colorMagenta, colorReset)
	fmt.Printf("   %s[5]%s 查询上一笔订单履约状态      (Query Order Detail)\n", colorCyan, colorReset)
	fmt.Printf("   %s[6]%s 取消上一笔订单并全额退款    (Cancel & Refund Order)\n", colorRed, colorReset)
	fmt.Printf("   %s[7]%s 自定义参数提交订单          (Custom User, SKU & Quantity)\n", colorBlue, colorReset)
	fmt.Printf("   %s[8]%s 并发压力录制流量注入 (Burst) (连续发送 10 笔并发请求生成录制数据)\n", colorYellow, colorReset)
	fmt.Printf("   %s[q]%s 退出客户端                  (Quit)\n", colorGray, colorReset)
	fmt.Printf("%s──────────────────────────────────────────────────────────────────────────────────────────%s\n", colorCyan, colorReset)
}

func (t *TUIClient) inspectPacket(pkt *crosrpc.ParsedPacket, rawPayload []byte, latency time.Duration) {
	fmt.Println(colorBold + " [CROS RPC 协议帧与 Trace 解剖台 (Protocol Inspector)]" + colorReset)
	if pkt == nil {
		fmt.Println("  (暂无响应帧)")
		return
	}

	traceIDStr := "0x0"
	spanIDStr := "0x0"
	if pkt.Trace != nil {
		traceIDStr = fmt.Sprintf("0x%X", pkt.Trace.TraceID)
		spanIDStr = fmt.Sprintf("0x%X", pkt.Trace.SpanID)
	}

	stCodeColor := colorGreen
	if pkt.Rsp != nil && pkt.Rsp.StatusCode != 0 {
		stCodeColor = colorRed
	}

	fmt.Printf("  • %s公共消息头 (24B)%s: Magic=%s%s%s SNO=%s#%d%s Length=%d HeadLen=%d MsgType=%d IsPacked=%d\n",
		colorCyan, colorReset,
		colorYellow, string(pkt.Comm.Magic[:]), colorReset,
		colorBold, pkt.Comm.SNO, colorReset,
		pkt.Comm.Length, pkt.Comm.HeadLen, pkt.Comm.MsgType, pkt.Comm.IsPacked,
	)

	if pkt.Rsp != nil {
		fmt.Printf("  • %s应答会话头 (V01)%s: Service=%s%s%s Method=%s%s%s StatusCode=%s%d%s\n",
			colorCyan, colorReset,
			colorBold, pkt.Rsp.ServiceName, colorReset,
			colorBold, pkt.Rsp.Operation, colorReset,
			stCodeColor, pkt.Rsp.StatusCode, colorReset,
		)
	}

	fmt.Printf("  • %s调用链 Trace%s   : TraceID=%s%s%s SpanID=%s Latency=%s%.2f ms%s\n",
		colorCyan, colorReset,
		colorYellow, traceIDStr, colorReset, spanIDStr,
		colorYellow, float64(latency.Microseconds())/1000.0, colorReset,
	)

	fmt.Printf("  • %s载荷 (Payload %d 字节)%s:\n", colorCyan, len(rawPayload), colorReset)

	// 十六进制 Dump (前 32 字节)
	hexLen := len(rawPayload)
	if hexLen > 32 {
		hexLen = 32
	}
	if hexLen > 0 {
		fmt.Printf("    %sHEX:  %s%s\n", colorGray, hex.EncodeToString(rawPayload[:hexLen]), colorReset)
	}
}

func (t *TUIClient) Run() {
	scanner := bufio.NewScanner(os.Stdin)
	var lastLatency time.Duration
	var lastPkt *crosrpc.ParsedPacket
	var lastPayload []byte
	var lastMsg string

	for {
		t.clearScreen()
		t.drawBanner(lastLatency, true)
		t.drawMenu()

		if lastMsg != "" {
			fmt.Println(colorBold + " [最新业务处理反馈 (Business Feedback)]" + colorReset)
			fmt.Println(lastMsg)
			fmt.Printf("%s──────────────────────────────────────────────────────────────────────────────────────────%s\n", colorCyan, colorReset)
		}

		if lastPkt != nil {
			t.inspectPacket(lastPkt, lastPayload, lastLatency)
			fmt.Printf("%s──────────────────────────────────────────────────────────────────────────────────────────%s\n", colorCyan, colorReset)
		}

		fmt.Print(colorBold + " 请输入指令序号 [1-8 / q]: " + colorReset)
		if !scanner.Scan() {
			break
		}
		cmd := strings.TrimSpace(scanner.Text())

		if cmd == "q" || cmd == "quit" || cmd == "exit" {
			fmt.Println("\n退出 CROS RPC 测试客户端。")
			break
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		start := time.Now()

		switch cmd {
		case "1":
			// 查询综合行情
			reqBytes, _ := proto.Marshal(&orderpb.CompositeQuoteReq{UserId: "Alice", SkuId: "SKU-101"})
			pkt, rspBytes, err := t.client.Invoke(ctx, "demo.order-OrderService", "GetCompositeQuote", t.nextTrace(), reqBytes)
			lastLatency = time.Since(start)
			lastPkt = pkt
			lastPayload = rspBytes
			if err != nil {
				lastMsg = colorRed + "RPC 调用异常: " + err.Error() + colorReset
			} else {
				var rsp orderpb.CompositeQuoteRsp
				_ = proto.Unmarshal(rspBytes, &rsp)
				jsonStr, _ := protojson.Marshal(&rsp)
				lastMsg = fmt.Sprintf("  %s✓ 查询成功%s:\n  买家: %s  可用余额: ￥%.2f\n  商品: %s (%s)  单价: ￥%.2f  可售库存: %d 件\n  %sJSON:%s %s",
					colorGreen, colorReset, rsp.UserId, float64(rsp.BalanceCents)/100.0,
					rsp.SkuName, rsp.SkuId, float64(rsp.UnitPriceCents)/100.0, rsp.AvailableStock,
					colorGray, colorReset, string(jsonStr))
			}

		case "2":
			// 正向下单
			reqBytes, _ := proto.Marshal(&orderpb.CreateOrderReq{UserId: "Alice", SkuId: "SKU-101", Quantity: 1})
			pkt, rspBytes, err := t.client.Invoke(ctx, "demo.order-OrderService", "CreateOrder", t.nextTrace(), reqBytes)
			lastLatency = time.Since(start)
			lastPkt = pkt
			lastPayload = rspBytes
			if err != nil {
				lastMsg = colorRed + "RPC 调用异常: " + err.Error() + colorReset
			} else {
				var rsp orderpb.CreateOrderRsp
				_ = proto.Unmarshal(rspBytes, &rsp)
				if rsp.Code == 0 {
					t.lastOrder = rsp.OrderId
					lastMsg = fmt.Sprintf("  %s✓ 下单成功 (Happy Path)%s:\n  订单号: %s\n  支付总额: ￥%.2f  状态: %s%s%s  消息: %s",
						colorGreen, colorReset, rsp.OrderId, float64(rsp.TotalAmountCents)/100.0,
						colorGreen, rsp.Status, colorReset, rsp.Message)
				} else {
					lastMsg = fmt.Sprintf("  %s✗ 下单失败%s (Code %d): %s (状态: %s)", colorRed, colorReset, rsp.Code, rsp.Message, rsp.Status)
				}
			}

		case "3":
			// 购买超额库存
			reqBytes, _ := proto.Marshal(&orderpb.CreateOrderReq{UserId: "Alice", SkuId: "SKU-102", Quantity: 99})
			pkt, rspBytes, err := t.client.Invoke(ctx, "demo.order-OrderService", "CreateOrder", t.nextTrace(), reqBytes)
			lastLatency = time.Since(start)
			lastPkt = pkt
			lastPayload = rspBytes
			if err != nil {
				lastMsg = colorRed + "RPC 调用异常: " + err.Error() + colorReset
			} else {
				var rsp orderpb.CreateOrderRsp
				_ = proto.Unmarshal(rspBytes, &rsp)
				lastMsg = fmt.Sprintf("  %s✗ 下单拦截 (预期库存不足)%s:\n  状态: %s%s%s (Code: %d)\n  反馈详情: %s",
					colorYellow, colorReset, colorYellow, rsp.Status, colorReset, rsp.Code, rsp.Message)
			}

		case "4":
			// 购买触发余额不足并回滚
			reqBytes, _ := proto.Marshal(&orderpb.CreateOrderReq{UserId: "Bob", SkuId: "SKU-101", Quantity: 1})
			pkt, rspBytes, err := t.client.Invoke(ctx, "demo.order-OrderService", "CreateOrder", t.nextTrace(), reqBytes)
			lastLatency = time.Since(start)
			lastPkt = pkt
			lastPayload = rspBytes
			if err != nil {
				lastMsg = colorRed + "RPC 调用异常: " + err.Error() + colorReset
			} else {
				var rsp orderpb.CreateOrderRsp
				_ = proto.Unmarshal(rspBytes, &rsp)
				lastMsg = fmt.Sprintf("  %s✗ 下单失败并回滚补偿 (预期余额不足)%s:\n  状态: %s%s%s (Code: %d)\n  反馈详情: %s",
					colorMagenta, colorReset, colorMagenta, rsp.Status, colorReset, rsp.Code, rsp.Message)
			}

		case "5":
			// 查询上一笔订单
			if t.lastOrder == "" {
				lastMsg = colorYellow + "当前暂无可查询的历史订单，请先执行 [2] 正向下单！" + colorReset
				lastPkt = nil
			} else {
				reqBytes, _ := proto.Marshal(&orderpb.OrderDetailReq{OrderId: t.lastOrder})
				pkt, rspBytes, err := t.client.Invoke(ctx, "demo.order-OrderService", "GetOrderDetail", t.nextTrace(), reqBytes)
				lastLatency = time.Since(start)
				lastPkt = pkt
				lastPayload = rspBytes
				if err != nil {
					lastMsg = colorRed + "RPC 调用异常: " + err.Error() + colorReset
				} else {
					var rsp orderpb.OrderDetailRsp
					_ = proto.Unmarshal(rspBytes, &rsp)
					lastMsg = fmt.Sprintf("  %s✓ 订单明细查询成功%s:\n  订单号: %s  买家: %s  商品: %s x %d\n  总金额: ￥%.2f  履约状态: %s%s%s",
						colorGreen, colorReset, rsp.OrderId, rsp.UserId, rsp.SkuId, rsp.Quantity,
						float64(rsp.TotalAmountCents)/100.0, colorGreen, rsp.Status, colorReset)
				}
			}

		case "6":
			// 取消上一笔订单并退款
			if t.lastOrder == "" {
				lastMsg = colorYellow + "当前暂无可取消的历史订单，请先执行 [2] 正向下单！" + colorReset
				lastPkt = nil
			} else {
				reqBytes, _ := proto.Marshal(&orderpb.CancelOrderReq{OrderId: t.lastOrder, Reason: "TUI用户主动退单"})
				pkt, rspBytes, err := t.client.Invoke(ctx, "demo.order-OrderService", "CancelOrder", t.nextTrace(), reqBytes)
				lastLatency = time.Since(start)
				lastPkt = pkt
				lastPayload = rspBytes
				if err != nil {
					lastMsg = colorRed + "RPC 调用异常: " + err.Error() + colorReset
				} else {
					var rsp orderpb.CancelOrderRsp
					_ = proto.Unmarshal(rspBytes, &rsp)
					lastMsg = fmt.Sprintf("  %s✓ 订单取消与全额退款成功%s:\n  订单号: %s\n  退还金额: ￥%.2f  释放库存: %d 件  详情: %s",
						colorGreen, colorReset, rsp.OrderId, float64(rsp.RefundedAmountCents)/100.0, rsp.ReleasedStock, rsp.Message)
				}
			}

		case "7":
			// 自定义参数表单
			fmt.Print("  请输入买家 UserID (如 Alice / Bob): ")
			if !scanner.Scan() {
				cancel()
				break
			}
			userID := strings.TrimSpace(scanner.Text())
			if userID == "" {
				userID = "Alice"
			}

			fmt.Print("  请输入商品 SKU (如 SKU-101 / SKU-102): ")
			if !scanner.Scan() {
				cancel()
				break
			}
			skuID := strings.TrimSpace(scanner.Text())
			if skuID == "" {
				skuID = "SKU-101"
			}

			fmt.Print("  请输入购买数量 (正整数，默认 1): ")
			if !scanner.Scan() {
				cancel()
				break
			}
			qtyStr := strings.TrimSpace(scanner.Text())
			qty, _ := strconv.Atoi(qtyStr)
			if qty <= 0 {
				qty = 1
			}

			reqBytes, _ := proto.Marshal(&orderpb.CreateOrderReq{
				UserId:   userID,
				SkuId:    skuID,
				Quantity: int32(qty),
			})
			pkt, rspBytes, err := t.client.Invoke(ctx, "demo.order-OrderService", "CreateOrder", t.nextTrace(), reqBytes)
			lastLatency = time.Since(start)
			lastPkt = pkt
			lastPayload = rspBytes
			if err != nil {
				lastMsg = colorRed + "RPC 调用异常: " + err.Error() + colorReset
			} else {
				var rsp orderpb.CreateOrderRsp
				_ = proto.Unmarshal(rspBytes, &rsp)
				if rsp.Code == 0 {
					t.lastOrder = rsp.OrderId
					lastMsg = fmt.Sprintf("  %s✓ 自定义下单成功%s: 订单: %s | 金额: ￥%.2f | 状态: %s",
						colorGreen, colorReset, rsp.OrderId, float64(rsp.TotalAmountCents)/100.0, rsp.Status)
				} else {
					lastMsg = fmt.Sprintf("  %s✗ 下单失败%s: %s (Code: %d, Status: %s)", colorRed, colorReset, rsp.Message, rsp.Code, rsp.Status)
				}
			}

		case "8":
			// 并发流量压测
			fmt.Println("\n  正在启动并发录制流（10 笔混合订单注入）...")
			var wg sync.WaitGroup
			var successCount, failCount int64
			var mu sync.Mutex

			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					subCtx, subCancel := context.WithTimeout(context.Background(), 3*time.Second)
					defer subCancel()

					user := "Alice"
					if idx%3 == 0 {
						user = "Bob"
					}
					reqB, _ := proto.Marshal(&orderpb.CreateOrderReq{
						UserId:   user,
						SkuId:    "SKU-101",
						Quantity: 1,
					})
					_, rBytes, err := t.client.Invoke(subCtx, "demo.order-OrderService", "CreateOrder", t.nextTrace(), reqB)
					mu.Lock()
					defer mu.Unlock()
					if err == nil {
						var r orderpb.CreateOrderRsp
						_ = proto.Unmarshal(rBytes, &r)
						if r.Code == 0 {
							successCount++
						} else {
							failCount++
						}
					} else {
						failCount++
					}
				}(i)
			}
			wg.Wait()
			lastLatency = time.Since(start)
			lastPkt = nil
			lastPayload = nil
			lastMsg = fmt.Sprintf("  %s✓ 批量注入完成 (耗时 %.2f ms)%s: 成功 %d 笔, 失败/熔断 %d 笔",
				colorGreen, float64(lastLatency.Microseconds())/1000.0, colorReset, successCount, failCount)

		default:
			lastMsg = colorRed + "未知指令序号，请输入 1-8 或 q 退出！" + colorReset
			lastPkt = nil
		}
		cancel()
	}
}
