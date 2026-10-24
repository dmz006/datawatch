package autonomous

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeWebhookResolver answers lookups from a function so a test can change
// the answer between calls (DNS rebinding).
type fakeWebhookResolver struct {
	mu    sync.Mutex
	calls int
	fn    func(call int, host string) ([]net.IPAddr, error)
}

func (f *fakeWebhookResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.mu.Unlock()
	return f.fn(n, host)
}

func ipAddrs(ips ...string) []net.IPAddr {
	out := make([]net.IPAddr, 0, len(ips))
	for _, s := range ips {
		out = append(out, net.IPAddr{IP: net.ParseIP(s)})
	}
	return out
}

func TestWebhookGuardHostAllowed(t *testing.T) {
	tests := []struct {
		name  string
		host  string
		allow []string
		want  bool
	}{
		{"exact public host", "hooks.example.net", []string{"hooks.example.net"}, true},
		{"unlisted host", "evil.example.net", []string{"hooks.example.net"}, false},
		{"empty allow-list", "hooks.example.net", nil, false},
		{"empty slice", "hooks.example.net", []string{}, false},
		{"blank entries only", "hooks.example.net", []string{"", "  "}, false},
		{"case-insensitive host", "HOOKS.Example.NET", []string{"hooks.example.net"}, true},
		{"case-insensitive entry", "hooks.example.net", []string{"Hooks.EXAMPLE.net"}, true},
		{"trailing dot on host", "hooks.example.net.", []string{"hooks.example.net"}, true},
		{"trailing dot on entry", "hooks.example.net", []string{"hooks.example.net."}, true},
		{"idna unicode host vs punycode entry", "bücher.example", []string{"xn--bcher-kva.example"}, true},
		{"idna punycode host vs unicode entry", "xn--bcher-kva.example", []string{"bücher.example"}, true},
		{"bare entry matches any port", "hooks.example.net:8443", []string{"hooks.example.net"}, true},
		{"port entry matches that port", "hooks.example.net:8443", []string{"hooks.example.net:8443"}, true},
		{"port entry rejects other port", "hooks.example.net:9000", []string{"hooks.example.net:8443"}, false},
		{"port entry rejects portless host", "hooks.example.net", []string{"hooks.example.net:443"}, false},
		{"no wildcard", "a.example.net", []string{"*.example.net"}, false},
		{"literal star host never matches", "*.example.net", []string{"*.example.net"}, false},
		{"no suffix match", "sub.hooks.example.net", []string{"hooks.example.net"}, false},
		{"no prefix match", "hooks.example.net.evil.test", []string{"hooks.example.net"}, false},
		{"ipv4 literal", "10.0.0.5", []string{"10.0.0.5"}, true},
		{"ipv4 literal with port", "10.0.0.5:8080", []string{"10.0.0.5:8080"}, true},
		{"ipv6 literal bracketed with port", "[::1]:8443", []string{"[::1]:8443"}, true},
		{"ipv6 literal bare entry", "[::1]:8443", []string{"::1"}, true},
		{"ipv6 compressed forms equal", "[0:0:0:0:0:0:0:1]:80", []string{"::1"}, true},
		{"obfuscated decimal never matches", "2130706433", []string{"127.0.0.1"}, false},
		{"obfuscated mapped never matches", "::ffff:127.0.0.1", []string{"127.0.0.1"}, false},
		{"obfuscated entry ignored", "127.0.0.1", []string{"2130706433"}, false},
		{"url as entry ignored", "hooks.example.net", []string{"https://hooks.example.net"}, false},
		{"entry with bad port ignored", "hooks.example.net", []string{"hooks.example.net:0"}, false},
		{"entry with empty port ignored", "hooks.example.net", []string{"hooks.example.net:"}, false},
		{"host with empty port", "hooks.example.net:", []string{"hooks.example.net"}, false},
		{"empty host", "", []string{"hooks.example.net"}, false},
		{"whitespace entry trimmed", "hooks.example.net", []string{"  hooks.example.net  "}, true},
		{"zone id refused", "fe80::1%eth0", []string{"fe80::1"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := WebhookHostAllowed(tc.host, tc.allow); got != tc.want {
				t.Errorf("WebhookHostAllowed(%q, %q) = %v, want %v", tc.host, tc.allow, got, tc.want)
			}
		})
	}
}

