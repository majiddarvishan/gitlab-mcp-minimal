package secret

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

const raw = "glpat-supersecretvalue"

type holder struct {
	Exported Secret
	hidden   Secret
}

func TestRevealAndZero(t *testing.T) {
	if got := New(raw).Reveal(); got != raw {
		t.Fatalf("Reveal() = %q", got)
	}
	var zero Secret
	if !zero.IsZero() || zero.Reveal() != "" {
		t.Fatal("zero value must be empty")
	}
	if New("").IsZero() != true {
		t.Fatal("empty secret must be zero")
	}
}

func TestNeverPrinted(t *testing.T) {
	s := New(raw)
	h := holder{Exported: s, hidden: s}
	outputs := []string{
		fmt.Sprint(s),
		fmt.Sprintf("%v %+v %#v %s %q", s, s, s, s, s),
		fmt.Sprintf("%v", h),
		fmt.Sprintf("%+v", h),
		fmt.Sprintf("%#v", h),
		fmt.Sprintf("%v", &h),
		fmt.Sprintf("%+v", []Secret{s}),
		fmt.Sprintf("%v", map[string]Secret{"k": s}),
	}
	for _, out := range outputs {
		if strings.Contains(out, raw) {
			t.Fatalf("secret leaked in %q", out)
		}
	}
}

func TestNeverEncoded(t *testing.T) {
	b, err := json.Marshal(struct{ Token Secret }{New(raw)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), raw) {
		t.Fatalf("secret leaked in JSON %s", b)
	}
	tb, _ := New(raw).MarshalText()
	if strings.Contains(string(tb), raw) {
		t.Fatal("secret leaked in text encoding")
	}
}

func TestNeverLogged(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	log.Info("x", "token", New(raw), slog.Any("h", holder{Exported: New(raw)}))
	if strings.Contains(buf.String(), raw) {
		t.Fatalf("secret leaked in log %s", buf.String())
	}
}
