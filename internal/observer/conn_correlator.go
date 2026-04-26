// Package observer — BL180 Phase 2 (v5.1.0).
//
// conn_correlator.go joins TCP socket tuples observed in /proc/<pid>/net/tcp
// with the envelope-by-pid map so each backend envelope can carry a
// per-caller breakdown (Envelope.Callers []CallerAttribution).
//
// The procfs path is the v5.1.0 baseline. A future patch (BL180 Phase 2
// follow-up, Q1 of the design doc) replaces the procfs scan with two
// new eBPF kprobes (__sys_connect + inet_csk_accept) feeding a shared
// conn_attribution map; the same join logic in this file consumes the
// map and the wire shape stays the same.
//
// Design doc: docs/plans/2026-04-26-bl180-phase2-ebpf-correlation.md
// Operator answers (verbatim, 2026-04-26):
//   Q1 → both kprobes (deferred to follow-up patch — procfs first)
//   Q2 → per-conn attribution map separate from per-byte counters
//   Q3 → Callers []CallerAttribution; loudest derived as Caller
//   Q4 → ALL TCP conns mapped, with kind:backend prominent
//   Q5 → localhost + same-namespace this release; cross-host stays open
//   Q6 → unit tests gate merge; Thor smoke test gates the closure

package observer

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// connRow is one line from /proc/<pid>/net/tcp parsed into typed fields.
type connRow struct {
	LocalIP    net.IP
	LocalPort  uint16
	RemoteIP   net.IP
	RemotePort uint16
	State      uint8 // tcp_states.h: 1 = ESTABLISHED, 2 = SYN_SENT, ...
	Inode      uint64
}

// CorrelateCallers walks the supplied envelopes' PIDs, reads each PID's
// /proc/<pid>/net/tcp, and for every ESTABLISHED outbound connection to
// a port owned by another envelope it emits a CallerAttribution row on
// the *target* envelope. The result is the same envelope list with
// `Callers []` populated where applicable; `Caller` / `CallerKind` are
// re-derived from the loudest entry.
//
// procRoot is "/proc" in production; tests pass a fixture root.
//
// Localhost-only this release (Q5): we keep only attributions where the
// remote IP is in the localhost / loopback / private-bridge ranges.
// Cross-host correlation per Q5(c) needs federation-aware joins and
// stays open per the design doc.
func CorrelateCallers(envelopes []Envelope, procRoot string) []Envelope {
	if len(envelopes) == 0 {
		return envelopes
	}
	if procRoot == "" {
		procRoot = "/proc"
	}

	// Index 1: pid → envelope index. Same PID can only belong to one
	// envelope per tick (the classifier owns that invariant).
	pidToEnv := make(map[int]int, 64)
	// Index 2: (localIP.String(), localPort) → envelope index. Used for
	// "this conn's remote endpoint is owned by envelope X".
	type listenKey struct {
		IP   string
		Port uint16
	}
	listenToEnv := make(map[listenKey]int, 64)

	for i := range envelopes {
		for _, pid := range envelopes[i].PIDs {
			pidToEnv[pid] = i
		}
		// We don't know the listen ports yet; the second pass fills them
		// from the LISTEN-state rows we encounter while reading procfs.
	}

	// First pass: read every tracked PID's /proc/<pid>/net/tcp once,
	// cache the rows, and learn LISTEN-state (localIP, localPort) →
	// envelope mappings as we go.
	rowsByPID := make(map[int][]connRow, len(pidToEnv))
	for pid, envIdx := range pidToEnv {
		rows, err := readProcTCP(procRoot, pid)
		if err != nil {
			continue
		}
		rowsByPID[pid] = rows
		for _, r := range rows {
			if r.State == 0x0a { // LISTEN
				listenToEnv[listenKey{IP: normalizeListenIP(r.LocalIP), Port: r.LocalPort}] = envIdx
			}
		}
	}

	// Second pass: for every ESTABLISHED outbound connection from a known
	// PID, look up the remote endpoint in listenToEnv. If found, that's
	// a caller→server attribution: client = pidToEnv[pid], server = the
	// listenToEnv hit. Also tolerate listen-on-0.0.0.0 by checking
	// (loopback + port).
	type aggKey struct {
		Server int
		Client int
	}
	type agg struct {
		Conns int
		PID   int
	}
	aggregated := make(map[aggKey]*agg, 64)

	for pid, rows := range rowsByPID {
		clientEnv, ok := pidToEnv[pid]
		if !ok {
			continue
		}
		for _, r := range rows {
			if r.State != 0x01 { // ESTABLISHED
				continue
			}
			if !isLocalhostScope(r.RemoteIP) {
				continue
			}
			// Try exact-IP listen first, then 0.0.0.0:port, then ::-port.
			lookup := []listenKey{
				{IP: r.RemoteIP.String(), Port: r.RemotePort},
				{IP: "0.0.0.0", Port: r.RemotePort},
				{IP: "::", Port: r.RemotePort},
			}
			var serverEnv int = -1
			for _, lk := range lookup {
				if idx, ok := listenToEnv[lk]; ok {
					serverEnv = idx
					break
				}
			}
			if serverEnv == -1 || serverEnv == clientEnv {
				continue
			}
			k := aggKey{Server: serverEnv, Client: clientEnv}
			a, ok := aggregated[k]
			if !ok {
				a = &agg{PID: pid}
				aggregated[k] = a
			}
			a.Conns++
		}
	}

	// Materialize Callers[] on each server envelope.
	collected := make(map[int][]CallerAttribution, len(aggregated))
	for k, a := range aggregated {
		clientEnv := envelopes[k.Client]
		row := CallerAttribution{
			Caller:     clientEnv.ID,
			CallerKind: callerKindFromEnvelopeKind(clientEnv.Kind),
			PID:        a.PID,
			Conns:      a.Conns,
		}
		collected[k.Server] = append(collected[k.Server], row)
	}

	for serverIdx, list := range collected {
		// Sort by Conns desc (BytesTotal is zero in procfs path so use Conns
		// as the tiebreaker for the loudest-caller derivation).
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].Conns != list[j].Conns {
				return list[i].Conns > list[j].Conns
			}
			return list[i].Caller < list[j].Caller
		})
		envelopes[serverIdx].Callers = list
		// Derived loudest-caller alias for back-compat single-caller renders.
		// Phase 1 (ollama tap) may have already set Caller — only overwrite
		// if Phase 1 didn't fill it, so the model-name attribution wins
		// for ollama envelopes.
		if envelopes[serverIdx].Caller == "" {
			envelopes[serverIdx].Caller = list[0].Caller
			envelopes[serverIdx].CallerKind = list[0].CallerKind
		}
	}

	return envelopes
}

