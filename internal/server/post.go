package server

import (
	"context"
	"encoding/json"
	"fmt"
)

// postJSON performs a POST with a JSON body and decodes the JSON reply into dst.
func (s *service) postJSON(ctx context.Context, dst any, body any, parts ...string) error {
	resp, err := s.GitLab.PostJSON(ctx, body, parts...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(resp.Body, dst); err != nil {
		return fmt.Errorf("decode GitLab response: %w", err)
	}
	return nil
}
