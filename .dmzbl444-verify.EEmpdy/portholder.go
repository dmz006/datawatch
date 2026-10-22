package main

import (
	"fmt"
	"net"
	"os"
	"time"
)

func main() {
	addr := os.Args[1]
	l, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Println("holder-bind-error:", err)
		os.Exit(1)
	}
	defer l.Close()
	time.Sleep(15 * time.Second)
	fmt.Println("holder: freed", addr)
}
