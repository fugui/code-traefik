package payment

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"code-traefik/demo/pkg/crosframe"
	"code-traefik/demo/pkg/proto/paymentpb"
	"code-traefik/pkg/crosrpc"
	"google.golang.org/protobuf/proto"
)

const (
	// ContractServiceName 为 LIB 接口契约库中定义的多实现统一标准服务名
	ContractServiceName = "demo.payment.PaymentService"

	ChannelAlipay = "ALIPAY"
	ChannelWechat = "WECHAT"
)

// PaymentTx 记录单笔渠道支付与退款状态
type PaymentTx struct {
	OrderID     string
	UserID      string
	Channel     string
	ChannelTxNo string
	AmountCents int64
	Refunded    bool
	RefundTxNo  string
	PaidAtUnix  int64
}

// PaymentGateway 实现 LIB demo.payment.PaymentService 接口契约的具体业务渠道实例
type PaymentGateway struct {
	mu          sync.RWMutex
	channel     string
	serviceName string
	txSeq       atomic.Uint64
	authCount   atomic.Int64
	refundCount atomic.Int64
	txByOrder   map[string]*PaymentTx
}

// NormalizeChannel 规范化支付渠道标识，缺省归为 ALIPAY
func NormalizeChannel(channel string) string {
	ch := strings.ToUpper(strings.TrimSpace(channel))
	if ch == "" {
		return ChannelAlipay
	}
	return ch
}

// DefaultPublishedServiceName 按 ADR-16 v2.0 派生规范生成渠道自定义发布服务名
func DefaultPublishedServiceName(channel string) string {
	return fmt.Sprintf("demo.payment.%s_PaymentService", NormalizeChannel(channel))
}

// NewPaymentGateway 创建指定渠道与发布服务名的支付实例（serviceName 留空时默认派生 {channel}_PaymentService）
func NewPaymentGateway(channel, serviceName string) *PaymentGateway {
	ch := NormalizeChannel(channel)
	svcName := strings.TrimSpace(serviceName)
	if svcName == "" {
		svcName = DefaultPublishedServiceName(ch)
	}
	return &PaymentGateway{
		channel:     ch,
		serviceName: svcName,
		txByOrder:   make(map[string]*PaymentTx),
	}
}

// ServiceName 返回本实例实际对外注册发布的 CROS RPC 服务名
func (g *PaymentGateway) ServiceName() string {
	return g.serviceName
}

// Channel 返回当前实例归属实现渠道标识 (impl_key)
func (g *PaymentGateway) Channel() string {
	return g.channel
}

// Stats 返回已受理的授权支付与退款笔数
func (g *PaymentGateway) Stats() (authorized int64, refunded int64) {
	return g.authCount.Load(), g.refundCount.Load()
}

// RegisterHandlers 向 CROS RPC Server 注册 AuthorizePay 与 RefundPay 处理器
func (g *PaymentGateway) RegisterHandlers(server *crosframe.Server) {
	server.Register(g.serviceName, "AuthorizePay", g.handleAuthorizePay)
	server.Register(g.serviceName, "RefundPay", g.handleRefundPay)
}