// callerKindFromEnvelopeKind maps EnvelopeKind values to the CallerKind
// alphabet so consumers can render the right icon without re-deriving.
func callerKindFromEnvelopeKind(k EnvelopeKind) string {
	switch k {
	case EnvelopeSession:
		return "session"
	case EnvelopeBackend:
		return "backend"
	case EnvelopeContainer:
		return "container"
	case EnvelopeSystem:
		return "system"
	}
	return "envelope"
}

// readProcTCP reads /proc/<pid>/net/tcp + /proc/<pid>/net/tcp6 and parses
// each row into a connRow. We open both files because backends commonly
// listen on dual-stack v6 sockets that show up in tcp6 only.
func readProcTCP(procRoot string, pid int) ([]connRow, error) {
	rows := []connRow{}
	for _, base := range []string{"net/tcp", "net/tcp6"} {
		path := filepath.Join(procRoot, strconv.Itoa(pid), base)
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		s := bufio.NewScanner(f)
		first := true
		for s.Scan() {
			if first {
				first = false
				continue // header line
			}
			r, ok := parseTCPLine(s.Text())
			if ok {
				rows = append(rows, r)
			}
		}
		f.Close()
	}
	return rows, nil
}

// parseTCPLine parses one row of /proc/<pid>/net/tcp{,6}. Field layout
// (whitespace-separated):
//
//	sl  local_address rem_address st  tx_queue:rx_queue  tr:tm->when retrnsmt  uid timeout inode  ...
//
// local_address / rem_address are hex of IP:port (4-byte for tcp, 16-byte
// for tcp6, both little-endian).
func parseTCPLine(line string) (connRow, bool) {
	parts := strings.Fields(line)
	if len(parts) < 10 {
		return connRow{}, false
	}
	localIP, localPort, ok := parseHexEndpoint(parts[1])
	if !ok {
		return connRow{}, false
	}
	remoteIP, remotePort, ok := parseHexEndpoint(parts[2])
	if !ok {
		return connRow{}, false
	}
	state64, err := strconv.ParseUint(parts[3], 16, 8)
	if err != nil {
		return connRow{}, false
	}
	inode, err := strconv.ParseUint(parts[9], 10, 64)
	if err != nil {
		return connRow{}, false
	}
	return connRow{
		LocalIP:    localIP,
		LocalPort:  localPort,
		RemoteIP:   remoteIP,
		RemotePort: remotePort,
		State:      uint8(state64),
		Inode:      inode,
	}, true
}

