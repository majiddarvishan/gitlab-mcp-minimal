// Package validate holds the strict, dependency-free input validators and the
// project allow-list used by every tool. Validators reject anything they do
// not explicitly understand and never echo unbounded input in their errors.
package validate

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits on identifier lengths.
const (
	MaxProjectPathLen = 255
	MaxBranchLen      = 200
	MaxBranchPrefix   = 64
	MaxFilePathLen    = 1024
	MaxTitleLen       = 255

	maxSafeInt     = 1<<53 - 1
	maxPathSegment = 20
	maxQuoted      = 64
)

// DefaultDenyPaths lists paths an agent may never write, because changing them
// can alter CI behaviour or review rules. Entries match the exact path or any
// path below them, case-insensitively.
var DefaultDenyPaths = []string{".gitlab-ci.yml", ".gitlab", "CODEOWNERS"}

// Error describes why a value was rejected.
type Error struct {
	Field  string
	Reason string
}

func (e *Error) Error() string { return e.Field + ": " + e.Reason }

func reject(field, format string, args ...any) error {
	return &Error{Field: field, Reason: fmt.Sprintf(format, args...)}
}

// quote renders untrusted input safely and boundedly for an error message.
func quote(s string) string {
	if len(s) > maxQuoted {
		s = s[:maxQuoted] + "..."
	}
	return strconv.Quote(s)
}

// ProjectPath validates a namespaced project path such as "group/project".
func ProjectPath(s string) error {
	const field = "project"
	if s == "" {
		return reject(field, "must not be empty")
	}
	if len(s) > MaxProjectPathLen {
		return reject(field, "path is too long (max %d bytes)", MaxProjectPathLen)
	}
	segs := strings.Split(s, "/")
	if len(segs) < 2 || len(segs) > maxPathSegment {
		return reject(field, "path must look like group/project (2 to %d segments)", maxPathSegment)
	}
	for _, seg := range segs {
		if !validProjectSegment(seg) {
			return reject(field, "invalid path segment %s", quote(seg))
		}
	}
	return nil
}

func validProjectSegment(seg string) bool {
	if seg == "" {
		return false
	}
	for i := 0; i < len(seg); i++ {
		c := seg[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_':
		case (c == '.' || c == '-') && i > 0:
		default:
			return false
		}
	}
	return !strings.HasSuffix(seg, ".git") && !strings.HasSuffix(seg, ".atom")
}

func branchChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '.' || c == '_' || c == '-' || c == '/'
}

func refName(field, name string) error {
	if name == "" {
		return reject(field, "must not be empty")
	}
	if len(name) > MaxBranchLen {
		return reject(field, "too long (max %d bytes)", MaxBranchLen)
	}
	for i := 0; i < len(name); i++ {
		if !branchChar(name[i]) {
			return reject(field, "contains a disallowed character (allowed: letters, digits, '.', '_', '-', '/')")
		}
	}
	switch name[0] {
	case '-', '/', '.':
		return reject(field, "must not start with '-', '/' or '.'")
	}
	switch name[len(name)-1] {
	case '/', '.':
		return reject(field, "must not end with '/' or '.'")
	}
	if strings.Contains(name, "..") || strings.Contains(name, "//") {
		return reject(field, "must not contain '..' or '//'")
	}
	for _, seg := range strings.Split(name, "/") {
		if strings.HasPrefix(seg, ".") || strings.HasSuffix(seg, ".lock") {
			return reject(field, "path components must not start with '.' or end with '.lock'")
		}
	}
	if name == "HEAD" {
		return reject(field, "must not be HEAD")
	}
	return nil
}

// BranchName validates a git branch name using a deliberately strict subset of
// git's own rules (ASCII letters, digits and . _ - / only).
func BranchName(name string) error { return refName("branch", name) }

// Ref validates a branch, tag or commit reference used for reading.
func Ref(name string) error { return refName("ref", name) }

// BranchPrefix validates the configured agent branch prefix, for example "agent/".
func BranchPrefix(p string) error {
	const field = "branch_prefix"
	if len(p) < 2 || len(p) > MaxBranchPrefix {
		return reject(field, "length must be between 2 and %d bytes", MaxBranchPrefix)
	}
	if !strings.HasSuffix(p, "/") {
		return reject(field, "must end with '/'")
	}
	return refName(field, strings.TrimSuffix(p, "/"))
}

