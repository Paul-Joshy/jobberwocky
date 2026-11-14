// Package ui is the Bubble Tea front end: one list of jobs, keys that start
// background tasks, and a task tray so nothing runs out of sight.
package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Paul-Joshy/jobberwocky/internal/config"
	"github.com/Paul-Joshy/jobberwocky/internal/states"
	"github.com/Paul-Joshy/jobberwocky/internal/workspace"
)

type mode int

const (
	modeList mode = iota
	modeTasks
	modeDoc
	modeInput
	modeHelp
)

// Model is the whole UI state.
type Model struct {
	cfg *config.Config
	run *runner

	all     []workspace.Job
	pending []workspace.Job
	view    []workspace.Job
	cursor  int
	offset  int
	tab     listTab
	states  *states.Store
	top     map[string]int // job key -> top-pick rank 1..3

	trackerSt map[string]string  // job key -> status in job_search_tracker.csv
	docs      map[string]docInfo // job key -> which stage files exist
	marked    map[string]bool    // jobs selected with space; f, d, r and p act on all of them

	fillQueue []workspace.Job // marked jobs still waiting for their form URL
	fillTotal int
	fillSlot  map[int]int // fill task id -> Chrome profile slot (one window per form)

	filter    textinput.Model
	filtering bool

	mode     mode
	prevMode mode
	input    textinput.Model
	inputJob workspace.Job

	tasks      []*Task
	byID       map[int]*Task
	taskCursor int

	doc      viewport.Model
	docTitle string

	spin      spinner.Model
	status    string
	quitArmed bool
	w, h      int
}

type reloadMsg struct {
	jobs    []workspace.Job
	tracker []workspace.TrackerRow
	err     error
}

// New builds the model and loads the job list.
func New(cfg *config.Config) (Model, error) {
	f := textinput.New()
	f.Prompt = "/ "
	f.Placeholder = "filter title, company, location"
	in := textinput.New()
	in.Prompt = "form URL › "
	in.CharLimit = 2000
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = styleAccent
	m := Model{
		cfg: cfg, run: newRunner(cfg), filter: f, input: in, spin: sp,
		byID: map[int]*Task{}, doc: viewport.New(80, 20),
		marked: map[string]bool{}, fillSlot: map[int]int{}, states: states.Load(cfg.DataDir),
		trackerSt: map[string]string{}, docs: map[string]docInfo{},
		status: defaultHint,
	}
	return m, nil
}

func (m Model) Init() tea.Cmd { return tea.Batch(m.spin.Tick, m.reload()) }

func (m Model) reload() tea.Cmd {
	ws := m.cfg.Workspace
	return func() tea.Msg {
		jobs, err := workspace.LoadJobs(ws)
		if err != nil {
			return reloadMsg{err: err}
		}
		tracker, _ := workspace.LoadTracker(ws) // a broken tracker only loses the applied marks
		return reloadMsg{jobs: jobs, tracker: tracker}
	}
}

// state is a job's effective state: jobberwocky's own mark, else "done" when
// the tracker already has the application (anything past drafted).
func (m Model) state(j workspace.Job) string {
	if s := m.states.Get(j.Key); s != "" {
		return s
	}
	if st, ok := m.trackerSt[j.Key]; ok && st != "drafted" {
		return states.Done
	}
	return ""
}

// countState counts stored jobs in a state (tracker-applied ones included).
func (m Model) countState(state string) int {
	n := 0
	for _, j := range m.all {
		if m.state(j) == state {
			n++
		}
	}
	return n
}

func (m *Model) rebuild() {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	m.view = m.view[:0]
	for _, src := range [][]workspace.Job{m.pending, m.all} {
		for _, j := range src {
			if !m.inTab(j) {
				continue
			}
			if q != "" && !strings.Contains(strings.ToLower(j.Title+" "+j.Company+" "+j.Location+" "+j.Portal), q) {
				continue
			}
			m.view = append(m.view, j)
		}
	}
	now := time.Now()
	workspace.Sort(m.view, now)
	m.top = m.topPicks(now)
	// top picks lead every list they appear in, in pick order
	sort.SliceStable(m.view, func(a, b int) bool {
		ra, oka := m.top[m.view[a].Key]
		rb, okb := m.top[m.view[b].Key]
		if oka != okb {
			return oka
		}
		return oka && ra < rb
	})
	if m.cursor >= len(m.view) {
		m.cursor = max(0, len(m.view)-1)
	}
}