func TestWebhookGuardNormaliseHost(t *testing.T) {
	tests := []struct {
		in         string
		want       string
		obfuscated bool
		wantErr    bool
	}{
		{"Example.COM", "example.com", false, false},
		{"example.com.", "example.com", false, false},
		{"example.com..", "", false, true},
		{".example.com", "", false, true},
		{"a..b", "", false, true},
		{"127.0.0.1", "127.0.0.1", false, false},
		{"2130706433", "127.0.0.1", true, false},
		{"0x7f000001", "127.0.0.1", true, false},
		{"0x7f.0.0.1", "127.0.0.1", true, false},
		{"0177.0.0.1", "127.0.0.1", true, false},
		{"017700000001", "127.0.0.1", true, false},
		{"127.1", "127.0.0.1", true, false},
		{"127.0.1", "127.0.0.1", true, false},
		{"169.254.43518", "169.254.169.254", true, false},
		{"0xa9fea9fe", "169.254.169.254", true, false},
		{"0x", "0.0.0.0", true, false},
		{"256.0.0.1", "", false, true},
		{"1.2.3.4.5", "", false, true},
		{"4294967296", "", false, true},
		{"08.0.0.1", "", false, true}, // 8 is not an octal digit
		{"::1", "::1", false, false},
		{"[::1]", "::1", false, false},
		{"0:0:0:0:0:0:0:1", "::1", false, false},
		{"::ffff:127.0.0.1", "127.0.0.1", true, false},
		{"::ffff:7f00:1", "127.0.0.1", true, false},
		{"fe80::1%eth0", "", false, true},
		{"not:an:ip", "", false, true},
		{" example.com", "", false, true},
		{"", "", false, true},
		{"exa mple.com", "", false, true},
		{"example.com/x", "", false, true},
		{"user@example.com", "", false, true},
		{"bad\x01host", "", false, true},
		{"bücher.example", "xn--bcher-kva.example", false, false},
	}
	for _, tc := range tests {
		got, obf, err := normaliseWebhookHost(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("normaliseWebhookHost(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if err != nil {
			continue
		}
		if got != tc.want || obf != tc.obfuscated {
			t.Errorf("normaliseWebhookHost(%q) = (%q, %v), want (%q, %v)", tc.in, got, obf, tc.want, tc.obfuscated)
		}
	}
}

func TestWebhookGuardValidateURL(t *testing.T) {
	public := []string{"hooks.example.net"}
	tests := []struct {
		name     string
		raw      string
		allow    []string
		wantErr  string // substring of the error; "" means accepted
		wantHost string // canonical u.Host when accepted
	}{
		{"allow-listed public https", "https://hooks.example.net/datawatch", public, "", "hooks.example.net"},
		{"upper-case host and trailing dot", "HTTPS://Hooks.Example.NET./x?y=1", public, "", "hooks.example.net"},
		{"explicit port kept", "https://hooks.example.net:8443/x", public, "", "hooks.example.net:8443"},
		{"port-qualified entry matches default port", "https://hooks.example.net/x", []string{"hooks.example.net:443"}, "", "hooks.example.net"},
		{"port-qualified entry wrong port", "https://hooks.example.net:444/x", []string{"hooks.example.net:443"}, "not on the webhook allow-list", ""},
		{"unlisted host", "https://evil.example.net/x", public, "not on the webhook allow-list", ""},
		{"empty allow-list", "https://hooks.example.net/x", nil, "allow-list is empty", ""},
		{"empty url", "", public, "empty", ""},
		{"blank url", "   ", public, "empty", ""},
		{"unparsable", "https://%zz/", public, "could not be parsed", ""},
		{"no scheme", "hooks.example.net/x", public, "scheme", ""},
		{"ftp scheme", "ftp://hooks.example.net/x", public, "scheme", ""},
		{"file scheme", "file:///etc/passwd", public, "scheme", ""},
		{"opaque url", "https:hooks.example.net", public, "form", ""},
		{"no host", "https:///path", public, "no host", ""},
		{"userinfo user", "https://bob@hooks.example.net/x", public, "user name or password", ""},
		{"userinfo with password", "https://bob:hunter2@hooks.example.net/x", public, "user name or password", ""},
		{"http not allow-listed", "http://evil.example.net/x", public, "http is accepted only for an allow-listed host", ""},
		{"http allow-listed host", "http://hooks.example.net/x", public, "", "hooks.example.net"},
		{"http with port entry", "http://hooks.example.net:8080/x", []string{"hooks.example.net:8080"}, "", "hooks.example.net:8080"},
		{"http default port vs https entry", "http://hooks.example.net/x", []string{"hooks.example.net:443"}, "http is accepted only", ""},
		{"bad port", "https://hooks.example.net:99999/x", public, "port", ""},
		{"zero port", "https://hooks.example.net:0/x", public, "port", ""},
		// Loopback / private / link-local literals: refused unless listed.
		{"127.0.0.1 unlisted", "https://127.0.0.1/x", public, "not on the webhook allow-list", ""},
		{"::1 unlisted", "https://[::1]/x", public, "not on the webhook allow-list", ""},
		{"localhost unlisted", "https://localhost/x", public, "not on the webhook allow-list", ""},
		{"10.x unlisted", "https://10.1.2.3/x", public, "not on the webhook allow-list", ""},
		{"172.16.x unlisted", "https://172.16.5.5/x", public, "not on the webhook allow-list", ""},
		{"192.168.x unlisted", "https://192.168.1.1/x", public, "not on the webhook allow-list", ""},
		{"metadata unlisted", "https://169.254.169.254/latest", public, "not on the webhook allow-list", ""},
		{"metadata v6 unlisted", "https://[fd00:ec2::254]/latest", public, "not on the webhook allow-list", ""},
		{"fe80 unlisted", "https://[fe80::1]/x", public, "not on the webhook allow-list", ""},
		{"0.0.0.0 unlisted", "https://0.0.0.0/x", public, "not on the webhook allow-list", ""},
		{"cgnat unlisted", "https://100.64.1.1/x", public, "not on the webhook allow-list", ""},
		// The same hosts accepted when explicitly allow-listed.
		{"127.0.0.1 listed", "https://127.0.0.1:8443/x", []string{"127.0.0.1"}, "", "127.0.0.1:8443"},
		{"::1 listed", "https://[::1]:8443/x", []string{"::1"}, "", "[::1]:8443"},
		{"::1 listed no port", "https://[::1]/x", []string{"::1"}, "", "[::1]"},
		{"localhost listed", "https://localhost/x", []string{"localhost"}, "", "localhost"},
		{"10.x listed", "https://10.1.2.3/x", []string{"10.1.2.3"}, "", "10.1.2.3"},
		{"172.16.x listed", "https://172.16.5.5/x", []string{"172.16.5.5"}, "", "172.16.5.5"},
		{"192.168.x listed", "https://192.168.1.1/x", []string{"192.168.1.1"}, "", "192.168.1.1"},
		{"fe80 listed", "https://[fe80::1]/x", []string{"fe80::1"}, "", "[fe80::1]"},
		{"0.0.0.0 listed", "https://0.0.0.0/x", []string{"0.0.0.0"}, "", "0.0.0.0"},
		{"cgnat listed", "https://100.64.1.1/x", []string{"100.64.1.1"}, "", "100.64.1.1"},
		// Metadata stays refused even when listed (proposal recommendation).
		{"metadata listed", "https://169.254.169.254/latest", []string{"169.254.169.254"}, "cloud metadata address", ""},
		{"metadata v6 listed", "https://[fd00:ec2::254]/latest", []string{"fd00:ec2::254"}, "cloud metadata address (IPv6)", ""},
		// Obfuscated forms are refused outright, even with the canonical entry.
		{"mapped ipv6 loopback", "https://[::ffff:127.0.0.1]/x", public, "non-canonical", ""},
		{"mapped ipv6 loopback with entry", "https://[::ffff:127.0.0.1]/x", []string{"127.0.0.1"}, "non-canonical", ""},
		{"decimal loopback", "https://2130706433/x", public, "non-canonical", ""},
		{"decimal loopback with entry", "http://2130706433/x", []string{"127.0.0.1"}, "non-canonical", ""},
		{"hex loopback", "https://0x7f000001/x", public, "non-canonical", ""},
		{"octal loopback", "https://0177.0.0.1/x", public, "non-canonical", ""},
		{"short loopback", "https://127.1/x", public, "non-canonical", ""},
		{"decimal metadata", "https://2852039166/x", []string{"169.254.169.254"}, "non-canonical", ""},
		{"numeric nonsense", "https://256.256.256.256/x", public, "not valid", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u, err := ValidateWebhookURL(tc.raw, tc.allow)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateWebhookURL(%q) unexpected error: %v", tc.raw, err)
				}
				if u.Host != tc.wantHost {
					t.Errorf("Host = %q, want %q", u.Host, tc.wantHost)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateWebhookURL(%q) accepted, want error containing %q", tc.raw, tc.wantErr)
			}
			if !errors.Is(err, ErrWebhookGuard) {
				t.Errorf("error does not wrap ErrWebhookGuard: %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestWebhookGuardValidateURLNeverEchoesSecrets(t *testing.T) {
	cases := []string{
		"https://bob:hunter2@hooks.example.net/x?token=s3cr3t",
		"https://hooks.example.net/x?token=s3cr3t#frag",
		"https://evil.example.net/x?token=s3cr3t",
		"ftp://bob:hunter2@hooks.example.net/x?token=s3cr3t",
		"https://%zz/?token=s3cr3t",
		"https://[::ffff:127.0.0.1]/x?token=s3cr3t",
		"https://bob:hunter2@169.254.169.254/x?token=s3cr3t",
	}
	for _, raw := range cases {
		_, err := ValidateWebhookURL(raw, []string{"hooks.example.net"})
		if raw == "https://hooks.example.net/x?token=s3cr3t#frag" {
			if err != nil {
				t.Fatalf("expected %q to be accepted: %v", raw, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%q: expected an error", raw)
			continue
		}
		for _, secret := range []string{"hunter2", "s3cr3t", "bob"} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("%q: error leaks %q: %v", raw, secret, err)
			}
		}
	}
}

func TestWebhookGuardBlockedIPReason(t *testing.T) {
	tests := []struct {
		ip       string
		allowed  bool
		wantText string // "" means not blocked
	}{
		{"8.8.8.8", false, ""},
		{"2606:4700:4700::1111", false, ""},
		{"127.0.0.1", false, "loopback"},
		{"127.5.5.5", false, "loopback"},
		{"::1", false, "loopback"},
		{"::ffff:127.0.0.1", false, "loopback"},
		{"10.0.0.1", false, "private-range"},
		{"172.16.0.1", false, "private-range"},
		{"172.31.255.255", false, "private-range"},
		{"172.32.0.1", false, ""},
		{"192.168.0.1", false, "private-range"},
		{"fd12:3456::1", false, "private-range"},
		{"169.254.1.1", false, "link-local"},
		{"fe80::1", false, "link-local"},
		{"ff02::1", false, "link-local"},
		{"ff01::1", false, "link-local"},
		{"224.0.0.1", false, "link-local"},
		{"239.1.1.1", false, "multicast"},
		{"ff0e::1", false, "multicast"},
		{"0.0.0.0", false, "unspecified"},
		{"::", false, "unspecified"},
		{"100.64.0.1", false, "CGNAT"},
		{"100.127.255.255", false, "CGNAT"},
		{"100.63.255.255", false, ""},
		{"100.128.0.0", false, ""},
		{"169.254.169.254", false, "cloud metadata address"},
		{"fd00:ec2::254", false, "cloud metadata address (IPv6)"},
		{"::ffff:169.254.169.254", false, "cloud metadata address"},
		{"64:ff9b::a9fe:a9fe", false, "cloud metadata address"},
		{"64:ff9b::7f00:1", false, "loopback"},
		{"64:ff9b::808:808", false, ""},
		{"64:ff9b:1::1", false, ""},
		// Explicitly allow-listed: everything but metadata passes.
		{"127.0.0.1", true, ""},
		{"::1", true, ""},
		{"10.0.0.1", true, ""},
		{"172.16.0.1", true, ""},
		{"192.168.0.1", true, ""},
		{"fe80::1", true, ""},
		{"0.0.0.0", true, ""},
		{"100.64.0.1", true, ""},
		{"fd12:3456::1", true, ""},
		{"169.254.169.254", true, "cloud metadata address"},
		{"fd00:ec2::254", true, "cloud metadata address (IPv6)"},
		{"::ffff:169.254.169.254", true, "cloud metadata address"},
		{"64:ff9b::a9fe:a9fe", true, "cloud metadata address"},
	}
	for _, tc := range tests {
		ip := net.ParseIP(tc.ip)
		if ip == nil {
			t.Fatalf("bad test ip %q", tc.ip)
		}
		got := webhookBlockedIPReason(ip, tc.allowed)
		if tc.wantText == "" {
			if got != "" {
				t.Errorf("webhookBlockedIPReason(%s, %v) = %q, want not blocked", tc.ip, tc.allowed, got)
			}
			continue
		}
		if !strings.Contains(got, tc.wantText) {
			t.Errorf("webhookBlockedIPReason(%s, %v) = %q, want it to contain %q", tc.ip, tc.allowed, got, tc.wantText)
		}
	}
	if got := webhookBlockedIPReason(nil, true); got == "" {
		t.Error("a nil ip must be refused")
	}
	if !webhookMetadataAlwaysRefused {
		t.Error("the proposal recommends refusing metadata addresses even when allow-listed")
	}
}

// ---------------------------------------------------------------------------
// Guarded client
// ---------------------------------------------------------------------------

func newGuardedGet(t *testing.T, client *http.Client, target string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	return client.Do(req)
}

func TestWebhookGuardClientLoopbackOnlyWhenAllowListed(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	hostPort := u.Host // 127.0.0.1:<port>

	// Not allow-listed: refused, server never reached.
	for _, allow := range [][]string{nil, {}, {"hooks.example.net"}, {"127.0.0.1:1"}, {"localhost"}} {
		c := NewWebhookHTTPClient(allow, 2*time.Second)
		resp, err := newGuardedGet(t, c, srv.URL)
		if err == nil {
			_ = resp.Body.Close()
			t.Fatalf("allow=%q: loopback server reached without being allow-listed", allow)
		}
		if !errors.Is(err, ErrWebhookGuard) {
			t.Errorf("allow=%q: error does not wrap ErrWebhookGuard: %v", allow, err)
		}
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("server was hit %d times while refused", n)
	}

	// Allow-listed by literal IP (any port) and by host:port: reachable.
	for _, allow := range [][]string{{"127.0.0.1"}, {hostPort}, {"hooks.example.net", "127.0.0.1"}} {
		c := NewWebhookHTTPClient(allow, 2*time.Second)
		resp, err := newGuardedGet(t, c, srv.URL)
		if err != nil {
			t.Fatalf("allow=%q: %v", allow, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != 200 || string(body) != "ok" {
			t.Errorf("allow=%q: got %d %q", allow, resp.StatusCode, body)
		}
	}
	if n := hits.Load(); n != 3 {
		t.Errorf("server hits = %d, want 3", n)
	}
}

func TestWebhookGuardClientAllowListedNameResolvesViaInjectedResolver(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "named")
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	_, port, _ := net.SplitHostPort(u.Host)

	r := &fakeWebhookResolver{fn: func(_ int, host string) ([]net.IPAddr, error) {
		if host != "hooks.example.test" {
			t.Errorf("resolver asked for %q", host)
		}
		return ipAddrs("127.0.0.1"), nil
	}}
	c := newWebhookHTTPClient([]string{"hooks.example.test"}, 2*time.Second, r)
	resp, err := newGuardedGet(t, c, "http://hooks.example.test:"+port+"/x")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "named" {
		t.Errorf("body = %q", body)
	}

	// A name that is not on the list is refused before any lookup.
	r2 := &fakeWebhookResolver{fn: func(int, string) ([]net.IPAddr, error) {
		t.Error("resolver must not be consulted for a name that is not allow-listed")
		return nil, nil
	}}
	c2 := newWebhookHTTPClient([]string{"hooks.example.test"}, 2*time.Second, r2)
	if _, err := newGuardedGet(t, c2, "http://other.example.test:"+port+"/x"); !errors.Is(err, ErrWebhookGuard) {
		t.Errorf("unlisted name: want ErrWebhookGuard, got %v", err)
	}
}

func TestWebhookGuardClientDNSRebindingRefusedAtDialTime(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	_, port, _ := net.SplitHostPort(u.Host)

	// First lookup (creation / first delivery) is safe; every later lookup
	// points at the cloud metadata address.
	r := &fakeWebhookResolver{fn: func(call int, _ string) ([]net.IPAddr, error) {
		if call == 1 {
			return ipAddrs("127.0.0.1"), nil
		}
		return ipAddrs("169.254.169.254"), nil
	}}
	c := newWebhookHTTPClient([]string{"hooks.example.test"}, 2*time.Second, r)
	target := "http://hooks.example.test:" + port + "/x"

	resp, err := newGuardedGet(t, c, target)
	if err != nil {
		t.Fatalf("first attempt should succeed: %v", err)
	}
	_ = resp.Body.Close()

	resp, err = newGuardedGet(t, c, target)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("second attempt reached a rebound address")
	}
	if !errors.Is(err, ErrWebhookGuard) || !strings.Contains(err.Error(), "cloud metadata address") {
		t.Errorf("want a guard refusal naming the metadata address, got %v", err)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("server hits = %d, want 1", n)
	}
}

func TestWebhookGuardClientRebindingFallsThroughBlockedAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "second")
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	_, port, _ := net.SplitHostPort(u.Host)

	// The blocked address is skipped by the Control hook, the next works.
	r := &fakeWebhookResolver{fn: func(int, string) ([]net.IPAddr, error) {
		return ipAddrs("169.254.169.254", "127.0.0.1"), nil
	}}
	c := newWebhookHTTPClient([]string{"hooks.example.test"}, 2*time.Second, r)
	resp, err := newGuardedGet(t, c, "http://hooks.example.test:"+port+"/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "second" {
		t.Errorf("body = %q", body)
	}

	// Only blocked addresses: refused.
	r2 := &fakeWebhookResolver{fn: func(int, string) ([]net.IPAddr, error) {
		return ipAddrs("169.254.169.254", "fd00:ec2::254"), nil
	}}
	c2 := newWebhookHTTPClient([]string{"hooks.example.test"}, 2*time.Second, r2)
	if _, err := newGuardedGet(t, c2, "http://hooks.example.test:"+port+"/"); !errors.Is(err, ErrWebhookGuard) {
		t.Errorf("want ErrWebhookGuard, got %v", err)
	}
}

func TestWebhookGuardClientMetadataLiteralRefusedEvenWhenAllowListed(t *testing.T) {
	c := NewWebhookHTTPClient([]string{"169.254.169.254", "fd00:ec2::254"}, time.Second)
	for _, target := range []string{"http://169.254.169.254/latest/meta-data", "http://[fd00:ec2::254]/latest"} {
		_, err := newGuardedGet(t, c, target)
		if !errors.Is(err, ErrWebhookGuard) {
			t.Errorf("%s: want ErrWebhookGuard, got %v", target, err)
		}
	}
}

func TestWebhookGuardClientRefusesObfuscatedAndResolverFailures(t *testing.T) {
	r := &fakeWebhookResolver{fn: func(int, string) ([]net.IPAddr, error) {
		t.Error("resolver must not be consulted for an obfuscated host")
		return ipAddrs("127.0.0.1"), nil
	}}
	c := newWebhookHTTPClient([]string{"127.0.0.1"}, time.Second, r)
	if _, err := newGuardedGet(t, c, "http://2130706433:9/"); !errors.Is(err, ErrWebhookGuard) {
		t.Errorf("decimal host: want ErrWebhookGuard, got %v", err)
	}

	boom := errors.New("lookup failed")
	r2 := &fakeWebhookResolver{fn: func(int, string) ([]net.IPAddr, error) { return nil, boom }}
	c2 := newWebhookHTTPClient([]string{"hooks.example.test"}, time.Second, r2)
	if _, err := newGuardedGet(t, c2, "http://hooks.example.test:9/"); !errors.Is(err, boom) {
		t.Errorf("resolver error not surfaced: %v", err)
	}

	r3 := &fakeWebhookResolver{fn: func(int, string) ([]net.IPAddr, error) { return nil, nil }}
	c3 := newWebhookHTTPClient([]string{"hooks.example.test"}, time.Second, r3)
	if _, err := newGuardedGet(t, c3, "http://hooks.example.test:9/"); !errors.Is(err, ErrWebhookGuard) {
		t.Errorf("empty answer: want ErrWebhookGuard, got %v", err)
	}
}

func TestWebhookGuardClientRedirectToPrivateAddressNotFollowed(t *testing.T) {
	var internalHits atomic.Int32
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		internalHits.Add(1)
		_, _ = io.WriteString(w, "internal secret")
	}))
	defer internal.Close()
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL+"/admin", http.StatusFound)
	}))
	defer front.Close()

	// Even with the redirect target's host allow-listed, nothing follows it.
	c := NewWebhookHTTPClient([]string{"127.0.0.1"}, 2*time.Second)
	resp, err := newGuardedGet(t, c, front.URL)
	if err != nil {
		t.Fatalf("a redirect response must be returned, not an error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, internal.URL) {
		t.Errorf("Location = %q", loc)
	}
	if n := internalHits.Load(); n != 0 {
		t.Errorf("redirect was followed (%d hits)", n)
	}
	if c.CheckRedirect == nil {
		t.Fatal("CheckRedirect must be set")
	}
	if err := c.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Errorf("CheckRedirect = %v, want http.ErrUseLastResponse", err)
	}
}