// AgentBranch validates a branch the agent may write to: a valid branch name
// that starts with prefix and has a non-empty remainder. prefix must already
// have passed BranchPrefix.
func AgentBranch(prefix, name string) error {
	if err := BranchName(name); err != nil {
		return err
	}
	if !strings.HasPrefix(name, prefix) || len(name) == len(prefix) {
		return reject("branch", "writes are only allowed on branches named %s<name>", prefix)
	}
	return nil
}

// FilePath validates a repository-relative file path.
func FilePath(p string) error {
	const field = "path"
	if p == "" {
		return reject(field, "must not be empty")
	}
	if len(p) > MaxFilePathLen {
		return reject(field, "too long (max %d bytes)", MaxFilePathLen)
	}
	if !utf8.ValidString(p) {
		return reject(field, "must be valid UTF-8")
	}
	for _, r := range p {
		switch {
		case r == '\\':
			return reject(field, "must not contain backslashes")
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r), unicode.Is(unicode.Zl, r), unicode.Is(unicode.Zp, r):
			return reject(field, "must not contain control or invisible formatting characters")
		}
	}
	if p[0] == '/' || p[len(p)-1] == '/' {
		return reject(field, "must be relative and must not end with '/'")
	}
	for _, seg := range strings.Split(p, "/") {
		switch {
		case seg == "":
			return reject(field, "must not contain empty segments")
		case seg == "." || seg == "..":
			return reject(field, "must not contain '.' or '..' segments")
		case strings.EqualFold(strings.TrimRight(seg, " ."), ".git"):
			return reject(field, "must not contain .git segments")
		}
	}
	return nil
}

// DenyPathEntry validates an entry of the write deny-list.
func DenyPathEntry(d string) error {
	if err := FilePath(strings.TrimSuffix(d, "/")); err != nil {
		return reject("write_deny_paths", "invalid entry %s: %s", quote(d), err.(*Error).Reason)
	}
	return nil
}

// WritePath validates a path the agent wants to write: it must be a valid file
// path and must not equal or sit below any deny-list entry.
func WritePath(p string, deny []string) error {
	if err := FilePath(p); err != nil {
		return err
	}
	lower := strings.ToLower(p)
	for _, d := range deny {
		d = strings.ToLower(strings.Trim(d, "/"))
		if d != "" && (lower == d || strings.HasPrefix(lower, d+"/")) {
			return reject("path", "is write-protected by policy")
		}
	}
	return nil
}

// Text validates free text such as commit messages, titles and notes.
func Text(field, s string, max int, multiline bool) error {
	if len(s) > max {
		return reject(field, "too long (max %d bytes)", max)
	}
	if !utf8.ValidString(s) {
		return reject(field, "must be valid UTF-8")
	}
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			if !multiline && r != '\t' {
				return reject(field, "must be a single line")
			}
		case unicode.IsControl(r):
			return reject(field, "must not contain control characters")
		case unicode.Is(unicode.Cf, r) && r != '\u200c' && r != '\u200d':
			return reject(field, "must not contain invisible formatting characters")
		}
	}
	return nil
}

// RequiredText is Text that additionally must not be blank.
func RequiredText(field, s string, max int, multiline bool) error {
	if strings.TrimSpace(s) == "" {
		return reject(field, "must not be empty")
	}
	return Text(field, s, max, multiline)
}

// Title validates a merge request title.
func Title(s string) error { return RequiredText("title", s, MaxTitleLen, false) }

// ID validates a positive numeric identifier (project, merge request, pipeline, job).
func ID(field string, n int64) error {
	if n <= 0 || n > maxSafeInt {
		return reject(field, "must be a positive integer")
	}
	return nil
}

// ParseID parses a decimal identifier given as a string.
func ParseID(field, s string) (int64, error) {
	if s == "" || len(s) > 15 {
		return 0, reject(field, "must be a positive integer")
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, reject(field, "must be a positive integer")
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, reject(field, "must be a positive integer")
	}
	return n, ID(field, n)
}
