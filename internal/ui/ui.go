// Package ui renders collected scan results.
//
// The same Result stream produced by internal/scan is consumed by either:
//   - the static renderers (Table, JSON, CSV, Markdown) for scripted output, or
//   - the Bubble Tea live view (TUI) for interactive runs.
//
// The "table" format builds a markdown table from the results and renders it
// through Glamour when stdout is a TTY (Unicode borders, aligned columns,
// syntax highlighting) — falling back to the raw markdown table when piped,
// which stays readable in any plaintext context (logs, PRs, devnotes).
package ui

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/aognio/gitscan/internal/scan"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// Format is the output format identifier.
type Format string

const (
	FormatTable    Format = "table"
	FormatJSON     Format = "json"
	FormatCSV      Format = "csv"
	FormatMarkdown Format = "markdown"
)

type tableAlignment uint8

const (
	alignLeft tableAlignment = iota
	alignRight
	alignCenter
)

func alignCell(value string, width int, alignment tableAlignment) string {
	padding := width - lipgloss.Width(value)
	if padding <= 0 {
		return value
	}
	switch alignment {
	case alignRight:
		return strings.Repeat(" ", padding) + value
	case alignCenter:
		left := padding / 2
		return strings.Repeat(" ", left) + value + strings.Repeat(" ", padding-left)
	default:
		return value + strings.Repeat(" ", padding)
	}
}

// Renderer is anything that can write a stream of scan.Results to a writer.
type Renderer interface {
	Header(w io.Writer)
	Row(w io.Writer, r scan.Result)
	Footer(w io.Writer, total int)
}

// New returns the right static Renderer for the requested format.
// For FormatTable, the renderer buffers rows and emits a Glamour-rendered
// markdown table in Footer (or the raw markdown when color is disabled).
func New(f Format, useColor bool) Renderer {
	return NewWithWidth(f, useColor, 0)
}

// NewWithWidth returns a renderer configured for a terminal width.
func NewWithWidth(f Format, useColor bool, width int) Renderer {
	switch f {
	case FormatJSON:
		return &jsonRenderer{}
	case FormatCSV:
		return &csvRenderer{}
	case FormatMarkdown:
		return &markdownRenderer{}
	default:
		return &glamourTableRenderer{color: useColor, width: width, fullStats: false}
	}
}

// NewFullStatsTable returns a table renderer that includes the plumbing
// columns (branches/commits/objects) — used when --full-stats is set.
func NewFullStatsTable(useColor bool) Renderer {
	return NewFullStatsTableWithWidth(useColor, 0)
}

// NewFullStatsTableWithWidth returns a full-stats table renderer configured
// for a terminal width.
func NewFullStatsTableWithWidth(useColor bool, width int) Renderer {
	return &glamourTableRenderer{color: useColor, width: width, fullStats: true}
}

// ---- Glamour-rendered markdown table ----

type glamourTableRenderer struct {
	color     bool
	fullStats bool
	width     int
	rows      []scan.Result
}

func (g *glamourTableRenderer) Header(w io.Writer) {}

func (g *glamourTableRenderer) Row(w io.Writer, r scan.Result) {
	g.rows = append(g.rows, r)
}

func (g *glamourTableRenderer) Footer(w io.Writer, total int) {
	md := g.buildMarkdown()
	if g.color {
		var out string
		var err error
		if g.width > 0 {
			renderer, rendererErr := glamour.NewTermRenderer(
				glamour.WithAutoStyle(),
				glamour.WithWordWrap(g.width),
			)
			if rendererErr == nil {
				out, err = renderer.Render(md)
			} else {
				err = rendererErr
			}
		} else {
			out, err = glamour.Render(md, "auto")
		}
		if err == nil {
			fmt.Fprint(w, out)
			g.printSummary(w)
			return
		}
	}
	fmt.Fprintln(w, md)
	g.printSummary(w)
}

func (g *glamourTableRenderer) printSummary(w io.Writer) {
	dirty, noremote := 0, 0
	for _, r := range g.rows {
		if r.Stat.Dirty {
			dirty++
		}
		if r.Stat.OriginURL == "" {
			noremote++
		}
	}
	fmt.Fprintf(w, "\n%d repos | %d clean | %d dirty | %d no-remote\n",
		len(g.rows), len(g.rows)-dirty, dirty, noremote)
}