func TestWebhookGuardClientConfiguration(t *testing.T) {
	c := NewWebhookHTTPClient([]string{"hooks.example.net"}, 3*time.Second)
	if c.Timeout != 3*time.Second {
		t.Errorf("Timeout = %v", c.Timeout)
	}
	if d := NewWebhookHTTPClient(nil, 0).Timeout; d != webhookDefaultAttemptTimeout {
		t.Errorf("default timeout = %v, want %v", d, webhookDefaultAttemptTimeout)
	}
	if d := NewWebhookHTTPClient(nil, -time.Second).Timeout; d != webhookDefaultAttemptTimeout {
		t.Errorf("negative timeout = %v, want default", d)
	}
	ct, ok := c.Transport.(cappedBodyTransport)
	if !ok {
		t.Fatalf("Transport is %T, want cappedBodyTransport", c.Transport)
	}
	tr, ok := ct.next.(*http.Transport)
	if !ok {
		t.Fatalf("inner transport is %T", ct.next)
	}
	if tr.Proxy != nil {
		t.Error("Transport.Proxy must be nil (environment proxies are never used)")
	}
	if !tr.DisableKeepAlives {
		t.Error("keep-alives must be off so every attempt is re-checked at dial time")
	}
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.MinVersion == 0 {
		t.Error("a TLS minimum version must be set")
	}
}

