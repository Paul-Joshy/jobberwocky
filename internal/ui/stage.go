package ui

import (
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/Paul-Joshy/jobberwocky/internal/states"
	"github.com/Paul-Joshy/jobberwocky/internal/workspace"
)

// A job moves through four stages: Fit checked → Drafted → Prefilled form → Applied.
// Fit checks, drafts and form reports are files in the data dir, so the
// stages survive restarts.

// docInfo caches what exists on disk for one job, so rendering never stats files.
type docInfo struct {
	fit, draft, form bool
	fitScore         *float64
	fitVerdict       string
}

// listTab is which slice of jobs the list shows. Number keys jump, tab cycles.
type listTab int

const (
	tabActive  listTab = iota // everything still in progress
	tabToCheck                // in progress, no fit check yet
	tabChecked                // fit checked, not drafted
	tabDrafted                // drafted, not applied
	tabDone                   // applied / finished (incl. tracker)
	tabArchived
	tabAll
)

var tabNames = []string{"📌 active", "🆕 to check", "🔍 checked", "📝 drafted", "✅ done", "📦 archived", "🌐 all"}

func (m Model) docsFor(j workspace.Job) docInfo { return m.docs[j.Key] }

// hasDoc reports a stage file from the cache ("fit", "drafts" or "forms").
func (m Model) hasDoc(kind string, j workspace.Job) bool {
	d := m.docsFor(j)
	switch kind {
	case "fit":
		return d.fit
	case "drafts":
		return d.draft
	case "forms":
		return d.form
	}
	return false
}

func (m Model) formReportPath(j workspace.Job) string {
	return strings.TrimSuffix(m.docPath("forms", j), ".md") + ".json"
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// refreshDocs re-reads which stage files exist and copies fit-check scores onto the jobs.
func (m *Model) refreshDocs() {
	m.docs = make(map[string]docInfo, len(m.all))
	for i, j := range m.all {
		d := docInfo{draft: exists(m.docPath("drafts", j)), form: exists(m.formReportPath(j))}
		if raw, err := os.ReadFile(m.docPath("fit", j)); err == nil {
			d.fit = true
			if s, v, ok := parseFit(string(raw)); ok {
				d.fitScore, d.fitVerdict = &s, v
			}
		}
		m.docs[j.Key] = d
		m.all[i].FitScore, m.all[i].FitVerdict = d.fitScore, d.fitVerdict
	}
}

var (
	// the first line new fit checks are asked to start with
	fitHeadline = regexp.MustCompile(`(?i)^\W*FIT:\s*(\d{1,3})\s*/\s*100\W*(.*)$`)
	// older fit checks: "Overall: 71/100, Good Fit", "## Score: 79 / 100. Verdict: good fit", "Fit: 71/100"
	fitLoose = regexp.MustCompile(`(?i)\b(?:overall|fit|score)\b[^0-9|\n]{0,12}(\d{1,3})\s*/\s*100[\s.,:·-]*(?:verdict:\s*)?([A-Za-z ]{0,30}fit)?`)
)

// parseFit finds the overall score and verdict in a fit check. Table rows
// (per-dimension scores) are ignored.
func parseFit(text string) (float64, string, bool) {
	lines := strings.Split(text, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if m := fitHeadline.FindStringSubmatch(l); m != nil {
			return fitScoreOf(m[1], strings.Trim(m[2], " ·*-.,"))
		}
		break
	}
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "|") {
			continue
		}
		if m := fitLoose.FindStringSubmatch(l); m != nil {
			return fitScoreOf(m[1], strings.TrimSpace(m[2]))
		}
	}
	return 0, "", false
}

func fitScoreOf(num, verdict string) (float64, string, bool) {
	n, err := strconv.Atoi(num)
	if err != nil || n > 100 {
		return 0, "", false
	}
	return float64(n), verdict, true
}

