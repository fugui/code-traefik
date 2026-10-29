package main

import (
	"flag"
	"fmt"
)

func main() {
	targetAddr := flag.String("addr", "127.0.0.1:9001", "CROS RPC target address of SUT (Order Center)")
	flag.Parse()

	fmt.Printf("Starting CROS RPC Benchmark Client connected to %s...\n", *targetAddr)
	client := NewTUIClient(*targetAddr)
	client.Run()
}
