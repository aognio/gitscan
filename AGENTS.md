# AGENTS.md — gitscan

Welcome, fellow agent. `gitscan` is a Go CLI that scans local filesystem
trees for Git repositories, extracts remotes, and collects per-repo stats
concurrently. This document is the contract for working in this repo.

## Project facts

- **Module:** `github.com/aognio/gitscan` (MIT licensed)
- **Toolchain:** Go 1.24+ (the `go.mod` toolchain line is authoritative)
- **Runtime dep:** the real `git` binary on `PATH` (for `--full-stats` plumbing)
- **Config path:** `$HOME/.gitscan/gitscan.toml` (created lazily on first
  `gitscan root add`). Usable with **zero config** thanks to compiled-in
  `github`/`gitlab` domain aliases.

## Architecture (in one screen)

```
main.go                       # entrypoint
cmd/root.go                   # all CLI commands (cobra): scan, root, alias, config, completion
internal/
├── alias/alias.go            # built-in github/gitlab + user merge, Resolve/ResolveMany
├── config/config.go          # TOML load/save at $HOME/.gitscan/gitscan.toml, Root add/remove
├── gitio/gitio.go            # hybrid access: ReadRemotes(.git/config), DotGitSize, CollectFast/CollectFull
├── scan/scan.go              # discover(.git dirs) + bounded worker pool → results stream over a channel
└── ui/
    ├── ui.go                 # static renderers: table (Lip Gloss), JSON, CSV, markdown
    └── tui.go                # Bubble Tea live view (TTY-aware default)
```

### Key design decisions (see `.development/decisions/`)

- **Hybrid access** — parse `.git/config` directly for free data (remotes,
  size); shell out to `git` for plumbing (branches/commits/objects/status),
  gated behind `--full-stats`.
- **Config has no `[filter]` section** — domain filtering is a per-invocation
  CLI concern, not stored state.
- **Built-in domain aliases compiled into the binary** (`github`, `gitlab`)
  so the tool is useful immediately after `go install` with no config file.
- **Charmbracelet UI stack** — Lip Gloss (styling), Bubble Tea (live TUI),
  Bubbles (components), Glamour (markdown rendering).
- **TTY-aware default UX** — live Bubble Tea view when stdout is a TTY, static
  rendered output when piped; `--plain` forces static, `--watch` forces live.
  The worker pool always streams results over a channel; the TUI and static
  renderers are two consumers of the same stream.

## Essential commands

```bash
# Build the binary into the repo root
go build -o gitscan .

# Run all tests (none yet — when added, use this)
go test ./...

# Vet
go vet ./...

# Format (prefer gofumpt; fall back to goimports / gofmt)
gofumpt -w .

# Smoke test against a real tree of Git repos
./gitscan scan --plain --root ~/code
./gitscan scan --plain --root ~/code --full-stats --dirty-only
./gitscan scan --plain --root ~/code --domain github --format json

# Subcommands
./gitscan root add ~/code --depth 4
./gitscan root list
./gitscan alias list
./gitscan config show
```

**Always run `go build ./...` and `go vet ./...` before declaring a task
complete.** If a lint/format command was provided to you, run it too.

## Workflow rules

1. **Git discipline** — all active development on the `wip` branch. Commit
   small, logical changes. Use semantic commit prefixes (`feat:`, `fix:`,
   `refactor:`, `docs:`, `chore:`, `test:`).
2. **Engineering memory** — `.development/` is an **independent Git repository**
   on branch `main`, ignored by the main repo. Keep it synchronized with
   architectural changes, decisions, investigation notes, and STATUS/TASKS
   updates. See `.development/README.md` for structure and conventions.
3. **No comments in code unless asked** — the codebase is comment-light by
   convention; the design rationale lives in `.development/decisions/`, not in
   source comments.
4. **No new dependencies without checking the codebase first** — the project
   already uses cobra, go-toml/v2, the Charmbracelet stack, and
   `golang.org/x/term`. Prefer extending what's there over pulling something
   new.
5. **Don't commit generated runtime data** — nothing under `~/.gitscan/`,
   no `coverage.*`, no built binaries (`/gitscan`, `/bin/`).
6. **Hybrid access is the core invariant** — never shell out to `git` for data
   that's free on the filesystem (remotes from `.git/config`, `.git` size),
   and never hand-roll Git plumbing that the real `git` binary already does.
   The `--full-stats` gate exists for a reason — keep fast mode fast.

## Code style

- Go formatting: `gofmt` at minimum; `gofumpt` preferred.
- Imports grouped: stdlib, then external, then internal (`github.com/aognio/...`).
- Exported identifiers get doc comments; unexported ones don't need them.
- Errors are returned explicitly and wrapped with `fmt.Errorf("...: %w", err)`.
- File permissions use octal (`0o755`, `0o644`).
- JSON tags use `snake_case` (already established on `gitio.Stat`).

## v0.2 backlog (see `.development/TASKS.md` for the full list)

- Unit tests for: alias merge, config round-trip, host parsing, URL
  normalization, scan discovery.
- Caching layer (`~/.gitscan/cache.json` keyed by repo path + `.git/refs` mtime).
- Health checks: detached HEAD, no upstream tracking branch, `.git` bloat.
- Duplicate/fork detection (repos sharing the same origin URL).
- `--stale` auto-enables plumbing (currently requires `--full-stats`).
- `--devnote` flag: dump scan as a `.development/`-style artifact.

Don't start any of these without explicit user direction.