func (g *PaymentGateway) handleAuthorizePay(_ context.Context, _ *crosrpc.ParsedPacket, payload []byte) (uint32, []byte, error) {
	var req paymentpb.AuthorizePayReq
	if err := proto.Unmarshal(payload, &req); err != nil {
		return 400, nil, fmt.Errorf("unmarshal AuthorizePayReq failed: %w", err)
	}

	if strings.TrimSpace(req.OrderId) == "" || req.AmountCents <= 0 {
		out, _ := proto.Marshal(&paymentpb.AuthorizePayRsp{
			Code:    4101,
			Message: "支付订单号与金额必须有效",
			OrderId: req.OrderId,
			Channel: g.channel,
		})
		return 0, out, nil
	}

	// 支持通过特定渠道名显式校验渠道隔离性；调用未指定 channel 时默认按当前实例渠道受理
	reqChannel := strings.ToUpper(strings.TrimSpace(req.Channel))
	if reqChannel != "" && reqChannel != g.channel {
		out, _ := proto.Marshal(&paymentpb.AuthorizePayRsp{
			Code:    4103,
			Message: fmt.Sprintf("渠道不匹配: 当前实例为 %s，收到 %s", g.channel, reqChannel),
			OrderId: req.OrderId,
			Channel: g.channel,
		})
		return 0, out, nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// 幂等重入直接返回原流水
	if existing, ok := g.txByOrder[req.OrderId]; ok {
		out, _ := proto.Marshal(&paymentpb.AuthorizePayRsp{
			Code:        0,
			Message:     fmt.Sprintf("[%s] 渠道幂等支付已确认", g.channel),
			OrderId:     existing.OrderID,
			Channel:     g.channel,
			ChannelTxNo: existing.ChannelTxNo,
			PaidAtUnix:  existing.PaidAtUnix,
		})
		return 0, out, nil
	}

	seq := g.txSeq.Add(1)
	txNo := fmt.Sprintf("%s-TX-%d-%04d", g.channel, time.Now().Unix()%100000, seq)
	paidAt := time.Now().Unix()

	g.txByOrder[req.OrderId] = &PaymentTx{
		OrderID:     req.OrderId,
		UserID:      req.UserId,
		Channel:     g.channel,
		ChannelTxNo: txNo,
		AmountCents: req.AmountCents,
		PaidAtUnix:  paidAt,
	}
	g.authCount.Add(1)

	log.Printf("[backend-payment:%s] AuthorizePay service=%s order=%s user=%s amount=%.2f tx=%s",
		g.channel, g.serviceName, req.OrderId, req.UserId, float64(req.AmountCents)/100.0, txNo)

	out, err := proto.Marshal(&paymentpb.AuthorizePayRsp{
		Code:        0,
		Message:     fmt.Sprintf("[%s] 渠道支付授权成功", g.channel),
		OrderId:     req.OrderId,
		Channel:     g.channel,
		ChannelTxNo: txNo,
		PaidAtUnix:  paidAt,
	})
	return 0, out, err
}

func (g *PaymentGateway) handleRefundPay(_ context.Context, _ *crosrpc.ParsedPacket, payload []byte) (uint32, []byte, error) {
	var req paymentpb.RefundPayReq
	if err := proto.Unmarshal(payload, &req); err != nil {
		return 400, nil, fmt.Errorf("unmarshal RefundPayReq failed: %w", err)
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	tx, exists := g.txByOrder[req.OrderId]
	if !exists {
		out, _ := proto.Marshal(&paymentpb.RefundPayRsp{
			Code:    4102,
			Message: fmt.Sprintf("[%s] 未找到订单 %s 的支付流水", g.channel, req.OrderId),
			OrderId: req.OrderId,
			Channel: g.channel,
		})
		return 0, out, nil
	}

	if tx.Refunded {
		out, _ := proto.Marshal(&paymentpb.RefundPayRsp{
			Code:       0,
			Message:    fmt.Sprintf("[%s] 渠道原路退款幂等确认", g.channel),
			OrderId:    req.OrderId,
			Channel:    g.channel,
			RefundTxNo: tx.RefundTxNo,
		})
		return 0, out, nil
	}

	seq := g.txSeq.Add(1)
	tx.Refunded = true
	tx.RefundTxNo = fmt.Sprintf("%s-RF-%d-%04d", g.channel, time.Now().Unix()%100000, seq)
	g.refundCount.Add(1)

	log.Printf("[backend-payment:%s] RefundPay service=%s order=%s refund_tx=%s reason=%s",
		g.channel, g.serviceName, req.OrderId, tx.RefundTxNo, req.Reason)

	out, err := proto.Marshal(&paymentpb.RefundPayRsp{
		Code:       0,
		Message:    fmt.Sprintf("[%s] 渠道原路退款成功", g.channel),
		OrderId:    req.OrderId,
		Channel:    g.channel,
		RefundTxNo: tx.RefundTxNo,
	})
	return 0, out, err
}
