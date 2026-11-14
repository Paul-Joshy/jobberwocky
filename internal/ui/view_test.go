package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Paul-Joshy/jobberwocky/internal/config"
	"github.com/Paul-Joshy/jobberwocky/internal/states"
	"github.com/Paul-Joshy/jobberwocky/internal/workspace"
)

func score(v float64) *float64 { return &v }

func testModel(t *testing.T, w, h int) Model {
	t.Helper()
	cfg := &config.Config{Workspace: t.TempDir(), DataDir: t.TempDir(), MaxParallelClaude: 1}
	for _, sub := range []string{"fit", "drafts", "forms"} {
		os.MkdirAll(filepath.Join(cfg.DataDir, sub), 0o700)
	}
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
	for _, size := range [][2]int{{170, 30}, {140, 30}, {100, 20}, {80, 20}, {30, 6}} {
		m := testModel(t, size[0], size[1])
		out := m.View()
		if !strings.Contains(out, "Senior Co") || !strings.Contains(out, "jobberwocky") {
			t.Fatalf("%v: missing content:\n%s", size, out)
		}
		if strings.Contains(out, "Old one") {
			t.Fatalf("%v: skipped job should be hidden", size)
		}
		lines := strings.Split(out, "\n")
		// one job per row: the second job sits on the line right after the first (no wrapping)
		if size[1] > 6 && !strings.Contains(lines[3], "Beta") {
			t.Errorf("%v: row 2 should be Beta, got %q (a row wrapped?)", size, lines[3])
		}
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
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("7")}) // all
	m = next.(Model)
	if m.tab != tabAll || len(m.view) != 3 {
		t.Fatalf("all tab: got tab %d with %d jobs", m.tab, len(m.view))
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

func TestMarkAndBatchDraftSkipsUnchecked(t *testing.T) {
	m := testModel(t, 120, 30)
	key := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	next, _ = next.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	m = next.(Model)
	if len(m.marked) != 2 || len(m.targets()) != 2 {
		t.Fatalf("marked %d, targets %d", len(m.marked), len(m.targets()))
	}
	next, _ = m.Update(key("d"))
	m = next.(Model)
	if len(m.tasks) != 0 || len(m.marked) != 0 {
		t.Fatalf("drafts started without fit checks: %d tasks", len(m.tasks))
	}
}

func TestDoneAndArchiveTabs(t *testing.T) {
	m := testModel(t, 120, 30)
	key := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
	// cursor is on the top job ("a", ranked 81): mark it done
	next, _ := m.Update(key("c"))
	m = next.(Model)
	if len(m.view) != 1 || m.view[0].Key != "b" {
		t.Fatalf("done job should leave the active tab, got %+v", m.view)
	}
	// archive the remaining one
	next, _ = m.Update(key("x"))
	m = next.(Model)
	if len(m.view) != 0 {
		t.Fatal("archived job should leave the active tab")
	}
	next, _ = m.Update(key("5")) // done view
	m = next.(Model)
	if len(m.view) != 1 || m.view[0].Key != "a" {
		t.Fatalf("done tab: %+v", m.view)
	}
	next, _ = m.Update(key("c")) // undo
	m = next.(Model)
	if len(m.view) != 0 || m.states.Get("a") != "" {
		t.Fatal("pressing c again should undo")
	}
	next, _ = m.Update(key("6")) // archived view
	m = next.(Model)
	if len(m.view) != 1 || m.view[0].Key != "b" {
		t.Fatalf("archived tab: %+v", m.view)
	}
	if !strings.Contains(m.View(), "6 archived 1") {
		t.Error("header should count archived jobs")
	}
}

func TestTopPicks(t *testing.T) {
	cfg := &config.Config{Workspace: t.TempDir(), DataDir: t.TempDir(), MaxParallelClaude: 1}
	m, _ := New(cfg)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	next, _ = next.Update(reloadMsg{jobs: []workspace.Job{
		{Key: "low", Title: "Low", RankScore: score(30)},
		{Key: "h1", Title: "H1", RankScore: score(82)},
		{Key: "h2", Title: "H2", RankScore: score(70)},
		{Key: "m1", Title: "M1", RankScore: score(50)},
		{Key: "m2", Title: "M2", RankScore: score(48)},
		{Key: "skipped", Title: "Skipped", RankScore: score(95), Status: "skipped"},
	}})
	m = next.(Model)
	if m.top["h1"] != 1 || m.top["h2"] != 2 || m.top["m1"] != 3 || len(m.top) != 3 {
		t.Fatalf("top = %v", m.top)
	}
	if m.view[0].Key != "h1" || m.view[3].Key != "m2" || m.view[4].Key != "low" {
		t.Fatalf("order: %v %v %v", m.view[0].Key, m.view[3].Key, m.view[4].Key)
	}
	if !strings.Contains(m.View(), "TOP PICK #1") {
		t.Error("detail should flag the top pick")
	}
	// marking the first pick done promotes the next job
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = next.(Model)
	if m.top["m2"] != 3 || len(m.top) != 3 {
		t.Fatalf("after done: top = %v", m.top)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("*")})
	m = next.(Model)
	if len(m.marked) != 3 || !m.marked["h2"] || !m.marked["m2"] {
		t.Fatalf("3 should mark the picks, got %v", m.marked)
	}
}

