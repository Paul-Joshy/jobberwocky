package ui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Paul-Joshy/jobberwocky/internal/workspace"
)

func TestParseFit(t *testing.T) {
	cases := map[string]float64{
		"FIT: 78/100 · Strong Fit · apply\n\nbody":                                78,
		"**Overall: 71/100, Good Fit. 🔴 Crowded**":                                71,
		"## Score: 79 / 100. Verdict: good fit (already applied)":                 79,
		"# GUS: Creative Copywriter. Fit: 71/100, Good fit, worth applying":       71,
		"intro\n| Technical Skills | 78/100 | notes |\n**Overall: 66/100, Good**": 66, // table rows ignored
	}
	for text, want := range cases {
		got, _, ok := parseFit(text)
		if !ok || got != want {
			t.Errorf("%q: got %v %v, want %v", text, got, ok, want)
		}
	}
	if _, _, ok := parseFit("| Skills | 80/100 | only a table |"); ok {
		t.Error("a table-only score must not count as the overall score")
	}
	if _, v, _ := parseFit("FIT: 78/100 · Strong Fit · apply"); v != "Strong Fit · apply" {
		t.Errorf("verdict %q", v)
	}
}

func TestStagesViewsAndFitScoreSort(t *testing.T) {
	m := testModel(t, 140, 30) // jobs a (rank 81), b (unranked medium), c (skipped)
	// b gets a fit check scoring 90: it should jump above a, show F, and appear in "checked"
	os.WriteFile(m.docPath("fit", workspace.Job{Key: "b"}), []byte("FIT: 90/100 · Strong Fit · apply\n"), 0o600)
	m.refreshDocs()
	m.rebuild()
	if m.view[0].Key != "b" {
		t.Fatalf("fit score should lead the sort, got %s first", m.view[0].Key)
	}
	if !strings.Contains(m.View(), "✓ Fit check 90 Strong Fit") {
		t.Error("detail should show the fit-check progress")
	}
	key := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
	next, _ := m.Update(key("3")) // checked view
	m = next.(Model)
	if len(m.view) != 1 || m.view[0].Key != "b" {
		t.Fatalf("checked view: %+v", m.view)
	}
	next, _ = m.Update(key("2")) // to check view
	m = next.(Model)
	if len(m.view) != 1 || m.view[0].Key != "a" {
		t.Fatalf("to-check view: %+v", m.view)
	}
	counts := m.tabCounts()
	if counts[tabActive] != 2 || counts[tabToCheck] != 1 || counts[tabChecked] != 1 || counts[tabAll] != 3 {
		t.Fatalf("counts %v", counts)
	}
	// persistence: a fresh model reading the same data dir still sees the fit check
	m2, _ := New(m.cfg)
	next, _ = m2.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	next, _ = next.Update(reloadMsg{jobs: m.all})
	if !next.(Model).docsFor(workspace.Job{Key: "b"}).fit {
		t.Fatal("fit check not found after restart")
	}
}

func TestVerdict(t *testing.T) {
	m := testModel(t, 140, 30)
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		j    workspace.Job
		want string
	}{
		{workspace.Job{Key: "x1", RankScore: f(75)}, "👍"},
		{workspace.Job{Key: "x2", RankScore: f(50)}, "🤔"},
		{workspace.Job{Key: "x3", RankScore: f(30)}, "👎"},
		{workspace.Job{Key: "x4"}, "  "},
	}
	for _, c := range cases {
		if got := m.verdict(c.j); got != c.want {
			t.Errorf("%s: got %q want %q", c.j.Key, got, c.want)
		}
	}
	// the fit check's own word beats the score
	m.docs["x1"] = docInfo{fit: true, fitVerdict: "Good Fit · skip"}
	if got := m.verdict(workspace.Job{Key: "x1", RankScore: f(75)}); got != "👎" {
		t.Errorf("fit verdict should win, got %q", got)
	}
}
