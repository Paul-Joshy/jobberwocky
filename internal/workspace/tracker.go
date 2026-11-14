package workspace

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// TrackerRow is one application from job_search_tracker.csv.
type TrackerRow struct {
	Company, Role, Status string
}

// LoadTracker reads the application tracker (read-only). A missing file is no rows.
func LoadTracker(ws string) ([]TrackerRow, error) {
	f, err := os.Open(filepath.Join(ws, "job_search_tracker.csv"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1 // older rows may lack the newest columns
	recs, err := r.ReadAll()
	if err != nil || len(recs) == 0 {
		return nil, err
	}
	col := map[string]int{}
	for i, h := range recs[0] {
		col[strings.TrimPrefix(strings.TrimSpace(h), "\ufeff")] = i
	}
	get := func(rec []string, name string) string {
		if i, ok := col[name]; ok && i < len(rec) {
			return rec[i]
		}
		return ""
	}
	rows := make([]TrackerRow, 0, len(recs)-1)
	for _, rec := range recs[1:] {
		rows = append(rows, TrackerRow{Company: get(rec, "company"), Role: get(rec, "role"), Status: get(rec, "status")})
	}
	return rows, nil
}

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)

func normWords(s string) []string {
	return strings.Fields(nonWord.ReplaceAllString(strings.ToLower(s), " "))
}

// sameRole matches titles that differ only in punctuation or an added
// qualifier: "Senior Content Strategist, Web Design" vs "... - Web Design",
// "Creative Content Lead" vs "Creative Content Lead (Contoso Studios)".
func sameRole(a, b string) bool {
	wa, wb := normWords(a), normWords(b)
	if len(wa) == 0 || len(wb) == 0 {
		return false
	}
	ja, jb := strings.Join(wa, " "), strings.Join(wb, " ")
	if ja == jb || strings.Contains(" "+ja+" ", " "+jb+" ") || strings.Contains(" "+jb+" ", " "+ja+" ") {
		return true
	}
	set := map[string]bool{}
	for _, w := range wa {
		set[w] = true
	}
	common := 0
	for _, w := range wb {
		if set[w] {
			common++
			delete(set, w)
		}
	}
	shorter := min(len(wa), len(wb))
	return shorter >= 2 && float64(common)/float64(shorter) >= 0.8
}

// TrackerStatus is the tracker status of the application matching this job, if any.
func TrackerStatus(j Job, rows []TrackerRow) (string, bool) {
	company := strings.Join(normWords(j.Company), " ")
	if company == "" {
		return "", false
	}
	for _, r := range rows {
		if strings.Join(normWords(r.Company), " ") == company && sameRole(j.Title, r.Role) {
			return r.Status, true
		}
	}
	return "", false
}
