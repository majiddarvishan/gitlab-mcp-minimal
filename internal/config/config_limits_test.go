package config

import "testing"

func TestDiffLimit(t *testing.T) {
	c, err := parse(t, validJSON)
	if err != nil {
		t.Fatal(err)
	}
	if c.Limits.MaxDiffBytes != 128<<10 {
		t.Errorf("default max_diff_bytes = %d", c.Limits.MaxDiffBytes)
	}
	c, err = parse(t, `{"base_url": "https://g.example.com", "token_env": "T", "projects": [{"id": 1, "path": "a/b"}], "limits": {"max_diff_bytes": 4096}}`)
	if err != nil || c.Limits.MaxDiffBytes != 4096 {
		t.Fatalf("explicit max_diff_bytes: %v %+v", err, c)
	}
	for _, v := range []string{"-1", "1048577"} {
		if _, err := parse(t, `{"base_url": "https://g.example.com", "token_env": "T", "projects": [{"id": 1, "path": "a/b"}], "limits": {"max_diff_bytes": `+v+`}}`); err == nil {
			t.Errorf("max_diff_bytes %s accepted", v)
		}
	}
}
