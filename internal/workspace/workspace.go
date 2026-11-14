// Package workspace reads the ai-job-search state files. It never writes them:
// every write goes through the workspace's own tools.
package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Job is one entry from job_scraper/seen_jobs.json (or a pending, not-yet-stored hit).
type Job struct {
	Key             string   `json:"-"`
	Title           string   `json:"title"`
	Company         string   `json:"company"`
	URL             string   `json:"url"`
	Location        string   `json:"location"`
	FirstSeen       string   `json:"first_seen"`
	PostedDate      string   `json:"posted_date"`
	Deadline        string   `json:"deadline"`
	Fit             string   `json:"fit"`
	Status          string   `json:"status"`
	Portal          string   `json:"portal"`
	Source          string   `json:"source"`
	RankScore       *float64 `json:"rank_score"`
	RankVerdict     string   `json:"rank_verdict"`
	RankDate        string   `json:"rank_date"`
	LocationVerdict string   `json:"location_verdict"`
	LanguageGate    string   `json:"language_gate"`
	LanguageNote    string   `json:"language_note"`
	Strengths       []string `json:"strengths"`
	Gaps            []string `json:"gaps"`

	Pending bool `json:"-"` // found this run, quick fit not stored yet
}

// StatePath is the seen-jobs file inside a workspace.
func StatePath(ws string) string { return filepath.Join(ws, "job_scraper", "seen_jobs.json") }

// LoadJobs reads every stored job.
func LoadJobs(ws string) ([]Job, error) {
	raw, err := os.ReadFile(StatePath(ws))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file struct {
		Seen map[string]json.RawMessage `json:"seen"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	jobs := make([]Job, 0, len(file.Seen))
	for key, entry := range file.Seen {
		var j Job
		if json.Unmarshal(entry, &j) != nil {
			continue // one malformed entry never hides the rest
		}
		j.Key = key
		jobs = append(jobs, j)
	}
	return jobs, nil
}

// Hidden reports statuses that are not worth looking at by default.
func (j Job) Hidden() bool {
	switch j.Status {
	case "skipped", "expired", "unreachable":
		return true
	}
	return false
}

// AgeDays is days since the posting date, falling back to first_seen.
func (j Job) AgeDays(now time.Time) (int, bool) {
	for _, d := range []string{j.PostedDate, j.FirstSeen} {
		if t, err := time.Parse("2006-01-02", d); err == nil {
			return int(now.Sub(t).Hours() / 24), true
		}
	}
	return 0, false
}

// Fresh is a one-glyph freshness signal: posted today/yesterday, this week, older, unknown.
func (j Job) Fresh(now time.Time) string {
	days, ok := j.AgeDays(now)
	switch {
	case !ok:
		return "⚪"
	case days <= 1:
		return "🟢"
	case days <= 7:
		return "🟡"
	default:
		return "🔴"
	}
}

// Band is the job's fit band: "high", "medium", "low" or "" (not rated yet).
// A rank score beats the quick fit. Thresholds follow the evaluation
// framework's verdicts: 60+ is Strong or Good Fit (apply), 45-59 Moderate
// (consider), below 45 Weak or Poor (skip).
func (j Job) Band() string {
	if j.RankScore != nil {
		switch s := *j.RankScore; {
		case s >= 60:
			return "high"
		case s >= 45:
			return "medium"
		default:
			return "low"
		}
	}
	switch f := strings.ToLower(j.Fit); f {
	case "high", "medium", "low":
		return f
	}
	return ""
}

func bandOrder(b string) int {
	switch b {
	case "high":
		return 0
	case "medium":
		return 1
	case "low":
		return 2
	}
	return 3
}

// Sort orders by band (H, M, L, unrated), then ranked before unranked, then
// score, then freshest. Pending hits go last: they are not rated yet.
func Sort(jobs []Job, now time.Time) {
	sort.SliceStable(jobs, func(a, b int) bool {
		ja, jb := jobs[a], jobs[b]
		if ja.Pending != jb.Pending {
			return jb.Pending
		}
		if oa, ob := bandOrder(ja.Band()), bandOrder(jb.Band()); oa != ob {
			return oa < ob
		}
		if (ja.RankScore != nil) != (jb.RankScore != nil) {
			return ja.RankScore != nil
		}
		if ja.RankScore != nil && *ja.RankScore != *jb.RankScore {
			return *ja.RankScore > *jb.RankScore
		}
		da, _ := ja.AgeDays(now)
		db, _ := jb.AgeDays(now)
		return da < db
	})
}

// FromHit turns a raw portal search hit into a pending Job for display.
func FromHit(h map[string]any) Job {
	s := func(k string) string {
		if v, ok := h[k].(string); ok {
			return v
		}
		return ""
	}
	return Job{
		Key: "pending:" + s("url"), Title: s("title"), Company: s("company"), URL: s("url"),
		Location: s("location"), PostedDate: s("date"), Portal: s("portal"), Source: s("source"),
		Status: "pending", Pending: true,
	}
}
