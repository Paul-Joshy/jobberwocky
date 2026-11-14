package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Paul-Joshy/jobberwocky/internal/config"
	"github.com/Paul-Joshy/jobberwocky/internal/workspace"
)

func score(v float64) *float64 { return &v }

func testModel(t *testing.T, w, h int) Model {
	t.Helper()
	cfg := &config.Config{Workspace: t.TempDir(), DataDir: t.TempDir(), MaxParallelClaude: 1}
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	next, _ = next.Update(reloadMsg{jobs: []workspace.Job{
		{Key: "a", Title: "Senior Copywriter", Company: "Acme", Location: "Bengaluru", RankScore: score(81), Fit: "high", Status: "ranked", PostedDate: "2026-11-13", Strengths: []string{"consumer brand copy"}, Gaps: []string{"no B2B"}},
		{Key: "b", Title: "UX Writer", Company: "Beta", Location: "Remote", Fit: "medium", Status: "new"},
		{Key: "c", Title: "Old one", Company: "Gamma", Status: "skipped"},
	}})
	return next.(Model)
}

func TestViewRendersAndHidesSkipped(t *testing.T) {
	for _, size := range [][2]int{{140, 30}, {80, 20}, {30, 6}} {
		m := testModel(t, size[0], size[1])
		out := m.View()
		if !strings.Contains(out, "Senior Co") || !strings.Contains(out, "jobberwocky") {
			t.Fatalf("%v: missing content:\n%s", size, out)
		}
		if strings.Contains(out, "Old one") {
			t.Fatalf("%v: skipped job should be hidden", size)
		}
		lines := strings.Split(out, "\n")
		if len(lines) != size[1] {
			t.Errorf("%v: view has %d lines, want %d", size, len(lines), size[1])
		}
		for i, l := range lines {
			if w := lipgloss.Width(l); w > size[0] {
				t.Errorf("%v: line %d is %d cells wide (terminal is %d): %q", size, i, w, size[0], l)
			}
		}
	}
}

func TestShowAllAndFilter(t *testing.T) {
	m := testModel(t, 120, 30)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = next.(Model)
	if len(m.view) != 3 {
		t.Fatalf("show all: got %d jobs", len(m.view))
	}
	m.filter.SetValue("beta")
	m.rebuild()
	if len(m.view) != 1 || m.view[0].Key != "b" {
		t.Fatalf("filter: got %+v", m.view)
	}
}

func TestDraftNeedsFitCheck(t *testing.T) {
	m := testModel(t, 120, 30)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = next.(Model)
	if len(m.tasks) != 0 || !strings.Contains(m.status, "fit check first") {
		t.Fatalf("draft without fit check started a task: %q", m.status)
	}
}

func TestFormURLOnlyGoogleForms(t *testing.T) {
	for url, want := range map[string]bool{
		"https://forms.gle/abc":                        true,
		"https://docs.google.com/forms/d/e/x/viewform": true,
		"https://evil.example/forms.gle/abc":           false,
		"http://forms.gle/abc":                         false,
	} {
		if formURL.MatchString(url) != want {
			t.Errorf("%s: want %v", url, want)
		}
	}
}
