package ui

import (
	"strings"
	"testing"

	"github.com/aognio/gitscan/internal/gitio"
	"github.com/aognio/gitscan/internal/scan"
)

func TestPagerPreservesCompleteValues(t *testing.T) {
	path := "/home/example/projects/a/very/deep/repository-name"
	origin := "https://git.example.com/a/very/deep/repository-name.git"
	rows := []scan.Result{{Stat: gitio.Stat{Path: path, OriginURL: origin}}}

	lines := pagerLines(rows, false)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, path) {
		t.Fatalf("pager output does not contain complete path: %q", path)
	}
	if !strings.Contains(joined, origin) {
		t.Fatalf("pager output does not contain complete origin: %q", origin)
	}
}

func TestPagerViewportUsesTerminalHeight(t *testing.T) {
	model := &tuiModel{height: 12}
	if got, want := model.pagerVisibleLines(), 11; got != want {
		t.Fatalf("pager visible lines = %d, want %d", got, want)
	}

	model.height = 1
	if got, want := model.pagerVisibleLines(), 1; got != want {
		t.Fatalf("small pager visible lines = %d, want %d", got, want)
	}
}

func TestPagerHorizontalMovementUsesHalfScreen(t *testing.T) {
	rows := []scan.Result{{Stat: gitio.Stat{Path: strings.Repeat("x", 100)}}}
	model := &tuiModel{rows: rows, pager: true, width: 40, height: 10}

	model.updatePager("right")
	if got, want := model.horizontalOffset, 20; got != want {
		t.Fatalf("horizontal offset = %d, want %d", got, want)
	}
	model.updatePager("shift+left")
	if model.horizontalOffset != 0 {
		t.Fatalf("horizontal offset after jump home = %d, want 0", model.horizontalOffset)
	}
}

func TestPagerVerticalMovementClampsToContent(t *testing.T) {
	rows := []scan.Result{{Stat: gitio.Stat{Path: "one"}}}
	model := &tuiModel{rows: rows, pager: true, width: 80, height: 10}

	model.updatePager("end")
	if model.verticalOffset != 0 {
		t.Fatalf("vertical offset = %d, want 0 for short content", model.verticalOffset)
	}
}

func TestTableAlignment(t *testing.T) {
	if got, want := alignCell("github.com", 14, alignRight), "    github.com"; got != want {
		t.Fatalf("right-aligned host = %q, want %q", got, want)
	}
	if got, want := alignCell("ok", 8, alignCenter), "   ok   "; got != want {
		t.Fatalf("center-aligned state = %q, want %q", got, want)
	}
}

func TestHumanSizeSeparatesUnit(t *testing.T) {
	if got, want := humanSize(1536), "1.5 KB"; got != want {
		t.Fatalf("human size = %q, want %q", got, want)
	}
}
