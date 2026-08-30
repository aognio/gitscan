// Package ui (TUI): a Bubble Tea live-updating model for `gitscan scan`
// when stdout is a TTY (see decisions/2026-08-13-bubble-tea-default-ux.md).
//
// The TUI renders inline (no alt-screen) so the scan progress and final state
// stay in the scrollback. The caller renders the final output through the
// same static renderer used for --plain, ensuring the two paths produce
// identical output.
package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aognio/gitscan/internal/scan"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const tickInterval = 100 * time.Millisecond

// tuiModel is the Bubble Tea program for the live scan view.
type tuiModel struct {
	results      <-chan scan.Result
	cancel       context.CancelFunc
	rows         []scan.Result
	total        int
	width        int
	height       int
	browse       bool
	fullStats    bool
	scanning     bool
	selected     int
	rowOffset    int
	columnOffset int
	spinner      spinnerState
	quitting     bool
}

type spinnerState struct {
	frame  int
	frames []string
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type tickMsg struct{}

type resultMsg struct {
	r    scan.Result
	done bool
}

// RunTUI starts the Bubble Tea program and blocks until it finishes. It
// consumes from results and returns the collected rows so the caller can
// render the final output through the same renderer used for --plain.
func RunTUI(results <-chan scan.Result, cancel context.CancelFunc, browse, fullStats bool) ([]scan.Result, error) {
	m := &tuiModel{
		results:   results,
		cancel:    cancel,
		browse:    browse,
		fullStats: fullStats,
		scanning:  true,
		spinner:   spinnerState{frames: spinnerFrames},
	}
	var options []tea.ProgramOption
	if browse {
		options = append(options, tea.WithAltScreen())
	}
	p := tea.NewProgram(m, options...)
	if _, err := p.Run(); err != nil {
		return nil, err
	}
	return m.rows, nil
}

func (m *tuiModel) Init() tea.Cmd {
	return tea.Batch(m.waitForResult(), m.spin())
}

func (m *tuiModel) waitForResult() tea.Cmd {
	return func() tea.Msg {
		r, ok := <-m.results
		if !ok {
			return resultMsg{done: true}
		}
		return resultMsg{r: r}
	}
}

func (m *tuiModel) spin() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg {
		return tickMsg{}
	})
}

func (m *tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.normalizeViewport()
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quit()
			return m, tea.Quit
		}
		if m.browse && !m.scanning {
			m.updateBrowserCursor(msg.String())
		}
	case tickMsg:
		m.spinner.frame = (m.spinner.frame + 1) % len(m.spinner.frames)
		return m, m.spin()
	case resultMsg:
		if msg.done {
			m.scanning = false
			m.normalizeViewport()
			if !m.browse {
				m.quitting = true
				return m, tea.Quit
			}
			return m, nil
		}
		m.rows = append(m.rows, msg.r)
		m.total++
		return m, m.waitForResult()
	}
	return m, nil
}

func (m *tuiModel) quit() {
	m.quitting = true
	if m.scanning {
		m.cancel()
	}
}

func (m *tuiModel) View() string {
	if m.quitting {
		return ""
	}
	if m.browse && !m.scanning {
		return m.renderBrowser()
	}
	var b strings.Builder
	spin := m.spinner.frames[m.spinner.frame]
	b.WriteString(fmt.Sprintf("%s scanning... %d repos found\n\n", spin, m.total))
	rows := m.rows
	maxRows := m.height - 5
	if maxRows < 1 {
		maxRows = 1
	}
	if len(rows) > maxRows {
		rows = rows[len(rows)-maxRows:]
	}
	for _, r := range rows {
		b.WriteString(renderRowLive(r, m.width))
		b.WriteString("\n")
	}
	b.WriteString("\n" + lipgloss.NewStyle().Faint(true).Render("press q to cancel"))
	return b.String()
}

