package workspace

import (
	"os"
	"testing"
)

func TestTrackerMatching(t *testing.T) {
	ws := t.TempDir()
	csv := "date,company,sector,role,role_type,channel,status\n" +
		"2026-11-05,Parallel,Design,Senior Content Strategist - Web Design,x,online,withdrawn\n" +
		"2026-11-12,Contoso,AI,Creative Content Lead (Contoso Studios),x,portal,applied\n" +
		"2026-11-13,Tailspin,Fintech,Brand Strategist & Copywriter,x,online,applied\n" +
		"2026-11-13,Wingtip,Ecom,Manager - Branding,x,online,drafted\n"
	os.WriteFile(ws+"/job_search_tracker.csv", []byte(csv), 0o644)
	rows, err := LoadTracker(ws)
	if err != nil || len(rows) != 4 {
		t.Fatalf("rows %d err %v", len(rows), err)
	}
	cases := []struct {
		company, title, want string
		ok                   bool
	}{
		{"Parallel", "Senior Content Strategist, Web Design", "withdrawn", true},
		{"Contoso", "Creative Content Lead", "applied", true},
		{"tailspin", "Brand Strategist & Copywriter", "applied", true},
		{"Wingtip", "Lead Copy", "", false}, // same company, different role
		{"Wingtip", "Manager - Branding", "drafted", true},
		{"Tailspin", "Senior Brand Strategist", "", false}, // too different
		{"Other Co", "Creative Content Lead", "", false},
	}
	for _, c := range cases {
		got, ok := TrackerStatus(Job{Company: c.company, Title: c.title}, rows)
		if got != c.want || ok != c.ok {
			t.Errorf("%s / %s: got %q %v, want %q %v", c.company, c.title, got, ok, c.want, c.ok)
		}
	}
}
