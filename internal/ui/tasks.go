package ui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Paul-Joshy/jobberwocky/internal/claude"
	"github.com/Paul-Joshy/jobberwocky/internal/config"
	"github.com/Paul-Joshy/jobberwocky/internal/run"
	"github.com/Paul-Joshy/jobberwocky/internal/scrape"
	"github.com/Paul-Joshy/jobberwocky/internal/workspace"
)

var program *tea.Program

// SetProgram lets background tasks send messages to the UI.
func SetProgram(p *tea.Program) { program = p }

func send(msg tea.Msg) {
	if program != nil {
		program.Send(msg)
	}
}

type taskState int

const (
	queued taskState = iota
	running
	done
	failed
)

// Task is one background job: a scrape, a rank, a fit check, a draft or a form fill.
type Task struct {
	ID      int
	Kind    string
	Label   string
	JobKey  string
	State   taskState
	Started time.Time
	Ended   time.Time
	Lines   []string
	Result  string
	Cost    float64
	Err     error
	LogPath string
	cancel  context.CancelFunc
	run     *run.Run // set when the task runs in tmux and survives a UI restart
}

func (t *Task) Last() string {
	if len(t.Lines) == 0 {
		return ""
	}
	return t.Lines[len(t.Lines)-1]
}

func (t *Task) Elapsed() time.Duration {
	end := t.Ended
	if end.IsZero() {
		end = time.Now()
	}
	if t.Started.IsZero() {
		return 0
	}
	return end.Sub(t.Started).Round(time.Second)
}

type taskStartMsg struct{ id int }
type taskLineMsg struct {
	id   int
	line string
}
type taskDoneMsg struct {
	id     int
	result string
	cost   float64
	err    error
}
type pendingMsg struct{ jobs []workspace.Job }
type chainQuickFitMsg struct {
	file string
	n    int
}

// runner owns task bookkeeping shared by goroutines.
type runner struct {
	cfg       *config.Config
	claudeSem chan struct{}
	mu        sync.Mutex
	nextID    int
	nextRun   int
}

func newRunner(cfg *config.Config) *runner {
	return &runner{cfg: cfg, claudeSem: make(chan struct{}, cfg.MaxParallelClaude)}
}

func (r *runner) newTask(kind, label, jobKey string) *Task {
	r.mu.Lock()
	r.nextID++
	id := r.nextID
	r.mu.Unlock()
	stamp := time.Now().Format("20060102-150405")
	return &Task{
		ID: id, Kind: kind, Label: label, JobKey: jobKey,
		LogPath: filepath.Join(r.cfg.DataDir, "logs", fmt.Sprintf("%s-%d-%s.log", stamp, id, kind)),
	}
}

func openLog(path string) io.WriteCloser {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nopCloser{io.Discard}
	}
	return f
}

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

// startClaude runs a prompt in the background, queued behind the parallel
// limit. With tmux installed it runs in a detached session, so it keeps going
// if the UI quits; otherwise it runs in-process.
func (r *runner) startClaude(t *Task, prompt, model, effort string) {
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	go func() {
		select {
		case r.claudeSem <- struct{}{}:
		case <-ctx.Done():
			send(taskDoneMsg{id: t.ID, err: ctx.Err()})
			return
		}
		defer func() { <-r.claudeSem }()
		if run.Available() {
			argv := append([]string{r.cfg.Claude}, claude.Args(r.cfg, model, effort)...)
			rn, err := r.startRun(t, "", false, 0, prompt, argv)
			if err != nil {
				send(taskDoneMsg{id: t.ID, err: err})
				return
			}
			r.follow(ctx, t.ID, rn)
			return
		}
		send(taskStartMsg{id: t.ID})
		log := openLog(t.LogPath)
		defer log.Close()
		fmt.Fprintf(log, "PROMPT\n%s\n\nSTREAM\n", prompt)
		res := claude.Run(ctx, r.cfg, prompt, model, effort, func(s string) { send(taskLineMsg{id: t.ID, line: s}) }, log)
		send(taskDoneMsg{id: t.ID, result: res.Text, cost: res.CostUSD, err: res.Err})
	}()
}

// startRun writes the run folder and starts it in tmux. The task's run and
// log path are set by the UI on taskStartMsg (tasks are owned by the UI loop).
func (r *runner) startRun(t *Task, cwd string, isExec bool, slot int, stdin string, argv []string) (*run.Run, error) {
	r.mu.Lock()
	r.nextRun++
	n := r.nextRun
	r.mu.Unlock()
	id := fmt.Sprintf("%s-%d-%d-%s", time.Now().Format("20060102-150405"), os.Getpid(), n, t.Kind)
	rn, err := run.New(filepath.Join(r.cfg.DataDir, "runs"), run.Meta{
		ID: id, Kind: t.Kind, Label: t.Label, JobKey: t.JobKey, Exec: isExec, Slot: slot, Started: time.Now(),
	})
	if err != nil {
		return nil, err
	}
	if cwd == "" {
		cwd = r.cfg.Workspace
	}
	if err := rn.Start(cwd, runEnv(r.cfg), stdin, argv); err != nil {
		return nil, err
	}
	return &rn, nil
}

// runEnv is what a tmux run needs on top of the tmux server's environment:
// the PATH with the configured bun and python first.
func runEnv(cfg *config.Config) map[string]string {
	env := map[string]string{}
	for _, kv := range claude.Env(cfg) {
		if strings.HasPrefix(kv, "PATH=") {
			env["PATH"] = strings.TrimPrefix(kv, "PATH=")
		}
	}
	return env
}

