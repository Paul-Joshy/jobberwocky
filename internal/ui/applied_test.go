package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"

	"github.com/Paul-Joshy/jobberwocky/internal/workspace"
)

func TestSlugifyMatchesRecordScript(t *testing.T) {
	cases := map[[2]string]string{
		{"Northwind Learning", "Creative Copywriter"}: "northwind_learning_creative_copywriter",
		{"Fabrikam", "Content Marketing Lead"}:         "fabrikam_content_marketing_lead",
		{"Blue Harbour", "Sr. Copywriter (Remote) – UK"}:  "blue_harbour_sr_copywriter_remote_uk",
	}
	for in, want := range cases {
		if got := slugify(in[0], in[1]); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAppliedArgs(t *testing.T) {
	j := workspace.Job{Company: "Northwind Learning (NWL)", Title: "Creative Copywriter", URL: "https://x/y"}
	row := workspace.TrackerRow{Company: "Northwind Learning", Role: "Creative Copywriter", Status: "drafted"}
	args, ok := appliedArgs(j, row, true, "2026-11-14")
	if !ok {
		t.Fatal("drafted row should be recorded as applied")
	}
	got := strings.Join(args, "|")
	for _, want := range []string{"northwind_learning_creative_copywriter", "--company|Northwind Learning|", "--status|applied", "--no-copy-pdfs"} {
		if !strings.Contains(got, want) {
			t.Errorf("args %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "--source") {
		t.Error("an existing row keeps its own source")
	}
	row.Status = "Interview"
	if _, ok := appliedArgs(j, row, true, "2026-11-14"); ok {
		t.Error("a row past applied must be left alone")
	}
	args, ok = appliedArgs(j, workspace.TrackerRow{}, false, "2026-11-14")
	if !ok || !strings.Contains(strings.Join(args, "|"), "--source|https://x/y") {
		t.Errorf("new row should carry the posting URL: %q", args)
	}
}

func TestReviewed(t *testing.T) {
	if !reviewed("files\n\n## Review\nverdict: strong") || reviewed("no section here") {
		t.Fatal("reviewed() misread a draft result")
	}
}

func TestBigBatchNeedsSecondPress(t *testing.T) {
	key := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
	m := testModel(t, 140, 30)
	var jobs []workspace.Job
	for _, k := range []string{"j1", "j2", "j3", "j4"} {
		jobs = append(jobs, workspace.Job{Key: k, Title: "Copywriter " + k, Company: "Co " + k, Fit: "high", Status: "ranked"})
	}
	next, _ := m.Update(reloadMsg{jobs: jobs})
	m = next.(Model)
	for _, j := range m.view {
		m.marked[j.Key] = true
	}
	next, _ = m.Update(key("f"))
	m = next.(Model)
	if m.bigArmed != "f" || len(m.tasks) != 0 {
		t.Fatalf("first f on 4 jobs should only arm: armed=%q tasks=%d", m.bigArmed, len(m.tasks))
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.bigArmed != "" {
		t.Fatal("any other key disarms")
	}
}
