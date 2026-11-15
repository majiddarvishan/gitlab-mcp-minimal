package validate

import (
	"errors"
	"strings"
	"testing"
)

func TestCommitSHA(t *testing.T) {
	for _, s := range []string{"abcdef1", "0123456789abcdef0123456789abcdef01234567", strings.Repeat("a", 64)} {
		if err := CommitSHA("sha", s); err != nil {
			t.Errorf("CommitSHA(%q) rejected: %v", s, err)
		}
	}
	for _, s := range []string{"", "abc", "abcdef", "ABCDEF1", "abcdefg", "abcdef1 ", "../abcdef1", strings.Repeat("a", 65), "abcdef\x001"} {
		if err := CommitSHA("sha", s); err == nil {
			t.Errorf("CommitSHA(%q) accepted", s)
		}
	}
}

func TestInvalid(t *testing.T) {
	err := Invalid("files", "needs %d entries", 2)
	var ve *Error
	if !errors.As(err, &ve) || ve.Field != "files" || ve.Reason != "needs 2 entries" {
		t.Errorf("Invalid = %#v", err)
	}
}
