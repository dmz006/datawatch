// Package dns implements a DNS tunneling communication backend for datawatch.
// Commands are encoded in DNS TXT queries; responses are returned as fragmented TXT records.
// Authentication uses HMAC-SHA256 with a shared secret. Nonce replay protection is included.
//
// Query format: <nonce8>.<hmac8>.<b64-label-1>.<b64-label-2>...cmd.<domain>
// Response format: TXT records with "0/N:<chunk>", "1/N:<chunk>", ...
package dns

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

const (
	maxLabelLen = 60  // max bytes per DNS label (leaving room for overhead)
	cmdLiteral  = "cmd"
)

var b64Encoding = base64.RawURLEncoding

// EncodeQuery encodes a command as a DNS TXT query name.
// Returns the fully qualified domain name to query.
func EncodeQuery(command, secret, domain string) (string, error) {
	// Generate 8-char hex nonce
	nonceBytes := make([]byte, 4)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	nonce := hex.EncodeToString(nonceBytes)

	// Base64url-encode the command
	payload := b64Encoding.EncodeToString([]byte(command))

	// Compute HMAC-SHA256(nonce + payload, secret), take first 8 hex chars
	mac := computeHMAC(nonce+payload, secret)

	// Split payload into labels of max maxLabelLen chars
	var labels []string
	for len(payload) > 0 {
		end := maxLabelLen
		if end > len(payload) {
			end = len(payload)
		}
		labels = append(labels, payload[:end])
		payload = payload[end:]
	}

	// Assemble: nonce.hmac.label1.label2...cmd.domain
	parts := []string{nonce, mac}
	parts = append(parts, labels...)
	parts = append(parts, cmdLiteral)
	parts = append(parts, domain)

	return strings.Join(parts, ".") + ".", nil
}

// DecodeQuery decodes and authenticates a DNS query name.
// Returns the plaintext command if HMAC verification passes.
func DecodeQuery(qname, domain, secret string) (string, error) {
	// Strip trailing dot and domain suffix
	qname = strings.TrimSuffix(qname, ".")
	domain = strings.TrimSuffix(domain, ".")
	if !strings.HasSuffix(qname, "."+domain) {
		return "", fmt.Errorf("domain mismatch")
	}
	prefix := strings.TrimSuffix(qname, "."+domain)

	// Split into labels
	labels := strings.Split(prefix, ".")
	if len(labels) < 3 {
		return "", fmt.Errorf("too few labels")
	}

	// Last label must be "cmd"
	if labels[len(labels)-1] != cmdLiteral {
		return "", fmt.Errorf("missing cmd literal")
	}

	nonce := labels[0]
	macReceived := labels[1]
	payloadLabels := labels[2 : len(labels)-1]
	payload := strings.Join(payloadLabels, "")

	// Verify HMAC
	macExpected := computeHMAC(nonce+payload, secret)
	if !hmac.Equal([]byte(macReceived), []byte(macExpected)) {
		return "", fmt.Errorf("HMAC verification failed")
	}

	// Decode base64url payload
	command, err := b64Encoding.DecodeString(payload)
	if err != nil {
		return "", fmt.Errorf("decode payload: %w", err)
	}

	return string(command), nil
}

// EncodeResponse encodes a response string into fragmented TXT records.
// Each record is prefixed with "idx/total:" for reassembly.
func EncodeResponse(response string, maxSize int) []string {
	if maxSize <= 0 {
		maxSize = 512
	}
	// Truncate response to maxSize
	if len(response) > maxSize {
		response = response[:maxSize]
	}

	encoded := b64Encoding.EncodeToString([]byte(response))

	// Fragment into TXT-safe chunks (max 250 chars per record to leave room for prefix)
	const chunkSize = 240
	var chunks []string
	for len(encoded) > 0 {
		end := chunkSize
		if end > len(encoded) {
			end = len(encoded)
		}
		chunks = append(chunks, encoded[:end])
		encoded = encoded[end:]
	}

	if len(chunks) == 0 {
		return []string{"0/1:"}
	}

	total := len(chunks)
	records := make([]string, total)
	for i, chunk := range chunks {
		records[i] = fmt.Sprintf("%d/%d:%s", i, total, chunk)
	}
	return records
}

// DecodeResponse reassembles fragmented TXT records into the original response.
func DecodeResponse(records []string) (string, error) {
	if len(records) == 0 {
		return "", nil
	}

	type fragment struct {
		idx   int
		total int
		data  string
	}
	var frags []fragment
	for _, r := range records {
		colIdx := strings.Index(r, ":")
		if colIdx < 0 {
			continue
		}
		prefix := r[:colIdx]
		data := r[colIdx+1:]
		parts := strings.SplitN(prefix, "/", 2)
		if len(parts) != 2 {
			continue
		}
		var idx, total int
		fmt.Sscanf(parts[0], "%d", &idx)
		fmt.Sscanf(parts[1], "%d", &total)
		frags = append(frags, fragment{idx: idx, total: total, data: data})
	}

	sort.Slice(frags, func(i, j int) bool { return frags[i].idx < frags[j].idx })

	var sb strings.Builder
	for _, f := range frags {
		sb.WriteString(f.data)
	}

	decoded, err := b64Encoding.DecodeString(sb.String())
	if err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return string(decoded), nil
}

// computeHMAC returns the first 8 hex chars of HMAC-SHA256(message, secret).
func computeHMAC(message, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))[:8]
}
