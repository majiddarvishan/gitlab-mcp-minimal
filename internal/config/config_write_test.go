package config

import "testing"

func TestWriteToolsAreOffByDefault(t *testing.T) {
	c, err := parse(t, validJSON)
	if err != nil || c.EnableWriteTools {
		t.Fatalf("write tools must default to off: %v %+v", err, c)
	}
	c, err = parse(t, `{"base_url": "https://g.example.com", "token_env": "T", "projects": [{"id": 1, "path": "a/b"}], "enable_write_tools": true}`)
	if err != nil || !c.EnableWriteTools {
		t.Fatalf("enable_write_tools not honoured: %v %+v", err, c)
	}
}
