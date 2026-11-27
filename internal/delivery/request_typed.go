// Typed input to a worker that cannot take: the claim is saved before typing,
// the text is typed at most once, it shows "typed, unconfirmed", and only
// independent proof closes it. Owner: part C.

package delivery

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

// mayType reports a request whose text may be typed into its holder: waiting,
// never handed over, within its pickup limit, held by a worker with no proven
// take. A hello is never typed (typing proves no take).
func mayType(r *Request) bool {
	return r.State == StateWaiting && r.Holder != nil && r.Kind != ReqHello && !pickupPassed(r) &&
		!r.Delivery.MaybeHandedOver() && !PolicyFor(r.Holder.Family).Proven && typableFamily(r.Holder.Family)
}

// typableFamily says whether anything but a shell can be expected to read what
// is typed. A shell would run the text, a one-shot run reads nothing and
// leaves a shell behind, and pi takes through its own extension. A family
// datawatch does not know the name of is not typed into.
func typableFamily(family string) bool {
	switch {
	case family == "", family == "shell", family == "subprocess":
		return false
	case family == "pi", strings.HasPrefix(family, "pi-"), strings.HasSuffix(family, "-prompt"):
		return false
	}
	return true
}

// TypeOnce types a request's text into its holder's session, once ever.
func TypeOnce(ctx context.Context, b RequestBook, ty Typer, id string) error {
	return typeOnce(ctx, b, ty, id, time.Now())
}

func typeOnce(ctx context.Context, b RequestBook, ty Typer, id string, now time.Time) error {
	r, ok := b.Request(id)
	if !ok {
		return ErrRequestNotFound
	}
	if !mayType(&r) {
		return ErrRequestState
	}
	if err := notReady(ctx, ty, r.Holder.SessionID, true); err != nil {
		return err
	}
	content, err := b.Content(id)
	if err != nil {
		return err
	}
	text := typedText(r, content)
	holder := r.Holder.SessionID
	// The claim first: after it the text is never typed again, whatever
	// happens next.
	if _, err := b.Change(id, func(r *Request) error {
		if !mayType(r) || r.Holder.SessionID != holder {
			return ErrRequestState
		}
		at := now
		r.Delivery.TypeClaimAt = &at
		return nil
	}); err != nil {
		return err
	}
	switch err := ty.Request(ctx, holder, id, text); {
	case errors.Is(err, ErrPromptRefused):
		// Nothing was typed, so the try does not count.
		_, cerr := b.Change(id, func(r *Request) error { r.Delivery.TypeClaimAt = nil; return nil })
		return errors.Join(err, cerr)
	case err != nil:
		return err // it may have been typed: the claim stays
	}
	_, err = b.Change(id, func(r *Request) error {
		at := now
		r.Delivery.ContentTypedAt = &at
		return nil
	})
	return err
}

// typedText is one line with no control bytes: a line break would submit a
// part of it, and a control byte could work the terminal.
func typedText(r Request, c RequestContent) string {
	parts := []string{fmt.Sprintf("datawatch request %s (%s):", r.ID, r.Kind), c.Ask, c.Detail}
	keys := make([]string, 0, len(c.Params))
	for k := range c.Params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, k+": "+c.Params[k])
	}
	for i, it := range c.Items {
		if i == 0 {
			parts = append(parts, "Done when:")
		}
		parts = append(parts, "- "+it.Statement)
	}
	clean := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return ' '
		}
		return r
	}, strings.Join(parts, " "))
	return strings.Join(strings.Fields(clean), " ")
}
