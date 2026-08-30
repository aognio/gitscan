# gitscan

`gitscan` is a Go CLI that scans local filesystem trees for Git repositories,
extracts remotes, and collects per-repo stats concurrently.

It is designed to be **useful immediately after `go install`**: a small set of
built-in domain aliases (`github → github.com`, `gitlab → gitlab.com`) lets
`--domain github` work with **zero configuration**.

---

## Install

```sh
go install github.com/aognio/gitscan/cmd/gitscan@latest
```

Or build from source:

```sh
git clone https://github.com/aognio/gitscan.git
cd gitscan
make build
```

`gitscan` shells out to the real `git` binary for plumbing stats, so Git must
be on your `PATH` (it almost certainly already is).

Check the installed version:

```sh
gitscan --version
```

### As a Go package

The internal packages are importable as a library:

```go
import (
    "github.com/aognio/gitscan/internal/alias"
    "github.com/aognio/gitscan/internal/config"
    "github.com/aognio/gitscan/internal/scan"
)
```

`go get github.com/aognio/gitscan` adds the module to your `go.mod`.

---

## Quick start

```sh
# Add a root to scan (creates ~/.gitscan/gitscan.toml on first add)
gitscan root add ~/projects
gitscan root add ~/work --depth 4

# List configured roots
gitscan root list

# Scan all configured roots (bare gitscan just scans — TUI when stdout is a TTY)
gitscan

# Scan a path ad-hoc (one-off; does not register it as a root)
gitscan ~/code

# Scan multiple paths at once
gitscan ~/code ~/work

# Same thing, made explicit
gitscan scan

# Only github.com repos, full plumbing stats, JSON output for scripting
gitscan --domain github --full-stats --format json

# Find repos with uncommitted changes
gitscan --dirty-only

# Find repos with no commit in the last 30 days
gitscan --stale 30

# Find orphaned local repos with no origin
gitscan --no-remote

# Scan a root ad-hoc, ignoring configured roots
gitscan ~/code

# Show merged domain aliases (built-in + user)
gitscan alias list
```

---

## Usage examples

### Default scan (fast mode)

The default scan reads only the filesystem — it parses `.git/config` for
remotes and walks `.git/` for size. No `git` binary is spawned, so it's fast
even across hundreds of repos.

When stdout is a TTY, the table is rendered through **Glamour** with Unicode
borders, aligned columns, and syntax highlighting. When piped, the raw
markdown table is emitted (readable in any plaintext context — logs, PRs,
devnotes):

```text
$ gitscan --plain ~/code
| # | Path | Host | Origin | .git size | State |
|---:|---|---|---|---:|---|
| 1 | .../code/gitscan |  | — | 25.6KB | no-remote |
| 2 | .../code/interdim | gitea.com | gitea.com/gnrfan/interdim | 95.6KB | ok |
| 3 | .../code/mmvault | gitea.com | gitea.com/gnrfan/mmvault | 48.4KB | ok |
| 4 | .../code/kangoo | github.com | github.com/aognio/kangoo | 444.0KB | ok |
| 5 | .../code/consolehub | github.com | github.com/aognio/consolehub | 18.2MB | ok |
| 6 | .../code/webcrush | gitea.com | gitea.com/webcrush | 38.1MB | ok |

**Total: 6 repos**

6 repos | 6 clean | 0 dirty | 1 no-remote
```

### Full stats (plumbing)

Pass `--full-stats` to shell out to `git` for branch/commit/object counts,
porcelain status, and last commit time. The table expands to include the
plumbing columns. Use `--dirty-only` to surface only repos with uncommitted
work:

```text
$ gitscan --plain ~/code --full-stats --dirty-only
| # | Path | Host | Origin | Branches | Commits | Objects | .git size | State |
|---:|---|---|---|---:|---:|---:|---:|---|
| 1 | .../code/gitscan |  | — | 0 | 0 | 0 | 25.6KB | dirty(8) |
| 2 | .../code/mmvault | gitea.com | gitea.com/gnrfan/mmvault | 1 | 1 | 36 | 48.4KB | dirty(14) |
| 3 | .../code/fsasap-mcp |  | — | 1 | 11 | 152 | 235.3KB | dirty(3) |
| 4 | .../code/studyn |  | — | 0 | 0 | 0 | 25.6KB | dirty(13) |
| 5 | .../code/telerep | github.com | github.com/user/telerep | 2 | 176 | 795 | 8.1MB | dirty(1) |

**Total: 5 repos**

5 repos | 0 clean | 5 dirty | 2 no-remote
```

