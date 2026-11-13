package demo_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"code-traefik/demo/pkg/crosframe"
	"code-traefik/demo/pkg/payment"
	"code-traefik/demo/pkg/proto/accountpb"
	"code-traefik/demo/pkg/proto/inventorypb"
	"code-traefik/demo/pkg/proto/orderpb"
	"code-traefik/demo/pkg/proto/paymentpb"
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
	payAlipayAddr := getFreeAddr()
	payWechatAddr := getFreeAddr()
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

	// 3.5 启动同一 LIB 契约 (demo.payment.PaymentService) 的两个自定义发布名实例
	payAlipayGW := payment.NewPaymentGateway(payment.ChannelAlipay, "")
	payAlipaySrv := crosframe.NewServer(payAlipayAddr, "TestPaymentAlipay")
	payAlipayGW.RegisterHandlers(payAlipaySrv)
	if err := payAlipaySrv.Start(); err != nil {
		t.Fatalf("Start payAlipaySrv failed: %v", err)
	}
	defer func() { _ = payAlipaySrv.Stop() }()

	payWechatGW := payment.NewPaymentGateway(payment.ChannelWechat, "")
	payWechatSrv := crosframe.NewServer(payWechatAddr, "TestPaymentWechat")
	payWechatGW.RegisterHandlers(payWechatSrv)
	if err := payWechatSrv.Start(); err != nil {
		t.Fatalf("Start payWechatSrv failed: %v", err)
	}
	defer func() { _ = payWechatSrv.Stop() }()

	// 4. 启动 SUT: Order Gateway
	sutOrchestrator := newOrderTestOrchestrator(accAddr, invAddr, map[string]paymentRouteTest{
		payment.ChannelAlipay: {Addr: payAlipayAddr, ServiceName: payAlipayGW.ServiceName()},
		payment.ChannelWechat: {Addr: payWechatAddr, ServiceName: payWechatGW.ServiceName()},
	})
	sutSrv := crosframe.NewServer(sutAddr, "TestSUT")
	sutOrchestrator.Register(sutSrv)
	if err := sutSrv.Start(); err != nil {
		t.Fatalf("Start sutSrv failed: %v", err)
	}
	defer func() {
		_ = sutSrv.Stop()
		_ = sutOrchestrator.accountCli.Close()
		_ = sutOrchestrator.inventoryCli.Close()
		for _, cli := range sutOrchestrator.paymentClis {
			_ = cli.Close()
		}
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
		pkt, rspBytes, err := client.Invoke(ctx, "demo.order.OrderService", "GetCompositeQuote", trace, reqBytes)
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
		pkt, rspBytes, err := client.Invoke(ctx, "demo.order.OrderService", "CreateOrder", trace, reqBytes)
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
		_, rspBytes, err := client.Invoke(ctx, "demo.order.OrderService", "CreateOrder", trace, reqBytes)
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
		_, rspBytes, err := client.Invoke(ctx, "demo.order.OrderService", "CreateOrder", trace, reqBytes)
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
		_, getRspBytes, err := client.Invoke(ctx, "demo.order.OrderService", "GetOrderDetail", trace, getReq)
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
		_, cancelRspBytes, err := client.Invoke(ctx, "demo.order.OrderService", "CancelOrder", trace, cancelReq)
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
		// 校验 Scenario 2 默认路由到 ALIPAY，取消后触发了 1 次 ALIPAY 退款且未误触发 WECHAT
		aliAuth, aliRef := payAlipayGW.Stats()
		wxAuth, wxRef := payWechatGW.Stats()
		if aliAuth != 1 || aliRef != 1 || wxAuth != 0 || wxRef != 0 {
			t.Fatalf("Unexpected payment stats after Scenario 5: alipay=(%d,%d), wechat=(%d,%d)", aliAuth, aliRef, wxAuth, wxRef)
		}
	})

	// 场景 6: 多实现 LIB 契约路由分发 — ALIPAY 与 WECHAT 双自定义发布服务名实例隔离验证
	t.Run("Scenario6_PaymentLibTwoInstances_CustomServiceNames", func(t *testing.T) {
		var wechatOrderID string

		// 6.1 显式选择 WECHAT 渠道下单 -> 命中 demo.payment.WECHAT_PaymentService
		wxReqBytes, _ := proto.Marshal(&orderpb.CreateOrderReq{
			UserId:         "Alice",
			SkuId:          "SKU-101",
			Quantity:       1,
			PaymentChannel: "WECHAT",
		})
		_, wxRspBytes, err := client.Invoke(ctx, "demo.order.OrderService", "CreateOrder", trace, wxReqBytes)
		if err != nil {
			t.Fatalf("CreateOrder (WECHAT) failed: %v", err)
		}
		var wxRsp orderpb.CreateOrderRsp
		_ = proto.Unmarshal(wxRspBytes, &wxRsp)
		if wxRsp.Code != 0 || wxRsp.Status != "SUCCESS" || wxRsp.PaymentChannel != "WECHAT" {
			t.Fatalf("Expected WECHAT order success, got: %+v", &wxRsp)
		}
		if len(wxRsp.PaymentTxNo) < 9 || wxRsp.PaymentTxNo[:9] != "WECHAT-TX" {
			t.Fatalf("Expected WECHAT-TX prefix, got: %s", wxRsp.PaymentTxNo)
		}
		wechatOrderID = wxRsp.OrderId

		// 6.2 校验 WECHAT 受理 +1，ALIPAY 未增加
		aliAuth, aliRef := payAlipayGW.Stats()
		wxAuth, wxRef := payWechatGW.Stats()
		if aliAuth != 1 || aliRef != 1 || wxAuth != 1 || wxRef != 0 {
			t.Fatalf("Expected WECHAT auth=1, got alipay=(%d,%d), wechat=(%d,%d)", aliAuth, aliRef, wxAuth, wxRef)
		}

		// 6.3 取消 WECHAT 订单 -> 退款精确路由到 WECHAT 实例
		cancelReq, _ := proto.Marshal(&orderpb.CancelOrderReq{OrderId: wechatOrderID, Reason: "渠道路由退款验证"})
		_, cancelRspB, err := client.Invoke(ctx, "demo.order.OrderService", "CancelOrder", trace, cancelReq)
		if err != nil {
			t.Fatalf("CancelOrder (WECHAT) failed: %v", err)
		}
		var cancelRsp orderpb.CancelOrderRsp
		_ = proto.Unmarshal(cancelRspB, &cancelRsp)
		if cancelRsp.Code != 0 {
			t.Fatalf("CancelOrder (WECHAT) error: %+v", &cancelRsp)
		}

		aliAuth, aliRef = payAlipayGW.Stats()
		wxAuth, wxRef = payWechatGW.Stats()
		if aliAuth != 1 || aliRef != 1 || wxAuth != 1 || wxRef != 1 {
			t.Fatalf("Expected WECHAT refund=1 and ALIPAY untouched, got alipay=(%d,%d), wechat=(%d,%d)", aliAuth, aliRef, wxAuth, wxRef)
		}
	})
}

