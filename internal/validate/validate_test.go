package validate

import (
	"path"
	"strings"
	"testing"
	"unicode/utf8"
)

func check(t *testing.T, name string, fn func(string) error, good, bad []string) {
	t.Helper()
	for _, in := range good {
		if err := fn(in); err != nil {
			t.Errorf("%s(%q) rejected: %v", name, in, err)
		}
	}
	for _, in := range bad {
		if err := fn(in); err == nil {
			t.Errorf("%s(%q) accepted, want rejection", name, in)
		}
	}
}

func TestProjectPath(t *testing.T) {
	check(t, "ProjectPath", ProjectPath,
		[]string{"group/project", "a/b/c", "My-Group/proj.name_1", "_x/y", "1group/2proj"},
		[]string{"", "project", "/group/project", "group/project/", "group//project", "group/../project",
			"group/./project", "-group/project", ".group/project", "group/pro ject", "group/project.git",
			"group/project.atom", "group\\project", "grp/pr\x00oj", "grp/pröj", "group/project?x=1",
			"group/project%2Fx", strings.Repeat("a/", 21) + "b", strings.Repeat("a", 256) + "/b"})
}

func TestBranchName(t *testing.T) {
	check(t, "BranchName", BranchName,
		[]string{"main", "agent/fix-1", "feature/a_b.c", "v1.2.3", "a/b/c"},
		[]string{"", "-x", "/x", ".x", "x/", "x.", "a..b", "a//b", "a/.b", "a/b.lock", "HEAD", "a b", "a~b",
			"a^b", "a:b", "a?b", "a*b", "a[b", "a\\b", "a@{b", "a\nb", "a\x00b", "ünï", strings.Repeat("a", 201)})
}

func TestBranchPrefix(t *testing.T) {
	check(t, "BranchPrefix", BranchPrefix,
		[]string{"agent/", "bots/claude/"},
		[]string{"", "/", "agent", "agent//", "-agent/", "a b/", ".agent/", "agent/../", strings.Repeat("a", 64) + "/"})
}

func TestAgentBranch(t *testing.T) {
	const prefix = "agent/"
	good := []string{"agent/x", "agent/feature/y", "agent/fix-123"}
	bad := []string{"main", "master", "agent/", "agent", "Agent/x", "agentx/y", "x/agent/y", "agent/../main",
		"agent//x", "agent/a b", "release/1.0", "", "agent/x.lock"}
	check(t, "AgentBranch", func(s string) error { return AgentBranch(prefix, s) }, good, bad)
}

func TestFilePath(t *testing.T) {
	check(t, "FilePath", FilePath,
		[]string{"README.md", "a/b/c.go", "dir with space/file name.txt", "a/.hidden", "a/b.git/c", "ü/ö.txt"},
		[]string{"", "/etc/passwd", "a/", "a//b", "a/./b", "a/../b", "..", ".", "../a", "a/..", "a\\b",
			"a\x00b", "a\nb", "a\x7fb", ".git/config", "a/.git/hooks/x", ".GIT/config", ".git./x", ".git /x",
			"a/\u202eb", "a\u200bb", "a\u2028b", "a\xffb", strings.Repeat("a", 1025)})
}

func TestWritePath(t *testing.T) {
	deny := DefaultDenyPaths
	check(t, "WritePath", func(s string) error { return WritePath(s, deny) },
		[]string{"main.go", "docs/readme.md", "src/.gitlab-ci.yml.txt", "my.gitlab/x", "CODEOWNERSX"},
		[]string{".gitlab-ci.yml", ".GitLab-CI.yml", ".gitlab/issue_templates/x.md", ".gitlab", "CODEOWNERS",
			"codeowners", "../x", "a/../.gitlab-ci.yml"})
	if err := WritePath(".gitlab-ci.yml", nil); err != nil {
		t.Errorf("empty deny-list must allow the path: %v", err)
	}
	if err := DenyPathEntry(".gitlab/"); err != nil {
		t.Errorf("DenyPathEntry rejected valid entry: %v", err)
	}
	if err := DenyPathEntry("../x"); err == nil {
		t.Error("DenyPathEntry accepted ../x")
	}
}

func TestText(t *testing.T) {
	if err := Text("msg", "line1\nline2\ttab\r\n", 100, true); err != nil {
		t.Errorf("multiline text rejected: %v", err)
	}
	if err := Text("title", "one line\twith tab", 100, false); err != nil {
		t.Errorf("single line with tab rejected: %v", err)
	}
	bad := []struct {
		s         string
		multiline bool
	}{
		{"a\nb", false}, {"a\rb", false}, {"a\x00b", true}, {"a\x1bb", true}, {"a\u202eb", true},
		{"a\ufeffb", true}, {"a\xffb", true}, {strings.Repeat("a", 101), true},
	}
	for _, c := range bad {
		if err := Text("f", c.s, 100, c.multiline); err == nil {
			t.Errorf("Text(%q, multiline=%v) accepted", c.s, c.multiline)
		}
	}
	if err := Text("f", "family \U0001F468\u200d\U0001F469\u200d\U0001F467", 100, false); err != nil {
		t.Errorf("ZWJ emoji sequence rejected: %v", err)
	}
	if err := RequiredText("f", " \t ", 10, false); err == nil {
		t.Error("blank text accepted")
	}
	if err := Title("Draft: fix thing"); err != nil {
		t.Errorf("title rejected: %v", err)
	}
	if err := Title(strings.Repeat("t", 256)); err == nil {
		t.Error("overlong title accepted")
	}
}

