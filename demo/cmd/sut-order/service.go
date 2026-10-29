package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"code-traefik/demo/pkg/crosframe"
	"code-traefik/demo/pkg/proto/accountpb"
	"code-traefik/demo/pkg/proto/inventorypb"
	"code-traefik/demo/pkg/proto/orderpb"
	"code-traefik/pkg/crosrpc"
	"google.golang.org/protobuf/proto"
)

type OrderRecord struct {
	OrderID          string
	UserID           string
	SkuID            string
	Quantity         int32
	TotalAmountCents int64
	Status           string
	CreatedAtUnix    int64
}

type OrderOrchestrator struct {
	mu           sync.RWMutex
	orders       map[string]*OrderRecord
	accountCli   *crosframe.Client
	inventoryCli *crosframe.Client
}

func NewOrderOrchestrator(accountAddr, inventoryAddr string) *OrderOrchestrator {
	return &OrderOrchestrator{
		orders:       make(map[string]*OrderRecord),
		accountCli:   crosframe.NewClient(accountAddr, 3*time.Second),
		inventoryCli: crosframe.NewClient(inventoryAddr, 3*time.Second),
	}
}

func (s *OrderOrchestrator) RegisterHandlers(server *crosframe.Server) {
	serviceName := "demo.order-OrderService"

	server.Register(serviceName, "GetCompositeQuote", s.handleGetCompositeQuote)
	server.Register(serviceName, "CreateOrder", s.handleCreateOrder)
	server.Register(serviceName, "GetOrderDetail", s.handleGetOrderDetail)
	server.Register(serviceName, "CancelOrder", s.handleCancelOrder)
}

// subTrace 派生子调用 Trace 信息，保持 TraceID 不变，串接 FatherSpanID
func subTrace(parent *crosrpc.TraceKeyExt, subSpanID uint64) *crosrpc.TraceKeyExt {
	if parent == nil {
		return &crosrpc.TraceKeyExt{
			TraceID:      uint64(time.Now().UnixNano()),
			SpanID:       subSpanID,
			FatherSpanID: 0,
			RecordSwitch: 1,
		}
	}
	return &crosrpc.TraceKeyExt{
		TraceID:      parent.TraceID,
		SpanID:       subSpanID,
		FatherSpanID: parent.SpanID,
		RecordSwitch: parent.RecordSwitch,
	}
}

func (s *OrderOrchestrator) handleGetCompositeQuote(ctx context.Context, pkt *crosrpc.ParsedPacket, reqPayload []byte) (uint32, []byte, error) {
	var req orderpb.CompositeQuoteReq
	if err := proto.Unmarshal(reqPayload, &req); err != nil {
		return 400, nil, fmt.Errorf("unmarshal CompositeQuoteReq failed: %w", err)
	}

	rsp := &orderpb.CompositeQuoteRsp{
		UserId: req.UserId,
		SkuId:  req.SkuId,
	}

	// 1. 调用 Backend 2 查询商品与可用库存
	invReqBytes, _ := proto.Marshal(&inventorypb.GetItemStockReq{SkuId: req.SkuId})
	invPkt, invRspBytes, err := s.inventoryCli.Invoke(ctx, "demo.inventory-InventoryService", "GetItemStock", subTrace(pkt.Trace, 0x101), invReqBytes)
	if err != nil {
		rsp.Code = 5002
		rsp.Message = fmt.Sprintf("调用库存服务失败: %v", err)
		out, _ := proto.Marshal(rsp)
		return 0, out, nil
	}
	_ = invPkt
	var invRsp inventorypb.GetItemStockRsp
	if err := proto.Unmarshal(invRspBytes, &invRsp); err != nil || invRsp.Code != 0 {
		rsp.Code = invRsp.Code
		rsp.Message = invRsp.Message
		out, _ := proto.Marshal(rsp)
		return 0, out, nil
	}
	rsp.SkuName = invRsp.SkuName
	rsp.UnitPriceCents = invRsp.UnitPriceCents
	rsp.AvailableStock = invRsp.AvailableStock

	// 2. 调用 Backend 1 查询买家账户可用余额
	accReqBytes, _ := proto.Marshal(&accountpb.GetBalanceReq{UserId: req.UserId})
	accPkt, accRspBytes, err := s.accountCli.Invoke(ctx, "demo.account-AccountService", "GetBalance", subTrace(pkt.Trace, 0x102), accReqBytes)
	if err != nil {
		rsp.Code = 5001
		rsp.Message = fmt.Sprintf("调用账户服务失败: %v", err)
		out, _ := proto.Marshal(rsp)
		return 0, out, nil
	}
	_ = accPkt
	var accRsp accountpb.GetBalanceRsp
	if err := proto.Unmarshal(accRspBytes, &accRsp); err != nil || accRsp.Code != 0 {
		rsp.Code = accRsp.Code
		rsp.Message = accRsp.Message
		out, _ := proto.Marshal(rsp)
		return 0, out, nil
	}
	rsp.BalanceCents = accRsp.BalanceCents
	rsp.Code = 0
	rsp.Message = "OK"

	out, err := proto.Marshal(rsp)
	return 0, out, err
}