The number in `dirty(N)` is the count of uncommitted file entries from
`git status --porcelain`.

### Filter by domain alias

Built-in aliases resolve through to their canonical host, so `--domain github`
matches any repo whose origin is on `github.com` (regardless of protocol —
SSH, HTTPS, or `git@` URLs all collapse to the same host):

```text
$ gitscan --plain ~/code --domain github
| # | Path | Host | Origin | .git size | State |
|---:|---|---|---|---:|---|
| 1 | .../code/kangoo | github.com | github.com/aognio/kangoo | 444.0KB | ok |
| 2 | .../code/consolehub | github.com | github.com/aognio/consolehub | 18.2MB | ok |
| 3 | .../code/telerep | github.com | github.com/user/telerep | 8.1MB | ok |

**Total: 3 repos**

3 repos | 3 clean | 0 dirty | 0 no-remote
```

Use `--exclude-domain` to drop hosts, and `--protocol ssh|https` to slice
further by transport.

### JSON output (for scripting)

```sh
$ gitscan --plain ~/code --format json | jq '.[] | select(.dirty) | .path'
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
$ gitscan --plain ~/code --format csv
path,host,origin_url,branches,commits,objects,dotgit_size,dotgit_files,dirty,dirty_count,last_commit
/home/user/code/gitscan,,,0,0,0,26246,18,false,0,0001-01-01
/home/user/code/interdim,gitea.com,https://gitea.com/gnrfan/interdim.git,0,0,0,97913,28,false,0,0001-01-01
/home/user/code/mmvault,gitea.com,https://gitea.com/gnrfan/mmvault.git,0,0,0,49562,32,false,0,0001-01-01
...
total,13
```

### Markdown output

`--format markdown` emits a raw markdown table (no Glamour rendering) — handy
for pasting into issues, notes, or `.development/` artifacts. The default
`--format table` also produces a markdown table, but renders it through
Glamour when stdout is a TTY for a richer visual experience:

```text
$ gitscan --plain ~/code --format markdown
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
through repos. The live view uses the terminal's current dimensions and adapts
when the terminal is resized:

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

Use `--browse` to open the completed results in a full-screen browser. It
supports row selection, vertical paging, horizontal scrolling, and a detail
area showing the complete path and origin URL:

```text
$ gitscan --browse ~/code
```

Use the arrow keys (or `h`/`j`/`k`/`l`) to navigate, `PageUp`/`PageDown` to
move by a screen, and `q` to leave the browser. `--browse` is available only
with the table format.

### Find stale repos

`--stale N` keeps only repos with no commit in the last N days. It relies on
the last-commit timestamp, so pass `--full-stats` to populate it:

```sh
# Repos untouched in the last 90 days
gitscan --full-stats --stale 90
```

### Find orphaned repos

`--no-remote` shows only repos with no configured origin — useful for finding
local-only work that was never pushed anywhere:

```text
$ gitscan --plain ~/code --no-remote
| # | Path | Host | Origin | .git size | State |
|---:|---|---|---|---:|---|
| 1 | .../code/gitscan |  | — | 25.6KB | no-remote |
| 2 | .../code/quickproxy |  | — | 5.8MB | no-remote |
| 3 | .../code/studyn |  | — | 25.6KB | no-remote |

**Total: 3 repos**

3 repos | 3 clean | 0 dirty | 3 no-remote
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
| `gitscan` | Scan all configured roots (default) |
| `gitscan scan` | Same as bare `gitscan` (explicit form) |
| `gitscan root add <path> [--depth N]` | Add a scan root |
| `gitscan root remove <path>` | Remove a scan root |
| `gitscan root list` | List configured roots |
| `gitscan alias list` | List built-in and user domain aliases |
| `gitscan config init` | Scaffold a default `gitscan.toml` |
| `gitscan config show` | Show current configuration |
| `gitscan config set <key> <value>` | Set a config value |
| `gitscan completion <shell>` | Generate shell completion |
| `gitscan --version` | Show version |

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
| `--raw` | Emit raw markdown table (no Glamour rendering) |
| `--watch` | Force live TUI even when piped |
| `--browse` | Browse completed table results interactively |
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

### Table rendering

The `table` format (default) builds a markdown table from the scan results
and renders it through **Glamour** (glow-style: Unicode borders, aligned
columns, syntax highlighting) when stdout is a TTY. Use `--raw` to force raw
markdown output even on a TTY — handy for piping into other tools or pasting
into notes. `--format markdown` always emits raw markdown (no Glamour).

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