func (m *tuiModel) normalizeViewport() {
	if len(m.rows) == 0 {
		m.selected = 0
		m.rowOffset = 0
		return
	}
	if m.selected >= len(m.rows) {
		m.selected = len(m.rows) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
	visible := m.browserVisibleRows()
	if m.selected < m.rowOffset {
		m.rowOffset = m.selected
	}
	if m.selected >= m.rowOffset+visible {
		m.rowOffset = m.selected - visible + 1
	}
	if m.rowOffset < 0 {
		m.rowOffset = 0
	}
	widths := browserColumnWidths(m.rows, m.fullStats)
	contentWidth := tableContentWidth(widths)
	if m.columnOffset < 0 {
		m.columnOffset = 0
	}
	if m.width > 0 && contentWidth > m.width && m.columnOffset > contentWidth-m.width {
		m.columnOffset = contentWidth - m.width
	}
	if contentWidth <= m.width {
		m.columnOffset = 0
	}
	if m.rowOffset > len(m.rows)-visible {
		m.rowOffset = len(m.rows) - visible
	}
	if m.rowOffset < 0 {
		m.rowOffset = 0
	}
}

func (m *tuiModel) browserVisibleRows() int {
	visible := m.height - 5
	if visible < 1 {
		visible = 1
	}
	return visible
}

func (m *tuiModel) updateBrowserCursor(key string) {
	if len(m.rows) == 0 {
		return
	}
	visible := m.browserVisibleRows()
	switch key {
	case "up", "k":
		m.selected--
	case "down", "j":
		m.selected++
	case "home", "g":
		m.selected = 0
	case "end", "G":
		m.selected = len(m.rows) - 1
	case "pgup", "ctrl+u":
		m.selected -= visible
	case "pgdown", "ctrl+d":
		m.selected += visible
	case "left", "h":
		m.columnOffset -= 8
	case "right", "l":
		m.columnOffset += 8
	}
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= len(m.rows) {
		m.selected = len(m.rows) - 1
	}
	m.normalizeViewport()
}

func (m *tuiModel) renderBrowser() string {
	width := m.width
	if width <= 0 {
		width = 80
	}
	widths := browserColumnWidths(m.rows, m.fullStats)
	var b strings.Builder
	b.WriteString(clipText(browserTableLine(browserHeaders(m.fullStats), widths), m.columnOffset, width))
	b.WriteByte('\n')
	b.WriteString(clipText(browserTableLine(browserDividers(widths), widths), m.columnOffset, width))
	b.WriteByte('\n')
	start := m.rowOffset
	end := start + m.browserVisibleRows()
	if end > len(m.rows) {
		end = len(m.rows)
	}
	for i := start; i < end; i++ {
		values := browserValues(m.rows[i], m.fullStats)
		values[0] = strconv.Itoa(i + 1)
		if i == m.selected {
			values[0] = ">" + values[0]
		}
		b.WriteString(clipText(browserTableLine(values, widths), m.columnOffset, width))
		b.WriteByte('\n')
	}
	if len(m.rows) > 0 {
		st := m.rows[m.selected].Stat
		b.WriteString(clipText("Path: "+st.Path, m.columnOffset, width))
		b.WriteByte('\n')
		origin := st.OriginURL
		if origin == "" {
			origin = "—"
		}
		b.WriteString(clipText("Origin: "+origin, m.columnOffset, width))
		b.WriteByte('\n')
	}
	footer := fmt.Sprintf(
		"%d/%d  ↑↓ row  ←→ horizontal  PgUp/PgDn page  q quit",
		m.selected+1, len(m.rows))
	b.WriteString(lipgloss.NewStyle().Faint(true).Render(clipText(footer, 0, width)))
	return b.String()
}

func browserHeaders(fullStats bool) []string {
	if fullStats {
		return []string{"#", "Path", "Host", "Origin", "Branches", "Commits", "Objects", ".git size", "State"}
	}
	return []string{"#", "Path", "Host", "Origin", ".git size", "State"}
}

func browserValues(r scan.Result, fullStats bool) []string {
	st := r.Stat
	state := "ok"
	if st.Dirty {
		state = fmt.Sprintf("dirty(%d)", st.DirtyCount)
	} else if st.OriginURL == "" {
		state = "no-remote"
	}
	values := []string{"", st.Path, st.Host, st.OriginURL}
	if fullStats {
		values = append(values, strconv.Itoa(st.Branches), strconv.Itoa(st.Commits), strconv.Itoa(st.Objects))
	}
	values = append(values, humanSize(st.DotGitSize), state)
	return values
}

func browserColumnWidths(rows []scan.Result, fullStats bool) []int {
	headers := browserHeaders(fullStats)
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = lipgloss.Width(header)
	}
	for _, row := range rows {
		for i, value := range browserValues(row, fullStats) {
			if w := lipgloss.Width(value); w > widths[i] {
				widths[i] = w
			}
		}
	}
	for i := range widths {
		limit := 24
		if i == 1 {
			limit = 48
		}
		if i == 3 {
			limit = 64
		}
		if widths[i] > limit {
			widths[i] = limit
		}
	}
	return widths
}

func browserDividers(widths []int) []string {
	dividers := make([]string, len(widths))
	for i, width := range widths {
		dividers[i] = strings.Repeat("─", width)
	}
	return dividers
}

func browserTableLine(values []string, widths []int) string {
	parts := make([]string, len(widths))
	for i, width := range widths {
		value := ""
		if i < len(values) {
			value = values[i]
		}
		parts[i] = padCell(fitCell(value, width), width)
	}
	return strings.Join(parts, " │ ")
}

func tableContentWidth(widths []int) int {
	width := 0
	for _, columnWidth := range widths {
		width += columnWidth
	}
	if len(widths) > 1 {
		width += (len(widths) - 1) * 3
	}
	return width
}

func padCell(value string, width int) string {
	return value + strings.Repeat(" ", width-lipgloss.Width(value))
}

func clipText(value string, offset, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if offset >= len(runes) {
		return ""
	}
	if offset < 0 {
		offset = 0
	}
	runes = runes[offset:]
	if len(runes) > width {
		runes = runes[:width]
	}
	return string(runes)
}

func renderRowLive(r scan.Result, width int) string {
	st := r.Stat
	state := "ok"
	if st.Dirty {
		state = fmt.Sprintf("dirty(%d)", st.DirtyCount)
	} else if st.OriginURL == "" {
		state = "no-remote"
	}
	values := []string{shortPath(st.Path), st.Host, trimURL(st.OriginURL), state}
	widths := []int{24, 16, 32, 12}
	if width <= 0 {
		width = 80
	}
	for i := range widths {
		values[i] = fitCell(values[i], widths[i])
	}
	line := strings.Join(values, "  ")
	return fitCell(line, width)
}

func fitCell(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	available := width - lipgloss.Width("…")
	var kept strings.Builder
	for _, char := range value {
		charWidth := lipgloss.Width(string(char))
		if charWidth > available {
			break
		}
		kept.WriteRune(char)
		available -= charWidth
	}
	return kept.String() + "…"
}
