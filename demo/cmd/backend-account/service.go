package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"code-traefik/demo/pkg/crosframe"
	"code-traefik/demo/pkg/proto/accountpb"
	"code-traefik/pkg/crosrpc"
	"google.golang.org/protobuf/proto"
)

// AccountStore 内存账户资产账本
type AccountStore struct {
	mu       sync.Mutex
	accounts map[string]*UserAccount
}

type UserAccount struct {
	UserID       string
	UserName     string
	BalanceCents int64
}

func NewAccountStore() *AccountStore {
	store := &AccountStore{
		accounts: make(map[string]*UserAccount),
	}
	// 预置默认账户数据
	store.accounts["Alice"] = &UserAccount{
		UserID:       "Alice",
		UserName:     "Alice (VIP Buyer)",
		BalanceCents: 100000, // ￥1000.00
	}
	store.accounts["Bob"] = &UserAccount{
		UserID:       "Bob",
		UserName:     "Bob (Budget Buyer)",
		BalanceCents: 5000, // ￥50.00
	}
	return store
}

// RegisterHandlers 注册账户业务 CROS RPC 服务
func (s *AccountStore) RegisterHandlers(server *crosframe.Server) {
	serviceName := "demo.account-AccountService"

	server.Register(serviceName, "GetBalance", s.handleGetBalance)
	server.Register(serviceName, "DeductBalance", s.handleDeductBalance)
	server.Register(serviceName, "RefundBalance", s.handleRefundBalance)
	server.Register(serviceName, "DepositBalance", s.handleDepositBalance)
}

func (s *AccountStore) handleGetBalance(ctx context.Context, pkt *crosrpc.ParsedPacket, reqPayload []byte) (uint32, []byte, error) {
	var req accountpb.GetBalanceReq
	if err := proto.Unmarshal(reqPayload, &req); err != nil {
		return 400, nil, fmt.Errorf("unmarshal GetBalanceReq failed: %w", err)
	}

	s.mu.Lock()
	acc, exists := s.accounts[req.UserId]
	s.mu.Unlock()

	rsp := &accountpb.GetBalanceRsp{
		UserId: req.UserId,
	}

	if !exists {
		rsp.Code = 2001
		rsp.Message = fmt.Sprintf("用户不存在: %s", req.UserId)
	} else {
		rsp.Code = 0
		rsp.Message = "OK"
		rsp.UserName = acc.UserName
		rsp.BalanceCents = acc.BalanceCents
	}

	out, err := proto.Marshal(rsp)
	return 0, out, err
}

func (s *AccountStore) handleDeductBalance(ctx context.Context, pkt *crosrpc.ParsedPacket, reqPayload []byte) (uint32, []byte, error) {
	var req accountpb.DeductBalanceReq
	if err := proto.Unmarshal(reqPayload, &req); err != nil {
		return 400, nil, fmt.Errorf("unmarshal DeductBalanceReq failed: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	rsp := &accountpb.DeductBalanceRsp{
		UserId: req.UserId,
	}

	acc, exists := s.accounts[req.UserId]
	if !exists {
		rsp.Code = 2001
		rsp.Message = fmt.Sprintf("用户不存在: %s", req.UserId)
		out, err := proto.Marshal(rsp)
		return 0, out, err
	}

	if acc.BalanceCents < req.AmountCents {
		rsp.Code = 2002
		rsp.Message = fmt.Sprintf("账户可用余额不足 (当前: ￥%.2f, 需扣除: ￥%.2f)",
			float64(acc.BalanceCents)/100.0, float64(req.AmountCents)/100.0)
		rsp.RemainingBalance = acc.BalanceCents
		out, err := proto.Marshal(rsp)
		return 0, out, err
	}

	acc.BalanceCents -= req.AmountCents
	rsp.Code = 0
	rsp.Message = "扣款成功"
	rsp.RemainingBalance = acc.BalanceCents
	rsp.TxSerialNo = fmt.Sprintf("TX-ACC-%d", time.Now().UnixNano())

	out, err := proto.Marshal(rsp)
	return 0, out, err
}

func (s *AccountStore) handleRefundBalance(ctx context.Context, pkt *crosrpc.ParsedPacket, reqPayload []byte) (uint32, []byte, error) {
	var req accountpb.RefundBalanceReq
	if err := proto.Unmarshal(reqPayload, &req); err != nil {
		return 400, nil, fmt.Errorf("unmarshal RefundBalanceReq failed: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	rsp := &accountpb.RefundBalanceRsp{
		UserId: req.UserId,
	}

	acc, exists := s.accounts[req.UserId]
	if !exists {
		rsp.Code = 2001
		rsp.Message = fmt.Sprintf("用户不存在: %s", req.UserId)
		out, err := proto.Marshal(rsp)
		return 0, out, err
	}

	acc.BalanceCents += req.AmountCents
	rsp.Code = 0
	rsp.Message = "退款已入账"
	rsp.CurrentBalance = acc.BalanceCents
	rsp.RefundSerialNo = fmt.Sprintf("REF-ACC-%d", time.Now().UnixNano())

	out, err := proto.Marshal(rsp)
	return 0, out, err
}

func (s *AccountStore) handleDepositBalance(ctx context.Context, pkt *crosrpc.ParsedPacket, reqPayload []byte) (uint32, []byte, error) {
	var req accountpb.DepositBalanceReq
	if err := proto.Unmarshal(reqPayload, &req); err != nil {
		return 400, nil, fmt.Errorf("unmarshal DepositBalanceReq failed: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	rsp := &accountpb.DepositBalanceRsp{}
	acc, exists := s.accounts[req.UserId]
	if !exists {
		acc = &UserAccount{
			UserID:       req.UserId,
			UserName:     req.UserId,
			BalanceCents: 0,
		}
		s.accounts[req.UserId] = acc
	}

	acc.BalanceCents += req.AmountCents
	rsp.Code = 0
	rsp.Message = "充值成功"
	rsp.CurrentBalance = acc.BalanceCents

	out, err := proto.Marshal(rsp)
	return 0, out, err
}