// topPicks are today's three best active jobs (H band first, then M), ignoring
// the filter so the picks stay put while you search. Done, archived, skipped
// and unrated jobs are never picks.
func (m Model) topPicks(now time.Time) map[string]int {
	var cands []workspace.Job
	for _, j := range m.all {
		if j.Pending || j.Hidden() || m.state(j) != "" {
			continue
		}
		if j.LanguageGate == "FAIL" || j.LocationVerdict == "FAIL" {
			continue // a hard veto is never a pick
		}
		if b := j.Band(); b == "high" || b == "medium" {
			cands = append(cands, j)
		}
	}
	workspace.Sort(cands, now)
	top := map[string]int{}
	for i := 0; i < len(cands) && i < 3; i++ {
		top[cands[i].Key] = i + 1
	}
	return top
}

func (m Model) selected() (workspace.Job, bool) {
	if m.cursor < 0 || m.cursor >= len(m.view) {
		return workspace.Job{}, false
	}
	return m.view[m.cursor], true
}

// targets is what an action applies to: every marked job (in list order), or
// the job under the cursor when nothing is marked. Pending hits are left out:
// they are not stored yet.
func (m Model) targets() []workspace.Job {
	var out []workspace.Job
	if len(m.marked) > 0 {
		for _, j := range m.view {
			if m.marked[j.Key] && !j.Pending {
				out = append(out, j)
			}
		}
		return out
	}
	if j, ok := m.selected(); ok && !j.Pending {
		out = append(out, j)
	}
	return out
}

func (m *Model) clearMarks() { m.marked = map[string]bool{} }

func names(jobs []workspace.Job) string {
	if len(jobs) == 1 {
		return jobs[0].Company
	}
	return fmt.Sprintf("%d jobs", len(jobs))
}

var unsafeKey = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func (m Model) docPath(kind string, j workspace.Job) string {
	return filepath.Join(m.cfg.DataDir, kind, unsafeKey.ReplaceAllString(j.Key, "_")+".md")
}

func (m Model) busy(kind, jobKey string) bool {
	for _, t := range m.tasks {
		if t.Kind == kind && t.JobKey == jobKey && (t.State == queued || t.State == running) {
			return true
		}
	}
	return false
}

func (m *Model) addTask(t *Task) {
	m.tasks = append(m.tasks, t)
	m.byID[t.ID] = t
}

func (m Model) runningCount() int {
	n := 0
	for _, t := range m.tasks {
		if t.State == queued || t.State == running {
			n++
		}
	}
	return n
}

const defaultHint = "s scrape · r rank · f fit · d draft · p fill · c applied · x archive · 1-7 views · space mark · * top 3 · ? help"

func (m *Model) setStatus(format string, a ...any) { m.status = fmt.Sprintf(format, a...) }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.doc.Width, m.doc.Height = msg.Width-2, max(3, msg.Height-4)
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case reloadMsg:
		if msg.err != nil {
			m.setStatus("could not read seen_jobs.json: %v", msg.err)
			return m, nil
		}
		m.all = msg.jobs
		m.trackerSt = map[string]string{}
		for _, j := range m.all {
			if st, ok := workspace.TrackerStatus(j, msg.tracker); ok {
				m.trackerSt[j.Key] = st
			}
		}
		m.refreshDocs()
		m.rebuild()
		return m, nil

	case pendingMsg:
		m.pending = msg.jobs
		m.rebuild()
		return m, nil

	case chainQuickFitMsg:
		t := m.run.newTask("quickfit", fmt.Sprintf("quick fit + store %d new", msg.n), "")
		m.addTask(t)
		m.run.startClaude(t, quickFitPrompt(msg.file), m.cfg.QuickFitModel, "low")
		return m, nil

	case taskStartMsg:
		if t := m.byID[msg.id]; t != nil {
			t.State, t.Started = running, time.Now()
		}
		return m, nil

	case taskLineMsg:
		if t := m.byID[msg.id]; t != nil {
			t.Lines = append(t.Lines, msg.line)
			if len(t.Lines) > 400 {
				t.Lines = t.Lines[len(t.Lines)-400:]
			}
		}
		return m, nil

	case taskDoneMsg:
		return m.finish(msg)

	case recordDoneMsg:
		if t := m.byID[msg.id]; t != nil {
			if msg.err != nil {
				t.Lines = append(t.Lines, "record failed: "+msg.err.Error()+" "+msg.line)
				m.setStatus("✗ %s: drafted but not recorded (t for the log)", t.Label)
			} else {
				t.Lines = append(t.Lines, "recorded: "+msg.line)
			}
		}
		return m, m.reload()

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m.quit()
		}
		switch m.mode {
		case modeInput:
			return m.updateInput(msg)
		case modeDoc:
			return m.updateDoc(msg)
		case modeTasks:
			return m.updateTasks(msg)
		case modeHelp:
			m.mode = modeList
			return m, nil
		}
		if m.filtering {
			return m.updateFilter(msg)
		}
		return m.updateList(msg)
	}
	return m, nil
}