// inTab decides which jobs a tab shows.
func (m Model) inTab(j workspace.Job) bool {
	st := m.state(j)
	active := st == "" && !j.Hidden()
	d := m.docsFor(j)
	switch m.tab {
	case tabActive:
		return active
	case tabToCheck:
		return active && !d.fit
	case tabChecked:
		return active && d.fit && !d.draft
	case tabDrafted:
		return active && d.draft
	case tabDone:
		return st == states.Done
	case tabArchived:
		return st == states.Archived
	}
	return true
}

// tabCounts counts jobs per tab (ignoring the text filter) for the header.
func (m Model) tabCounts() []int {
	counts := make([]int, len(tabNames))
	saved := m.tab
	for _, src := range [][]workspace.Job{m.pending, m.all} {
		for _, j := range src {
			for t := range tabNames {
				m.tab = listTab(t)
				if m.inTab(j) {
					counts[t]++
				}
			}
		}
	}
	m.tab = saved
	return counts
}

// Status emojis, one per stage. Each row shows the furthest stage reached.
const (
	emojiNew      = "🆕"
	emojiFit      = "🔍"
	emojiDraft    = "📝"
	emojiForm     = "📋"
	emojiApplied  = "✅"
	emojiArchived = "📦"
	emojiRunning  = "⏳"
)

// stageEmoji is the row's status: running beats everything, then applied or
// archived, then the furthest stage on the way (form > draft > fit > new).
func (m Model) stageEmoji(j workspace.Job) string {
	d := m.docsFor(j)
	switch {
	case m.busy("fit", j.Key) || m.busy("draft", j.Key) || m.busy("fill", j.Key):
		return emojiRunning
	case m.state(j) == states.Done:
		return emojiApplied
	case m.state(j) == states.Archived:
		return emojiArchived
	case d.form:
		return emojiForm
	case d.draft:
		return emojiDraft
	case d.fit:
		return emojiFit
	}
	return emojiNew
}

// nextStep is what a job needs next on its way to submitted.
func (m Model) nextStep(j workspace.Job) string {
	d := m.docsFor(j)
	switch {
	case m.state(j) == states.Done:
		return "applied ✔"
	case m.state(j) == states.Archived:
		return "archived (x to restore)"
	case m.busy("draft", j.Key):
		return "drafting…"
	case m.busy("fit", j.Key):
		return "fit check running…"
	case m.busy("fill", j.Key):
		return "form open in Chrome: check it and press Submit"
	case d.form:
		return "form prefilled: submit it, then c to mark applied"
	case d.draft:
		return "ready to submit: p fills the form"
	case d.fit:
		return "ready to draft: d"
	default:
		return "ready for a fit check: f"
	}
}

// progress is the detail panel's one-line journey for a job.
func (m Model) progress(j workspace.Job) string {
	d := m.docsFor(j)
	step := func(on bool, emoji, label string) string {
		if on {
			return emoji + " " + styleOK.Render(label+" ✓")
		}
		return emoji + " " + styleDim.Render(label)
	}
	fit := "Fit check"
	if d.fitScore != nil {
		fit += " " + strconv.Itoa(int(*d.fitScore))
		if d.fitVerdict != "" {
			fit += " " + d.fitVerdict
		}
	}
	arrow := styleDim.Render(" → ")
	return step(d.fit, emojiFit, fit) + arrow + step(d.draft, emojiDraft, "Draft") + arrow +
		step(d.form, emojiForm, "Form") + arrow + step(m.state(j) == states.Done, emojiApplied, "Applied")
}

// verdict is 👍 apply, 🤔 consider or 👎 skip. The fit check's own word wins
// ("FIT: 71/100 · Good Fit · apply"); otherwise the score band decides
// (60+ apply, 45-59 consider, below 45 skip). No score yet: blank.
func (m Model) verdict(j workspace.Job) string {
	v := strings.ToLower(m.docsFor(j).fitVerdict)
	switch {
	case strings.Contains(v, "skip"):
		return "👎"
	case strings.Contains(v, "consider"):
		return "🤔"
	case strings.Contains(v, "apply"):
		return "👍"
	}
	if s := j.Score(); s != nil {
		switch {
		case *s >= 60:
			return "👍"
		case *s >= 45:
			return "🤔"
		default:
			return "👎"
		}
	}
	return "  "
}
