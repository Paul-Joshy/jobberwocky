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
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Paul-Joshy/jobberwocky/internal/config"
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
	showAll bool

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
	jobs []workspace.Job
	err  error
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
		status: "s scrape · r rank · f fit check · d draft · p fill form · ? help",
	}
	return m, nil
}

func (m Model) Init() tea.Cmd { return tea.Batch(m.spin.Tick, m.reload()) }

func (m Model) reload() tea.Cmd {
	ws := m.cfg.Workspace
	return func() tea.Msg {
		jobs, err := workspace.LoadJobs(ws)
		return reloadMsg{jobs: jobs, err: err}
	}
}

func (m *Model) rebuild() {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	m.view = m.view[:0]
	for _, src := range [][]workspace.Job{m.pending, m.all} {
		for _, j := range src {
			if !m.showAll && j.Hidden() {
				continue
			}
			if q != "" && !strings.Contains(strings.ToLower(j.Title+" "+j.Company+" "+j.Location+" "+j.Portal), q) {
				continue
			}
			m.view = append(m.view, j)
		}
	}
	workspace.Sort(m.view, time.Now())
	if m.cursor >= len(m.view) {
		m.cursor = max(0, len(m.view)-1)
	}
}

func (m Model) selected() (workspace.Job, bool) {
	if m.cursor < 0 || m.cursor >= len(m.view) {
		return workspace.Job{}, false
	}
	return m.view[m.cursor], true
}

var unsafeKey = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func (m Model) docPath(kind string, j workspace.Job) string {
	return filepath.Join(m.cfg.DataDir, kind, unsafeKey.ReplaceAllString(j.Key, "_")+".md")
}

func (m Model) hasDoc(kind string, j workspace.Job) bool {
	_, err := os.Stat(m.docPath(kind, j))
	return err == nil
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
		m.rebuild()
		return m, nil

	case pendingMsg:
		m.pending = msg.jobs
		m.rebuild()
		return m, nil

	case chainQuickFitMsg:
		t := m.run.newTask("quickfit", fmt.Sprintf("quick fit + store %d new", msg.n), "")
		m.addTask(t)
		m.run.startClaude(t, quickFitPrompt(msg.file), m.cfg.QuickFitModel)
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
			_ = os.WriteFile(m.docPath("fit", j), []byte(msg.result), 0o600)
			m.setStatus("✓ fit check ready: %s (v to read, d to draft)", j.Company)
			m.run.notify("Fit check ready", j.Company+": "+j.Title)
		}
	case "draft":
		if j, ok := m.jobByKey(t.JobKey); ok {
			_ = os.WriteFile(m.docPath("drafts", j), []byte(msg.result), 0o600)
			m.setStatus("✓ draft ready: %s (v to read, p to fill the form)", j.Company)
			m.run.notify("Draft ready", j.Company+": "+j.Title)
		}
		cmd = m.reload()
	case "fill":
		m.setStatus("✓ form closed. %s", msg.result)
	}
	return m, cmd
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
	case "a":
		m.showAll = !m.showAll
		m.rebuild()
		m.setStatus("showing %s jobs", map[bool]string{true: "all", false: "active"}[m.showAll])
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
	case "r":
		if m.busy("rank", "") {
			m.setStatus("ranking is already running")
			break
		}
		t := m.run.newTask("rank", "rank new jobs", "")
		m.addTask(t)
		m.run.startClaude(t, rankPrompt(), "")
		m.setStatus("ranking in the background (claude -p /rank)")
	case "f":
		j, ok := m.selected()
		if !ok {
			break
		}
		if j.Pending {
			m.setStatus("wait for the quick-fit step to store this job, then press f")
			break
		}
		if m.busy("fit", j.Key) {
			m.setStatus("fit check already running for %s", j.Company)
			break
		}
		t := m.run.newTask("fit", "fit · "+j.Company+" · "+j.Title, j.Key)
		m.addTask(t)
		m.run.startClaude(t, fitPrompt(j), "")
		m.setStatus("fit check started for %s. Pick another job meanwhile.", j.Company)
	case "d":
		j, ok := m.selected()
		if !ok {
			break
		}
		if !m.hasDoc("fit", j) {
			m.setStatus("fit check first: press f, read it with v, then d")
			break
		}
		if m.busy("draft", j.Key) {
			m.setStatus("draft already running for %s", j.Company)
			break
		}
		fit, _ := os.ReadFile(m.docPath("fit", j))
		t := m.run.newTask("draft", "draft · "+j.Company+" · "+j.Title, j.Key)
		m.addTask(t)
		m.run.startClaude(t, draftPrompt(j, string(fit)), "")
		m.setStatus("drafting CV + cover letter for %s in the background", j.Company)
	case "p":
		j, ok := m.selected()
		if !ok {
			break
		}
		m.inputJob = j
		m.input.SetValue("")
		m.input.Placeholder = "paste the Google Form link (docs.google.com/forms or forms.gle)"
		m.input.Focus()
		m.prevMode, m.mode = modeList, modeInput
		return m, textinput.Blink
	case "o":
		if j, ok := m.selected(); ok && j.URL != "" {
			openURL(j.URL)
			m.setStatus("opened %s", j.URL)
		}
	case "enter", "v":
		if j, ok := m.selected(); ok {
			m.openJobDoc(j)
		}
	}
	return m, nil
}