func (m Model) finish(msg taskDoneMsg) (tea.Model, tea.Cmd) {
	t := m.byID[msg.id]
	if t == nil {
		return m, nil
	}
	t.Ended, t.Result, t.Cost, t.Err = time.Now(), msg.result, msg.cost, msg.err
	if t.Started.IsZero() {
		t.Started = t.Ended
	}
	if msg.err != nil {
		t.State = failed
		m.setStatus("✗ %s failed: %v (t for the log)", t.Label, msg.err)
		return m, nil
	}
	t.State = done
	var cmd tea.Cmd
	switch t.Kind {
	case "scrape":
		m.setStatus("✓ scrape: %s", msg.result)
		if strings.HasSuffix(msg.result, " 0 new") {
			m.run.notify("Scrape done", "No new jobs this time")
		}
	case "quickfit":
		m.pending = nil
		cmd = m.reload()
		m.setStatus("✓ %s (r to rank)", lastLine(msg.result))
		m.run.notify("Scrape done", lastLine(msg.result)+": open jobberwocky to rank")
	case "rank":
		cmd = m.reload()
		m.setStatus("✓ ranking done")
		m.run.notify("Ranking done", "Shortlist ready in jobberwocky")
	case "fit":
		if j, ok := m.jobByKey(t.JobKey); ok {
			if strings.TrimSpace(msg.result) == "" {
				t.State = failed
				m.setStatus("✗ fit check for %s came back empty; the saved one (if any) is kept", j.Company)
				break
			}
			if err := os.WriteFile(m.docPath("fit", j), []byte(msg.result), 0o600); err != nil {
				m.setStatus("✗ could not save the fit check: %v", err)
				break
			}
			m.setStatus("✓ fit check saved: %s (v to read, d to draft)", j.Company)
			m.run.notify("Fit check ready", j.Company+": "+j.Title)
		}
	case "draft":
		if j, ok := m.jobByKey(t.JobKey); ok {
			_ = os.WriteFile(m.docPath("drafts", j), []byte(msg.result), 0o600)
			m.setStatus("✓ draft ready: %s (v to read, p to fill the form)", j.Company)
			m.run.notify("Draft ready", j.Company+": "+j.Title)
			cmd = m.record(t, msg.result)
		}
	case "fill":
		delete(m.fillSlot, t.ID)
		m.setStatus("✓ form closed. %s", msg.result)
	}
	if t.Kind == "fit" || t.Kind == "draft" || t.Kind == "fill" {
		m.refreshDocs()
		m.rebuild()
	}
	return m, cmd
}

type recordDoneMsg struct {
	id   int
	line string
	err  error
}

// record runs the draft's tracker/STATUS write, serialised with every other draft's.
func (m Model) record(t *Task, result string) tea.Cmd {
	args, err := extractRecordCommand(result)
	if err != nil {
		t.Lines = append(t.Lines, "not recorded: "+err.Error())
		return nil
	}
	t.Lines = append(t.Lines, "recording (queued behind other drafts)…")
	run := m.run
	return func() tea.Msg {
		line, err := run.runRecord(args)
		return recordDoneMsg{id: t.ID, line: line, err: err}
	}
}

func (m Model) jobByKey(key string) (workspace.Job, bool) {
	for _, src := range [][]workspace.Job{m.all, m.pending} {
		for _, j := range src {
			if j.Key == key {
				return j, true
			}
		}
	}
	return workspace.Job{}, false
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[i+1:])
	}
	return s
}

func (m Model) quit() (tea.Model, tea.Cmd) {
	if m.runningCount() > 0 && !m.quitArmed {
		m.quitArmed = true
		m.setStatus("%d task(s) still running. Press q again to stop them and quit.", m.runningCount())
		return m, nil
	}
	for _, t := range m.tasks {
		if t.cancel != nil {
			t.cancel()
		}
	}
	return m, tea.Quit
}

