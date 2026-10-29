package demo_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"code-traefik/demo/pkg/crosframe"
	"code-traefik/demo/pkg/proto/accountpb"
	"code-traefik/demo/pkg/proto/inventorypb"
	"code-traefik/demo/pkg/proto/orderpb"
	"code-traefik/pkg/crosrpc"
	"google.golang.org/protobuf/proto"
)

// TestDemoFourProcessClusterEndToEnd 模拟四进程集群端到端集成测试
func TestDemoFourProcessClusterEndToEnd(t *testing.T) {
	// 1. 获取三个随机可用本地端口
	getFreeAddr := func() string {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Listen free addr failed: %v", err)
		}
		addr := ln.Addr().String()
		_ = ln.Close()
		return addr
	}

	accAddr := getFreeAddr()
	invAddr := getFreeAddr()
	sutAddr := getFreeAddr()

	// 2. 启动 Backend 1: Account
	accStore := newAccountTestStore()
	accSrv := crosframe.NewServer(accAddr, "TestAccount")
	accStore.Register(accSrv)
	if err := accSrv.Start(); err != nil {
		t.Fatalf("Start accSrv failed: %v", err)
	}
	defer func() { _ = accSrv.Stop() }()

	// 3. 启动 Backend 2: Inventory
	invStore := newInventoryTestStore()
	invSrv := crosframe.NewServer(invAddr, "TestInventory")
	invStore.Register(invSrv)
	if err := invSrv.Start(); err != nil {
		t.Fatalf("Start invSrv failed: %v", err)
	}
	defer func() { _ = invSrv.Stop() }()

	// 4. 启动 SUT: Order Gateway
	sutOrchestrator := newOrderTestOrchestrator(accAddr, invAddr)
	sutSrv := crosframe.NewServer(sutAddr, "TestSUT")
	sutOrchestrator.Register(sutSrv)
	if err := sutSrv.Start(); err != nil {
		t.Fatalf("Start sutSrv failed: %v", err)
	}
	defer func() {
		_ = sutSrv.Stop()
		_ = sutOrchestrator.accountCli.Close()
		_ = sutOrchestrator.inventoryCli.Close()
	}()

	// 5. 模拟 Client 发起调用
	client := crosframe.NewClient(sutAddr, 3*time.Second)
	defer func() { _ = client.Close() }()

	trace := &crosrpc.TraceKeyExt{
		TraceID:      0x88889999AAAABBBB,
		SpanID:       0x1,
		RecordSwitch: 1,
	}

	ctx := context.Background()

	// 场景 1: 综合查询 (Composite Quote)
	t.Run("Scenario1_GetCompositeQuote", func(t *testing.T) {
		reqBytes, _ := proto.Marshal(&orderpb.CompositeQuoteReq{
			UserId: "Alice",
			SkuId:  "SKU-101",
		})
		pkt, rspBytes, err := client.Invoke(ctx, "demo.order-OrderService", "GetCompositeQuote", trace, reqBytes)
		if err != nil {
			t.Fatalf("GetCompositeQuote failed: %v", err)
		}
		if pkt.Rsp.StatusCode != 0 {
			t.Fatalf("Rsp status code expected 0, got %d", pkt.Rsp.StatusCode)
		}
		var rsp orderpb.CompositeQuoteRsp
		if err := proto.Unmarshal(rspBytes, &rsp); err != nil {
			t.Fatalf("Unmarshal CompositeQuoteRsp failed: %v", err)
		}
		if rsp.Code != 0 || rsp.BalanceCents != 100000 || rsp.AvailableStock != 10 {
			t.Fatalf("Unexpected CompositeQuoteRsp: %+v", &rsp)
		}
	})

	// 场景 2: 正常正向下单 (Happy Path)
	var createdOrderID string
	t.Run("Scenario2_CreateOrder_HappyPath", func(t *testing.T) {
		reqBytes, _ := proto.Marshal(&orderpb.CreateOrderReq{
			UserId:   "Alice",
			SkuId:    "SKU-101",
			Quantity: 1,
		})
		pkt, rspBytes, err := client.Invoke(ctx, "demo.order-OrderService", "CreateOrder", trace, reqBytes)
		if err != nil {
			t.Fatalf("CreateOrder failed: %v", err)
		}
		if pkt.Rsp.StatusCode != 0 {
			t.Fatalf("Rsp status code expected 0, got %d", pkt.Rsp.StatusCode)
		}
		var rsp orderpb.CreateOrderRsp
		if err := proto.Unmarshal(rspBytes, &rsp); err != nil {
			t.Fatalf("Unmarshal CreateOrderRsp failed: %v", err)
		}
		if rsp.Code != 0 || rsp.Status != "SUCCESS" || rsp.TotalAmountCents != 29900 {
			t.Fatalf("Unexpected CreateOrderRsp: %+v", &rsp)
		}
		createdOrderID = rsp.OrderId

		// 验证 Alice 余额减少为 70100 (100000 - 29900)
		if accStore.accounts["Alice"].BalanceCents != 70100 {
			t.Fatalf("Expected Alice balance 70100, got %d", accStore.accounts["Alice"].BalanceCents)
		}
		// 验证 SKU-101 库存减少为 9
		if invStore.items["SKU-101"].TotalStock != 9 {
			t.Fatalf("Expected SKU-101 stock 9, got %d", invStore.items["SKU-101"].TotalStock)
		}
	})

	// 场景 3: 库存不足快速失败 (Stock Insufficient)
	t.Run("Scenario3_StockInsufficient", func(t *testing.T) {
		reqBytes, _ := proto.Marshal(&orderpb.CreateOrderReq{
			UserId:   "Alice",
			SkuId:    "SKU-102",
			Quantity: 99, // 只有 2 件
		})
		_, rspBytes, err := client.Invoke(ctx, "demo.order-OrderService", "CreateOrder", trace, reqBytes)
		if err != nil {
			t.Fatalf("CreateOrder failed: %v", err)
		}
		var rsp orderpb.CreateOrderRsp
		if err := proto.Unmarshal(rspBytes, &rsp); err != nil {
			t.Fatalf("Unmarshal CreateOrderRsp failed: %v", err)
		}
		if rsp.Code != 3002 || rsp.Status != "FAILED_STOCK" {
			t.Fatalf("Expected FAILED_STOCK (3002), got: %+v", &rsp)
		}
		// Alice 余额未受影响
		if accStore.accounts["Alice"].BalanceCents != 70100 {
			t.Fatalf("Balance changed unexpectedly: %d", accStore.accounts["Alice"].BalanceCents)
		}
	})

	// 场景 4: 余额不足与库存补偿回滚 (Balance Insufficient & Compensating Rollback)
	t.Run("Scenario4_BalanceInsufficient_And_Compensation", func(t *testing.T) {
		reqBytes, _ := proto.Marshal(&orderpb.CreateOrderReq{
			UserId:   "Bob", // Bob 只有 5000 分 (￥50.00)，买不起 29900 的键盘
			SkuId:    "SKU-101",
			Quantity: 1,
		})
		_, rspBytes, err := client.Invoke(ctx, "demo.order-OrderService", "CreateOrder", trace, reqBytes)
		if err != nil {
			t.Fatalf("CreateOrder failed: %v", err)
		}
		var rsp orderpb.CreateOrderRsp
		if err := proto.Unmarshal(rspBytes, &rsp); err != nil {
			t.Fatalf("Unmarshal CreateOrderRsp failed: %v", err)
		}
		if rsp.Code != 2002 || rsp.Status != "FAILED_BALANCE" {
			t.Fatalf("Expected FAILED_BALANCE (2002), got: %+v", &rsp)
		}
		// 校验预占的库存已被补偿释放：SKU-101 可用库存依然为 9，无遗留锁
		if invStore.items["SKU-101"].TotalStock-invStore.items["SKU-101"].LockedStock != 9 {
			t.Fatalf("Stock was not restored correctly: %+v", invStore.items["SKU-101"])
		}
		if len(invStore.locks) != 0 {
			t.Fatalf("Locks should be empty after compensation rollback, got %d", len(invStore.locks))
		}
	})

	// 场景 5: 查询订单详情与取消退款 (Cancel & Refund)
	t.Run("Scenario5_CancelAndRefund", func(t *testing.T) {
		// 5.1 查订单
		getReq, _ := proto.Marshal(&orderpb.OrderDetailReq{OrderId: createdOrderID})
		_, getRspBytes, err := client.Invoke(ctx, "demo.order-OrderService", "GetOrderDetail", trace, getReq)
		if err != nil {
			t.Fatalf("GetOrderDetail failed: %v", err)
		}
		var getRsp orderpb.OrderDetailRsp
		_ = proto.Unmarshal(getRspBytes, &getRsp)
		if getRsp.Code != 0 || getRsp.Status != "SUCCESS" {
			t.Fatalf("Unexpected GetOrderDetail: %+v", &getRsp)
		}

		// 5.2 取消退款
		cancelReq, _ := proto.Marshal(&orderpb.CancelOrderReq{OrderId: createdOrderID, Reason: "买多了"})
		_, cancelRspBytes, err := client.Invoke(ctx, "demo.order-OrderService", "CancelOrder", trace, cancelReq)
		if err != nil {
			t.Fatalf("CancelOrder failed: %v", err)
		}
		var cancelRsp orderpb.CancelOrderRsp
		_ = proto.Unmarshal(cancelRspBytes, &cancelRsp)
		if cancelRsp.Code != 0 || cancelRsp.RefundedAmountCents != 29900 {
			t.Fatalf("Unexpected CancelOrderRsp: %+v", &cancelRsp)
		}

		// 校验 Alice 余额恢复为 100000
		if accStore.accounts["Alice"].BalanceCents != 100000 {
			t.Fatalf("Balance not restored after refund: %d", accStore.accounts["Alice"].BalanceCents)
		}
	})
}

