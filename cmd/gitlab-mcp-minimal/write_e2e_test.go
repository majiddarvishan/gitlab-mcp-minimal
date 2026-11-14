package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestWriteToolsNeedExplicitOptIn(t *testing.T) {
	bin := buildBinary(t)
	for _, tc := range []struct {
		name, config string
		wantTools    int
	}{
		{"read-only by default", goodConfig, 7},
		{"opt-in", strings.Replace(goodConfig, `"token_env"`, `"enable_write_tools": true, "token_env"`, 1), 11},
	} {
		cmd := exec.Command(bin, "-config", writeConfig(t, tc.config))
		cmd.Env = append(os.Environ(), "GITLAB_TOKEN="+e2eToken)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cs, err := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "v0"}, nil).Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
		if err != nil {
			cancel()
			t.Fatalf("%s: connect: %v", tc.name, err)
		}
		tools, err := cs.ListTools(ctx, nil)
		if err != nil || len(tools.Tools) != tc.wantTools {
			t.Errorf("%s: %d tools, err %v; want %d", tc.name, len(tools.Tools), err, tc.wantTools)
		}
		cs.Close()
		cancel()
	}
}
