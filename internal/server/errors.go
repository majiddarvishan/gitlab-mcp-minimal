package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/audit"
	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/gitlab"
	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/validate"
)

// policyError is a refusal by server policy (as opposed to invalid input).
type policyError struct{ msg string }

func (e *policyError) Error() string { return e.msg }

func deny(format string, args ...any) error { return &policyError{msg: fmt.Sprintf(format, args...)} }

// classify maps an error to an audit outcome, the text shown to the model, and
// the reason written to the audit log. The model only ever sees short, safe
// messages; anything unexpected is reported as an internal error.
func classify(err error) (outcome audit.Outcome, public, reason string) {
	var (
		ve *validate.Error
		pe *policyError
		ae *gitlab.APIError
	)
	switch {
	case errors.As(err, &ve):
		return audit.Denied, "invalid input: " + ve.Error(), ve.Error()
	case errors.As(err, &pe):
		return audit.Denied, "not allowed: " + pe.Error(), pe.Error()
	case errors.As(err, &ae):
		msg := fmt.Sprintf("GitLab returned HTTP %d", ae.Status)
		if ae.Message != "" {
			msg += ": " + ae.Message
		}
		return audit.Failed, msg, msg
	case errors.Is(err, gitlab.ErrResponseTooLarge):
		return audit.Failed, "the GitLab response was too large to return", err.Error()
	case errors.Is(err, gitlab.ErrRedirect):
		return audit.Failed, "GitLab answered with a redirect, which this server refuses to follow", err.Error()
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return audit.Failed, "the request to GitLab timed out or was cancelled", err.Error()
	default:
		return audit.Failed, "internal error", err.Error()
	}
}
