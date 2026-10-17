package config

import (
	"fmt"
	"regexp"
	"slices"
)

// ToolPolicy is a server-wide ceiling, independent of instance confirmation modes.
// A nil Allow preserves upstream behaviour; an explicitly empty list denies all.
type ToolPolicy struct {
	Allow           []string `yaml:"allow"`
	Deny            []string `yaml:"deny"`
	DenyDestructive bool     `yaml:"denyDestructive"`
}

// ImportPolicy optionally replaces caller-supplied imports with expiring plans.
type ImportPolicy struct {
	Guarded bool `yaml:"guarded"`
}

// Allows reports whether a tool is inside the operator's server-wide policy.
func (p ToolPolicy) Allows(name string, destructive bool) bool {
	if destructive && p.DenyDestructive || slices.Contains(p.Deny, name) {
		return false
	}
	return p.Allow == nil || slices.Contains(p.Allow, name)
}

var toolNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*_[a-z0-9_]+$`)

func (p ToolPolicy) validate() error {
	for _, list := range []struct {
		name   string
		values []string
	}{{"allow", p.Allow}, {"deny", p.Deny}} {
		seen := map[string]bool{}
		for _, name := range list.values {
			if !toolNamePattern.MatchString(name) || seen[name] {
				return fmt.Errorf("tools.%s: require unique exact tool names; invalid entry %q", list.name, name)
			}
			seen[name] = true
		}
	}
	return nil
}
