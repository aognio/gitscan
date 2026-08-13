// Package alias manages domain aliases for gitscan.
//
// A small set of aliases (github -> github.com, gitlab -> gitlab.com) is
// compiled into the binary so that --domain github works with zero config.
// User aliases from [aliases] in gitscan.toml are merged on top, and may
// override the built-ins.
package alias

import "sort"

// BuiltIn is the compiled-in default alias map. It is used even when no
// gitscan.toml exists.
var BuiltIn = map[string]string{
	"github": "github.com",
	"gitlab": "gitlab.com",
}

// Map is a merged alias map (built-in + user overrides).
type Map map[string]string

// Merge returns a new Map combining built-in and user aliases. User aliases
// override built-ins on key collision.
func Merge(user map[string]string) Map {
	out := make(Map, len(BuiltIn)+len(user))
	for k, v := range BuiltIn {
		out[k] = v
	}
	for k, v := range user {
		out[k] = v
	}
	return out
}

// Resolve canonicalizes an alias key or a host. If the input is a known alias
// key (e.g. "github"), it returns its canonical host ("github.com"). If the
// input is already a canonical host for a built-in alias, it returns it
// unchanged. Otherwise it returns the input unchanged.
func (m Map) Resolve(in string) string {
	if v, ok := m[in]; ok {
		return v
	}
	return in
}

// ResolveMany resolves a slice of alias keys / hosts to canonical hosts,
// de-duplicating the result.
func (m Map) ResolveMany(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		r := m.Resolve(s)
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	return out
}

// IsBuiltIn reports whether the alias key is one of the compiled-in defaults.
func (m Map) IsBuiltIn(key string) bool {
	_, ok := BuiltIn[key]
	return ok
}

// IsOverridden reports whether the user's value for key differs from (or was
// absent from) the built-in map.
func (m Map) IsOverridden(key string) bool {
	bv, builtin := BuiltIn[key]
	if !builtin {
		return false
	}
	uv, present := m[key]
	if !present {
		return false
	}
	return uv != bv
}

// SortedKeys returns the alias keys in stable sorted order (useful for
// deterministic `gitscan alias list` output).
func (m Map) SortedKeys() []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}