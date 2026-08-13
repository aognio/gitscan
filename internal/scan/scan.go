// Package scan discovers Git repositories under one or more roots and
// collects per-repo stats concurrently over a bounded worker pool.
//
// Results stream out of Run over a channel so that either a Bubble Tea TUI
// model or a static renderer can consume them live.
package scan

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aognio/gitscan/internal/alias"
	"github.com/aognio/gitscan/internal/config"
	"github.com/aognio/gitscan/internal/gitio"
)

// Filter captures the per-invocation filtering options applied after
// collection (domain allow/deny, dirty-only, stale, no-remote, protocol).
type Filter struct {
	Domains      []string // canonical hosts to keep (empty = all)
	ExcludeHosts []string // canonical hosts to drop
	DirtyOnly    bool
	NoRemote     bool
	Protocol     string // ssh | https | "" (any)
	StaleDays    int     // >0 => only repos with no commit in the last N days
}

// Options controls a scan run.
type Options struct {
	Roots        []config.Root
	Exclude      []string
	Concurrency  int
	FullStats    bool
	Filter       Filter
	Aliases      alias.Map
}

// Result is what streams out of Run for each discovered repo.
type Result struct {
	Repo string
	Stat gitio.Stat
	Err  error
}

// Run discovers repos under the configured roots and streams results. It
// returns a channel closed when the scan is done, plus a context cancel
// function the caller may invoke to abort.
func Run(ctx context.Context, opts Options) (<-chan Result, context.CancelFunc) {
	if opts.Concurrency <= 0 {
		opts.Concurrency = config.DefaultConcurrency
	}
	ctx, cancel := context.WithCancel(ctx)
	out := make(chan Result, opts.Concurrency*2)

	go func() {
		defer close(out)
		repos := discover(ctx, opts)
		collect(ctx, opts, repos, out)
	}()
	return out, cancel
}

// discover walks the configured roots and returns a list of repo directories.
// Honors per-root Depth and the global ExcludePatterns. Walking is aborted if
// ctx is cancelled.
func discover(ctx context.Context, opts Options) []string {
	var repos []string
	seen := make(map[string]struct{})

	for _, root := range opts.Roots {
		if err := ctx.Err(); err != nil {
			break
		}
		abs := expandPath(root.Path)
		maxDepth := root.Depth
		if maxDepth <= 0 {
			maxDepth = config.DefaultDepth
		}

		_ = filepath.WalkDir(abs, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // skip unreadable entries
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if d.IsDir() {
				// Skip excluded directory names entirely.
				if isExcluded(d.Name(), opts.Exclude) && p != abs {
					return filepath.SkipDir
				}
				// Depth check relative to the root.
				rel, _ := filepath.Rel(abs, p)
				if rel != "." && strings.Count(rel, string(os.PathSeparator)) >= maxDepth {
					return filepath.SkipDir
				}
				if gitio.IsGitRepo(p) {
					if _, ok := seen[p]; !ok {
						seen[p] = struct{}{}
						repos = append(repos, p)
					}
					return filepath.SkipDir // do not descend into .git
				}
			}
			return nil
		})
	}
	return repos
}

// collect spawns a bounded worker pool over repos and streams results to out.
// Each worker fast-collects first (filesystem-only), then optionally shells
// out for plumbing if FullStats is set. Filtering is applied after collection;
// filtered-out repos do not produce a Result.
func collect(ctx context.Context, opts Options, repos []string, out chan<- Result) {
	if len(repos) == 0 {
		return
	}
	conc := opts.Concurrency
	if conc > len(repos) {
		conc = len(repos)
	}
	jobs := make(chan string, conc)
	var wg sync.WaitGroup
	for i := 0; i < conc; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for repo := range jobs {
				if ctx.Err() != nil {
					return
				}
				st := gitio.CollectFast(repo)
				if opts.FullStats {
					gitio.CollectFull(&st)
				}
				if opts.Filter.accepts(st, opts.Aliases) {
					select {
					case out <- Result{Repo: repo, Stat: st}:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}
	for _, r := range repos {
		select {
		case jobs <- r:
		case <-ctx.Done():
			break
		}
	}
	close(jobs)
	wg.Wait()
}

// accepts reports whether a collected Stat passes the applied Filter.
func (f Filter) accepts(st gitio.Stat, aliases alias.Map) bool {
	if f.NoRemote && st.OriginURL != "" {
		return false
	}
	if f.NoRemote && st.OriginURL == "" {
		// no-remote filter: keep only repos with no origin
	}
	if !f.NoRemote && st.OriginURL == "" && len(f.Domains) > 0 {
		return false // a domain filter requires an origin
	}
	if len(f.Domains) > 0 && !hostInList(st.Host, f.Domains) {
		return false
	}
	if len(f.ExcludeHosts) > 0 && hostInList(st.Host, f.ExcludeHosts) {
		return false
	}
	if f.DirtyOnly && !st.Dirty {
		return false
	}
	if f.Protocol != "" {
		if protocolOf(st.OriginURL) != f.Protocol {
			return false
		}
	}
	if f.StaleDays > 0 && !st.LastCommit.IsZero() {
		if daysSince(st.LastCommit) < f.StaleDays {
			return false
		}
	}
	return true
}

func hostInList(host string, list []string) bool {
	for _, l := range list {
		if l == host {
			return true
		}
	}
	return false
}

func protocolOf(u string) string {
	switch {
	case strings.HasPrefix(u, "git@"), strings.HasPrefix(u, "ssh://"):
		return "ssh"
	case strings.HasPrefix(u, "http://"), strings.HasPrefix(u, "https://"):
		return "https"
	}
	return ""
}

// isExcluded checks a directory name against the exclude patterns. Patterns
// are plain name matches (no glob), matching the common case
// (node_modules, vendor, .cache).
func isExcluded(name string, patterns []string) bool {
	for _, p := range patterns {
		if p == "" {
			continue
		}
		if name == p {
			return true
		}
		if strings.ContainsAny(p, "*?") {
			ok, _ := filepath.Match(p, name)
			if ok {
				return true
			}
		}
	}
	return false
}

// daysSince returns the number of days since t (UTC).
func daysSince(t time.Time) int {
	return int(time.Since(t).Hours() / 24)
}

// expandPath resolves a leading ~ to the user's home directory.
func expandPath(p string) string {
	if p == "" {
		return p
	}
	if p == "~" {
		home, _ := os.UserHomeDir()
		return home
	}
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	abs, _ := filepath.Abs(p)
	return abs
}