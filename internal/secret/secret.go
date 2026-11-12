// Package secret provides a string wrapper that never reveals its value
// through formatting, JSON encoding or structured logging.
package secret

import "log/slog"

const redacted = "[REDACTED]"

// Secret holds a sensitive string. The value is only available through
// Reveal. It is kept behind a pointer so that even fmt's reflection-based
// printing of an unexported Secret field cannot print the raw value.
type Secret struct {
	v *string
}

// New wraps v.
func New(v string) Secret { return Secret{v: &v} }

// Reveal returns the underlying value. Call it only where the value is
// actually needed (for example when setting an HTTP header).
func (s Secret) Reveal() string {
	if s.v == nil {
		return ""
	}
	return *s.v
}

// IsZero reports whether the secret is empty.
func (s Secret) IsZero() bool { return s.Reveal() == "" }

// String implements fmt.Stringer.
func (s Secret) String() string { return redacted }

// GoString implements fmt.GoStringer.
func (s Secret) GoString() string { return redacted }

// MarshalText implements encoding.TextMarshaler.
func (s Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// MarshalJSON implements json.Marshaler.
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }

// LogValue implements slog.LogValuer.
func (s Secret) LogValue() slog.Value { return slog.StringValue(redacted) }
