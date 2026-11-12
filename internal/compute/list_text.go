package compute

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Headings for the two report sections shown after the node list on the
// CLI and comm channels.
const (
	ObservedPeersHeading = "Observed peers with no node"
	UnansweredHeading    = "Auto-created nodes that never answered"
)

// listBody is the shape of GET /api/compute/nodes.
type listBody struct {
	Nodes []struct {
		Name    string `json:"name"`
		Kind    string `json:"kind"`
		Address string `json:"address"`
	} `json:"nodes"`
	ObservedPeers []ObservedPeer   `json:"observed_peers"`
	Unanswered    []UnansweredNode `json:"unanswered_auto_created"`
}

// FormatListSections renders the two report sections from a node-list
// REST body. addLine is a format with one %s verb for the peer name
// (e.g. the channel's add command). Both headings always appear so the
// operator can see an empty report.
func FormatListSections(body []byte, addLine string) (string, error) {
	var lb listBody
	if err := json.Unmarshal(body, &lb); err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s:\n", ObservedPeersHeading)
	if len(lb.ObservedPeers) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, p := range lb.ObservedPeers {
		shape := ""
		if p.Shape != "" {
			shape = " [" + p.Shape + "]"
		}
		fmt.Fprintf(&b, "  %s%s - %s\n", p.Name, shape, fmt.Sprintf(addLine, p.Name))
	}
	fmt.Fprintf(&b, "\n%s:\n", UnansweredHeading)
	if len(lb.Unanswered) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, n := range lb.Unanswered {
		fmt.Fprintf(&b, "  %s (%s, %s)\n", n.Name, n.Kind, n.Address)
	}
	return b.String(), nil
}

// FormatListText renders the whole node-list body as plain text: one line
// per node, then the two report sections. On a parse error it returns the
// error so the caller can fall back to the raw body.
func FormatListText(body []byte, addLine string) (string, error) {
	var lb listBody
	if err := json.Unmarshal(body, &lb); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("Nodes:\n")
	if len(lb.Nodes) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, n := range lb.Nodes {
		fmt.Fprintf(&b, "  %s (%s, %s)\n", n.Name, n.Kind, n.Address)
	}
	b.WriteString("\n")
	sec, err := FormatListSections(body, addLine)
	if err != nil {
		return "", err
	}
	b.WriteString(sec)
	return b.String(), nil
}

// Add-line formats per channel, one %s verb for the peer name.
const (
	CLIAddLine  = "Add as a compute node: datawatch compute node add %s --address <address you enter>"
	CommAddLine = "Add as a compute node: compute node add %s kind=<kind> address=<address you enter>"
)
