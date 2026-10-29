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
	addr := flag.String("addr", "127.0.0.1:9002", "TCP listen address for Account Service")
	flag.Parse()

	log.Printf("[backend-account] Initializing CROS RPC Account Service...")
	store := NewAccountStore()
	server := crosframe.NewServer(*addr, "backend-account")
	store.RegisterHandlers(server)

	if err := server.Start(); err != nil {
		log.Fatalf("[backend-account] Start failed: %v", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	log.Printf("[backend-account] Shutting down...")
	_ = server.Stop()
	log.Printf("[backend-account] Stopped gracefully.")
}