func TestFillQueueAsksPerJob(t *testing.T) {
	m := testModel(t, 120, 30)
	m.marked = map[string]bool{"a": true, "b": true}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = next.(Model)
	if m.mode != modeInput || m.fillTotal != 2 || len(m.fillQueue) != 1 {
		t.Fatalf("mode %v total %d queue %d", m.mode, m.fillTotal, len(m.fillQueue))
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(Model)
	if m.mode != modeInput || len(m.fillQueue) != 0 {
		t.Fatal("tab should move to the second job")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if next.(Model).mode != modeList {
		t.Fatal("queue done should return to the list")
	}
}

func TestFormURLNeedsHTTPS(t *testing.T) {
	for url, want := range map[string]bool{
		"https://forms.gle/abc":                        true,
		"https://docs.google.com/forms/d/e/x/viewform": true,
		"https://careers.example.com/apply/123":        true,
		"http://forms.gle/abc":                         false,
		"javascript:alert(1)":                          false,
	} {
		if formURL.MatchString(url) != want {
			t.Errorf("%s: want %v", url, want)
		}
	}
}

func TestTrackerAppliedJobsAreDoneAndNeverPicks(t *testing.T) {
	cfg := &config.Config{Workspace: t.TempDir(), DataDir: t.TempDir(), MaxParallelClaude: 1}
	m, _ := New(cfg)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	next, _ = next.Update(reloadMsg{
		jobs: []workspace.Job{
			{Key: "applied", Company: "Tailspin", Title: "Brand Strategist & Copywriter", RankScore: score(90)},
			{Key: "drafted", Company: "Wingtip", Title: "Lead Copy", RankScore: score(80)},
			{Key: "veto", Company: "X", Title: "Writer", RankScore: score(85), LanguageGate: "FAIL"},
			{Key: "free", Company: "Y", Title: "Writer", RankScore: score(70)},
		},
		tracker: []workspace.TrackerRow{
			{Company: "Tailspin", Role: "Brand Strategist & Copywriter", Status: "applied"},
			{Company: "Wingtip", Role: "Lead Copy", Status: "drafted"},
		},
	})
	m = next.(Model)
	if _, ok := m.top["applied"]; ok {
		t.Error("an applied job must not be a top pick")
	}
	if _, ok := m.top["veto"]; ok {
		t.Error("a language FAIL must not be a top pick")
	}
	if m.top["drafted"] != 1 || m.top["free"] != 2 {
		t.Errorf("top = %v", m.top)
	}
	for _, j := range m.view {
		if j.Key == "applied" {
			t.Error("applied job should be in the done tab, not active")
		}
	}
	if m.countState(states.Done) != 1 {
		t.Errorf("done count %d", m.countState(states.Done))
	}
}