func TestWebhookGuardClientIgnoresProxyEnvironment(t *testing.T) {
	var proxied atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		proxied.Add(1)
	}))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("http_proxy", proxy.URL)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "direct")
	}))
	defer srv.Close()
	c := NewWebhookHTTPClient([]string{"127.0.0.1"}, 2*time.Second)
	resp, err := newGuardedGet(t, c, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "direct" || proxied.Load() != 0 {
		t.Errorf("body=%q proxied=%d: the proxy environment must be ignored", body, proxied.Load())
	}
}

func TestWebhookGuardClientResponseBodyCapped(t *testing.T) {
	big := strings.Repeat("A", webhookMaxResponseBytes*3)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, big)
	}))
	defer srv.Close()
	c := NewWebhookHTTPClient([]string{"127.0.0.1"}, 5*time.Second)
	resp, err := newGuardedGet(t, c, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if len(body) != webhookMaxResponseBytes {
		t.Errorf("read %d bytes, want the cap %d", len(body), webhookMaxResponseBytes)
	}
}

func TestWebhookGuardClientPostWithBodyAndHeaders(t *testing.T) {
	var gotBody, gotSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotSig = r.Header.Get(WebhookSignatureHeader)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := NewWebhookHTTPClient([]string{"127.0.0.1"}, 2*time.Second)
	body := []byte(`{"version":1}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(string(body)))
	sig := SignWebhookBody([]byte("k"), time.Unix(1700000000, 0), body)
	req.Header.Set(WebhookSignatureHeader, sig)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || gotBody != string(body) || gotSig != sig {
		t.Errorf("status=%d body=%q sig=%q", resp.StatusCode, gotBody, gotSig)
	}
}

func TestWebhookGuardClientAllowListIsCopied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	allow := []string{"127.0.0.1"}
	c := NewWebhookHTTPClient(allow, 2*time.Second)
	allow[0] = "hooks.example.net" // caller mutates its slice afterwards
	resp, err := newGuardedGet(t, c, srv.URL)
	if err != nil {
		t.Fatalf("the client must keep its own copy of the allow-list: %v", err)
	}
	_ = resp.Body.Close()
}
