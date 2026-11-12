package gate

import "context"

// ProgressReport is one step of a tool call that takes long enough for a client
// to mistake silence for a dead connection.
type ProgressReport struct {
	// Progress is how far the work has got. Each report for one call has to be
	// greater than the last one the caller sent; the server does not forward a repeat.
	Progress float64
	// Total is the whole amount, when it is known.
	Total    float64
	HasTotal bool
	// Message is a single line a person, or a model, can read.
	Message string
}

type progressKey struct{}

// WithProgress returns a context whose tool call reports progress to fn.
// A nil fn leaves ctx unchanged.
func WithProgress(ctx context.Context, fn func(ProgressReport)) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, progressKey{}, fn)
}

// ReportProgress delivers one step to the reporter on ctx, if the caller of
// the tool asked for any. It does nothing otherwise.
func ReportProgress(ctx context.Context, report ProgressReport) {
	fn, _ := ctx.Value(progressKey{}).(func(ProgressReport))
	if fn != nil {
		fn(report)
	}
}
