// Package ui renders collected scan results.
//
// The same Result stream produced by internal/scan is consumed by either:
//   - the static renderers (Table, JSON, CSV, Markdown) for scripted output, or
//   - the Bubble Tea live view (TUI) for interactive runs.
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

var (
	boldStyle  = lipgloss.NewStyle().Bold(true)
	dirtyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	cleanStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	headStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
)

// Format is the output format identifier.
type Format string

const (
	FormatTable    Format = "table"
	FormatJSON     Format = "json"
	FormatCSV      Format = "csv"
	FormatMarkdown Format = "markdown"
)

// Renderer is anything that can write a stream of scan.Results to a writer.
type Renderer interface {
	// Header is called once before results stream (may be a no-op).
	Header(w io.Writer)
	// Row is called once per result.
	Row(w io.Writer, r scan.Result)
	// Footer is called once after the last result.
	Footer(w io.Writer, total int)
}

// New returns the right static Renderer for the requested format.
func New(f Format, useColor bool) Renderer {
	switch f {
	case FormatJSON:
		return &jsonRenderer{}
	case FormatCSV:
		return &csvRenderer{}
	case FormatMarkdown:
		return &markdownRenderer{}
	default:
		return &tableRenderer{color: useColor}
	}
}

// ---- Table renderer ----

type tableRenderer struct {
	color bool
	rows  [][]string
}

func (t *tableRenderer) Header(w io.Writer) {
	header := []string{"PATH", "HOST", "ORIGIN", "BR", "CM", "OBJ", "GIT-SIZE", "STATE"}
	if t.color {
		for i, h := range header {
			header[i] = headStyle.Render(h)
		}
	}
	fmt.Fprintln(w, strings.Join(header, "\t"))
}

func (t *tableRenderer) Row(w io.Writer, r scan.Result) {
	st := r.Stat
	state := "ok"
	if st.Dirty {
		state = "dirty(" + strconv.Itoa(st.DirtyCount) + ")"
	} else if st.OriginURL == "" {
		state = "no-remote"
	}
	row := []string{
		shortPath(st.Path),
		st.Host,
		trimURL(st.OriginURL),
		strconv.Itoa(st.Branches),
		strconv.Itoa(st.Commits),
		strconv.Itoa(st.Objects),
		humanSize(st.DotGitSize),
		state,
	}
	for i, cell := range row {
		if t.color && i == len(row)-1 {
			if st.Dirty {
				row[i] = dirtyStyle.Render(cell)
			} else if st.OriginURL == "" {
				row[i] = cleanStyle.Render(cell)
			} else {
				row[i] = cleanStyle.Render(cell)
			}
		}
	}
	fmt.Fprintln(w, strings.Join(row, "\t"))
}

func (t *tableRenderer) Footer(w io.Writer, total int) {
	if t.color {
		fmt.Fprintf(w, "\n%s\n", boldStyle.Render(fmt.Sprintf("%d repos", total)))
	} else {
		fmt.Fprintf(w, "\n%d repos\n", total)
	}
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
	md := "| Path | Host | Origin | Branches | Commits | Objects | .git size | State |\n|---|---|---|---:|---:|---:|---:|---|\n"
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
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for nn := n / unit; nn >= unit; nn /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}