// parseHexEndpoint decodes "C0A80101:1F90" → (192.168.1.1, 8080) for v4
// or the 32-hex-char v6 form. The IP bytes are little-endian per word.
func parseHexEndpoint(s string) (net.IP, uint16, bool) {
	c := strings.IndexByte(s, ':')
	if c < 0 {
		return nil, 0, false
	}
	hexIP := s[:c]
	hexPort := s[c+1:]
	port64, err := strconv.ParseUint(hexPort, 16, 16)
	if err != nil {
		return nil, 0, false
	}
	switch len(hexIP) {
	case 8:
		// IPv4: 4 bytes little-endian.
		ip := make(net.IP, 4)
		for i := 0; i < 4; i++ {
			b, err := strconv.ParseUint(hexIP[i*2:i*2+2], 16, 8)
			if err != nil {
				return nil, 0, false
			}
			ip[3-i] = byte(b)
		}
		return ip, uint16(port64), true
	case 32:
		// IPv6: 16 bytes, little-endian per 4-byte word.
		ip := make(net.IP, 16)
		for w := 0; w < 4; w++ {
			for i := 0; i < 4; i++ {
				b, err := strconv.ParseUint(hexIP[w*8+i*2:w*8+i*2+2], 16, 8)
				if err != nil {
					return nil, 0, false
				}
				ip[w*4+(3-i)] = byte(b)
			}
		}
		return ip, uint16(port64), true
	default:
		return nil, 0, false
	}
}

// normalizeListenIP collapses listen-on-any to a stable string for the
// listenToEnv map. 0.0.0.0 / :: stay as-is so the second-pass lookup can
// fall back to them; specific IPs are returned verbatim.
func normalizeListenIP(ip net.IP) string {
	if ip.IsUnspecified() {
		// Distinguish v4 vs v6 unspecified for the lookup fallback.
		if ip.To4() != nil {
			return "0.0.0.0"
		}
		return "::"
	}
	return ip.String()
}

// isLocalhostScope reports whether the remote endpoint is in scope for
// this release (Q5 → localhost + same-namespace). True for 127.0.0.0/8,
// ::1, and the common docker/k8s bridge ranges. Cross-host attribution
// (Q5c) needs federation-aware joins and is intentionally rejected.
func isLocalhostScope(ip net.IP) bool {
	if ip.IsLoopback() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		// docker default bridge 172.17.0.0/16; k8s common 10.0.0.0/8 +
		// 192.168.0.0/16. The dev workstation testing cluster lives in
		// 192.168.x.x per operator's Q5 answer.
		switch v4[0] {
		case 10, 172:
			return true
		case 192:
			return v4[1] == 168
		}
	}
	return false
}

// FormatCallerSummary renders the loudest-N callers of an envelope as a
// "60% opencode, 40% openwebui" string for log lines / debug surfaces.
// PWA renders the structured Callers[] directly; this helper is for
// non-UI callers (logs, MCP responses, REST diff output).
func FormatCallerSummary(callers []CallerAttribution, max int) string {
	if len(callers) == 0 {
		return ""
	}
	if max <= 0 || max > len(callers) {
		max = len(callers)
	}
	totalConns := 0
	for _, c := range callers {
		totalConns += c.Conns
	}
	if totalConns == 0 {
		totalConns = 1
	}
	parts := make([]string, 0, max)
	for i := 0; i < max; i++ {
		pct := 100 * callers[i].Conns / totalConns
		parts = append(parts, fmt.Sprintf("%d%% %s", pct, callers[i].Caller))
	}
	return strings.Join(parts, ", ")
}