func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() != "q" {
		m.quitArmed = false
	}
	page := max(1, m.bodyHeight()-1)
	switch msg.String() {
	case "q":
		return m.quit()
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = min(len(m.view)-1, m.cursor+1)
	case "pgup", "ctrl+u":
		m.cursor = max(0, m.cursor-page)
	case "pgdown", "ctrl+d":
		m.cursor = min(len(m.view)-1, m.cursor+page)
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = max(0, len(m.view)-1)
	case "/":
		m.filtering = true
		m.filter.Focus()
		return m, textinput.Blink
	case "1", "2", "3", "4", "5", "6", "7":
		m.tab = listTab(int(msg.String()[0] - '1'))
		m.cursor = 0
		m.clearMarks()
		m.rebuild()
		m.setStatus("%s jobs", tabNames[m.tab])
	case "tab", "shift+tab":
		step := 1
		if msg.String() == "shift+tab" {
			step = len(tabNames) - 1
		}
		m.tab = listTab((int(m.tab) + step) % len(tabNames))
		m.cursor = 0
		m.clearMarks()
		m.rebuild()
		m.setStatus("%s jobs", tabNames[m.tab])
	case "c", "x":
		state, verb := states.Done, "done"
		if msg.String() == "x" {
			state, verb = states.Archived, "archived"
		}
		jobs := m.targets()
		if len(jobs) == 0 {
			break
		}
		keys := make([]string, len(jobs))
		for i, j := range jobs {
			keys[i] = j.Key
		}
		set, err := m.states.Toggle(keys, state)
		if err != nil {
			m.setStatus("could not save: %v", err)
			break
		}
		m.clearMarks()
		m.rebuild()
		if set {
			m.setStatus("%s marked %s (press %s again in the %s tab to undo)", names(jobs), verb, msg.String(), verb)
		} else {
			m.setStatus("%s no longer %s", names(jobs), verb)
		}
	case "R":
		m.setStatus("reloaded")
		return m, m.reload()
	case "?":
		m.mode = modeHelp
	case "t":
		m.mode = modeTasks
		m.taskCursor = max(0, len(m.tasks)-1)
	case "s":
		if m.busy("scrape", "") || m.busy("quickfit", "") {
			m.setStatus("a scrape is already running (t to watch)")
			break
		}
		if len(m.cfg.Searches) == 0 {
			m.setStatus(`no "searches" in %s`, config.Path())
			break
		}
		t := m.run.newTask("scrape", fmt.Sprintf("scrape %d searches", len(m.cfg.Searches)), "")
		m.addTask(t)
		m.run.startScrape(t)
		m.setStatus("scraping in the background. Keep browsing; t to watch.")
	case " ":
		if j, ok := m.selected(); ok && !j.Pending {
			if m.marked[j.Key] {
				delete(m.marked, j.Key)
			} else {
				m.marked[j.Key] = true
			}
			m.cursor = min(len(m.view)-1, m.cursor+1)
			m.setStatus("%d marked · f fit · d draft · r rank · p fill · esc clear", len(m.marked))
		}
	case "*":
		m.clearMarks()
		for k := range m.top {
			m.marked[k] = true
		}
		m.setStatus("★ top %d marked · f fit · d draft · p fill", len(m.top))
	case "ctrl+a":
		for _, j := range m.view {
			if !j.Pending {
				m.marked[j.Key] = true
			}
		}
		m.setStatus("%d marked · f fit · d draft · r rank · p fill · esc clear", len(m.marked))
	case "esc", "u":
		if len(m.marked) > 0 {
			m.clearMarks()
			m.setStatus("marks cleared")
		}
	case "r":
		if m.busy("rank", "") {
			m.setStatus("ranking is already running (one ranker at a time: it is the only writer of seen_jobs.json)")
			break
		}
		if len(m.marked) > 0 {
			jobs := m.targets()
			t := m.run.newTask("rank", fmt.Sprintf("rank %d marked", len(jobs)), "")
			m.addTask(t)
			m.run.startClaude(t, rankSelectedPrompt(jobs), m.cfg.RankModel, "")
			m.clearMarks()
			m.setStatus("ranking %d jobs in parallel inside one /rank run", len(jobs))
			break
		}
		t := m.run.newTask("rank", "rank new jobs", "")
		m.addTask(t)
		m.run.startClaude(t, rankPrompt(), m.cfg.RankModel, "")
		m.setStatus("ranking new jobs in the background (claude -p /rank)")
	case "f":
		jobs := m.targets()
		if len(jobs) == 0 {
			m.setStatus("nothing to check (new hits need the quick-fit step to finish first)")
			break
		}
		started := 0
		for _, j := range jobs {
			if m.busy("fit", j.Key) {
				continue
			}
			t := m.run.newTask("fit", "fit · "+j.Company+" · "+j.Title, j.Key)
			m.addTask(t)
			m.run.startClaude(t, fitPrompt(j, m.trackerSt[j.Key]), m.cfg.FitModel, m.cfg.FitEffort)
			started++
		}
		m.clearMarks()
		m.setStatus("%d fit check(s) started, %d at a time. Keep browsing.", started, m.cfg.MaxParallelClaude)
	case "d":
		started, noFit := 0, 0
		for _, j := range m.targets() {
			if !m.hasDoc("fit", j) {
				noFit++
				continue
			}
			if m.busy("draft", j.Key) {
				continue
			}
			fit, _ := os.ReadFile(m.docPath("fit", j))
			t := m.run.newTask("draft", "draft · "+j.Company+" · "+j.Title, j.Key)
			m.addTask(t)
			m.run.startClaude(t, draftPrompt(j, string(fit)), m.cfg.DraftModel, "")
			started++
		}
		m.clearMarks()
		switch {
		case started == 0 && noFit > 0:
			m.setStatus("fit check first: press f, read it with v, then d")
		case noFit > 0:
			m.setStatus("%d draft(s) started; %d skipped with no fit check yet (press f on them)", started, noFit)
		default:
			m.setStatus("%d draft(s) started, %d at a time", started, m.cfg.MaxParallelClaude)
		}
	case "p":
		m.fillQueue = m.targets()
		m.fillTotal = len(m.fillQueue)
		m.clearMarks()
		if m.fillTotal == 0 {
			break
		}
		m.askNextForm()
		return m, textinput.Blink
	case "o":
		if j, ok := m.selected(); ok && j.URL != "" {
			openURL(j.URL)
			m.setStatus("opened %s", j.URL)
		}
	case "enter", "ctrl+j", "ctrl+m", "v":
		if j, ok := m.selected(); ok {
			m.openJobDoc(j)
		}
	}
	return m, nil
}

