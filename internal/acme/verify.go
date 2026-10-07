package acme

import (
	"context"
	"net"
	"net/http"
	"time"
)

// VerifyResult is the pre-flight check result for Manager.Verify — meant
// to catch misconfiguration (a name that doesn't resolve, an ACME
// directory the daemon can't reach) before an actual order burns a
// rate-limited attempt. It does not perform an ACME order.
type VerifyResult struct {
	OK                 bool              `json:"ok"`
	DirectoryReachable bool              `json:"directory_reachable"`
	DirectoryError     string            `json:"directory_error,omitempty"`
	Domains            map[string]string `json:"domains"` // domain -> resolved address(es) or error
}

// Verify runs the pre-flight checks: DNS resolution for every configured
// domain, and HTTP reachability of the configured ACME directory
// (staging or production per cfg.Endpoint). It deliberately does NOT
// check inbound port-80 reachability from the daemon's own side — that
// has to be validated externally (there's no reliable "can a remote
// Let's Encrypt validator reach my port 80" self-check), so a clean
// Verify() result means "necessary conditions look right," not "port 80
// is definitely open to the internet."
func (m *Manager) Verify(ctx context.Context) VerifyResult {
	res := VerifyResult{Domains: make(map[string]string)}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, directoryURL(m.cfg.Endpoint), nil)
	if err != nil {
		res.DirectoryError = err.Error()
	} else {
		resp, err := client.Do(req)
		if err != nil {
			res.DirectoryError = err.Error()
		} else {
			resp.Body.Close() //nolint:errcheck
			res.DirectoryReachable = resp.StatusCode < 500
		}
	}

	allResolved := true
	resolver := &net.Resolver{}
	for _, d := range m.cfg.Domains {
		rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		addrs, err := resolver.LookupHost(rctx, d)
		cancel()
		if err != nil {
			res.Domains[d] = "resolve error: " + err.Error()
			allResolved = false
			continue
		}
		if len(addrs) == 0 {
			res.Domains[d] = "no addresses returned"
			allResolved = false
			continue
		}
		res.Domains[d] = addrs[0]
	}

	res.OK = res.DirectoryReachable && allResolved
	return res
}