func (s *OrderOrchestrator) handleCreateOrder(ctx context.Context, pkt *crosrpc.ParsedPacket, reqPayload []byte) (uint32, []byte, error) {
	var req orderpb.CreateOrderReq
	if err := proto.Unmarshal(reqPayload, &req); err != nil {
		return 400, nil, fmt.Errorf("unmarshal CreateOrderReq failed: %w", err)
	}

	orderID := fmt.Sprintf("ORD-%s-%d", req.UserId, time.Now().UnixNano()/1e6)
	rsp := &orderpb.CreateOrderRsp{
		OrderId:       orderID,
		CreatedAtUnix: time.Now().Unix(),
	}

	// 0. 查验商品行情单价
	invReqBytes, _ := proto.Marshal(&inventorypb.GetItemStockReq{SkuId: req.SkuId})
	_, invRspBytes, err := s.inventoryCli.Invoke(ctx, "demo.inventory-InventoryService", "GetItemStock", subTrace(pkt.Trace, 0x201), invReqBytes)
	if err != nil {
		rsp.Code = 5002
		rsp.Status = "ERROR_INVENTORY_UNAVAILABLE"
		rsp.Message = fmt.Sprintf("库存服务不可用: %v", err)
		out, _ := proto.Marshal(rsp)
		return 0, out, nil
	}
	var stockRsp inventorypb.GetItemStockRsp
	if err := proto.Unmarshal(invRspBytes, &stockRsp); err != nil || stockRsp.Code != 0 {
		rsp.Code = stockRsp.Code
		rsp.Status = "FAILED_STOCK"
		rsp.Message = stockRsp.Message
		out, _ := proto.Marshal(rsp)
		return 0, out, nil
	}

	totalAmount := stockRsp.UnitPriceCents * int64(req.Quantity)
	rsp.TotalAmountCents = totalAmount

	// 1. 预占库存 (LockStock)
	lockReqBytes, _ := proto.Marshal(&inventorypb.LockStockReq{
		SkuId:      req.SkuId,
		Quantity:   req.Quantity,
		BizOrderId: orderID,
	})
	_, lockRspBytes, err := s.inventoryCli.Invoke(ctx, "demo.inventory-InventoryService", "LockStock", subTrace(pkt.Trace, 0x202), lockReqBytes)
	if err != nil {
		rsp.Code = 5002
		rsp.Status = "ERROR_INVENTORY_LOCK"
		rsp.Message = fmt.Sprintf("锁定库存超时/网络错误: %v", err)
		out, _ := proto.Marshal(rsp)
		return 0, out, nil
	}
	var lockRsp inventorypb.LockStockRsp
	if err := proto.Unmarshal(lockRspBytes, &lockRsp); err != nil || lockRsp.Code != 0 {
		// 库存不足快速失败，不发起扣款
		rsp.Code = lockRsp.Code
		rsp.Status = "FAILED_STOCK"
		rsp.Message = lockRsp.Message
		out, _ := proto.Marshal(rsp)
		return 0, out, nil
	}
	lockID := lockRsp.LockId

	// 2. 扣除账户余额 (DeductBalance)
	deductReqBytes, _ := proto.Marshal(&accountpb.DeductBalanceReq{
		UserId:      req.UserId,
		AmountCents: totalAmount,
		BizOrderId:  orderID,
	})
	_, deductRspBytes, err := s.accountCli.Invoke(ctx, "demo.account-AccountService", "DeductBalance", subTrace(pkt.Trace, 0x203), deductReqBytes)
	if err != nil {
		// 扣款超时触发补偿释放
		s.compensateRelease(ctx, pkt.Trace, lockID, "DEDUCT_TIMEOUT")
		rsp.Code = 5001
		rsp.Status = "ERROR_ACCOUNT_TIMEOUT"
		rsp.Message = fmt.Sprintf("扣除余额异常，已回滚库存: %v", err)
		out, _ := proto.Marshal(rsp)
		return 0, out, nil
	}
	var deductRsp accountpb.DeductBalanceRsp
	if err := proto.Unmarshal(deductRspBytes, &deductRsp); err != nil || deductRsp.Code != 0 {
		// 余额不足！触发补偿回滚预占的库存
		log.Printf("[sut-order] Balance insufficient for user %s, triggering ReleaseStock(%s)...", req.UserId, lockID)
		s.compensateRelease(ctx, pkt.Trace, lockID, "BALANCE_INSUFFICIENT")

		rsp.Code = deductRsp.Code
		rsp.Status = "FAILED_BALANCE"
		rsp.Message = fmt.Sprintf("下单失败: %s (已自动释放预占库存)", deductRsp.Message)
		out, _ := proto.Marshal(rsp)
		return 0, out, nil
	}

	// 3. 确认实物扣除 (ConfirmStock)
	confirmReqBytes, _ := proto.Marshal(&inventorypb.ConfirmStockReq{
		LockId:     lockID,
		BizOrderId: orderID,
	})
	_, _, _ = s.inventoryCli.Invoke(ctx, "demo.inventory-InventoryService", "ConfirmStock", subTrace(pkt.Trace, 0x204), confirmReqBytes)

	// 4. 落单成功
	s.mu.Lock()
	s.orders[orderID] = &OrderRecord{
		OrderID:          orderID,
		UserID:           req.UserId,
		SkuID:            req.SkuId,
		Quantity:         req.Quantity,
		TotalAmountCents: totalAmount,
		Status:           "SUCCESS",
		CreatedAtUnix:    rsp.CreatedAtUnix,
	}
	s.mu.Unlock()

	rsp.Code = 0
	rsp.Status = "SUCCESS"
	rsp.Message = "订单创建成功，库存已扣除，款项已结清"

	out, err := proto.Marshal(rsp)
	return 0, out, err
}

