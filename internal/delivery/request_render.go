// Plain text of requests for the command line, chat and the assistant tools.
// Owner: part B. A view holds identifiers, codes, counts and times only, and
// nothing here adds a word of a request's content. The one exception is
// RenderTake, whose reply goes to the holder that fetched it.

package delivery

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// requestLabels are the readings a view may carry that need no proof to show.
var requestLabels = []string{LabelWaiting, LabelTypedUnconfirmed, LabelFetched, LabelDelivered, LabelInModelCall,
	LabelAccepted, LabelNotAccepted, LabelFailed, LabelExpired, LabelCancelled}

// proofLabels are how a request in StateDone reads, by how it was proven.
var proofLabels = map[string]string{
	ProofDone:             LabelDone,
	ProofDelivered:        LabelDeliveredOnly,
	ProofAcceptedUnproven: LabelAcceptedUnproven,
}

// RenderLabel is the reading a surface shows. Only a request datawatch's check
// closed with plain proof reads "done", whatever label the view carries.
func RenderLabel(v RequestView) string {
	if v.State == StateDone {
		if l, ok := proofLabels[v.ProofAs]; ok {
			return l
		}
		return "closed, proof not stated"
	}
	if slices.Contains(requestLabels, v.Label) {
		return v.Label
	}
	return string(v.State)
}

func renderAddress(role Role, scope Scope) string {
	return fmt.Sprintf("%s@%s:%s", role, scope.Kind, scope.ID)
}

func renderLine(v RequestView) string {
	bits := []string{v.ID, RenderLabel(v), string(v.Kind), renderAddress(v.Role, v.Scope)}
	if v.Holder != nil {
		bits = append(bits, "holder "+v.Holder.SessionID)
	}
	if v.ReasonCode != "" {
		bits = append(bits, "reason "+v.ReasonCode)
	}
	if v.Overdue {
		bits = append(bits, "overdue")
	}
	if v.MayRunTwice {
		bits = append(bits, "may run twice")
	}
	return strings.Join(bits, " · ")
}

// RenderRequests is one line per request, at most max of them (0: all).
func RenderRequests(views []RequestView, max int) string {
	if len(views) == 0 {
		return "no requests"
	}
	shown := views
	if max > 0 && len(shown) > max {
		shown = shown[:max]
	}
	lines := []string{fmt.Sprintf("%d request(s)", len(views))}
	for _, v := range shown {
		lines = append(lines, "  "+renderLine(v))
	}
	if n := len(views) - len(shown); n > 0 {
		lines = append(lines, fmt.Sprintf("  … %d more", n))
	}
	return strings.Join(lines, "\n")
}

func renderRequestTime(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") }

// renderCalls bounds the call lines of one request.
const renderCalls = 10

// RenderRequest is everything a view says about one request.
func RenderRequest(v RequestView) string {
	lines := []string{renderLine(v), "state " + string(v.State)}
	add := func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }
	if !v.Created.IsZero() {
		add("created %s by %s", renderRequestTime(v.Created), v.Creator)
	}
	if v.HeldCause != "" {
		add("held: %s", v.HeldCause)
	}
	if v.After != "" {
		add("after %s", v.After)
	}
	if v.ReissueOf != "" {
		add("reissue of %s", v.ReissueOf)
	}
	if v.ReissuedAs != "" {
		add("reissued as %s", v.ReissuedAs)
	}
	if v.Verdict != nil {
		add("last check %s: %s", renderRequestTime(v.Verdict.At), strings.TrimSpace(v.Verdict.Status+" "+v.Verdict.Reason))
	}
	if v.NonOperatorActions > 0 {
		add("operator actions by a session or peer: %d", v.NonOperatorActions)
	}
	calls := v.Calls
	if len(calls) > renderCalls {
		add("calls (last %d of %d):", renderCalls, len(calls))
		calls = calls[len(calls)-renderCalls:]
	} else if len(calls) > 0 {
		add("calls:")
	}
	for _, c := range calls {
		refused := ""
		if c.Refused != "" {
			refused = " refused " + c.Refused
		}
		add("  %s %s by %s via %s%s", renderRequestTime(c.At), c.Verb, c.Observed, c.Surface, refused)
	}
	return strings.Join(lines, "\n")
}

// RenderRoles is one line per held role.
func RenderRoles(bindings []RoleBinding) string {
	if len(bindings) == 0 {
		return "no roles are held"
	}
	lines := make([]string, 0, len(bindings))
	for _, b := range bindings {
		bits := []string{renderAddress(b.Role, b.Scope), "held by " + b.SessionID, "given by " + b.GivenBy}
		if b.SelfGiven {
			bits = append(bits, "self-given")
		}
		if b.CanTakeAt == nil {
			bits = append(bits, "take not yet proven")
			if b.HelloID != "" {
				bits = append(bits, "hello "+b.HelloID)
			}
		} else {
			bits = append(bits, "take proven "+renderRequestTime(*b.CanTakeAt))
		}
		lines = append(lines, strings.Join(bits, " · "))
	}
	return strings.Join(lines, "\n")
}

// RenderMine is everything open one session holds.
func RenderMine(m MineReply, max int) string {
	out := "session " + m.SessionID + "\n" + RenderRequests(m.Items, max)
	if len(m.Roles) > 0 {
		out += "\n" + RenderRoles(m.Roles)
	}
	return out
}

// RenderTake is the reply to the holder's own take: it holds the content.
func RenderTake(t TakeReply) string {
	head := fmt.Sprintf("%s (%s), digest %s", t.Request.ID, t.Request.Kind, t.Digest)
	if t.Repeat {
		head += "\nfetched before: do not act on it twice"
		if t.ResendOK {
			head += " (this kind may be sent again)"
		}
	}
	return head + "\n\n" + t.Content
}