func (m *Model) openJobDoc(j workspace.Job) {
	var parts []string
	for _, kind := range []string{"drafts", "fit"} {
		if raw, err := os.ReadFile(m.docPath(kind, j)); err == nil {
			title := map[string]string{"drafts": "Draft result", "fit": "Fit check"}[kind]
			parts = append(parts, "# "+title+"\n\n"+strings.TrimSpace(string(raw)))
		}
	}
	m.docTitle = j.Company + " · " + j.Title
	if len(parts) == 0 {
		m.doc.SetContent(wrap(m.detailText(j, max(40, m.w-4))+"\n\nNo fit check yet. Press esc, then f.", m.doc.Width))
	} else {
		m.doc.SetContent(renderMarkdown(strings.Join(parts, "\n\n---\n\n"), m.doc.Width))
	}
	m.doc.GotoTop()
	m.prevMode, m.mode = modeList, modeDoc
}

func (m Model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filter.SetValue("")
		fallthrough
	case "enter", "ctrl+j", "ctrl+m":
		m.filtering = false
		m.filter.Blur()
		m.rebuild()
		return m, nil
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	m.cursor = 0
	m.rebuild()
	return m, cmd
}

// formURL accepts any https link: tools/fill_form.py handles Google Forms and,
// in general mode, ordinary careers-portal forms.
var formURL = regexp.MustCompile(`^https://[^\s]+$`)

// askNextForm pops the next job off the fill queue and asks for its form link,
// prefilled with the posting URL (often the form itself on careers portals).
func (m *Model) askNextForm() {
	if len(m.fillQueue) == 0 {
		m.input.Blur()
		m.mode = modeList
		return
	}
	j := m.fillQueue[0]
	m.fillQueue = m.fillQueue[1:]
	m.inputJob = j
	n := m.fillTotal - len(m.fillQueue)
	m.input.Prompt = fmt.Sprintf("form for %s (%d/%d) › ", j.Company, n, m.fillTotal)
	m.input.SetValue("")
	if looksLikeForm(j.URL) {
		m.input.SetValue(j.URL)
		m.input.CursorEnd()
	}
	m.input.Placeholder = "paste the APPLICATION FORM link (the page with the fields, not the job ad) · enter fill · tab skip · esc stop"
	m.input.Focus()
	m.prevMode, m.mode = modeList, modeInput
}

