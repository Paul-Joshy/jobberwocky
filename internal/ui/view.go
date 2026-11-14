package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/Paul-Joshy/jobberwocky/internal/workspace"
)

var (
	styleTitle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
	styleAccent = lipgloss.NewStyle().Foreground(lipgloss.Color("213"))
	styleDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	styleSel    = lipgloss.NewStyle().Background(lipgloss.Color("237")).Bold(true)
	styleHigh   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	styleMed    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	styleLow    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	styleOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	styleErr    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	styleBorder = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderLeft(true).
			BorderForeground(lipgloss.Color("238")).PaddingLeft(1)
)

func (m Model) bodyHeight() int { return max(3, m.h-3) }

// fit pads or truncates s to exactly w terminal cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = runewidth.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(0, w-runewidth.StringWidth(s)))
}

func wrap(s string, w int) string {
	if w <= 10 {
		return s
	}
	return lipgloss.NewStyle().Width(w).Render(s)
}

func (m Model) View() string {
	if m.w == 0 {
		return "loading…"
	}
	switch m.mode {
	case modeDoc:
		return m.frame(styleTitle.Render(m.docTitle), m.doc.View(), styleDim.Render("↑↓ scroll · esc back"))
	case modeHelp:
		return m.frame(styleTitle.Render("keys"), helpText, styleDim.Render("any key to go back"))
	case modeTasks:
		return m.frame(styleTitle.Render("tasks"), m.tasksView(), styleDim.Render("↑↓ move · enter log · x cancel · esc back"))
	}
	body := m.listAndDetail()
	if m.mode == modeInput {
		body = lipgloss.JoinVertical(lipgloss.Left, body, m.input.View())
	}
	return m.frame(m.header(), body, m.footer())
}

func (m Model) frame(head, body, foot string) string {
	bh := m.bodyHeight()
	lines := strings.Split(body, "\n")
	if len(lines) > bh {
		lines = lines[:bh]
	}
	for len(lines) < bh {
		lines = append(lines, "")
	}
	all := append(append([]string{head}, lines...), strings.Split(foot, "\n")...)
	clamp := lipgloss.NewStyle().MaxWidth(m.w)
	for i, l := range all {
		all[i] = clamp.Render(l)
	}
	return strings.Join(all, "\n")
}

func (m Model) header() string {
	ranked, active := 0, 0
	for _, j := range m.all {
		if j.RankScore != nil {
			ranked++
		}
		if !j.Hidden() {
			active++
		}
	}
	h := styleTitle.Render("jobberwocky") + styleDim.Render(fmt.Sprintf("  %d shown · %d active · %d ranked", len(m.view), active, ranked))
	if len(m.pending) > 0 {
		h += styleAccent.Render(fmt.Sprintf(" · %d new", len(m.pending)))
	}
	if len(m.marked) > 0 {
		h += styleAccent.Render(fmt.Sprintf(" · ● %d marked", len(m.marked)))
	}
	if m.filtering || m.filter.Value() != "" {
		h += "  " + m.filter.View()
	}
	return h
}

func (m Model) footer() string {
	var parts []string
	run, ok, bad := 0, 0, 0
	for _, t := range m.tasks {
		switch t.State {
		case queued, running:
			run++
		case done:
			ok++
		case failed:
			bad++
		}
	}
	if run > 0 {
		parts = append(parts, m.spin.View()+fmt.Sprintf(" %d running", run))
		for i := len(m.tasks) - 1; i >= 0; i-- {
			if t := m.tasks[i]; t.State == running {
				parts = append(parts, styleDim.Render(fit(t.Label+": "+t.Last(), 60)))
				break
			}
		}
	}
	if ok > 0 {
		parts = append(parts, styleOK.Render(fmt.Sprintf("✓ %d", ok)))
	}
	if bad > 0 {
		parts = append(parts, styleErr.Render(fmt.Sprintf("✗ %d", bad)))
	}
	tray := strings.Join(parts, "  ")
	return fit(tray, m.w) + "\n" + styleDim.Render(fit(m.status, m.w))
}

func fitGlyph(f string) string {
	switch strings.ToLower(f) {
	case "high":
		return styleHigh.Render("H")
	case "medium":
		return styleMed.Render("M")
	case "low":
		return styleLow.Render("L")
	}
	return styleDim.Render("·")
}

func (m Model) listAndDetail() string {
	bh := m.bodyHeight()
	listW, detailW := m.w, 0
	if m.w >= 100 {
		detailW = m.w * 2 / 5
		listW = m.w - detailW - 1
	}
	// keep the cursor on screen
	off := m.offset
	if m.cursor < off {
		off = m.cursor
	}
	if m.cursor >= off+bh {
		off = m.cursor - bh + 1
	}
	now := time.Now()
	compW, locW := 18, 14
	titleW := max(10, listW-2-4-3-2-compW-locW-4)
	var rows []string
	if len(m.view) == 0 {
		rows = append(rows, styleDim.Render("No jobs to show. Press s to scrape, a to show hidden ones."))
	}
	for i := off; i < len(m.view) && i < off+bh; i++ {
		j := m.view[i]
		score := "  ·"
		if j.RankScore != nil {
			score = fmt.Sprintf("%3.0f", *j.RankScore)
		} else if j.Pending {
			score = styleAccent.Render("new")
		}
		marks := ""
		if m.hasDoc("drafts", j) {
			marks = "✎"
		} else if m.hasDoc("fit", j) {
			marks = "✓"
		}
		sel := " "
		if m.marked[j.Key] {
			sel = styleAccent.Render("●")
		}
		row := fmt.Sprintf("%s%s %s %s %s %s %s %s",
			sel, score, j.Fresh(now), fitGlyph(j.Fit), fit(marks, 1),
			fit(j.Title, titleW), fit(j.Company, compW), fit(j.Location, locW))
		// rows carry colour codes, so cut them with an ANSI-aware width limit
		if i == m.cursor {
			row = styleSel.Width(listW).MaxWidth(listW).Render(row)
		} else {
			row = lipgloss.NewStyle().MaxWidth(listW).Render(row)
		}
		rows = append(rows, row)
	}
	list := strings.Join(rows, "\n")
	if detailW == 0 {
		return list
	}
	detail := ""
	if j, ok := m.selected(); ok {
		detail = m.detailText(j, detailW-2)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(listW).Render(list),
		styleBorder.Width(detailW-1).Height(bh).Render(wrap(detail, detailW-3)))
}

