// Package cmd implements the gitscan CLI commands.
package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aognio/gitscan/internal/alias"
	"github.com/aognio/gitscan/internal/config"
	"github.com/aognio/gitscan/internal/scan"
	"github.com/aognio/gitscan/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// expandUser resolves a leading ~ to the user's home directory.
func expandUser(p string) (string, error) {
	if p == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return home, nil
	}
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, p[2:]), nil
	}
	return p, nil
}

// New returns the root gitscan command. Running `gitscan` with no subcommand
// is equivalent to `gitscan scan` — it scans all configured roots with the
// default options. All scan flags are persistent flags on the root command,
// so `gitscan --plain --domain github` works just like
// `gitscan scan --plain --domain github`.
func New() *cobra.Command {
	root := &cobra.Command{
		Use:   "gitscan [path...]",
		Short: "Scan local filesystem for Git repos and collect stats",
		Long: "gitscan discovers Git repositories under configured roots, " +
			"extracts remotes, and collects per-repo stats concurrently. " +
			"Config: $HOME/.gitscan/gitscan.toml (usable with zero config " +
			"thanks to built-in domain aliases).\n\n" +
			"Run `gitscan` with no arguments to scan all configured roots. " +
			"Run `gitscan ~/code` to scan a path ad-hoc (not persisted). " +
			"Use `gitscan root add <path>` to register a root first.",
		Version:      Version,
		SilenceUsage: true,
		Args:         cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScan(cmd, args)
		},
	}
	root.SetVersionTemplate("gitscan {{.Version}}\n")

	addScanFlags(root)
	root.AddCommand(
		newScanCmd(),
		newRootCmd(),
		newAliasCmd(),
		newConfigCmd(),
		newCompletionCmd(),
	)
	if f := root.Flags().Lookup("version"); f != nil {
		f.Shorthand = "v"
	}
	return root
}

// scanFlags holds the scan-related flag values. Shared by the root command
// (bare `gitscan`) and the `scan` subcommand.
type scanFlags struct {
	domains      []string
	excludeHosts []string
	dirtyOnly    bool
	noRemote     bool
	protocol     string
	staleDays    int
	fullStats    bool
	format       string
	plain        bool
	raw          bool
	watch        bool
	browse       bool
	concurrency  int
	roots        []string
}

// addScanFlags registers the scan flags as persistent flags on cmd so they
// are honored by both `gitscan` (bare) and `gitscan scan`.
func addScanFlags(cmd *cobra.Command) {
	pf := cmd.PersistentFlags()
	pf.StringSliceVarP(&shared.domains, "domain", "d", nil,
		"filter by domain alias or host (comma-separated, repeatable)")
	pf.StringSliceVar(&shared.excludeHosts, "exclude-domain", nil,
		"exclude domains by alias or host")
	pf.BoolVar(&shared.dirtyOnly, "dirty-only", false, "show only repos with uncommitted changes")
	pf.BoolVar(&shared.noRemote, "no-remote", false, "show only repos with no configured origin")
	pf.StringVar(&shared.protocol, "protocol", "", "filter by remote protocol (ssh|https)")
	pf.IntVar(&shared.staleDays, "stale", 0, "show only repos with no commit in the last N days")
	pf.BoolVar(&shared.fullStats, "full-stats", false, "collect git plumbing stats (slower)")
	pf.StringVarP(&shared.format, "format", "f", "", "output format: table|json|csv|markdown (default table)")
	pf.BoolVar(&shared.plain, "plain", false, "force static output (no TUI)")
	pf.BoolVar(&shared.raw, "raw", false, "emit raw markdown table (no Glamour rendering)")
	pf.BoolVar(&shared.watch, "watch", false, "force live TUI output even when piped")
	pf.BoolVar(&shared.browse, "browse", false, "browse scan results interactively after completion")
	pf.IntVarP(&shared.concurrency, "concurrency", "j", 0, "worker pool size (default from config)")
	pf.StringSliceVar(&shared.roots, "root", nil, "scan this root (repeatable; overrides config roots)")
}

// shared is the singleton holding the parsed scan flag values.
var shared scanFlags

