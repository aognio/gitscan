// Package ui provides the Bubble Tea live scan view and result pager.
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

type tuiModel struct {
	results          <-chan scan.Result
	cancel           context.CancelFunc
	rows             []scan.Result
	total            int
	width            int
	height           int
	pager            bool
	fullStats        bool
	scanning         bool
	verticalOffset   int
	horizontalOffset int
	spinner          spinnerState
	quitting         bool
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

// RunTUI starts the live scan view and optionally enters a result pager.
func RunTUI(results <-chan scan.Result, cancel context.CancelFunc, pager, fullStats bool) ([]scan.Result, error) {
	m := &tuiModel{
		results:   results,
		cancel:    cancel,
		pager:     pager,
		fullStats: fullStats,
		scanning:  true,
		spinner:   spinnerState{frames: spinnerFrames},
	}
	var options []tea.ProgramOption
	if pager {
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
	return tea.Tick(tickInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.normalizePager()
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			if m.scanning {
				m.cancel()
			}
			return m, tea.Quit
		}
		if m.pager && !m.scanning {
			m.updatePager(msg.String())
		}
	case tickMsg:
		m.spinner.frame = (m.spinner.frame + 1) % len(m.spinner.frames)
		return m, m.spin()
	case resultMsg:
		if msg.done {
			m.scanning = false
			m.normalizePager()
			if !m.pager {
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

func (m *tuiModel) View() string {
	if m.quitting {
		return ""
	}
	if m.pager && !m.scanning {
		return m.renderPager()
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s scanning... %d repos found\n\n",
		m.spinner.frames[m.spinner.frame], m.total))
	maxRows := m.height - 5
	if maxRows < 1 {
		maxRows = 1
	}
	rows := m.rows
	if len(rows) > maxRows {
		rows = rows[len(rows)-maxRows:]
	}
	for _, row := range rows {
		b.WriteString(renderRowLive(row, m.width))
		b.WriteByte('\n')
	}
	b.WriteString("\n" + lipgloss.NewStyle().Faint(true).Render("press q to cancel"))
	return b.String()
}

func (m *tuiModel) renderPager() string {
	width := m.width
	if width <= 0 {
		width = 80
	}
	lines := pagerLines(m.rows, m.fullStats)
	visible := m.pagerVisibleLines()
	start := m.verticalOffset
	end := start + visible
	if end > len(lines) {
		end = len(lines)
	}
	var b strings.Builder
	for _, line := range lines[start:end] {
		b.WriteString(clipText(line, m.horizontalOffset, width))
		b.WriteByte('\n')
	}
	footer := fmt.Sprintf("lines %d-%d/%d  columns %d  q quit",
		start+1, end, len(lines), m.horizontalOffset+1)
	b.WriteString(lipgloss.NewStyle().Faint(true).Render(clipText(footer, 0, width)))
	return b.String()
}

func (m *tuiModel) pagerVisibleLines() int {
	visible := m.height - 1
	if visible < 1 {
		visible = 1
	}
	return visible
}

func (m *tuiModel) normalizePager() {
	lines := pagerLines(m.rows, m.fullStats)
	visible := m.pagerVisibleLines()
	maxVertical := len(lines) - visible
	if maxVertical < 0 {
		maxVertical = 0
	}
	if m.verticalOffset > maxVertical {
		m.verticalOffset = maxVertical
	}
	if m.verticalOffset < 0 {
		m.verticalOffset = 0
	}
	maxHorizontal := maxPagerHorizontal(m.rows, m.fullStats, m.width)
	if m.horizontalOffset > maxHorizontal {
		m.horizontalOffset = maxHorizontal
	}
	if m.horizontalOffset < 0 {
		m.horizontalOffset = 0
	}
}

func (m *tuiModel) updatePager(key string) {
	visible := m.pagerVisibleLines()
	horizontalStep := m.width / 2
	if horizontalStep < 1 {
		horizontalStep = 1
	}
	switch key {
	case "up", "k", "ctrl+p":
		m.verticalOffset--
	case "down", "j", "enter", "ctrl+n":
		m.verticalOffset++
	case "pgup", "b", "ctrl+b":
		m.verticalOffset -= visible
	case "pgdown", "space", "f", "ctrl+f":
		m.verticalOffset += visible
	case "u", "ctrl+u":
		m.verticalOffset -= visible / 2
	case "d", "ctrl+d":
		m.verticalOffset += visible / 2
	case "home", "g":
		m.verticalOffset = 0
	case "end", "G":
		m.verticalOffset = len(pagerLines(m.rows, m.fullStats))
	case "left", "h":
		m.horizontalOffset -= horizontalStep
	case "right", "l":
		m.horizontalOffset += horizontalStep
	case "shift+left", "ctrl+left":
		m.horizontalOffset = 0
	case "shift+right", "ctrl+right":
		m.horizontalOffset = maxPagerHorizontal(m.rows, m.fullStats, m.width)
	}
	m.normalizePager()
}

func maxPagerHorizontal(rows []scan.Result, fullStats bool, width int) int {
	maxWidth := 0
	for _, line := range pagerLines(rows, fullStats) {
		if lineWidth := lipgloss.Width(line); lineWidth > maxWidth {
			maxWidth = lineWidth
		}
	}
	if maxWidth <= width {
		return 0
	}
	return maxWidth - width
}

func pagerLines(rows []scan.Result, fullStats bool) []string {
	values := make([][]string, 0, len(rows)+2)
	values = append(values, pagerHeaders(fullStats))
	for i, row := range rows {
		rowValues := pagerValues(row, fullStats)
		rowValues[0] = strconv.Itoa(i + 1)
		values = append(values, rowValues)
	}
	widths := pagerColumnWidths(values)
	alignments := pagerAlignments(fullStats)
	lines := []string{
		pagerTableLine(values[0], widths, alignments),
		pagerTableLine(pagerDividers(widths), widths, alignments),
	}
	for _, row := range values[1:] {
		lines = append(lines, pagerTableLine(row, widths, alignments))
	}
	lines = append(lines, "", fmt.Sprintf("Total: %d repos", len(rows)))
	return lines
}

func pagerHeaders(fullStats bool) []string {
	if fullStats {
		return []string{"#", "Path", "Host", "Origin", "Branches", "Commits", "Objects", ".git size", "State"}
	}
	return []string{"#", "Path", "Host", "Origin", ".git size", "State"}
}

func pagerValues(row scan.Result, fullStats bool) []string {
	st := row.Stat
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
	return append(values, humanSize(st.DotGitSize), state)
}

func pagerColumnWidths(rows [][]string) []int {
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, value := range row {
			if width := lipgloss.Width(value); width > widths[i] {
				widths[i] = width
			}
		}
	}
	return widths
}

func pagerDividers(widths []int) []string {
	dividers := make([]string, len(widths))
	for i, width := range widths {
		dividers[i] = strings.Repeat("─", width)
	}
	return dividers
}

func pagerTableLine(values []string, widths []int, alignments []tableAlignment) string {
	parts := make([]string, len(widths))
	for i, width := range widths {
		parts[i] = alignCell(values[i], width, alignments[i])
	}
	return strings.Join(parts, " │ ")
}

func pagerAlignments(fullStats bool) []tableAlignment {
	if fullStats {
		return []tableAlignment{
			alignRight, alignLeft, alignRight, alignLeft,
			alignRight, alignRight, alignRight, alignRight, alignCenter,
		}
	}
	return []tableAlignment{alignRight, alignLeft, alignRight, alignLeft, alignRight, alignCenter}
}

func clipText(value string, offset, width int) string {
	if width <= 0 {
		return ""
	}
	if offset < 0 {
		offset = 0
	}
	var b strings.Builder
	cellOffset := 0
	cellWidth := 0
	for _, char := range value {
		charWidth := lipgloss.Width(string(char))
		if cellOffset < offset {
			cellOffset += charWidth
			continue
		}
		if cellWidth+charWidth > width {
			break
		}
		b.WriteRune(char)
		cellWidth += charWidth
	}
	return b.String()
}

func renderRowLive(row scan.Result, width int) string {
	st := row.Stat
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
	return fitCell(strings.Join(values, "  "), width)
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
