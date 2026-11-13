package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"code-traefik/demo/pkg/crosframe"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9001", "TCP listen address for SUT Order Center")
	accountAddr := flag.String("account-addr", "127.0.0.1:9002", "Address of Backend Account Service")
	inventoryAddr := flag.String("inventory-addr", "127.0.0.1:9003", "Address of Backend Inventory Service")
	payAlipayAddr := flag.String("payment-alipay-addr", "127.0.0.1:9004", "Address of Payment Channel Instance (ALIPAY)")
	payAlipaySvc := flag.String("payment-alipay-service", "demo.payment.ALIPAY_PaymentService", "Published CROS service name for ALIPAY payment instance")
	payWechatAddr := flag.String("payment-wechat-addr", "127.0.0.1:9005", "Address of Payment Channel Instance (WECHAT)")
	payWechatSvc := flag.String("payment-wechat-service", "demo.payment.WECHAT_PaymentService", "Published CROS service name for WECHAT payment instance")
	flag.Parse()

	log.Printf("[sut-order] Initializing SUT CROS RPC Order Gateway on %s...", *addr)
	log.Printf("[sut-order] Downstream Account Service: %s, Inventory Service: %s", *accountAddr, *inventoryAddr)
	log.Printf("[sut-order] Payment Dispatch Routes: ALIPAY=%s (%s), WECHAT=%s (%s)",
		*payAlipayAddr, *payAlipaySvc, *payWechatAddr, *payWechatSvc)

	orchestrator := NewOrderOrchestrator(*accountAddr, *inventoryAddr, map[string]PaymentRoute{
		"ALIPAY": {Addr: *payAlipayAddr, ServiceName: *payAlipaySvc},
		"WECHAT": {Addr: *payWechatAddr, ServiceName: *payWechatSvc},
	})
	server := crosframe.NewServer(*addr, "sut-order")
	orchestrator.RegisterHandlers(server)

	if err := server.Start(); err != nil {
		log.Fatalf("[sut-order] Start failed: %v", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	log.Printf("[sut-order] Shutting down...")
	_ = server.Stop()
	orchestrator.Close()
	log.Printf("[sut-order] SUT Order Gateway stopped gracefully.")
}
