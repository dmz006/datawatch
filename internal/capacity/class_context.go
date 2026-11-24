package capacity

import "context"

type classCtxKey struct{}

// WithClass returns ctx carrying the admission class, so an admission
// callback with a fixed signature can read the class the daemon decided.
func WithClass(ctx context.Context, c Class) context.Context {
	return context.WithValue(ctx, classCtxKey{}, c)
}

// ClassFrom returns the class WithClass stored. A context with none is an
// operator start: the daemon only sets a lower class for work it launched
// itself, so the safe default for an unknown caller is the accounted one.
func ClassFrom(ctx context.Context) Class {
	if c, ok := ctx.Value(classCtxKey{}).(Class); ok && c != "" {
		return c
	}
	return ClassOperator
}