// formHosts are application-form sites: a posting URL on one of these is
// usually the form itself, so it is safe to prefill. Job boards (LinkedIn,
// Himalayas, ...) only link onward to the real form.
var formHosts = regexp.MustCompile(`(?i)^https://([a-z0-9-]+\.)*(forms\.gle|docs\.google\.com/forms|greenhouse\.io|lever\.co|ashbyhq\.com|workable\.com|smartrecruiters\.com|myworkdayjobs\.com|recruitee\.com|bamboohr\.com|jobvite\.com|teamtailor\.com|keka\.com|zohorecruit\.)`)

func looksLikeForm(url string) bool { return formHosts.MatchString(url) }

// freeSlot is the lowest Chrome profile slot no running fill is using, so
// parallel fills never fight over one profile's lock.
func (m Model) freeSlot() int {
	used := map[int]bool{}
	for id, s := range m.fillSlot {
		if t := m.byID[id]; t != nil && (t.State == queued || t.State == running) {
			used[s] = true
		}
	}
	for s := 1; ; s++ {
		if !used[s] {
			return s
		}
	}
}

func (m Model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.fillQueue = nil
		m.input.Blur()
		m.mode = modeList
		return m, nil
	case "tab":
		m.askNextForm()
		return m, nil
	case "enter", "ctrl+j", "ctrl+m":
		url := strings.TrimSpace(m.input.Value())
		if !formURL.MatchString(url) {
			m.setStatus("that is not an https link (tab skips this job)")
			return m, nil
		}
		j := m.inputJob
		t := m.run.newTask("fill", "fill form · "+j.Company, j.Key)
		m.addTask(t)
		slot := m.freeSlot()
		m.fillSlot[t.ID] = slot
		profile := filepath.Join(m.cfg.DataDir, "chrome", fmt.Sprintf("slot-%d", slot))
		report := strings.TrimSuffix(m.docPath("forms", j), ".md") + ".json"
		m.run.startExec(t, m.cfg.FormPython, "tools/fill_form.py", url, "--report", report, "--profile", profile)
		m.setStatus("filling %s in its own Chrome window. It never submits: check it and press Submit yourself.", j.Company)
		m.askNextForm()
		return m, textinput.Blink
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) updateDoc(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "v", "enter", "ctrl+j", "ctrl+m":
		m.mode = m.prevMode
		return m, nil
	}
	var cmd tea.Cmd
	m.doc, cmd = m.doc.Update(msg)
	return m, cmd
}

func (m Model) updateTasks(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "t", "q":
		m.mode = modeList
	case "up", "k":
		m.taskCursor = max(0, m.taskCursor-1)
	case "down", "j":
		m.taskCursor = min(len(m.tasks)-1, m.taskCursor+1)
	case "x":
		if m.taskCursor < len(m.tasks) {
			t := m.tasks[m.taskCursor]
			if t.cancel != nil && (t.State == queued || t.State == running) {
				t.cancel()
				m.setStatus("cancelling %s", t.Label)
			}
		}
	case "enter", "ctrl+j", "ctrl+m":
		if m.taskCursor < len(m.tasks) {
			t := m.tasks[m.taskCursor]
			var b strings.Builder
			fmt.Fprintf(&b, "%s\nlog: %s\n\n", t.Label, t.LogPath)
			for _, l := range t.Lines {
				b.WriteString(l + "\n")
			}
			if t.Err != nil {
				b.WriteString("\n" + styleErr.Render("ERROR: "+t.Err.Error()) + "\n")
			}
			content := wrap(b.String(), m.doc.Width)
			if t.Result != "" {
				content += "\n" + renderMarkdown("# Result\n\n"+t.Result, m.doc.Width)
			}
			m.docTitle = t.Label
			m.doc.SetContent(content)
			m.doc.GotoBottom()
			m.prevMode, m.mode = modeTasks, modeDoc
		}
	}
	return m, nil
}

func openURL(u string) {
	cmd := "xdg-open"
	if runtime.GOOS == "darwin" {
		cmd = "open"
	}
	_ = exec.Command(cmd, u).Start()
}
