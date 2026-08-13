// Package gitio gathers per-repo data.
//
// It implements gitscan's hybrid access strategy:
//   - "Free" data (remotes from .git/config, folder size) is read directly
//     from the filesystem, no git binary spawned.
//   - "Plumbing" data (object/branch/commit counts, porcelain status) is
//     gathered by shelling out to the real git executable.
package gitio

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Remote is a configured remote: its name and the list of fetch/push URLs
// (typically one, but a remote may have several).
type Remote struct {
	Name string
	URLs []string
}

// Stat is the per-repo data collected by a single pass.
type Stat struct {
	Path        string        `json:"path"`
	Remotes     []Remote      `json:"remotes,omitempty"`
	OriginURL   string        `json:"origin_url,omitempty"`
	Host        string        `json:"host,omitempty"`
	DotGitSize  int64         `json:"dotgit_size"`
	DotGitFiles int           `json:"dotgit_files"`
	Dirty       bool          `json:"dirty"`
	DirtyCount  int           `json:"dirty_count,omitempty"`
	Branches    int           `json:"branches,omitempty"`
	Commits     int           `json:"commits,omitempty"`
	Objects     int           `json:"objects,omitempty"`
	LastCommit  time.Time     `json:"last_commit,omitempty"`
	Error       string        `json:"error,omitempty"`
	CollectedAt time.Time     `json:"collected_at"`
}

// IsGitRepo reports whether dir contains a ".git" entry (directory or file,
// the latter for worktrees/submodules).
func IsGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// ReadRemotes parses .git/config directly (no git binary) and returns the
// configured remotes. The first remote named "origin" is also returned as a
// convenience.
func ReadRemotes(repoDir string) (remotes []Remote, origin string) {
	cfg := filepath.Join(repoDir, ".git", "config")
	f, err := os.Open(cfg)
	if err != nil {
		return nil, ""
	}
	defer f.Close()

	byName := make(map[string]*Remote)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	var cur *Remote
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			head := strings.TrimSpace(line[1 : len(line)-1])
			// e.g. remote "origin" or remote origin
			if !strings.HasPrefix(head, "remote") {
				cur = nil
				continue
			}
			name := strings.TrimSpace(strings.TrimPrefix(head, "remote"))
			name = strings.Trim(name, "\"")
			cur = &Remote{Name: name}
			byName[name] = cur
			continue
		}
		if cur == nil {
			continue
		}
		if eq := strings.IndexByte(line, '='); eq > 0 {
			key := strings.TrimSpace(line[:eq])
			val := strings.TrimSpace(line[eq+1:])
			if key == "url" || strings.HasSuffix(key, ".url") ||
				strings.HasSuffix(key, "pushurl") {
				cur.URLs = append(cur.URLs, val)
			}
		}
	}
	for _, r := range byName {
		remotes = append(remotes, *r)
	}
	if o, ok := byName["origin"]; ok && len(o.URLs) > 0 {
		origin = o.URLs[0]
	}
	return remotes, origin
}

// HostFromURL extracts the host portion of a git remote URL across the common
// protocols (git@host:path, https://host/path, ssh://git@host/path).
func HostFromURL(u string) string {
	u = strings.TrimSpace(u)
	switch {
	case strings.HasPrefix(u, "git@"):
		// git@github.com:org/repo.git
		if i := strings.Index(u, "@"); i >= 0 {
			rest := u[i+1:]
			if j := strings.IndexAny(rest, ":/"); j >= 0 {
				return rest[:j]
			}
			return rest
		}
	case strings.HasPrefix(u, "ssh://"):
		rest := strings.TrimPrefix(u, "ssh://")
		if i := strings.Index(rest, "@"); i >= 0 {
			rest = rest[i+1:]
		}
		if j := strings.Index(rest, "/"); j >= 0 {
			return rest[:j]
		}
		return rest
	case strings.HasPrefix(u, "http://"), strings.HasPrefix(u, "https://"):
		rest := u
		if i := strings.Index(rest, "://"); i >= 0 {
			rest = rest[i+3:]
		}
		if i := strings.Index(rest, "@"); i >= 0 {
			rest = rest[i+1:]
		}
		if j := strings.Index(rest, "/"); j >= 0 {
			return rest[:j]
		}
		return rest
	case strings.HasPrefix(u, "file://"):
		return ""
	}
	return ""
}

