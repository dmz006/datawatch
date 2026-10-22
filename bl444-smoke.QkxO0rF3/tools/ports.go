package main

import (
	"fmt"
	"net"
	"os"
)

// Pick two free ports: listen on :0, record port, close, then verify each
// port refuses connections (i.e. nothing else grabbed it).
func freePort() int {
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil { panic(err) }
	p := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return p
}

func verifyFree(port int) bool {
	c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil { return true } // refused => free
	c.Close()
	return false
}

func main() {
	for {
		sse, web := freePort(), freePort()
		if sse != web && sse > 30000 && web > 30000 && verifyFree(sse) && verifyFree(web) {
			fmt.Printf("%d %d\n", sse, web)
			return
		}
	}
	_ = os.Args
}
