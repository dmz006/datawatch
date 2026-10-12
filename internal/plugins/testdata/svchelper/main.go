// Command svchelper is a tiny managed-service stand-in used by the
// ServiceSupervisor tests. It lives under testdata so `go build ./...`
// and `go vet ./...` skip it; the tests build it on demand.
//
// Flags:
//
//	-port N           listen on 127.0.0.1:N (0 = any free port)
//	-port-file PATH   write the bound port to PATH
//	-health MODE      ok (200), fail (500) or never (do not bind at all)
//	-fail-after DUR   with -health=ok, answer 500 once DUR has passed
//	-exit-after DUR   exit on its own after DUR (crash simulation)
//	-exit-code N      exit code used by -exit-after
//	-ignore-sigterm   trap SIGTERM and keep running
//	-term-delay DUR   on SIGTERM, wait DUR and then exit 0
package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

func main() {
	port := flag.Int("port", 0, "port to listen on (0 = any)")
	portFile := flag.String("port-file", "", "file to write the bound port to")
	health := flag.String("health", "ok", "ok | fail | never")
	failAfter := flag.Duration("fail-after", 0, "start failing health checks after this duration")
	exitAfter := flag.Duration("exit-after", 0, "exit after this duration")
	exitCode := flag.Int("exit-code", 0, "exit code used with -exit-after")
	ignoreTerm := flag.Bool("ignore-sigterm", false, "ignore SIGTERM")
	termDelay := flag.Duration("term-delay", 0, "delay before exiting on SIGTERM")
	flag.Parse()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM)

	if *health != "never" {
		ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(*port)))
		if err != nil {
			fmt.Fprintln(os.Stderr, "listen:", err)
			os.Exit(2)
		}
		if *portFile != "" {
			p := ln.Addr().(*net.TCPAddr).Port
			if err := os.WriteFile(*portFile, []byte(strconv.Itoa(p)), 0o600); err != nil {
				fmt.Fprintln(os.Stderr, "port-file:", err)
				os.Exit(2)
			}
		}
		status := http.StatusOK
		if *health == "fail" {
			status = http.StatusInternalServerError
		}
		mux := http.NewServeMux()
		started := time.Now()
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
			if *failAfter > 0 && time.Since(started) >= *failAfter {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(status)
		})
		srv := &http.Server{Handler: mux, ReadHeaderTimeout: time.Second}
		go func() { _ = srv.Serve(ln) }()
	}

	var exitC <-chan time.Time
	if *exitAfter > 0 {
		exitC = time.After(*exitAfter)
	}
	for {
		select {
		case <-exitC:
			os.Exit(*exitCode)
		case <-sigs:
			if *ignoreTerm {
				continue
			}
			time.Sleep(*termDelay)
			os.Exit(0)
		}
	}
}
