// Package ui (TUI): a Bubble Tea live-updating model for `gitscan scan`
// when stdout is a TTY (see decisions/2026-08-13-bubble-tea-default-ux.md).
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
	spinner  spinnerState
	quitting bool
}

type spinnerState struct {
	frame int
	frames []string
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type tickMsg struct{}
type resultMsg struct {
	r scan.Result
	done bool
}

// RunTUI starts the Bubble Tea program and blocks until it finishes. It
// consumes from results; when the channel is closed it renders the final
// table and exits.
func RunTUI(results <-chan scan.Result, cancel context.CancelFunc) error {
	m := &tuiModel{
		results: results,
		cancel:  cancel,
		spinner: spinnerState{frames: spinnerFrames},
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
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
		return m.finalView()
	}
	var b strings.Builder
	spin := m.spinner.frames[m.spinner.frame]
	b.WriteString(fmt.Sprintf("%s scanning... %d repos found\n\n", spin, m.total))
	if len(m.rows) > 10 {
		for _, r := range m.rows[len(m.rows)-10:] {
			b.WriteString(renderRowLive(r))
			b.WriteString("\n")
		}
	} else {
		for _, r := range m.rows {
			b.WriteString(renderRowLive(r))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n" + lipgloss.NewStyle().Faint(true).Render("press q to quit"))
	return b.String()
}

func (m *tuiModel) finalView() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("gitscan — %d repos\n\n", m.total))
	b.WriteString(renderTable(m.rows))
	return b.String()
}

func renderRowLive(r scan.Result) string {
	st := r.Stat
	state := "ok"
	if st.Dirty {
		state = fmt.Sprintf("dirty(%d)", st.DirtyCount)
	} else if st.OriginURL == "" {
		state = "no-remote"
	}
	return fmt.Sprintf("%s\t%s\t%s\t%s",
		shortPath(st.Path), st.Host, trimURL(st.OriginURL), state)
}

func renderTable(rows []scan.Result) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
		"PATH", "HOST", "ORIGIN", "BR", "CM", "OBJ", "SIZE", "STATE"))
	for _, r := range rows {
		st := r.Stat
		state := "ok"
		if st.Dirty {
			state = "dirty"
		} else if st.OriginURL == "" {
			state = "no-remote"
		}
		b.WriteString(fmt.Sprintf("%s\t%s\t%s\t%d\t%d\t%d\t%s\t%s\n",
			shortPath(st.Path), st.Host, trimURL(st.OriginURL),
			st.Branches, st.Commits, st.Objects, humanSize(st.DotGitSize), state))
	}
	return b.String()
}