// 辅助适配器
type accountTestStore struct {
	accounts map[string]*accountpb.GetBalanceRsp
}

func newAccountTestStore() *accountTestStore {
	return &accountTestStore{
		accounts: map[string]*accountpb.GetBalanceRsp{
			"Alice": {UserId: "Alice", UserName: "Alice", BalanceCents: 100000},
			"Bob":   {UserId: "Bob", UserName: "Bob", BalanceCents: 5000},
		},
	}
}

func (s *accountTestStore) Register(server *crosframe.Server) {
	serviceName := "demo.account-AccountService"
	server.Register(serviceName, "GetBalance", func(ctx context.Context, pkt *crosrpc.ParsedPacket, payload []byte) (uint32, []byte, error) {
		var req accountpb.GetBalanceReq
		_ = proto.Unmarshal(payload, &req)
		acc, ok := s.accounts[req.UserId]
		if !ok {
			out, _ := proto.Marshal(&accountpb.GetBalanceRsp{Code: 2001, Message: "用户不存在"})
			return 0, out, nil
		}
		out, _ := proto.Marshal(&accountpb.GetBalanceRsp{Code: 0, UserId: acc.UserId, UserName: acc.UserName, BalanceCents: acc.BalanceCents})
		return 0, out, nil
	})
	server.Register(serviceName, "DeductBalance", func(ctx context.Context, pkt *crosrpc.ParsedPacket, payload []byte) (uint32, []byte, error) {
		var req accountpb.DeductBalanceReq
		_ = proto.Unmarshal(payload, &req)
		acc, ok := s.accounts[req.UserId]
		if !ok {
			out, _ := proto.Marshal(&accountpb.DeductBalanceRsp{Code: 2001, Message: "用户不存在"})
			return 0, out, nil
		}
		if acc.BalanceCents < req.AmountCents {
			out, _ := proto.Marshal(&accountpb.DeductBalanceRsp{Code: 2002, Message: "余额不足", RemainingBalance: acc.BalanceCents})
			return 0, out, nil
		}
		acc.BalanceCents -= req.AmountCents
		out, _ := proto.Marshal(&accountpb.DeductBalanceRsp{Code: 0, Message: "OK", RemainingBalance: acc.BalanceCents, TxSerialNo: "TX-123"})
		return 0, out, nil
	})
	server.Register(serviceName, "RefundBalance", func(ctx context.Context, pkt *crosrpc.ParsedPacket, payload []byte) (uint32, []byte, error) {
		var req accountpb.RefundBalanceReq
		_ = proto.Unmarshal(payload, &req)
		acc, ok := s.accounts[req.UserId]
		if !ok {
			out, _ := proto.Marshal(&accountpb.RefundBalanceRsp{Code: 2001, Message: "用户不存在"})
			return 0, out, nil
		}
		acc.BalanceCents += req.AmountCents
		out, _ := proto.Marshal(&accountpb.RefundBalanceRsp{Code: 0, CurrentBalance: acc.BalanceCents, RefundSerialNo: "REF-123"})
		return 0, out, nil
	})
}