// runScan is the implementation of both `gitscan` and `gitscan scan`.
// Positional args (e.g. `gitscan ~/code`) are ad-hoc roots scanned as a
// one-off — they bypass configured roots and are never persisted. --root
// does the same; the two are additive.
func runScan(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	merged := alias.Merge(cfg.Aliases)
	domains := shared.domains
	if len(domains) > 0 {
		domains = merged.ResolveMany(domains)
	}
	excludeHosts := shared.excludeHosts
	if len(excludeHosts) > 0 {
		excludeHosts = merged.ResolveMany(excludeHosts)
	}
	concurrency := shared.concurrency
	if concurrency <= 0 {
		concurrency = cfg.Scan.Concurrency
	}

	var rootsOpt []config.Root
	adHoc := append([]string{}, args...)
	adHoc = append(adHoc, shared.roots...)
	if len(adHoc) > 0 {
		for _, r := range adHoc {
			expanded, err := expandUser(r)
			if err != nil {
				return err
			}
			abs, err := filepath.Abs(expanded)
			if err != nil {
				return err
			}
			rootsOpt = append(rootsOpt, config.Root{Path: abs, Depth: config.DefaultDepth})
		}
	} else {
		rootsOpt = cfg.Roots
	}
	if len(rootsOpt) == 0 {
		return fmt.Errorf("no roots configured — run `gitscan root add <path>` or pass a path argument, e.g. `gitscan ~/code`")
	}

	opts := scan.Options{
		Roots:       rootsOpt,
		Exclude:     cfg.Scan.ExcludePatterns,
		Concurrency: concurrency,
		FullStats:   shared.fullStats,
		Aliases:     merged,
		Filter: scan.Filter{
			Domains:      domains,
			ExcludeHosts: excludeHosts,
			DirtyOnly:    shared.dirtyOnly,
			NoRemote:     shared.noRemote,
			Protocol:     shared.protocol,
			StaleDays:    shared.staleDays,
		},
	}

	ctx := context.Background()
	results, cancel := scan.Run(ctx, opts)
	defer cancel()

	f := ui.Format(shared.format)
	if f == "" {
		f = ui.FormatTable
	}
	tty := term.IsTerminal(int(os.Stdout.Fd()))
	useGlamour := !shared.raw && tty
	live := !shared.plain && (shared.watch || (tty && f == ui.FormatTable))
	if shared.browse && f != ui.FormatTable {
		return fmt.Errorf("--browse requires --format table")
	}
	if shared.browse {
		live = true
	}

	if f == ui.FormatJSON || f == ui.FormatCSV || f == ui.FormatMarkdown {
		live = false
	}

	renderFinal := func(rows []scan.Result) {
		var r ui.Renderer
		if f == ui.FormatTable {
			if shared.fullStats {
				r = ui.NewFullStatsTable(useGlamour)
			} else {
				r = ui.New(f, useGlamour)
			}
		} else {
			r = ui.New(f, useGlamour)
		}
		r.Header(os.Stdout)
		for _, res := range rows {
			r.Row(os.Stdout, res)
		}
		r.Footer(os.Stdout, len(rows))
	}

	if live {
		rows, err := ui.RunTUI(results, cancel)
		if err != nil {
			return err
		}
		renderFinal(rows)
		return nil
	}

	var rows []scan.Result
	for res := range results {
		rows = append(rows, res)
	}
	renderFinal(rows)
	return nil
}

// ---------- scan ----------

func newScanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scan",
		Short: "Discover repos and collect stats (same as bare `gitscan`)",
		RunE:  runScan,
	}
}

// ---------- root add/remove/list ----------

func newRootCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "root",
		Short: "Manage scan roots",
	}
	c.AddCommand(newRootAddCmd(), newRootRemoveCmd(), newRootListCmd())
	return c
}

func newRootAddCmd() *cobra.Command {
	var depth int
	c := &cobra.Command{
		Use:   "add <path>",
		Short: "Add a scan root",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if err := cfg.AddRoot(args[0], depth); err != nil {
				return err
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Printf("added root: %s (depth=%d)\n", args[0], depth)
			return nil
		},
	}
	c.Flags().IntVar(&depth, "depth", config.DefaultDepth, "max scan depth for this root")
	return c
}

func newRootRemoveCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "remove <path>",
		Short: "Remove a scan root",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if err := cfg.RemoveRoot(args[0]); err != nil {
				return err
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Printf("removed root: %s\n", args[0])
			return nil
		},
	}
	return c
}

func newRootListCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "list",
		Short: "List configured scan roots",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if len(cfg.Roots) == 0 {
				fmt.Println("(no roots configured — run `gitscan root add <path>`)")
				return nil
			}
			out := renderRootsTable(cfg.Roots)
			fmt.Print(out)
			return nil
		},
	}
	return c
}

