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
	"strings"
	"time"

	"github.com/aognio/gitscan/internal/scan"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const tickInterval = 100 * time.Millisecond

// tuiModel is the Bubble Tea program for the live scan view.
type tuiModel struct {
	results  <-chan scan.Result
	cancel   context.CancelFunc
	rows     []scan.Result
	total    int
	width    int
	height   int
	spinner  spinnerState
	quitting bool
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
func RunTUI(results <-chan scan.Result, cancel context.CancelFunc) ([]scan.Result, error) {
	m := &tuiModel{
		results: results,
		cancel:  cancel,
		spinner: spinnerState{frames: spinnerFrames},
	}
	p := tea.NewProgram(m)
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
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			m.cancel()
			return m, tea.Quit
		}
	case tickMsg:
		m.spinner.frame = (m.spinner.frame + 1) % len(m.spinner.frames)
		return m, m.spin()
	case resultMsg:
		if msg.done {
			m.quitting = true
			return m, tea.Quit
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
	runes := []rune(value)
	if len(runes) > width-1 {
		runes = runes[:width-1]
	}
	return string(runes) + "…"
}