type taskRunMsg struct {
	id  int
	run *run.Run
}

// follow streams a tmux run's output to the UI until it ends. It is used for
// new runs and for runs picked up again after a restart.
func (r *runner) follow(ctx context.Context, id int, rn *run.Run) {
	send(taskRunMsg{id: id, run: rn})
	send(taskStartMsg{id: id})
	if rn.Meta.Exec {
		var summary string
		err := rn.Follow(ctx, func(b []byte) {
			if line := string(b); isFillSummary(line) {
				summary = line
				send(taskLineMsg{id: id, line: line})
			}
		})
		send(taskDoneMsg{id: id, result: summary, err: err})
		return
	}
	var p claude.Parser
	err := rn.Follow(ctx, func(b []byte) {
		for _, s := range p.Line(b) {
			send(taskLineMsg{id: id, line: s})
		}
	})
	if errors.Is(err, context.Canceled) || errors.Is(err, run.ErrLost) {
		send(taskDoneMsg{id: id, err: err})
		return
	}
	res := p.Result(err)
	send(taskDoneMsg{id: id, result: res.Text, cost: res.CostUSD, err: res.Err})
}

// resume follows a run left over from an earlier session of the UI.
func (r *runner) resume(t *Task, rn *run.Run) {
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	go r.follow(ctx, t.ID, rn)
}

func isFillSummary(line string) bool {
	return strings.HasPrefix(line, "FILLED ") || strings.HasPrefix(line, "READY_FOR_REVIEW") || strings.HasPrefix(line, "error")
}

// startScrape runs the portal CLIs and dedupes; it hands new hits to the quick-fit step.
func (r *runner) startScrape(t *Task) {
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	go func() {
		send(taskStartMsg{id: t.ID})
		log := openLog(t.LogPath)
		defer log.Close()
		hits := scrape.Run(ctx, r.cfg, func(p scrape.PortalResult) {
			fmt.Fprintln(log, p.String())
			send(taskLineMsg{id: t.ID, line: p.String()})
		})
		send(taskLineMsg{id: t.ID, line: fmt.Sprintf("%d hits; deduping against seen jobs and the tracker", len(hits))})
		sel, file, err := scrape.Candidates(ctx, r.cfg, hits)
		if err != nil {
			send(taskDoneMsg{id: t.ID, err: err})
			return
		}
		pending := make([]workspace.Job, 0, len(sel))
		for _, h := range sel {
			pending = append(pending, workspace.FromHit(h))
		}
		send(pendingMsg{jobs: pending})
		summary := fmt.Sprintf("%d hits, %d new", len(hits), len(sel))
		fmt.Fprintln(log, summary)
		send(taskDoneMsg{id: t.ID, result: summary})
		if len(sel) > 0 {
			send(chainQuickFitMsg{file: file, n: len(sel)})
		}
	}()
}

// startExec runs a plain command (the form filler) and streams its output.
// With tmux it runs in a detached session, so the form window stays open
// even if the UI quits.
func (r *runner) startExec(t *Task, slot int, name string, args ...string) {
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	go func() {
		if run.Available() {
			rn, err := r.startRun(t, "", true, slot, "", append([]string{name}, args...))
			if err != nil {
				send(taskDoneMsg{id: t.ID, err: err})
				return
			}
			r.follow(ctx, t.ID, rn)
			return
		}
		send(taskStartMsg{id: t.ID})
		log := openLog(t.LogPath)
		defer log.Close()
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir = r.cfg.Workspace
		out, err := cmd.StdoutPipe()
		if err != nil {
			send(taskDoneMsg{id: t.ID, err: err})
			return
		}
		cmd.Stderr = log
		if err := cmd.Start(); err != nil {
			send(taskDoneMsg{id: t.ID, err: err})
			return
		}
		var summary string
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for sc.Scan() {
			line := sc.Text()
			fmt.Fprintln(log, line)
			if isFillSummary(line) {
				summary = line
				send(taskLineMsg{id: t.ID, line: line})
			}
		}
		err = cmd.Wait()
		send(taskDoneMsg{id: t.ID, result: summary, err: err})
	}()
}

// notify pings the phone through the workspace's tools/notify.py, if set up.
// The script screens out contact details and salaries itself.
// Every ping also shows as a macOS notification, which needs no setup.
func (r *runner) notify(title, msg string) {
	if !r.cfg.Notify {
		return
	}
	go desktopNotify(title, msg)
	script := filepath.Join(r.cfg.Workspace, "tools", "notify.py")
	if _, err := os.Stat(script); err != nil {
		return
	}
	go func() {
		cmd := exec.Command(r.cfg.Python, "tools/notify.py", title, msg)
		cmd.Dir = r.cfg.Workspace
		_ = cmd.Run()
	}()
}

// desktopNotify shows a macOS notification. The text goes in as arguments,
// never spliced into the script, so nothing in it can be run.
func desktopNotify(title, msg string) {
	if _, err := exec.LookPath("osascript"); err != nil {
		return
	}
	_ = exec.Command("osascript",
		"-e", "on run argv",
		"-e", `display notification (item 2 of argv) with title "jobberwocky" subtitle (item 1 of argv) sound name "Glass"`,
		"-e", "end run", title, msg).Run()
}