func renderRootsTable(roots []config.Root) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%-40s  %s\n", "PATH", "DEPTH"))
	b.WriteString(strings.Repeat("-", 50))
	b.WriteString("\n")
	for _, r := range roots {
		b.WriteString(fmt.Sprintf("%-40s  %d\n", r.Path, r.Depth))
	}
	return b.String()
}

// ---------- alias list ----------

func newAliasCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "alias",
		Short: "Manage domain aliases",
	}
	c.AddCommand(newAliasListCmd())
	return c
}

func newAliasListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List built-in and user domain aliases",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			merged := alias.Merge(cfg.Aliases)
			out := renderAliasesTable(merged)
			fmt.Print(out)
			return nil
		},
	}
}

func renderAliasesTable(m alias.Map) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%-15s  %-30s  %s\n", "ALIAS", "HOST", "SOURCE"))
	b.WriteString(strings.Repeat("-", 60))
	b.WriteString("\n")
	for _, k := range m.SortedKeys() {
		src := "built-in"
		if m.IsOverridden(k) {
			src = "user (override)"
		} else if !m.IsBuiltIn(k) {
			src = "user"
		}
		b.WriteString(fmt.Sprintf("%-15s  %-30s  %s\n", k, m[k], src))
	}
	return b.String()
}

// ---------- config init/show/set ----------

func newConfigCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "Manage gitscan configuration",
	}
	c.AddCommand(newConfigInitCmd(), newConfigShowCmd(), newConfigSetCmd())
	return c
}

func newConfigInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Scaffold a default gitscan.toml",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := config.Path()
			if err != nil {
				return err
			}
			if _, err := os.Stat(p); err == nil {
				return fmt.Errorf("config already exists at %s", p)
			}
			cfg := &config.Config{
				Scan:   config.DefaultScan(),
				Roots:  nil,
				Output: config.Output{Format: config.DefaultFormat, Color: true},
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Printf("created %s\n", p)
			return nil
		},
	}
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			p, _ := config.Path()
			fmt.Printf("# %s\n\n", p)
			fmt.Printf("[scan]\nconcurrency = %d\nexclude_patterns = %v\n\n",
				cfg.Scan.Concurrency, cfg.Scan.ExcludePatterns)
			fmt.Printf("[output]\nformat = %q\ncolor = %v\n\n", cfg.Output.Format, cfg.Output.Color)
			if len(cfg.Roots) > 0 {
				fmt.Println("[roots]")
				for _, r := range cfg.Roots {
					fmt.Printf("  %s (depth=%d)\n", r.Path, r.Depth)
				}
			}
			merged := alias.Merge(cfg.Aliases)
			fmt.Println("[aliases] (merged)")
			for _, k := range merged.SortedKeys() {
				fmt.Printf("  %s = %s\n", k, merged[k])
			}
			return nil
		},
	}
}

func newConfigSetCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value (e.g. scan.concurrency 16, output.format json)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, val := args[0], args[1]
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			switch key {
			case "scan.concurrency":
				n := 0
				if _, err := fmt.Sscanf(val, "%d", &n); err != nil {
					return fmt.Errorf("invalid integer: %s", val)
				}
				cfg.Scan.Concurrency = n
			case "output.format":
				if val != "table" && val != "json" && val != "csv" && val != "markdown" {
					return fmt.Errorf("invalid format: %s (table|json|csv|markdown)", val)
				}
				cfg.Output.Format = val
			case "output.color":
				cfg.Output.Color = val == "true" || val == "1" || val == "yes"
			default:
				return fmt.Errorf("unsupported config key: %s\nSupported: scan.concurrency, output.format, output.color", key)
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Printf("set %s = %s\n", key, val)
			return nil
		},
	}
	return c
}

// ---------- completion (cobra default) ----------

func newCompletionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion script",
		Long: "Generate a shell completion script for gitscan. " +
			"Source it from your shell rc file.",
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		Run: func(cmd *cobra.Command, args []string) {
			switch args[0] {
			case "bash":
				_ = cmd.Root().GenBashCompletion(os.Stdout)
			case "zsh":
				_ = cmd.Root().GenZshCompletion(os.Stdout)
			case "fish":
				_ = cmd.Root().GenFishCompletion(os.Stdout, true)
			case "powershell":
				_ = cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
			}
		},
	}
}

// to avoid unused-import when io moves
var _ = io.Discard
