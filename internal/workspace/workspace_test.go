package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func score(v float64) *float64 { return &v }

func TestSortOrder(t *testing.T) {
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	jobs := []Job{
		{Key: "low-unranked", Fit: "low", FirstSeen: "2026-11-13"},
		{Key: "ranked-60", RankScore: score(60), FirstSeen: "2026-11-01"},
		{Key: "pending", Pending: true},
		{Key: "ranked-80", RankScore: score(80), FirstSeen: "2026-11-01"},
		{Key: "high-unranked", Fit: "high", FirstSeen: "2026-11-01"},
	}
	Sort(jobs, now)
	want := []string{"pending", "ranked-80", "ranked-60", "high-unranked", "low-unranked"}
	for i, k := range want {
		if jobs[i].Key != k {
			t.Fatalf("position %d: got %s, want %s", i, jobs[i].Key, k)
		}
	}
}

func TestFresh(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	cases := map[string]Job{
		"🟢": {PostedDate: "2026-11-14"},
		"🟡": {PostedDate: "2026-11-10"},
		"🔴": {PostedDate: "2026-10-01"},
		"⚪": {},
	}
	for want, j := range cases {
		if got := j.Fresh(now); got != want {
			t.Errorf("%+v: got %s, want %s", j, got, want)
		}
	}
	// falls back to first_seen when the posting date is unknown
	if got := (Job{FirstSeen: "2026-11-14"}).Fresh(now); got != "🟢" {
		t.Errorf("first_seen fallback: got %s", got)
	}
}

func TestLoadJobsSkipsMalformed(t *testing.T) {
	ws := t.TempDir()
	os.MkdirAll(filepath.Join(ws, "job_scraper"), 0o755)
	data := `{"seen": {"a": {"title": "Copywriter", "company": "Acme", "rank_score": 72},
	                    "b": "not an object"}}`
	os.WriteFile(StatePath(ws), []byte(data), 0o644)
	jobs, err := LoadJobs(ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Key != "a" || *jobs[0].RankScore != 72 {
		t.Fatalf("got %+v", jobs)
	}
}

func TestLoadJobsMissingFile(t *testing.T) {
	jobs, err := LoadJobs(t.TempDir())
	if err != nil || len(jobs) != 0 {
		t.Fatalf("got %v, %v", jobs, err)
	}
}
