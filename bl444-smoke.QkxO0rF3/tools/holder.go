package main

import (
	"fmt"
	"net"
	"os"
	"time"
)

// Bind 127.0.0.1:<port>, hold for the given duration, then close.
func main() {
	port := os.Args[1]
	hold, err := time.ParseDuration(os.Args[2])
	if err != nil { hold = 6 * time.Second }
	addr := "127.0.0.1:" + port
	ln, err := net.Listen("tcp", addr)
	if err != nil { fmt.Fprintln(os.Stderr, "holder bind:", err); os.Exit(1) }
	fmt.Printf("holder: holding %s for %s\n", addr, hold)
	time.Sleep(hold)
	ln.Close()
	fmt.Println("holder: released")
}
