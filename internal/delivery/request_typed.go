// Typed input to a worker that cannot take: the claim is saved before typing,
// the text is typed at most once, it shows "typed, unconfirmed", and only
// independent proof closes it. Owner: part C. STUB.

package delivery

import "context"

// TypeOnce types a request's text into its holder's session, once ever.
func TypeOnce(context.Context, RequestBook, Typer, string) error { return ErrRequestsNotBuilt }
