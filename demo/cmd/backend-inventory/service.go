package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"code-traefik/demo/pkg/crosframe"
	"code-traefik/demo/pkg/proto/inventorypb"
	"code-traefik/pkg/crosrpc"
	"google.golang.org/protobuf/proto"
)

type ItemStock struct {
	SkuID          string
	SkuName        string
	UnitPriceCents int64
	TotalStock     int32
	LockedStock    int32
}

type StockLock struct {
	LockID     string
	SkuID      string
	Quantity   int32
	BizOrderID string
	CreatedAt  time.Time
}

type InventoryStore struct {
	mu    sync.Mutex
	items map[string]*ItemStock
	locks map[string]*StockLock
}

func NewInventoryStore() *InventoryStore {
	store := &InventoryStore{
		items: make(map[string]*ItemStock),
		locks: make(map[string]*StockLock),
	}
	// 预置默认商品数据
	store.items["SKU-101"] = &ItemStock{
		SkuID:          "SKU-101",
		SkuName:        "机械键盘 (Mechanical Keyboard)",
		UnitPriceCents: 29900, // ￥299.00
		TotalStock:     10,
		LockedStock:    0,
	}
	store.items["SKU-102"] = &ItemStock{
		SkuID:          "SKU-102",
		SkuName:        "4K电竞显示器 (4K Gaming Monitor)",
		UnitPriceCents: 199900, // ￥1999.00
		TotalStock:     2,
		LockedStock:    0,
	}
	return store
}

// RegisterHandlers 注册库存履约 CROS RPC 服务
func (s *InventoryStore) RegisterHandlers(server *crosframe.Server) {
	serviceName := "demo.inventory-InventoryService"

	server.Register(serviceName, "GetItemStock", s.handleGetItemStock)
	server.Register(serviceName, "LockStock", s.handleLockStock)
	server.Register(serviceName, "ConfirmStock", s.handleConfirmStock)
	server.Register(serviceName, "ReleaseStock", s.handleReleaseStock)
}

func (s *InventoryStore) handleGetItemStock(ctx context.Context, pkt *crosrpc.ParsedPacket, reqPayload []byte) (uint32, []byte, error) {
	var req inventorypb.GetItemStockReq
	if err := proto.Unmarshal(reqPayload, &req); err != nil {
		return 400, nil, fmt.Errorf("unmarshal GetItemStockReq failed: %w", err)
	}

	s.mu.Lock()
	item, exists := s.items[req.SkuId]
	s.mu.Unlock()

	rsp := &inventorypb.GetItemStockRsp{
		SkuId: req.SkuId,
	}

	if !exists {
		rsp.Code = 3001
		rsp.Message = fmt.Sprintf("商品不存在: %s", req.SkuId)
	} else {
		rsp.Code = 0
		rsp.Message = "OK"
		rsp.SkuName = item.SkuName
		rsp.UnitPriceCents = item.UnitPriceCents
		rsp.TotalStock = item.TotalStock
		rsp.LockedStock = item.LockedStock
		rsp.AvailableStock = item.TotalStock - item.LockedStock
	}

	out, err := proto.Marshal(rsp)
	return 0, out, err
}

func (s *InventoryStore) handleLockStock(ctx context.Context, pkt *crosrpc.ParsedPacket, reqPayload []byte) (uint32, []byte, error) {
	var req inventorypb.LockStockReq
	if err := proto.Unmarshal(reqPayload, &req); err != nil {
		return 400, nil, fmt.Errorf("unmarshal LockStockReq failed: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	rsp := &inventorypb.LockStockRsp{}
	item, exists := s.items[req.SkuId]
	if !exists {
		rsp.Code = 3001
		rsp.Message = fmt.Sprintf("商品不存在: %s", req.SkuId)
		out, err := proto.Marshal(rsp)
		return 0, out, err
	}

	available := item.TotalStock - item.LockedStock
	if available < req.Quantity {
		rsp.Code = 3002
		rsp.Message = fmt.Sprintf("商品可用库存不足 (当前剩余: %d件, 申请锁定: %d件)", available, req.Quantity)
		out, err := proto.Marshal(rsp)
		return 0, out, err
	}

	lockID := fmt.Sprintf("LCK-INV-%d", time.Now().UnixNano())
	item.LockedStock += req.Quantity
	s.locks[lockID] = &StockLock{
		LockID:     lockID,
		SkuID:      req.SkuId,
		Quantity:   req.Quantity,
		BizOrderID: req.BizOrderId,
		CreatedAt:  time.Now(),
	}

	rsp.Code = 0
	rsp.Message = "库存预占成功"
	rsp.LockId = lockID
	rsp.LockedQuantity = req.Quantity

	out, err := proto.Marshal(rsp)
	return 0, out, err
}

func (s *InventoryStore) handleConfirmStock(ctx context.Context, pkt *crosrpc.ParsedPacket, reqPayload []byte) (uint32, []byte, error) {
	var req inventorypb.ConfirmStockReq
	if err := proto.Unmarshal(reqPayload, &req); err != nil {
		return 400, nil, fmt.Errorf("unmarshal ConfirmStockReq failed: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	rsp := &inventorypb.ConfirmStockRsp{}
	lock, exists := s.locks[req.LockId]
	if !exists {
		rsp.Code = 3003
		rsp.Message = fmt.Sprintf("库存锁定凭证无效或已核销: %s", req.LockId)
		out, err := proto.Marshal(rsp)
		return 0, out, err
	}

	item := s.items[lock.SkuID]
	if item != nil {
		item.TotalStock -= lock.Quantity
		item.LockedStock -= lock.Quantity
		rsp.RemainingStock = item.TotalStock - item.LockedStock
	}

	delete(s.locks, req.LockId)

	rsp.Code = 0
	rsp.Message = "实物库存扣减确认完成"
	out, err := proto.Marshal(rsp)
	return 0, out, err
}

func (s *InventoryStore) handleReleaseStock(ctx context.Context, pkt *crosrpc.ParsedPacket, reqPayload []byte) (uint32, []byte, error) {
	var req inventorypb.ReleaseStockReq
	if err := proto.Unmarshal(reqPayload, &req); err != nil {
		return 400, nil, fmt.Errorf("unmarshal ReleaseStockReq failed: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	rsp := &inventorypb.ReleaseStockRsp{}
	lock, exists := s.locks[req.LockId]
	if !exists {
		rsp.Code = 3003
		rsp.Message = fmt.Sprintf("库存锁定凭证无效或已释放: %s", req.LockId)
		out, err := proto.Marshal(rsp)
		return 0, out, err
	}

	item := s.items[lock.SkuID]
	if item != nil {
		item.LockedStock -= lock.Quantity
		rsp.RestoredStock = item.TotalStock - item.LockedStock
	}

	delete(s.locks, req.LockId)

	rsp.Code = 0
	rsp.Message = "预占库存已释放回退"
	out, err := proto.Marshal(rsp)
	return 0, out, err
}
