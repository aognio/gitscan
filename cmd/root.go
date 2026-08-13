// Package cmd implements the gitscan CLI commands.
package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aognio/gitscan/internal/alias"
	"github.com/aognio/gitscan/internal/config"
	"github.com/aognio/gitscan/internal/scan"
	"github.com/aognio/gitscan/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// New returns the root gitscan command.
func New() *cobra.Command {
	root := &cobra.Command{
		Use:   "gitscan",
		Short: "Scan local filesystem for Git repos and collect stats",
		Long: "gitscan discovers Git repositories under configured roots, " +
			"extracts remotes, and collects per-repo stats concurrently. " +
			"Config: $HOME/.gitscan/gitscan.toml (usable with zero config " +
			"thanks to built-in domain aliases).",
		SilenceUsage: true,
	}
	root.AddCommand(
		newScanCmd(),
		newRootCmd(),
		newAliasCmd(),
		newConfigCmd(),
		newCompletionCmd(),
	)
	return root
}

// ---------- scan ----------

func newScanCmd() *cobra.Command {
	var (
		domains      []string
		excludeHosts []string
		dirtyOnly    bool
		noRemote     bool
		protocol     string
		staleDays    int
		fullStats    bool
		format       string
		plain        bool
		watch        bool
		concurrency  int
		roots        []string
	)

	c := &cobra.Command{
		Use:   "scan",
		Short: "Discover repos and collect stats",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			merged := alias.Merge(cfg.Aliases)
			if len(domains) > 0 {
				domains = merged.ResolveMany(domains)
			}
			if len(excludeHosts) > 0 {
				excludeHosts = merged.ResolveMany(excludeHosts)
			}
			if concurrency <= 0 {
				concurrency = cfg.Scan.Concurrency
			}

			rootsOpt := cfg.Roots
			if len(roots) > 0 {
				rootsOpt = nil
				for _, r := range roots {
					rootsOpt = append(rootsOpt, config.Root{Path: r, Depth: config.DefaultDepth})
				}
			}
			if len(rootsOpt) == 0 {
				return fmt.Errorf("no roots configured — run `gitscan root add <path>` first or pass --root")
			}

			opts := scan.Options{
				Roots:       rootsOpt,
				Exclude:     cfg.Scan.ExcludePatterns,
				Concurrency: concurrency,
				FullStats:   fullStats,
				Aliases:     merged,
				Filter: scan.Filter{
					Domains:      domains,
					ExcludeHosts: excludeHosts,
					DirtyOnly:    dirtyOnly,
					NoRemote:     noRemote,
					Protocol:     protocol,
					StaleDays:    staleDays,
				},
			}

			ctx := context.Background()
			results, cancel := scan.Run(ctx, opts)
			defer cancel()

			f := ui.Format(format)
			useColor := !plain && cfg.Output.Color && term.IsTerminal(int(os.Stdout.Fd()))
			live := !plain && (watch || (term.IsTerminal(int(os.Stdout.Fd())) && f == ui.FormatTable))

			if f == ui.FormatJSON || f == ui.FormatCSV || f == ui.FormatMarkdown {
				live = false
			}

			if live {
				return ui.RunTUI(results, cancel)
			}

			r := ui.New(f, useColor)
			r.Header(os.Stdout)
			total := 0
			for res := range results {
				r.Row(os.Stdout, res)
				total++
			}
			r.Footer(os.Stdout, total)
			return nil
		},
	}
	c.Flags().StringSliceVarP(&domains, "domain", "d", nil,
		"filter by domain alias or host (comma-separated, repeatable)")
	c.Flags().StringSliceVar(&excludeHosts, "exclude-domain", nil,
		"exclude domains by alias or host")
	c.Flags().BoolVar(&dirtyOnly, "dirty-only", false, "show only repos with uncommitted changes")
	c.Flags().BoolVar(&noRemote, "no-remote", false, "show only repos with no configured origin")
	c.Flags().StringVar(&protocol, "protocol", "", "filter by remote protocol (ssh|https)")
	c.Flags().IntVar(&staleDays, "stale", 0, "show only repos with no commit in the last N days")
	c.Flags().BoolVar(&fullStats, "full-stats", false, "collect git plumbing stats (slower)")
	c.Flags().StringVarP(&format, "format", "f", "", "output format: table|json|csv|markdown (default table)")
	c.Flags().BoolVar(&plain, "plain", false, "force static output (no TUI)")
	c.Flags().BoolVar(&watch, "watch", false, "force live TUI output even when piped")
	c.Flags().IntVarP(&concurrency, "concurrency", "j", 0, "worker pool size (default from config)")
	c.Flags().StringSliceVar(&roots, "root", nil, "scan this root (repeatable; overrides config roots)")
	return c
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
	c.Flags().IntVarP(&depth, "depth", "d", config.DefaultDepth, "max scan depth for this root")
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