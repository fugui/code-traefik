package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"code-traefik/demo/pkg/crosframe"
	"code-traefik/demo/pkg/payment"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9004", "TCP listen address for Payment Channel Instance")
	channel := flag.String("channel", payment.ChannelAlipay, "Payment implementation channel key (e.g. ALIPAY, WECHAT)")
	serviceName := flag.String("service-name", "", "Published CROS RPC service name (defaults to demo.payment.<CHANNEL>_PaymentService; set to demo.payment.PaymentService for shared-name mode)")
	flag.Parse()

	gateway := payment.NewPaymentGateway(*channel, *serviceName)
	logPrefix := fmt.Sprintf("backend-payment:%s", gateway.Channel())

	log.Printf("[%s] Initializing CROS RPC Payment Instance (contract=%s, published_service=%s)...",
		logPrefix, payment.ContractServiceName, gateway.ServiceName())

	server := crosframe.NewServer(*addr, logPrefix)
	gateway.RegisterHandlers(server)

	if err := server.Start(); err != nil {
		log.Fatalf("[%s] Start failed: %v", logPrefix, err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	log.Printf("[%s] Shutting down...", logPrefix)
	_ = server.Stop()
	log.Printf("[%s] Stopped gracefully.", logPrefix)
}
