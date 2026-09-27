package obieproto

import "time"

// Option configures [Event.Validate] and [Decode].
type Option func(*options)

type options struct {
	now                func() time.Time
	allowDocumentation bool
}

func newOptions(opts []Option) options {
	o := options{now: time.Now}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// WithClock sets the clock that issued_at is checked against. The default is
// [time.Now].
func WithClock(now func() time.Time) Option {
	return func(o *options) { o.now = now }
}

// AllowDocumentationRanges accepts indicators in the documentation ranges
// (192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24, 2001:db8::/32, 3fff::/20).
// It exists for tests and examples only; production code must not use it.
func AllowDocumentationRanges() Option {
	return func(o *options) { o.allowDocumentation = true }
}