type itemTestStock struct {
	SkuID          string
	SkuName        string
	UnitPriceCents int64
	TotalStock     int32
	LockedStock    int32
}

type inventoryTestStore struct {
	items map[string]*itemTestStock
	locks map[string]int32 // lockId -> qty
}

func newInventoryTestStore() *inventoryTestStore {
	return &inventoryTestStore{
		items: map[string]*itemTestStock{
			"SKU-101": {SkuID: "SKU-101", SkuName: "机械键盘", UnitPriceCents: 29900, TotalStock: 10},
			"SKU-102": {SkuID: "SKU-102", SkuName: "4K显示器", UnitPriceCents: 199900, TotalStock: 2},
		},
		locks: make(map[string]int32),
	}
}

func (s *inventoryTestStore) Register(server *crosframe.Server) {
	serviceName := "demo.inventory-InventoryService"
	server.Register(serviceName, "GetItemStock", func(ctx context.Context, pkt *crosrpc.ParsedPacket, payload []byte) (uint32, []byte, error) {
		var req inventorypb.GetItemStockReq
		_ = proto.Unmarshal(payload, &req)
		it, ok := s.items[req.SkuId]
		if !ok {
			out, _ := proto.Marshal(&inventorypb.GetItemStockRsp{Code: 3001, Message: "商品不存在"})
			return 0, out, nil
		}
		out, _ := proto.Marshal(&inventorypb.GetItemStockRsp{
			Code:           0,
			SkuId:          it.SkuID,
			SkuName:        it.SkuName,
			UnitPriceCents: it.UnitPriceCents,
			TotalStock:     it.TotalStock,
			LockedStock:    it.LockedStock,
			AvailableStock: it.TotalStock - it.LockedStock,
		})
		return 0, out, nil
	})
	server.Register(serviceName, "LockStock", func(ctx context.Context, pkt *crosrpc.ParsedPacket, payload []byte) (uint32, []byte, error) {
		var req inventorypb.LockStockReq
		_ = proto.Unmarshal(payload, &req)
		it, ok := s.items[req.SkuId]
		if !ok {
			out, _ := proto.Marshal(&inventorypb.LockStockRsp{Code: 3001, Message: "商品不存在"})
			return 0, out, nil
		}
		if it.TotalStock-it.LockedStock < req.Quantity {
			out, _ := proto.Marshal(&inventorypb.LockStockRsp{Code: 3002, Message: "可用库存不足"})
			return 0, out, nil
		}
		it.LockedStock += req.Quantity
		lockID := fmt.Sprintf("LCK-%d", time.Now().UnixNano())
		s.locks[lockID] = req.Quantity
		out, _ := proto.Marshal(&inventorypb.LockStockRsp{Code: 0, LockId: lockID, LockedQuantity: req.Quantity})
		return 0, out, nil
	})
	server.Register(serviceName, "ConfirmStock", func(ctx context.Context, pkt *crosrpc.ParsedPacket, payload []byte) (uint32, []byte, error) {
		var req inventorypb.ConfirmStockReq
		_ = proto.Unmarshal(payload, &req)
		qty, ok := s.locks[req.LockId]
		if !ok {
			out, _ := proto.Marshal(&inventorypb.ConfirmStockRsp{Code: 3003, Message: "凭证无效"})
			return 0, out, nil
		}
		delete(s.locks, req.LockId)
		it := s.items["SKU-101"]
		it.TotalStock -= qty
		it.LockedStock -= qty
		out, _ := proto.Marshal(&inventorypb.ConfirmStockRsp{Code: 0, RemainingStock: it.TotalStock})
		return 0, out, nil
	})
	server.Register(serviceName, "ReleaseStock", func(ctx context.Context, pkt *crosrpc.ParsedPacket, payload []byte) (uint32, []byte, error) {
		var req inventorypb.ReleaseStockReq
		_ = proto.Unmarshal(payload, &req)
		qty, ok := s.locks[req.LockId]
		if !ok {
			out, _ := proto.Marshal(&inventorypb.ReleaseStockRsp{Code: 3003, Message: "凭证无效"})
			return 0, out, nil
		}
		delete(s.locks, req.LockId)
		it := s.items["SKU-101"]
		it.LockedStock -= qty
		out, _ := proto.Marshal(&inventorypb.ReleaseStockRsp{Code: 0, RestoredStock: it.TotalStock - it.LockedStock})
		return 0, out, nil
	})
}

