package validate

// Invalid builds a validation error for callers outside this package.
func Invalid(field, format string, args ...any) error { return reject(field, format, args...) }

// CommitSHA validates an abbreviated or full hexadecimal commit id.
func CommitSHA(field, s string) error {
	if len(s) < 7 || len(s) > 64 {
		return reject(field, "must be a commit id of 7 to 64 hexadecimal characters")
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return reject(field, "must be a commit id of 7 to 64 lowercase hexadecimal characters")
		}
	}
	return nil
}
