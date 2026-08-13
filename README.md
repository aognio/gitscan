# gitscan

`gitscan` is a Go CLI that scans local filesystem trees for Git repositories,
extracts remotes, and collects per-repo stats concurrently.

It is designed to be **useful immediately after `go install`**: a small set of
built-in domain aliases (`github → github.com`, `gitlab → gitlab.com`) lets
`--domain github` work with **zero configuration**.

---

## Install

```sh
go install github.com/aognio/gitscan@latest
```

Or build from source:

```sh
git clone https://github.com/aognio/gitscan.git
cd gitscan
go build -o gitscan .
./gitscan --help
```

`gitscan` shells out to the real `git` binary for plumbing stats, so Git must
be on your `PATH` (it almost certainly already is).

---

## Quick start

```sh
# Add a root to scan (creates ~/.gitscan/gitscan.toml on first add)
gitscan root add ~/projects
gitscan root add ~/work --depth 4

# List configured roots
gitscan root list

# Scan all configured roots (live TUI when stdout is a TTY)
gitscan scan

# Only github.com repos, full plumbing stats, JSON output for scripting
gitscan scan --domain github --full-stats --format json

# Find repos with uncommitted changes
gitscan scan --dirty-only

# Find repos with no commit in the last 30 days
gitscan scan --stale 30

# Find orphaned local repos with no origin
gitscan scan --no-remote

# Scan a root ad-hoc, ignoring configured roots
gitscan scan --root ~/code

# Show merged domain aliases (built-in + user)
gitscan alias list
```

---

## Usage examples

### Default scan (fast mode)

The default scan reads only the filesystem — it parses `.git/config` for
remotes and walks `.git/` for size. No `git` binary is spawned, so it's fast
even across hundreds of repos.

```text
$ gitscan scan --plain --root ~/code
PATH                       HOST       ORIGIN                           BR  CM  OBJ  GIT-SIZE  STATE
.../code/gitscan                         0   0   0    25.6KB   no-remote
.../code/interdim           gitea.com  gitea.com/gnrfan/interdim         0   0   0    95.6KB   ok
.../code/mmvault            gitea.com  gitea.com/gnrfan/mmvault          0   0   0    48.4KB   ok
.../code/kangoo             github.com git@github.com:aognio/kangoo      0   0   0    444.0KB  ok
.../code/consolehub        github.com git@github.com:aognio/consolehub  0   0   0    18.2MB   ok
.../code/webcrush           gitea.com  gitea.com/webcrush                 0   0   0    38.1MB   ok

6 repos
```

Columns: **BR** = branches, **CM** = commits, **OBJ** = loose objects.
These are zero in fast mode — they populate with `--full-stats`.

### Full stats (plumbing)

Pass `--full-stats` to shell out to `git` for branch/commit/object counts,
porcelain status, and last commit time. Use `--dirty-only` to surface only
repos with uncommitted work:

```text
$ gitscan scan --plain --root ~/code --full-stats --dirty-only
PATH                       HOST       ORIGIN                          BR  CM   OBJ  GIT-SIZE  STATE
.../code/gitscan                         0   0    0    25.6KB   dirty(8)
.../code/mmvault            gitea.com  gitea.com/gnrfan/mmvault         1   1    36   48.4KB   dirty(14)
.../code/fsasap-mcp                      1   11   152  235.3KB  dirty(3)
.../code/studyn                          0   0    0    25.6KB   dirty(13)
.../code/telerep            github.com git@github.com:user/telerep     2   176  795  8.1MB    dirty(1)

5 repos
```

The number in `dirty(N)` is the count of uncommitted file entries from
`git status --porcelain`.

### Filter by domain alias

Built-in aliases resolve through to their canonical host, so `--domain github`
matches any repo whose origin is on `github.com` (regardless of protocol —
SSH, HTTPS, or `git@` URLs all collapse to the same host):

```text
$ gitscan scan --plain --root ~/code --domain github
PATH                       HOST       ORIGIN                           BR  CM  OBJ  GIT-SIZE  STATE
.../code/kangoo             github.com git@github.com:aognio/kangoo      0   0   0    444.0KB  ok
.../code/consolehub        github.com git@github.com:aognio/consolehub  0   0   0    18.2MB   ok
.../code/telerep            github.com git@github.com:user/telerep      0   0   0    8.1MB    ok

3 repos
```

Use `--exclude-domain` to drop hosts, and `--protocol ssh|https` to slice
further by transport.

### JSON output (for scripting)