func (m *Model) openJobDoc(j workspace.Job) {
	var b strings.Builder
	for _, kind := range []string{"drafts", "fit"} {
		if raw, err := os.ReadFile(m.docPath(kind, j)); err == nil {
			title := map[string]string{"drafts": "DRAFT RESULT", "fit": "FIT CHECK"}[kind]
			fmt.Fprintf(&b, "── %s ──\n\n%s\n\n", title, strings.TrimSpace(string(raw)))
		}
	}
	if b.Len() == 0 {
		b.WriteString(m.detailText(j, max(40, m.w-4)))
		b.WriteString("\n\nNo fit check yet. Press esc, then f.")
	}
	m.docTitle = j.Company + " · " + j.Title
	m.doc.SetContent(wrap(b.String(), m.doc.Width))
	m.doc.GotoTop()
	m.prevMode, m.mode = modeList, modeDoc
}

func (m Model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filter.SetValue("")
		fallthrough
	case "enter":
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

var formURL = regexp.MustCompile(`^https://(docs\.google\.com/forms/|forms\.gle/)`)

func (m Model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.input.Blur()
		m.mode = m.prevMode
		return m, nil
	case "enter":
		url := strings.TrimSpace(m.input.Value())
		if !formURL.MatchString(url) {
			m.setStatus("only Google Forms links work for now (docs.google.com/forms or forms.gle)")
			return m, nil
		}
		m.input.Blur()
		m.mode = m.prevMode
		j := m.inputJob
		t := m.run.newTask("fill", "fill form · "+j.Company, j.Key)
		m.addTask(t)
		report := m.docPath("forms", j)
		report = strings.TrimSuffix(report, ".md") + ".json"
		m.run.startExec(t, m.cfg.FormPython, "tools/fill_form.py", url, "--report", report)
		m.setStatus("filling the form in Chrome. It never submits: check it and press Submit yourself.")
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) updateDoc(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "v", "enter":
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
	case "enter":
		if m.taskCursor < len(m.tasks) {
			t := m.tasks[m.taskCursor]
			var b strings.Builder
			fmt.Fprintf(&b, "%s\nlog: %s\n\n", t.Label, t.LogPath)
			for _, l := range t.Lines {
				b.WriteString(l + "\n")
			}
			if t.Result != "" {
				b.WriteString("\n── RESULT ──\n\n" + t.Result + "\n")
			}
			if t.Err != nil {
				b.WriteString("\n── ERROR ──\n\n" + t.Err.Error() + "\n")
			}
			m.docTitle = t.Label
			m.doc.SetContent(wrap(b.String(), m.doc.Width))
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