// TestDemoPaymentLib_SharedContractNameMode 验证同一份 PaymentGateway 代码亦可通过共享契约名模式部署双实例
func TestDemoPaymentLib_SharedContractNameMode(t *testing.T) {
	getFreeAddr := func() string {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Listen free addr failed: %v", err)
		}
		addr := ln.Addr().String()
		_ = ln.Close()
		return addr
	}

	addrA := getFreeAddr()
	addrB := getFreeAddr()

	// 两个独立进程均以 LIB 原始契约名 demo.payment.PaymentService 注册对外服务
	gwA := payment.NewPaymentGateway("ALIPAY", payment.ContractServiceName)
	srvA := crosframe.NewServer(addrA, "SharedPayA")
	gwA.RegisterHandlers(srvA)
	if err := srvA.Start(); err != nil {
		t.Fatalf("Start srvA failed: %v", err)
	}
	defer func() { _ = srvA.Stop() }()

	gwB := payment.NewPaymentGateway("WECHAT", payment.ContractServiceName)
	srvB := crosframe.NewServer(addrB, "SharedPayB")
	gwB.RegisterHandlers(srvB)
	if err := srvB.Start(); err != nil {
		t.Fatalf("Start srvB failed: %v", err)
	}
	defer func() { _ = srvB.Stop() }()

	if gwA.ServiceName() != payment.ContractServiceName || gwB.ServiceName() != payment.ContractServiceName {
		t.Fatalf("Expected shared contract service name %s", payment.ContractServiceName)
	}

	cliA := crosframe.NewClient(addrA, 2*time.Second)
	defer func() { _ = cliA.Close() }()
	cliB := crosframe.NewClient(addrB, 2*time.Second)
	defer func() { _ = cliB.Close() }()

	ctx := context.Background()
	trace := &crosrpc.TraceKeyExt{TraceID: 0x9999, SpanID: 1, RecordSwitch: 1}

	reqA, _ := proto.Marshal(&paymentpb.AuthorizePayReq{OrderId: "ORD-SHARED-1", UserId: "Alice", AmountCents: 1000, Channel: "ALIPAY"})
	_, rspABytes, err := cliA.Invoke(ctx, payment.ContractServiceName, "AuthorizePay", trace, reqA)
	if err != nil {
		t.Fatalf("cliA Invoke failed: %v", err)
	}
	var rspA paymentpb.AuthorizePayRsp
	_ = proto.Unmarshal(rspABytes, &rspA)
	if rspA.Code != 0 || rspA.Channel != "ALIPAY" {
		t.Fatalf("Unexpected shared mode rspA: %+v", &rspA)
	}

	reqB, _ := proto.Marshal(&paymentpb.AuthorizePayReq{OrderId: "ORD-SHARED-2", UserId: "Alice", AmountCents: 2000, Channel: "WECHAT"})
	_, rspBBytes, err := cliB.Invoke(ctx, payment.ContractServiceName, "AuthorizePay", trace, reqB)
	if err != nil {
		t.Fatalf("cliB Invoke failed: %v", err)
	}
	var rspB paymentpb.AuthorizePayRsp
	_ = proto.Unmarshal(rspBBytes, &rspB)
	if rspB.Code != 0 || rspB.Channel != "WECHAT" {
		t.Fatalf("Unexpected shared mode rspB: %+v", &rspB)
	}
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
	serviceName := "demo.account.AccountService"
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
	serviceName := "demo.inventory.InventoryService"
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