```sh
$ gitscan scan --plain --root ~/code --format json | jq '.[] | select(.dirty) | .path'
"/home/user/code/gitscan"
"/home/user/code/mmvault"
"/home/user/code/fsasap-mcp"
```

Each element is a full `Stat` object:

```json
[
  {
    "path": "/home/user/code/interdim",
    "remotes": [{"Name": "origin", "URLs": ["https://gitea.com/gnrfan/interdim.git"]}],
    "origin_url": "https://gitea.com/gnrfan/interdim.git",
    "host": "gitea.com",
    "dotgit_size": 97913,
    "dotgit_files": 28,
    "dirty": false,
    "branches": 1,
    "commits": 1,
    "objects": 0,
    "last_commit": "2026-08-01T10:23:41Z",
    "collected_at": "2026-08-13T12:44:55.145621623Z"
  }
]
```

### CSV output

```text
$ gitscan scan --plain --root ~/code --format csv
path,host,origin_url,branches,commits,objects,dotgit_size,dotgit_files,dirty,dirty_count,last_commit
/home/user/code/gitscan,,,0,0,0,26246,18,false,0,0001-01-01
/home/user/code/interdim,gitea.com,https://gitea.com/gnrfan/interdim.git,0,0,0,97913,28,false,0,0001-01-01
/home/user/code/mmvault,gitea.com,https://gitea.com/gnrfan/mmvault.git,0,0,0,49562,32,false,0,0001-01-01
...
total,13
```

### Markdown output

Renders as a GitHub-flavored markdown table — handy for pasting into issues,
notes, or `.development/` artifacts:

```text
$ gitscan scan --plain --root ~/code --format markdown
| Path | Host | Origin | Branches | Commits | Objects | .git size | State |
|---|---|---|---:|---:|---:|---:|---|
| /home/user/code/interdim | gitea.com | gitea.com/gnrfan/interdim | 0 | 0 | 0 | 95.6KB | ok |
| /home/user/code/kangoo | github.com | github.com/aognio/kangoo | 0 | 0 | 0 | 444.0KB | ok |
...
**Total: 6 repos**
```

### Live TUI

When stdout is a terminal, `gitscan scan` opens a Bubble Tea live view with a
spinner, running repo count, and streaming rows as the worker pool churns
through repos:

```text
⠹ scanning... 6 repos found

.../code/gitscan                        ok
.../code/interdim        gitea.com  gitea.com/gnrfan/interdim  ok
.../code/mmvault         gitea.com  gitea.com/gnrfan/mmvault   dirty(14)

press q to quit
```

Press `q` to cancel the scan and exit. Pipe or redirect stdout and gitscan
falls back to the static table automatically — use `--plain` to force static
output in a TTY, or `--watch` to force the live view even when piped.

### Find stale repos

`--stale N` keeps only repos with no commit in the last N days. It relies on
the last-commit timestamp, so it auto-enables the plumbing path:

```sh
# Repos untouched in the last 90 days
gitscan scan --stale 90
```

### Find orphaned repos

`--no-remote` shows only repos with no configured origin — useful for finding
local-only work that was never pushed anywhere:

```text
$ gitscan scan --plain --root ~/code --no-remote
PATH                       HOST  ORIGIN  BR  CM  OBJ  GIT-SIZE  STATE
.../code/gitscan                         0   0   0    25.6KB   no-remote
.../code/quickproxy                       0   0   0    5.8MB    no-remote
.../code/studyn                           0   0   0    25.6KB   no-remote

3 repos
```

### Root management

```sh
# Add with a per-root depth override
gitscan root add ~/projects --depth 8

# Adding the same path again updates its depth (idempotent)
gitscan root add ~/projects --depth 4

# Remove
gitscan root remove ~/projects

# List
gitscan root list
```

```text
$ gitscan root list
PATH                                      DEPTH
--------------------------------------------------
/home/user/code/experiments                         6
/home/user/work                                    4
```

### Domain aliases

```text
$ gitscan alias list
ALIAS            HOST                            SOURCE
------------------------------------------------------------
github           github.com                      built-in
gitlab           gitlab.com                      built-in
```

Add your own in `~/.gitscan/gitscan.toml`:

```toml
[aliases]
acme = "git.acme-internal.com"
gitlab = "gitlab.mycompany.com"   # overrides the built-in
```

```text
$ gitscan alias list
ALIAS            HOST                            SOURCE
------------------------------------------------------------
acme             git.acme-internal.com             user
github           github.com                       built-in
gitlab           gitlab.mycompany.com             user (override)
```

### Config init / show / set