func TestIDs(t *testing.T) {
	for _, n := range []int64{1, 42, 1<<53 - 1} {
		if err := ID("id", n); err != nil {
			t.Errorf("ID(%d) rejected", n)
		}
	}
	for _, n := range []int64{0, -1, 1 << 53, 1<<63 - 1} {
		if err := ID("id", n); err == nil {
			t.Errorf("ID(%d) accepted", n)
		}
	}
	for s, want := range map[string]int64{"1": 1, "123456": 123456} {
		if got, err := ParseID("id", s); err != nil || got != want {
			t.Errorf("ParseID(%q) = %d, %v", s, got, err)
		}
	}
	for _, s := range []string{"", "0", "-1", "+1", "1.5", "1e3", " 1", "1 ", "0x10", "٣", "9999999999999999", "abc"} {
		if _, err := ParseID("id", s); err == nil {
			t.Errorf("ParseID(%q) accepted", s)
		}
	}
}

func TestErrorsDoNotEchoLongInput(t *testing.T) {
	long := strings.Repeat("x", 5000) + "/" + strings.Repeat("y", 5000)
	err := ProjectPath(long)
	if err == nil {
		t.Fatal("expected error")
	}
	if len(err.Error()) > 300 {
		t.Fatalf("error echoes unbounded input (%d bytes)", len(err.Error()))
	}
	err = ProjectPath("bad seg/x" + strings.Repeat("z", 1000))
	if err == nil || len(err.Error()) > 300 {
		t.Fatal("error message is not bounded")
	}
}

// Fuzz targets. `go test` runs the seed corpus; run `go test -fuzz=FuzzX` to explore.

func FuzzBranchName(f *testing.F) {
	for _, s := range []string{"main", "agent/x", "a..b", "-x", "a/.b", "x.lock", "\x00", "a b", "é"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if BranchName(s) != nil {
			return
		}
		if strings.Contains(s, "..") || strings.Contains(s, "//") || strings.HasPrefix(s, "-") ||
			strings.HasPrefix(s, "/") || strings.HasPrefix(s, ".") || strings.HasSuffix(s, "/") ||
			strings.HasSuffix(s, ".") || s == "" || s == "HEAD" {
			t.Fatalf("BranchName accepted %q", s)
		}
		for i := 0; i < len(s); i++ {
			if !branchChar(s[i]) {
				t.Fatalf("BranchName accepted %q with byte %#x", s, s[i])
			}
		}
		for _, seg := range strings.Split(s, "/") {
			if strings.HasPrefix(seg, ".") || strings.HasSuffix(seg, ".lock") {
				t.Fatalf("BranchName accepted %q with bad component %q", s, seg)
			}
		}
	})
}

func FuzzFilePath(f *testing.F) {
	for _, s := range []string{"a/b", "../x", "a/./b", "a\\b", ".git/x", "a\x00", "\u202e", "a//b", "/a", "a/"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if FilePath(s) != nil {
			return
		}
		if !utf8.ValidString(s) || path.Clean(s) != s || strings.HasPrefix(s, "/") || strings.Contains(s, "\\") {
			t.Fatalf("FilePath accepted %q", s)
		}
		for _, seg := range strings.Split(s, "/") {
			if seg == "" || seg == "." || seg == ".." || strings.EqualFold(strings.TrimRight(seg, " ."), ".git") {
				t.Fatalf("FilePath accepted %q with segment %q", s, seg)
			}
		}
		for _, r := range s {
			if r < 0x20 || r == 0x7f {
				t.Fatalf("FilePath accepted %q with control character", s)
			}
		}
	})
}

func FuzzProjectPath(f *testing.F) {
	for _, s := range []string{"a/b", "a/b/c", "../x", "a/b.git", "", "a//b", "é/x"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if ProjectPath(s) != nil {
			return
		}
		for _, seg := range strings.Split(s, "/") {
			if !validProjectSegment(seg) || seg == "." || seg == ".." {
				t.Fatalf("ProjectPath accepted %q", s)
			}
		}
	})
}

func FuzzText(f *testing.F) {
	for _, s := range []string{"ok", "a\nb", "\x00", "\u202e", "\xff"} {
		f.Add(s, false)
	}
	f.Fuzz(func(t *testing.T, s string, multiline bool) {
		if Text("f", s, 1000, multiline) != nil {
			return
		}
		if !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
			t.Fatalf("Text accepted %q", s)
		}
		if !multiline && strings.ContainsAny(s, "\r\n") {
			t.Fatalf("single-line Text accepted %q", s)
		}
	})
}
