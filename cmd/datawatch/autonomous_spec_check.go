package main

import (
	"fmt"
	"io"
	"os"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// printSpecCheck writes the spec-check findings of a create or revise reply
// to out, one line per finding (severity, rule, the exact phrase and the
// suggested rewording), using the formatter every surface shares. A clean
// spec prints nothing.
func printSpecCheck(out io.Writer, resp []byte) {
	for _, line := range autonomous.FormatSpecFindings(autonomous.SpecCheckFromReply(resp)) {
		_, _ = fmt.Fprintln(out, "spec check: "+line)
	}
}

// daemonJSONWithSpecCheck is daemonJSON for a create or revise call: it
// prints the reply, then the spec-check findings after it.
func daemonJSONWithSpecCheck(method, path string, body any) error {
	resp, err := daemonJSONBody(method, path, body)
	if err != nil {
		return err
	}
	if len(resp) > 0 {
		prettyPrint(resp)
	} else {
		fmt.Println("ok")
	}
	printSpecCheck(os.Stderr, resp)
	return nil
}
