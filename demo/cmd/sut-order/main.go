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
	flag.Parse()

	log.Printf("[sut-order] Initializing SUT CROS RPC Order Gateway on %s...", *addr)
	log.Printf("[sut-order] Downstream Account Service: %s, Inventory Service: %s", *accountAddr, *inventoryAddr)

	orchestrator := NewOrderOrchestrator(*accountAddr, *inventoryAddr)
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
	_ = orchestrator.accountCli.Close()
	_ = orchestrator.inventoryCli.Close()
	log.Printf("[sut-order] SUT Order Gateway stopped gracefully.")
}