type orderTestOrchestrator struct {
	accountCli   *crosframe.Client
	inventoryCli *crosframe.Client
	orders       map[string]*orderpb.OrderDetailRsp
}

func newOrderTestOrchestrator(accAddr, invAddr string) *orderTestOrchestrator {
	return &orderTestOrchestrator{
		accountCli:   crosframe.NewClient(accAddr, 2*time.Second),
		inventoryCli: crosframe.NewClient(invAddr, 2*time.Second),
		orders:       make(map[string]*orderpb.OrderDetailRsp),
	}
}

func (o *orderTestOrchestrator) Register(server *crosframe.Server) {
	serviceName := "demo.order-OrderService"
	server.Register(serviceName, "GetCompositeQuote", func(ctx context.Context, pkt *crosrpc.ParsedPacket, payload []byte) (uint32, []byte, error) {
		var req orderpb.CompositeQuoteReq
		_ = proto.Unmarshal(payload, &req)

		invReq, _ := proto.Marshal(&inventorypb.GetItemStockReq{SkuId: req.SkuId})
		_, invRspB, _ := o.inventoryCli.Invoke(ctx, "demo.inventory-InventoryService", "GetItemStock", pkt.Trace, invReq)
		var invRsp inventorypb.GetItemStockRsp
		_ = proto.Unmarshal(invRspB, &invRsp)

		accReq, _ := proto.Marshal(&accountpb.GetBalanceReq{UserId: req.UserId})
		_, accRspB, _ := o.accountCli.Invoke(ctx, "demo.account-AccountService", "GetBalance", pkt.Trace, accReq)
		var accRsp accountpb.GetBalanceRsp
		_ = proto.Unmarshal(accRspB, &accRsp)

		rsp := &orderpb.CompositeQuoteRsp{
			Code:           0,
			UserId:         req.UserId,
			BalanceCents:   accRsp.BalanceCents,
			SkuId:          req.SkuId,
			SkuName:        invRsp.SkuName,
			UnitPriceCents: invRsp.UnitPriceCents,
			AvailableStock: invRsp.AvailableStock,
		}
		out, _ := proto.Marshal(rsp)
		return 0, out, nil
	})
	server.Register(serviceName, "CreateOrder", func(ctx context.Context, pkt *crosrpc.ParsedPacket, payload []byte) (uint32, []byte, error) {
		var req orderpb.CreateOrderReq
		_ = proto.Unmarshal(payload, &req)

		// 查价
		invReq, _ := proto.Marshal(&inventorypb.GetItemStockReq{SkuId: req.SkuId})
		_, invRspB, _ := o.inventoryCli.Invoke(ctx, "demo.inventory-InventoryService", "GetItemStock", pkt.Trace, invReq)
		var invRsp inventorypb.GetItemStockRsp
		_ = proto.Unmarshal(invRspB, &invRsp)

		total := invRsp.UnitPriceCents * int64(req.Quantity)
		orderID := fmt.Sprintf("ORD-%s-%d", req.UserId, time.Now().UnixNano()/1e6)

		// 1. 锁库存
		lockReq, _ := proto.Marshal(&inventorypb.LockStockReq{SkuId: req.SkuId, Quantity: req.Quantity, BizOrderId: orderID})
		_, lockRspB, _ := o.inventoryCli.Invoke(ctx, "demo.inventory-InventoryService", "LockStock", pkt.Trace, lockReq)
		var lockRsp inventorypb.LockStockRsp
		_ = proto.Unmarshal(lockRspB, &lockRsp)
		if lockRsp.Code != 0 {
			out, _ := proto.Marshal(&orderpb.CreateOrderRsp{Code: lockRsp.Code, Status: "FAILED_STOCK", Message: lockRsp.Message})
			return 0, out, nil
		}

		// 2. 扣款
		deductReq, _ := proto.Marshal(&accountpb.DeductBalanceReq{UserId: req.UserId, AmountCents: total, BizOrderId: orderID})
		_, deductRspB, _ := o.accountCli.Invoke(ctx, "demo.account-AccountService", "DeductBalance", pkt.Trace, deductReq)
		var deductRsp accountpb.DeductBalanceRsp
		_ = proto.Unmarshal(deductRspB, &deductRsp)
		if deductRsp.Code != 0 {
			// 补偿释放库存
			relReq, _ := proto.Marshal(&inventorypb.ReleaseStockReq{LockId: lockRsp.LockId, Reason: "BALANCE_INSUFFICIENT"})
			_, _, _ = o.inventoryCli.Invoke(ctx, "demo.inventory-InventoryService", "ReleaseStock", pkt.Trace, relReq)
			out, _ := proto.Marshal(&orderpb.CreateOrderRsp{Code: deductRsp.Code, Status: "FAILED_BALANCE", Message: deductRsp.Message})
			return 0, out, nil
		}

		// 3. 确认库存
		confReq, _ := proto.Marshal(&inventorypb.ConfirmStockReq{LockId: lockRsp.LockId, BizOrderId: orderID})
		_, _, _ = o.inventoryCli.Invoke(ctx, "demo.inventory-InventoryService", "ConfirmStock", pkt.Trace, confReq)

		// 4. 落单
		o.orders[orderID] = &orderpb.OrderDetailRsp{
			Code:             0,
			OrderId:          orderID,
			UserId:           req.UserId,
			SkuId:            req.SkuId,
			Quantity:         req.Quantity,
			TotalAmountCents: total,
			Status:           "SUCCESS",
		}

		out, _ := proto.Marshal(&orderpb.CreateOrderRsp{
			Code:             0,
			OrderId:          orderID,
			TotalAmountCents: total,
			Status:           "SUCCESS",
			Message:          "OK",
		})
		return 0, out, nil
	})
	server.Register(serviceName, "GetOrderDetail", func(ctx context.Context, pkt *crosrpc.ParsedPacket, payload []byte) (uint32, []byte, error) {
		var req orderpb.OrderDetailReq
		_ = proto.Unmarshal(payload, &req)
		ord, ok := o.orders[req.OrderId]
		if !ok {
			out, _ := proto.Marshal(&orderpb.OrderDetailRsp{Code: 4001, Message: "不存在"})
			return 0, out, nil
		}
		out, _ := proto.Marshal(ord)
		return 0, out, nil
	})
	server.Register(serviceName, "CancelOrder", func(ctx context.Context, pkt *crosrpc.ParsedPacket, payload []byte) (uint32, []byte, error) {
		var req orderpb.CancelOrderReq
		_ = proto.Unmarshal(payload, &req)
		ord, ok := o.orders[req.OrderId]
		if !ok {
			out, _ := proto.Marshal(&orderpb.CancelOrderRsp{Code: 4001, Message: "不存在"})
			return 0, out, nil
		}
		ord.Status = "CANCELLED"
		refReq, _ := proto.Marshal(&accountpb.RefundBalanceReq{UserId: ord.UserId, AmountCents: ord.TotalAmountCents, BizOrderId: req.OrderId})
		_, _, _ = o.accountCli.Invoke(ctx, "demo.account-AccountService", "RefundBalance", pkt.Trace, refReq)

		out, _ := proto.Marshal(&orderpb.CancelOrderRsp{
			Code:                0,
			OrderId:             req.OrderId,
			RefundedAmountCents: ord.TotalAmountCents,
			ReleasedStock:       ord.Quantity,
		})
		return 0, out, nil
	})
}
