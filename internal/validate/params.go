package validate

import "strings"

// MaxPage is the highest page number a tool accepts.
const MaxPage = 10000

// Page validates an optional 1-based page number; 0 selects the first page.
func Page(p int) (int, error) {
	switch {
	case p == 0:
		return 1, nil
	case p < 1 || p > MaxPage:
		return 0, reject("page", "must be between 1 and %d", MaxPage)
	}
	return p, nil
}

// OneOf validates that v is one of allowed. An empty v means "not set" and is accepted.
func OneOf(field, v string, allowed ...string) error {
	if v == "" {
		return nil
	}
	for _, a := range allowed {
		if v == a {
			return nil
		}
	}
	return reject(field, "must be one of: %s", strings.Join(allowed, ", "))
}