// NormalizeURL canonicalizes a remote URL to an https form when it is an
// ssh-style git@host:path URL, so "origins regardless of protocol" actually
// match. Non-git URLs are returned mostly unchanged (host lowercased).
func NormalizeURL(u string) string {
	u = strings.TrimSpace(u)
	if strings.HasPrefix(u, "git@") {
		if i := strings.Index(u, "@"); i >= 0 {
			rest := u[i+1:]
			if j := strings.Index(rest, ":"); j >= 0 {
				return "https://" + rest[:j] + "/" + rest[j+1:]
			}
		}
	}
	if strings.HasPrefix(u, "ssh://") {
		rest := strings.TrimPrefix(u, "ssh://")
		if i := strings.Index(rest, "@"); i >= 0 {
			rest = rest[i+1:]
		}
		return "https://" + rest
	}
	return u
}

// DotGitSize walks dir/.git and returns its on-disk size and file count.
func DotGitSize(repoDir string) (size int64, files int) {
	root := filepath.Join(repoDir, ".git")
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		size += info.Size()
		files++
		return nil
	})
	return size, files
}

// gitBinary returns the path to git, or an error if it isn't on PATH.
func gitBinary() (string, error) {
	return exec.LookPath("git")
}

// runGit runs `git -C dir args...` and returns its stdout (trimmed) and any
// error. Errors from git are surfaced as-is (caller decides whether to set
// Stat.Error or skip).
func runGit(dir string, args ...string) (string, error) {
	git, err := gitBinary()
	if err != nil {
		return "", errors.New("git executable not found on PATH")
	}
	cmd := exec.Command(git, append([]string{"-C", dir}, args...)...)
	cmd.Stderr = nil
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// CollectFast gathers the free filesystem data: remotes, origin, host, and
// .git folder size. No git binary is spawned.
func CollectFast(repoDir string) Stat {
	st := Stat{
		Path:        repoDir,
		CollectedAt: time.Now().UTC(),
	}
	remotes, origin := ReadRemotes(repoDir)
	st.Remotes = remotes
	st.OriginURL = origin
	st.Host = HostFromURL(origin)
	size, files := DotGitSize(repoDir)
	st.DotGitSize = size
	st.DotGitFiles = files
	return st
}

// CollectFull extends a fast-collected Stat with the expensive plumbing data:
// dirty flag + count, branch count, commit count, object count, last commit.
// It shells out to git. Any per-field failure is recorded on the Stat but does
// not abort the others.
func CollectFull(st *Stat) {
	st.Dirty, st.DirtyCount = porcelainStatus(st.Path)
	if n, err := strconv.Atoi(runGitOrZero(st.Path, "rev-list", "--count", "HEAD")); err == nil {
		st.Commits = n
	}
	if n, err := strconv.Atoi(runGitOrZero(st.Path, "rev-parse", "--abbrev-ref", "--branches", "--quiet")); err == nil {
		_ = n
	}
	st.Branches = countBranches(st.Path)
	st.Objects = countObjects(st.Path)
	st.LastCommit = lastCommitTime(st.Path)
}

func runGitOrZero(dir string, args ...string) string {
	out, err := runGit(dir, args...)
	if err != nil {
		return "0"
	}
	return out
}

func porcelainStatus(dir string) (dirty bool, count int) {
	out, err := runGit(dir, "status", "--porcelain")
	if err != nil {
		return false, 0
	}
	if out == "" {
		return false, 0
	}
	count = strings.Count(out, "\n") + 1
	return true, count
}

func countBranches(dir string) int {
	out, err := runGit(dir, "branch", "--list")
	if err != nil || out == "" {
		return 0
	}
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(strings.TrimPrefix(line, "*")) != "" {
			n++
		}
	}
	return n
}

func countObjects(dir string) int {
	out, err := runGit(dir, "count-objects", "-v")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "count: ") {
			if n, err := strconv.Atoi(strings.TrimPrefix(line, "count: ")); err == nil {
				return n
			}
		}
	}
	return 0
}

func lastCommitTime(dir string) time.Time {
	out, err := runGit(dir, "log", "-1", "--format=%cI")
	if err != nil || out == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, out)
	if err != nil {
		return time.Time{}
	}
	return t
}