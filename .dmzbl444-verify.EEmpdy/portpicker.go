package main

import (
	"fmt"
	"net"
)

func main() {
	for i := 0; i < 2; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			panic(err)
		}
		fmt.Println(l.Addr().(*net.TCPAddr).Port)
		l.Close()
	}
}
