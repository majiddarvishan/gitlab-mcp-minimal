package validate

import "testing"

func TestPage(t *testing.T) {
	for in, want := range map[int]int{0: 1, 1: 1, 2: 2, MaxPage: MaxPage} {
		if got, err := Page(in); err != nil || got != want {
			t.Errorf("Page(%d) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []int{-1, MaxPage + 1, 1 << 40} {
		if _, err := Page(in); err == nil {
			t.Errorf("Page(%d) accepted", in)
		}
	}
}

func TestOneOf(t *testing.T) {
	if err := OneOf("status", "", "a", "b"); err != nil {
		t.Errorf("empty value must be accepted: %v", err)
	}
	if err := OneOf("status", "b", "a", "b"); err != nil {
		t.Errorf("listed value rejected: %v", err)
	}
	for _, v := range []string{"c", "A", "a ", "a,b"} {
		if err := OneOf("status", v, "a", "b"); err == nil {
			t.Errorf("OneOf accepted %q", v)
		}
	}
}