func (m Model) detailText(j workspace.Job, w int) string {
	var b strings.Builder
	b.WriteString(styleTitle.Render(j.Title) + "\n")
	b.WriteString(j.Company)
	if j.Location != "" {
		b.WriteString(" · " + j.Location)
	}
	b.WriteString("\n\n")
	now := time.Now()
	posted := j.PostedDate
	if posted == "" {
		posted = "unknown (first seen " + j.FirstSeen + ")"
	}
	fmt.Fprintf(&b, "%s posted %s\n", j.Fresh(now), posted)
	if j.RankScore != nil {
		fmt.Fprintf(&b, "score %.0f · %s\n", *j.RankScore, j.RankVerdict)
	}
	if j.Fit != "" {
		fmt.Fprintf(&b, "quick fit %s · status %s\n", j.Fit, j.Status)
	}
	if j.Deadline != "" {
		fmt.Fprintf(&b, "deadline %s\n", j.Deadline)
	}
	if j.LanguageGate != "" && j.LanguageGate != "PASS" {
		fmt.Fprintf(&b, styleErr.Render("language %s: %s")+"\n", j.LanguageGate, j.LanguageNote)
	}
	if j.LocationVerdict != "" && j.LocationVerdict != "PASS" {
		fmt.Fprintf(&b, styleErr.Render("location %s")+"\n", j.LocationVerdict)
	}
	b.WriteString(styleDim.Render(j.Portal) + "\n" + styleDim.Render(j.URL) + "\n")
	if len(j.Strengths) > 0 {
		b.WriteString("\n" + styleHigh.Render("strengths") + "\n")
		for _, s := range j.Strengths {
			b.WriteString("+ " + s + "\n")
		}
	}
	if len(j.Gaps) > 0 {
		b.WriteString("\n" + styleLow.Render("gaps") + "\n")
		for _, s := range j.Gaps {
			b.WriteString("- " + s + "\n")
		}
	}
	b.WriteString("\n")
	switch {
	case m.hasDoc("drafts", j):
		b.WriteString(styleOK.Render("✎ drafted") + styleDim.Render(" · v read · p fill form"))
	case m.hasDoc("fit", j):
		b.WriteString(styleOK.Render("✓ fit check saved") + styleDim.Render(" · v read · d draft"))
	case m.busy("fit", j.Key):
		b.WriteString(m.spin.View() + " fit check running")
	default:
		b.WriteString(styleDim.Render("f fit check · o open posting"))
	}
	return b.String()
}

func (m Model) tasksView() string {
	if len(m.tasks) == 0 {
		return styleDim.Render("No tasks yet.")
	}
	var rows []string
	for i, t := range m.tasks {
		icon := map[taskState]string{queued: "…", running: m.spin.View(), done: styleOK.Render("✓"), failed: styleErr.Render("✗")}[t.State]
		extra := ""
		if t.Cost > 0 {
			extra = fmt.Sprintf(" $%.2f", t.Cost)
		}
		last := t.Last()
		if t.State == done && t.Result != "" {
			last = lastLine(t.Result)
		}
		if t.Err != nil {
			last = t.Err.Error()
		}
		row := fmt.Sprintf(" %s %s %6s%s  %s", icon, fit(t.Label, 40), t.Elapsed(), extra, last)
		row = fit(row, m.w)
		if i == m.taskCursor {
			row = styleSel.Render(row)
		}
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n")
}

const helpText = `  Jobs list
    ↑/↓ j/k   move               g/G    top / bottom
    /         filter             a      show hidden (skipped, expired)
    enter, v  read fit check / draft for the job
    o         open the posting in your browser
    R         reload from seen_jobs.json

  Select several (f, d, r and p then act on all marked jobs at once)
    space     mark / unmark and move down     ctrl+a  mark all shown
    esc, u    clear marks

  Pipeline (everything runs in the background, in parallel; keep working)
    s         scrape: portal CLIs in parallel, dedupe, quick-fit new hits
    r         rank: marked jobs (scored in parallel in one /rank run),
              or all new jobs when nothing is marked
    f         fit check the marked jobs (or the one under the cursor)
    d         draft CV + cover letter for each (needs a fit check first);
              drafts run in parallel and are recorded one at a time
    p         fill each one's form in its own Chrome window: you get asked
              for each form link in turn (tab skips). Never submits:
              you check each form and press Submit yourself.

  Tasks
    t         task tray: enter for the log, x to cancel

  Columns: score · freshness (🟢 ≤1 day, 🟡 ≤7, 🔴 older, ⚪ unknown) · quick fit · ✓ fit / ✎ drafted

    q         quit (asks again if tasks are running)`
