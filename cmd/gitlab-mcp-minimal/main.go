// Command gitlab-mcp-minimal is a minimal MCP server for a private GitLab
// instance. It speaks MCP over stdin/stdout, so nothing else may ever write to
// stdout; all diagnostics go to stderr.
package main

import (
	"context"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/audit"
	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/config"
	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/gitlab"
	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/server"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gitlab-mcp-minimal:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("gitlab-mcp-minimal", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfgPath := fs.String("config", os.Getenv("GITLAB_MCP_CONFIG"), "path to the JSON configuration file (default: $GITLAB_MCP_CONFIG)")
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *showVersion {
		fmt.Println(version)
		return nil
	}
	if *cfgPath == "" {
		return errors.New("no configuration: use -config or set GITLAB_MCP_CONFIG")
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	token, err := cfg.LoadToken(os.Getenv)
	if err != nil {
		return err
	}
	allow, err := cfg.Allowlist()
	if err != nil {
		return err
	}
	var roots *x509.CertPool
	if cfg.CAFile != "" {
		if roots, err = gitlab.LoadCAFile(cfg.CAFile); err != nil {
			return err
		}
	}
	gl, err := gitlab.New(gitlab.Options{
		BaseURL:           cfg.BaseURL,
		Token:             token,
		Timeout:           cfg.RequestTimeout(),
		MaxResponseBytes:  int64(cfg.Limits.MaxResponseBytes),
		AllowInsecureHTTP: cfg.AllowInsecureHTTP,
		RootCAs:           roots,
		UserAgent:         server.ServerName + "/" + version,
	})
	if err != nil {
		return err
	}
	srv, err := server.New(server.Deps{Config: cfg, Allow: allow, GitLab: gl, Audit: audit.NewStderr(token), Version: version})
	if err != nil {
		return err
	}
	if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