func (s *OrderOrchestrator) compensateRelease(ctx context.Context, parentTrace *crosrpc.TraceKeyExt, lockID, reason string) {
	relReqBytes, _ := proto.Marshal(&inventorypb.ReleaseStockReq{
		LockId: lockID,
		Reason: reason,
	})
	_, _, _ = s.inventoryCli.Invoke(ctx, "demo.inventory-InventoryService", "ReleaseStock", subTrace(parentTrace, 0x299), relReqBytes)
}

func (s *OrderOrchestrator) handleGetOrderDetail(ctx context.Context, pkt *crosrpc.ParsedPacket, reqPayload []byte) (uint32, []byte, error) {
	var req orderpb.OrderDetailReq
	if err := proto.Unmarshal(reqPayload, &req); err != nil {
		return 400, nil, fmt.Errorf("unmarshal OrderDetailReq failed: %w", err)
	}

	s.mu.RLock()
	ord, exists := s.orders[req.OrderId]
	s.mu.RUnlock()

	rsp := &orderpb.OrderDetailRsp{
		OrderId: req.OrderId,
	}
	if !exists {
		rsp.Code = 4001
		rsp.Message = fmt.Sprintf("订单不存在: %s", req.OrderId)
	} else {
		rsp.Code = 0
		rsp.Message = "OK"
		rsp.UserId = ord.UserID
		rsp.SkuId = ord.SkuID
		rsp.Quantity = ord.Quantity
		rsp.TotalAmountCents = ord.TotalAmountCents
		rsp.Status = ord.Status
		rsp.CreatedAtUnix = ord.CreatedAtUnix
	}

	out, err := proto.Marshal(rsp)
	return 0, out, err
}

func (s *OrderOrchestrator) handleCancelOrder(ctx context.Context, pkt *crosrpc.ParsedPacket, reqPayload []byte) (uint32, []byte, error) {
	var req orderpb.CancelOrderReq
	if err := proto.Unmarshal(reqPayload, &req); err != nil {
		return 400, nil, fmt.Errorf("unmarshal CancelOrderReq failed: %w", err)
	}

	s.mu.Lock()
	ord, exists := s.orders[req.OrderId]
	if !exists {
		s.mu.Unlock()
		rsp := &orderpb.CancelOrderRsp{
			Code:    4001,
			Message: fmt.Sprintf("订单不存在: %s", req.OrderId),
		}
		out, _ := proto.Marshal(rsp)
		return 0, out, nil
	}

	if ord.Status == "CANCELLED" {
		s.mu.Unlock()
		rsp := &orderpb.CancelOrderRsp{
			Code:    4002,
			Message: "订单此前已取消",
		}
		out, _ := proto.Marshal(rsp)
		return 0, out, nil
	}

	ord.Status = "CANCELLED"
	refundAmount := ord.TotalAmountCents
	releasedQty := ord.Quantity
	userID := ord.UserID
	s.mu.Unlock()

	// 1. 调用 Account 退款
	refReqBytes, _ := proto.Marshal(&accountpb.RefundBalanceReq{
		UserId:      userID,
		AmountCents: refundAmount,
		BizOrderId:  req.OrderId,
		Reason:      req.Reason,
	})
	_, _, _ = s.accountCli.Invoke(ctx, "demo.account-AccountService", "RefundBalance", subTrace(pkt.Trace, 0x301), refReqBytes)

	rsp := &orderpb.CancelOrderRsp{
		Code:                0,
		Message:             "订单已取消并全额退款",
		OrderId:             req.OrderId,
		RefundedAmountCents: refundAmount,
		ReleasedStock:       releasedQty,
	}

	out, err := proto.Marshal(rsp)
	return 0, out, err
}
