package dns

import (
	"fmt"
	"time"

	mdns "github.com/miekg/dns"

	"github.com/dmz006/datawatch/internal/config"
)

// Client sends commands to a remote datawatch DNS channel server via DNS TXT queries.
type Client struct {
	cfg config.DNSChannelConfig
}

// NewClient creates a DNS channel client.
func NewClient(cfg config.DNSChannelConfig) *Client {
	return &Client{cfg: cfg}
}

// Execute sends a command and returns the response.
func (c *Client) Execute(command string) (string, error) {
	upstream := c.cfg.Upstream
	if upstream == "" {
		return "", fmt.Errorf("dns client: upstream resolver not configured")
	}

	// Encode the command as a DNS TXT query
	qname, err := EncodeQuery(command, c.cfg.Secret, c.cfg.Domain)
	if err != nil {
		return "", fmt.Errorf("encode query: %w", err)
	}

	// Build DNS query message
	msg := new(mdns.Msg)
	msg.SetQuestion(qname, mdns.TypeTXT)
	msg.RecursionDesired = true

	// Send query
	client := &mdns.Client{
		Timeout: 15 * time.Second,
	}
	resp, _, err := client.Exchange(msg, upstream)
	if err != nil {
		return "", fmt.Errorf("dns query failed: %w", err)
	}

	if resp.Rcode == mdns.RcodeRefused {
		return "", fmt.Errorf("dns server refused query (bad HMAC or replayed nonce)")
	}
	if resp.Rcode != mdns.RcodeSuccess {
		return "", fmt.Errorf("dns error: %s", mdns.RcodeToString[resp.Rcode])
	}

	// Extract TXT records
	var records []string
	for _, rr := range resp.Answer {
		if txt, ok := rr.(*mdns.TXT); ok {
			records = append(records, txt.Txt...)
		}
	}

	if len(records) == 0 {
		return "", fmt.Errorf("no TXT records in response")
	}

	// Decode fragmented response
	return DecodeResponse(records)
}