```sh
# Scaffold a default config file at ~/.gitscan/gitscan.toml
gitscan config init

# Show the merged config (file values + defaults)
gitscan config show

# Set individual values (rewrites the TOML)
gitscan config set scan.concurrency 16
gitscan config set output.format json
gitscan config set output.color true
```

### Shell completion

```sh
# Bash (append to ~/.bashrc)
gitscan completion bash >> ~/.bashrc

# Zsh
gitscan completion zsh > ~/.zsh/completions/_gitscan

# Fish
gitscan completion fish > ~/.config/fish/completions/gitscan.fish
```

---

## Configuration

Config lives at `$HOME/.gitscan/gitscan.toml` and is created lazily on first
`gitscan root add`. There is intentionally **no `[filter]` section** — domain
filtering is a per-invocation CLI concern, not stored config.

```toml
[scan]
concurrency = 8
exclude_patterns = ["node_modules", "vendor", ".cache"]

[[roots]]
path = "/home/user/projects"
depth = 6

[[roots]]
path = "/home/user/work"
depth = 4

[aliases]
# user aliases merge with (and may override) the built-in defaults
acme = "git.acme-internal.com"

[output]
format = "table"
color = true
```

Per-invocation flags always override the TOML (standard CLI precedence).

### Built-in domain aliases

| Alias    | Host          | Source    |
|----------|---------------|-----------|
| `github` | `github.com`  | built-in  |
| `gitlab` | `gitlab.com`  | built-in  |

`gitscan alias list` shows the merged table (built-in + user), and marks which
entries the user has overridden. The built-ins are usable even when **no
config file exists** — the tool works out of the box after `go install`.

---

## Commands

| Command | Description |
|---|---|
| `gitscan scan` | Discover repos and collect stats |
| `gitscan root add <path> [--depth N]` | Add a scan root |
| `gitscan root remove <path>` | Remove a scan root |
| `gitscan root list` | List configured roots |
| `gitscan alias list` | List built-in and user domain aliases |
| `gitscan config init` | Scaffold a default `gitscan.toml` |
| `gitscan config show` | Show current configuration |
| `gitscan config set <key> <value>` | Set a config value |
| `gitscan completion <shell>` | Generate shell completion |

## Scan flags

| Flag | Description |
|---|---|
| `--domain/-d <alias,host>` | Filter by domain alias or host (repeatable) |
| `--exclude-domain <host>` | Exclude domains by alias or host |
| `--dirty-only` | Show only repos with uncommitted changes |
| `--no-remote` | Show only repos with no configured origin |
| `--protocol ssh\|https` | Filter by remote protocol |
| `--stale <days>` | Repos with no commit in the last N days |
| `--full-stats` | Collect git plumbing stats (slower) |
| `--format/-f <fmt>` | `table` (default), `json`, `csv`, `markdown` |
| `--plain` | Force static output (no TUI) |
| `--watch` | Force live TUI even when piped |
| `--concurrency/-j <N>` | Worker pool size |
| `--root <path>` | Scan this root (repeatable; overrides config) |

---

## How it works

`gitscan` uses a **hybrid access strategy**:

- **Free filesystem data** (remote URLs from `.git/config`, `.git` folder size,
  file count) is read directly — no `git` binary spawned.
- **Real plumbing** (object/branch/commit counts, `status --porcelain`, last
  commit time) is collected by shelling out to `git`, gated behind
  `--full-stats` to keep the default scan fast.

Concurrency uses a bounded worker pool (errgroup-style), capped around
`runtime.NumCPU()` since these are I/O-bound git subprocess calls. Results
stream over a channel into either the Bubble Tea TUI (when stdout is a TTY)
or a static renderer (table/JSON/CSV/markdown).

### Module layout

```
gitscan/
├── main.go                  # entrypoint
├── cmd/root.go              # all CLI commands (cobra)
├── internal/
│   ├── alias/alias.go       # built-in github/gitlab aliases + user merge
│   ├── config/config.go      # $HOME/.gitscan/gitscan.toml load/save
│   ├── gitio/gitio.go        # hybrid access: parse .git/config + shell out
│   ├── scan/scan.go          # discovery + bounded worker pool
│   └── ui/
│       ├── ui.go             # table/JSON/CSV/markdown renderers
│       └── tui.go            # Bubble Tea live view
├── README.md
├── LICENSE                  # MIT
├── go.mod / go.sum
└── .gitignore
```

---

## Requirements

- Go 1.24 or newer (for building from source)
- Git on `PATH` (for `--full-stats` plumbing)

## License

MIT — see [LICENSE](LICENSE).