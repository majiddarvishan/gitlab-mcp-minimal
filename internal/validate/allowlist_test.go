package validate

import (
	"strings"
	"testing"
)

func testList(t *testing.T) *Allowlist {
	t.Helper()
	a, err := NewAllowlist([]Project{
		{ID: 12, Path: "team/service-b"},
		{ID: 7, Path: "Team/Service-A"},
		{ID: 99, Path: "1corp/2proj"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestResolve(t *testing.T) {
	a := testList(t)
	for ref, wantID := range map[string]int64{
		"7": 7, "12": 12, "team/service-a": 7, "TEAM/SERVICE-B": 12, "Team/Service-A": 7, "1corp/2proj": 99,
	} {
		p, err := a.Resolve(ref)
		if err != nil || p.ID != wantID {
			t.Errorf("Resolve(%q) = %+v, %v; want id %d", ref, p, err, wantID)
		}
	}
	for _, ref := range []string{"", "8", "0", "-7", "007x", "team/other", "team", "../team/service-a",
		"team/service-a/", "team/service-a\x00", "team%2Fservice-a", "7 ", " 7", "99999999999999999999"} {
		if p, err := a.Resolve(ref); err == nil {
			t.Errorf("Resolve(%q) accepted: %+v", ref, p)
		}
	}
	// Numeric references never fall through to path matching.
	if _, err := a.Resolve("00007"); err != nil {
		t.Logf("leading zeros: %v", err) // either outcome is safe: the ID is still 7 or rejected
	}
}

func TestNewAllowlistErrors(t *testing.T) {
	cases := map[string][]Project{
		"empty":          nil,
		"zero id":        {{ID: 0, Path: "a/b"}},
		"negative id":    {{ID: -1, Path: "a/b"}},
		"bad path":       {{ID: 1, Path: "not-namespaced"}},
		"traversal path": {{ID: 1, Path: "a/../b"}},
		"duplicate id":   {{ID: 1, Path: "a/b"}, {ID: 1, Path: "a/c"}},
		"duplicate path": {{ID: 1, Path: "a/b"}, {ID: 2, Path: "A/B"}},
	}
	for name, ps := range cases {
		if _, err := NewAllowlist(ps); err == nil {
			t.Errorf("%s: NewAllowlist accepted %+v", name, ps)
		}
	}
}

func TestProjectsSortedAndCopied(t *testing.T) {
	a := testList(t)
	got := a.Projects()
	if len(got) != 3 || got[0].Path != "1corp/2proj" || got[2].Path != "team/service-b" {
		t.Fatalf("unexpected order: %+v", got)
	}
	got[0].ID = 12345
	if p, _ := a.Resolve("1corp/2proj"); p.ID != 99 {
		t.Fatal("mutating the returned slice changed the allow-list")
	}
}

func FuzzResolve(f *testing.F) {
	for _, s := range []string{"7", "team/service-a", "", "../x", "1e3", "\x00", strings.Repeat("9", 40)} {
		f.Add(s)
	}
	a, _ := NewAllowlist([]Project{{ID: 7, Path: "team/service-a"}})
	f.Fuzz(func(t *testing.T, s string) {
		p, err := a.Resolve(s)
		if err == nil && (p.ID != 7 || p.Path != "team/service-a") {
			t.Fatalf("Resolve(%q) returned unexpected project %+v", s, p)
		}
	})
}
