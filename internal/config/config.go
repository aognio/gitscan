// Package config loads and persists gitscan's TOML config at
// $HOME/.gitscan/gitscan.toml.
//
// The config stores where to scan (roots), a few scan tunables, user-defined
// domain aliases, and output defaults. There is intentionally no [filter]
// section — domain filtering is a per-invocation CLI concern.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/pelletier/go-toml/v2"
)

// Root is a single scanning root with an optional per-root depth override.
type Root struct {
	Path  string `toml:"path"`
	Depth int    `toml:"depth,omitempty"`
}

// Scan holds scan tunables.
type Scan struct {
	Concurrency     int      `toml:"concurrency,omitempty"`
	ExcludePatterns []string `toml:"exclude_patterns,omitempty"`
}

// Output holds output defaults.
type Output struct {
	Format string `toml:"format,omitempty"`
	Color  bool   `toml:"color,omitempty"`
}

// Config is the full gitscan config file model.
type Config struct {
	Scan   Scan             `toml:"scan,omitempty"`
	Roots  []Root           `toml:"roots,omitempty"`
	Aliases map[string]string `toml:"aliases,omitempty"`
	Output Output           `toml:"output,omitempty"`
}

// Defaults applied when values are unset / file missing.
const (
	DefaultConcurrency = 8
	DefaultDepth       = 6
	DefaultFormat      = "table"
)

// DefaultScan returns a Scan with defaults filled in where unset.
func DefaultScan() Scan {
	return Scan{Concurrency: DefaultConcurrency}
}

// Dir returns the gitscan config directory ($HOME/.gitscan).
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gitscan"), nil
}

// Path returns the config file path ($HOME/.gitscan/gitscan.toml).
func Path() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "gitscan.toml"), nil
}

// Load reads the config file. If the file does not exist, a zero-value Config
// with defaults applied is returned (the tool is usable with no file at all).
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c := &Config{Scan: DefaultScan()}
			return c, nil
		}
		return nil, fmt.Errorf("read config %s: %w", p, err)
	}
	var c Config
	if err := toml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", p, err)
	}
	c.applyDefaults()
	return &c, nil
}

// applyDefaults fills in zero/empty values with sensible defaults.
func (c *Config) applyDefaults() {
	if c.Scan.Concurrency <= 0 {
		c.Scan.Concurrency = DefaultConcurrency
	}
	if c.Output.Format == "" {
		c.Output.Format = DefaultFormat
	}
}

// Save writes the config to $HOME/.gitscan/gitscan.toml, creating the
// directory if needed.
func (c *Config) Save() error {
	d, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		return fmt.Errorf("create config dir %s: %w", d, err)
	}
	p, err := Path()
	if err != nil {
		return err
	}
	data, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return fmt.Errorf("write config %s: %w", p, err)
	}
	return nil
}

// AddRoot adds or updates a root. If the root already exists, its depth is
// updated (idempotent).
func (c *Config) AddRoot(path string, depth int) error {
	path, err := normalizePath(path)
	if err != nil {
		return err
	}
	if depth <= 0 {
		depth = DefaultDepth
	}
	for i := range c.Roots {
		if c.Roots[i].Path == path {
			c.Roots[i].Depth = depth
			return nil
		}
	}
	c.Roots = append(c.Roots, Root{Path: path, Depth: depth})
	c.sortRoots()
	return nil
}

// RemoveRoot removes a root. Returns ErrNotFound if it isn't present.
func (c *Config) RemoveRoot(path string) error {
	path, err := normalizePath(path)
	if err != nil {
		return err
	}
	for i := range c.Roots {
		if c.Roots[i].Path == path {
			c.Roots = append(c.Roots[:i], c.Roots[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

// ErrNotFound is returned by RemoveRoot when the path is not configured.
var ErrNotFound = errors.New("root not found")

func (c *Config) sortRoots() {
	sort.Slice(c.Roots, func(i, j int) bool {
		return c.Roots[i].Path < c.Roots[j].Path
	})
}

// normalizePath expands a leading ~ and returns an absolute path.
func normalizePath(p string) (string, error) {
	if p == "" {
		return "", errors.New("empty path")
	}
	if p == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return home, nil
	}
	if len(p) >= 2 && p[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, p[2:])
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return abs, nil
}