type paymentRouteTest struct {
	Addr        string
	ServiceName string
}

type orderTestRecord struct {
	detail         *orderpb.OrderDetailRsp
	paymentChannel string
	paymentTxNo    string
}

type orderTestOrchestrator struct {
	accountCli     *crosframe.Client
	inventoryCli   *crosframe.Client
	paymentClis    map[string]*crosframe.Client
	paymentSvcName map[string]string
	orders         map[string]*orderTestRecord
}

func newOrderTestOrchestrator(accAddr, invAddr string, payRoutes map[string]paymentRouteTest) *orderTestOrchestrator {
	pClis := make(map[string]*crosframe.Client, len(payRoutes))
	pSvcs := make(map[string]string, len(payRoutes))
	for ch, r := range payRoutes {
		pClis[ch] = crosframe.NewClient(r.Addr, 2*time.Second)
		pSvcs[ch] = r.ServiceName
	}
	return &orderTestOrchestrator{
		accountCli:     crosframe.NewClient(accAddr, 2*time.Second),
		inventoryCli:   crosframe.NewClient(invAddr, 2*time.Second),
		paymentClis:    pClis,
		paymentSvcName: pSvcs,
		orders:         make(map[string]*orderTestRecord),
	}
}

func (o *orderTestOrchestrator) Register(server *crosframe.Server) {
	serviceName := "demo.order.OrderService"
	server.Register(serviceName, "GetCompositeQuote", func(ctx context.Context, pkt *crosrpc.ParsedPacket, payload []byte) (uint32, []byte, error) {
		var req orderpb.CompositeQuoteReq
		_ = proto.Unmarshal(payload, &req)

		invReq, _ := proto.Marshal(&inventorypb.GetItemStockReq{SkuId: req.SkuId})
		_, invRspB, _ := o.inventoryCli.Invoke(ctx, "demo.inventory.InventoryService", "GetItemStock", pkt.Trace, invReq)
		var invRsp inventorypb.GetItemStockRsp
		_ = proto.Unmarshal(invRspB, &invRsp)

		accReq, _ := proto.Marshal(&accountpb.GetBalanceReq{UserId: req.UserId})
		_, accRspB, _ := o.accountCli.Invoke(ctx, "demo.account.AccountService", "GetBalance", pkt.Trace, accReq)
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

		payCh := payment.NormalizeChannel(req.PaymentChannel)
		pCli, ok := o.paymentClis[payCh]
		if !ok {
			out, _ := proto.Marshal(&orderpb.CreateOrderRsp{Code: 4003, Status: "FAILED_PAYMENT", Message: "不支持的支付渠道"})
			return 0, out, nil
		}
		pSvc := o.paymentSvcName[payCh]

		// 查价
		invReq, _ := proto.Marshal(&inventorypb.GetItemStockReq{SkuId: req.SkuId})
		_, invRspB, _ := o.inventoryCli.Invoke(ctx, "demo.inventory.InventoryService", "GetItemStock", pkt.Trace, invReq)
		var invRsp inventorypb.GetItemStockRsp
		_ = proto.Unmarshal(invRspB, &invRsp)

		total := invRsp.UnitPriceCents * int64(req.Quantity)
		orderID := fmt.Sprintf("ORD-%s-%d", req.UserId, time.Now().UnixNano()/1e6)

		// 1. 锁库存
		lockReq, _ := proto.Marshal(&inventorypb.LockStockReq{SkuId: req.SkuId, Quantity: req.Quantity, BizOrderId: orderID})
		_, lockRspB, _ := o.inventoryCli.Invoke(ctx, "demo.inventory.InventoryService", "LockStock", pkt.Trace, lockReq)
		var lockRsp inventorypb.LockStockRsp
		_ = proto.Unmarshal(lockRspB, &lockRsp)
		if lockRsp.Code != 0 {
			out, _ := proto.Marshal(&orderpb.CreateOrderRsp{Code: lockRsp.Code, Status: "FAILED_STOCK", Message: lockRsp.Message})
			return 0, out, nil
		}

		// 2. 扣款
		deductReq, _ := proto.Marshal(&accountpb.DeductBalanceReq{UserId: req.UserId, AmountCents: total, BizOrderId: orderID})
		_, deductRspB, _ := o.accountCli.Invoke(ctx, "demo.account.AccountService", "DeductBalance", pkt.Trace, deductReq)
		var deductRsp accountpb.DeductBalanceRsp
		_ = proto.Unmarshal(deductRspB, &deductRsp)
		if deductRsp.Code != 0 {
			// 补偿释放库存
			relReq, _ := proto.Marshal(&inventorypb.ReleaseStockReq{LockId: lockRsp.LockId, Reason: "BALANCE_INSUFFICIENT"})
			_, _, _ = o.inventoryCli.Invoke(ctx, "demo.inventory.InventoryService", "ReleaseStock", pkt.Trace, relReq)
			out, _ := proto.Marshal(&orderpb.CreateOrderRsp{Code: deductRsp.Code, Status: "FAILED_BALANCE", Message: deductRsp.Message})
			return 0, out, nil
		}

		// 2.5 调用 LIB 多实现支付实例 AuthorizePay
		payReq, _ := proto.Marshal(&paymentpb.AuthorizePayReq{OrderId: orderID, UserId: req.UserId, AmountCents: total, Channel: payCh})
		_, payRspB, payErr := pCli.Invoke(ctx, pSvc, "AuthorizePay", pkt.Trace, payReq)
		var payRsp paymentpb.AuthorizePayRsp
		if payErr != nil || proto.Unmarshal(payRspB, &payRsp) != nil || payRsp.Code != 0 {
			refReq, _ := proto.Marshal(&accountpb.RefundBalanceReq{UserId: req.UserId, AmountCents: total, BizOrderId: orderID, Reason: "PAY_FAIL"})
			_, _, _ = o.accountCli.Invoke(ctx, "demo.account.AccountService", "RefundBalance", pkt.Trace, refReq)
			relReq, _ := proto.Marshal(&inventorypb.ReleaseStockReq{LockId: lockRsp.LockId, Reason: "PAY_FAIL"})
			_, _, _ = o.inventoryCli.Invoke(ctx, "demo.inventory.InventoryService", "ReleaseStock", pkt.Trace, relReq)
			out, _ := proto.Marshal(&orderpb.CreateOrderRsp{Code: 5003, Status: "FAILED_PAYMENT", Message: payRsp.Message})
			return 0, out, nil
		}

		// 3. 确认库存
		confReq, _ := proto.Marshal(&inventorypb.ConfirmStockReq{LockId: lockRsp.LockId, BizOrderId: orderID})
		_, _, _ = o.inventoryCli.Invoke(ctx, "demo.inventory.InventoryService", "ConfirmStock", pkt.Trace, confReq)

		// 4. 落单
		o.orders[orderID] = &orderTestRecord{
			paymentChannel: payCh,
			paymentTxNo:    payRsp.ChannelTxNo,
			detail: &orderpb.OrderDetailRsp{
				Code:             0,
				OrderId:          orderID,
				UserId:           req.UserId,
				SkuId:            req.SkuId,
				Quantity:         req.Quantity,
				TotalAmountCents: total,
				Status:           "SUCCESS",
				PaymentChannel:   payCh,
				PaymentTxNo:      payRsp.ChannelTxNo,
			},
		}

		out, _ := proto.Marshal(&orderpb.CreateOrderRsp{
			Code:             0,
			OrderId:          orderID,
			TotalAmountCents: total,
			Status:           "SUCCESS",
			Message:          "OK",
			PaymentChannel:   payCh,
			PaymentTxNo:      payRsp.ChannelTxNo,
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
		out, _ := proto.Marshal(ord.detail)
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
		ord.detail.Status = "CANCELLED"
		refReq, _ := proto.Marshal(&accountpb.RefundBalanceReq{UserId: ord.detail.UserId, AmountCents: ord.detail.TotalAmountCents, BizOrderId: req.OrderId})
		_, _, _ = o.accountCli.Invoke(ctx, "demo.account.AccountService", "RefundBalance", pkt.Trace, refReq)

		if pCli, ok := o.paymentClis[ord.paymentChannel]; ok && ord.paymentTxNo != "" {
			payRefReq, _ := proto.Marshal(&paymentpb.RefundPayReq{OrderId: req.OrderId, ChannelTxNo: ord.paymentTxNo, AmountCents: ord.detail.TotalAmountCents, Reason: req.Reason})
			_, _, _ = pCli.Invoke(ctx, o.paymentSvcName[ord.paymentChannel], "RefundPay", pkt.Trace, payRefReq)
		}

		out, _ := proto.Marshal(&orderpb.CancelOrderRsp{
			Code:                0,
			OrderId:             req.OrderId,
			RefundedAmountCents: ord.detail.TotalAmountCents,
			ReleasedStock:       ord.detail.Quantity,
		})
		return 0, out, nil
	})
}
