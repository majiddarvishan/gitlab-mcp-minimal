package validate

import (
	"sort"
	"strings"
)

// Project is an allow-listed GitLab project. The numeric ID is what is sent to
// GitLab; the path exists so callers can refer to the project by name.
type Project struct {
	ID   int64  `json:"id"`
	Path string `json:"path"`
}

// Allowlist resolves tool-supplied project references to configured projects.
type Allowlist struct {
	byID   map[int64]Project
	byPath map[string]Project
}

// NewAllowlist validates and indexes the configured projects.
func NewAllowlist(projects []Project) (*Allowlist, error) {
	if len(projects) == 0 {
		return nil, reject("projects", "at least one project is required")
	}
	a := &Allowlist{byID: map[int64]Project{}, byPath: map[string]Project{}}
	for _, p := range projects {
		if err := ID("projects.id", p.ID); err != nil {
			return nil, err
		}
		if err := ProjectPath(p.Path); err != nil {
			return nil, reject("projects.path", "%s", err.(*Error).Reason)
		}
		key := strings.ToLower(p.Path)
		if _, dup := a.byID[p.ID]; dup {
			return nil, reject("projects", "duplicate id %d", p.ID)
		}
		if _, dup := a.byPath[key]; dup {
			return nil, reject("projects", "duplicate path %s", quote(p.Path))
		}
		a.byID[p.ID] = p
		a.byPath[key] = p
	}
	return a, nil
}

// Resolve maps a numeric ID or a namespaced path to an allow-listed project.
// Anything else, including unlisted projects, is rejected before any request
// can be made.
func (a *Allowlist) Resolve(ref string) (Project, error) {
	if ref == "" {
		return Project{}, reject("project", "must not be empty")
	}
	if allDigits(ref) {
		id, err := ParseID("project", ref)
		if err != nil {
			return Project{}, err
		}
		if p, ok := a.byID[id]; ok {
			return p, nil
		}
		return Project{}, reject("project", "is not in the allow-list")
	}
	if err := ProjectPath(ref); err != nil {
		return Project{}, err
	}
	if p, ok := a.byPath[strings.ToLower(ref)]; ok {
		return p, nil
	}
	return Project{}, reject("project", "is not in the allow-list")
}

// Projects returns the allow-listed projects sorted by path.
func (a *Allowlist) Projects() []Project {
	out := make([]Project, 0, len(a.byID))
	for _, p := range a.byID {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}
