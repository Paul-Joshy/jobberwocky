package ui

import (
	"bufio"
	"context"
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

// startClaude runs a prompt in the background, queued behind the parallel limit.
func (r *runner) startClaude(t *Task, prompt, model string) {
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
		send(taskStartMsg{id: t.ID})
		log := openLog(t.LogPath)
		defer log.Close()
		fmt.Fprintf(log, "PROMPT\n%s\n\nSTREAM\n", prompt)
		res := claude.Run(ctx, r.cfg, prompt, model, func(s string) { send(taskLineMsg{id: t.ID, line: s}) }, log)
		send(taskDoneMsg{id: t.ID, result: res.Text, cost: res.CostUSD, err: res.Err})
	}()
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
func (r *runner) startExec(t *Task, name string, args ...string) {
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	go func() {
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
			if strings.HasPrefix(line, "FILLED ") || strings.HasPrefix(line, "READY_FOR_REVIEW") || strings.HasPrefix(line, "error") {
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
func (r *runner) notify(title, msg string) {
	if !r.cfg.Notify {
		return
	}
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