func (g *glamourTableRenderer) buildMarkdown() string {
	var b strings.Builder
	if g.fullStats {
		b.WriteString("| # | Path | Host | Origin | Branches | Commits | Objects | .git size | State |\n")
		b.WriteString("|---:|---|---:|---|---:|---:|---:|---:|:---:|\n")
	} else {
		b.WriteString("| # | Path | Host | Origin | .git size | State |\n")
		b.WriteString("|---:|---|---:|---|---:|:---:|\n")
	}
	for i, r := range g.rows {
		st := r.Stat
		state := "ok"
		if st.Dirty {
			state = fmt.Sprintf("dirty(%d)", st.DirtyCount)
		} else if st.OriginURL == "" {
			state = "no-remote"
		}
		path := st.Path
		origin := st.OriginURL
		if origin == "" {
			origin = "—"
		}
		if host := st.Host; host == "" {
			host = "—"
		} else {
			_ = host
		}
		if g.fullStats {
			b.WriteString(fmt.Sprintf("| %d | %s | %s | %s | %d | %d | %d | %s | %s |\n",
				i+1, path, st.Host, origin,
				st.Branches, st.Commits, st.Objects, humanSize(st.DotGitSize), state))
		} else {
			b.WriteString(fmt.Sprintf("| %d | %s | %s | %s | %s | %s |\n",
				i+1, path, st.Host, origin, humanSize(st.DotGitSize), state))
		}
	}
	b.WriteString(fmt.Sprintf("\n**Total: %d repos**\n", len(g.rows)))
	return b.String()
}

// ---- JSON renderer ----

type jsonRenderer struct {
	first bool
}

func (j *jsonRenderer) Header(w io.Writer) {
	fmt.Fprint(w, "[")
	j.first = true
}

func (j *jsonRenderer) Row(w io.Writer, r scan.Result) {
	if !j.first {
		fmt.Fprint(w, ",")
	}
	if r.Err != nil {
		_, _ = fmt.Fprintf(w, `{"path":%q,"error":%q}`, r.Stat.Path, r.Err.Error())
		j.first = false
		return
	}
	b, _ := json.Marshal(r.Stat)
	if !j.first {
		fmt.Fprint(w, "")
	}
	_, _ = w.Write(b)
	j.first = false
}

func (j *jsonRenderer) Footer(w io.Writer, total int) {
	fmt.Fprintf(w, "]\n")
}

// ---- CSV renderer ----

type csvRenderer struct{}

var csvHeader = []string{"path", "host", "origin_url", "branches", "commits", "objects", "dotgit_size", "dotgit_files", "dirty", "dirty_count", "last_commit"}

func (c *csvRenderer) Header(w io.Writer) {
	cw := csv.NewWriter(w)
	_ = cw.Write(csvHeader)
	cw.Flush()
}

func (c *csvRenderer) Row(w io.Writer, r scan.Result) {
	cw := csv.NewWriter(w)
	st := r.Stat
	_ = cw.Write([]string{
		st.Path,
		st.Host,
		st.OriginURL,
		strconv.Itoa(st.Branches),
		strconv.Itoa(st.Commits),
		strconv.Itoa(st.Objects),
		strconv.FormatInt(st.DotGitSize, 10),
		strconv.Itoa(st.DotGitFiles),
		strconv.FormatBool(st.Dirty),
		strconv.Itoa(st.DirtyCount),
		st.LastCommit.Format("2006-01-02"),
	})
	cw.Flush()
}

func (c *csvRenderer) Footer(w io.Writer, total int) {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"total", strconv.Itoa(total)})
	cw.Flush()
}

// ---- Markdown renderer ----

type markdownRenderer struct {
	rows []scan.Result
}

func (m *markdownRenderer) Header(w io.Writer) {
	md := "| Path | Host | Origin | Branches | Commits | Objects | .git size | State |\n|---|---:|---|---:|---:|---:|---:|:---:|\n"
	_, _ = w.Write([]byte(md))
}

func (m *markdownRenderer) Row(w io.Writer, r scan.Result) {
	st := r.Stat
	state := "ok"
	if st.Dirty {
		state = "dirty"
	} else if st.OriginURL == "" {
		state = "no-remote"
	}
	row := fmt.Sprintf("| %s | %s | %s | %d | %d | %d | %s | %s |\n",
		st.Path, st.Host, trimURL(st.OriginURL), st.Branches,
		st.Commits, st.Objects, humanSize(st.DotGitSize), state)
	_, _ = w.Write([]byte(row))
}

func (m *markdownRenderer) Footer(w io.Writer, total int) {
	_, _ = w.Write([]byte(fmt.Sprintf("\n**Total: %d repos**\n", total)))
}

// RenderMarkdown renders a markdown string glamour-style to w. Used by the
// markdown renderer path when stdout is a TTY and the user wants inline
// rendered markdown.
func RenderMarkdown(src string, w io.Writer) error {
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(120),
	)
	if err != nil {
		return err
	}
	out, err := r.Render(src)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(w, out)
	return nil
}

// ---- helpers ----

func shortPath(p string) string {
	// show last two path segments for compactness
	parts := strings.Split(p, string("/"))
	if len(parts) <= 2 {
		return p
	}
	return ".../" + strings.Join(parts[len(parts)-2:], "/")
}

func trimURL(u string) string {
	if u == "" {
		return ""
	}
	u = strings.TrimSuffix(u, ".git")
	if strings.HasPrefix(u, "https://") {
		u = strings.TrimPrefix(u, "https://")
	}
	return u
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for nn := n / unit; nn >= unit; nn /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
