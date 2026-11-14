// Package run starts long commands in detached tmux sessions, so they keep
// going when the UI quits, and follows their output through files.
//
// Each run lives in its own folder under <DataDir>/runs/<id>/:
//
//	meta.json   what the run is (kind, label, job), written before it starts
//	cmd.sh      the script tmux runs
//	stdin       optional input (the claude prompt)
//	out.log     the command's stdout
//	err.log     the command's stderr
//	exit        the exit code, written when the command ends
//	collected   written by the UI once it has processed the result
package run

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Meta describes a run so a restarted UI can pick it up again.
type Meta struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Label   string    `json:"label"`
	JobKey  string    `json:"job_key,omitempty"`
	Exec    bool      `json:"exec,omitempty"` // a plain command, not a claude stream
	Slot    int       `json:"slot,omitempty"` // form filler's Chrome profile slot
	Started time.Time `json:"started"`
}

// Run is one run folder.
type Run struct {
	Dir  string
	Meta Meta
}

// Available reports whether tmux is installed.
func Available() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

func (r Run) path(name string) string { return filepath.Join(r.Dir, name) }

// Out is the command's stdout log.
func (r Run) Out() string { return r.path("out.log") }

// Session is the tmux session name.
func (r Run) Session() string { return "jw-" + r.Meta.ID }

// New creates the run folder and writes its meta. It does not start anything.
func New(root string, meta Meta) (Run, error) {
	r := Run{Dir: filepath.Join(root, meta.ID), Meta: meta}
	if err := os.MkdirAll(r.Dir, 0o700); err != nil {
		return r, err
	}
	b, _ := json.MarshalIndent(meta, "", "  ")
	return r, os.WriteFile(r.path("meta.json"), b, 0o600)
}

// Start runs argv in dir inside a detached tmux session. stdin may be empty.
func (r Run) Start(dir string, env map[string]string, stdin string, argv []string) error {
	in := "/dev/null"
	if stdin != "" {
		if err := os.WriteFile(r.path("stdin"), []byte(stdin), 0o600); err != nil {
			return err
		}
		in = r.path("stdin")
	}
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	fmt.Fprintf(&b, "cd %s || { echo 127 > %s; exit 1; }\n", quote(dir), quote(r.path("exit")))
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "export %s=%s\n", k, quote(env[k]))
	}
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = quote(a)
	}
	fmt.Fprintf(&b, "%s < %s > %s 2> %s\n", strings.Join(q, " "), quote(in), quote(r.Out()), quote(r.path("err.log")))
	fmt.Fprintf(&b, "echo $? > %s.tmp && mv %s.tmp %s\n", quote(r.path("exit")), quote(r.path("exit")), quote(r.path("exit")))
	if err := os.WriteFile(r.path("cmd.sh"), []byte(b.String()), 0o700); err != nil {
		return err
	}
	out, err := exec.Command("tmux", "new-session", "-d", "-s", r.Session(), "/bin/sh", r.path("cmd.sh")).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Alive reports whether the run's tmux session still exists.
func (r Run) Alive() bool {
	return exec.Command("tmux", "has-session", "-t", "="+r.Session()).Run() == nil
}

// Kill ends the run's tmux session.
func (r Run) Kill() { _ = exec.Command("tmux", "kill-session", "-t", "="+r.Session()).Run() }

// ExitCode returns the command's exit code once it has ended.
func (r Run) ExitCode() (int, bool) {
	b, err := os.ReadFile(r.path("exit"))
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 1, true
	}
	return n, true
}

// Collected reports whether the UI already processed this run's result.
func (r Run) Collected() bool {
	_, err := os.Stat(r.path("collected"))
	return err == nil
}

// MarkCollected records that the result was processed, so a restart skips it.
func (r Run) MarkCollected() { _ = os.WriteFile(r.path("collected"), nil, 0o600) }

// Stderr is the tail of the command's stderr, for error messages.
func (r Run) Stderr() string {
	b, _ := os.ReadFile(r.path("err.log"))
	s := strings.TrimSpace(string(b))
	if len(s) > 400 {
		s = "…" + s[len(s)-400:]
	}
	return s
}

// ErrLost means the session vanished without writing an exit code
// (tmux server killed, machine restarted).
var ErrLost = errors.New("the run's tmux session ended without finishing (tmux killed or the Mac restarted)")

// Follow streams out.log line by line until the command ends, then returns
// its exit error. Cancelling ctx kills the session.
func (r Run) Follow(ctx context.Context, onLine func([]byte)) error {
	var f *os.File
	var rd *bufio.Reader
	var partial []byte
	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	defer func() {
		if f != nil {
			f.Close()
		}
	}()
	for {
		if f == nil {
			if ff, err := os.Open(r.Out()); err == nil {
				f, rd = ff, bufio.NewReaderSize(ff, 64*1024)
			}
		}
		code, ended := r.ExitCode()
		if f != nil {
			for {
				chunk, err := rd.ReadBytes('\n')
				partial = append(partial, chunk...)
				if err == io.EOF {
					break
				}
				if err != nil {
					return err
				}
				onLine(trimNL(partial))
				partial = partial[:0]
			}
		}
		if ended {
			if len(partial) > 0 {
				onLine(trimNL(partial))
			}
			if code != 0 {
				if s := r.Stderr(); s != "" {
					return fmt.Errorf("exit status %d: %s", code, s)
				}
				return fmt.Errorf("exit status %d", code)
			}
			return nil
		}
		if !r.Alive() {
			// the exit file may land a moment after the session ends
			time.Sleep(500 * time.Millisecond)
			if _, ok := r.ExitCode(); !ok {
				return ErrLost
			}
			continue
		}
		select {
		case <-ctx.Done():
			r.Kill()
			return ctx.Err()
		case <-tick.C:
		}
	}
}

// Pending lists runs the UI has not collected yet, oldest first.
func Pending(root string) []Run {
	entries, _ := os.ReadDir(root)
	var out []Run
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		r := Run{Dir: filepath.Join(root, e.Name())}
		b, err := os.ReadFile(r.path("meta.json"))
		if err != nil || json.Unmarshal(b, &r.Meta) != nil || r.Meta.ID == "" || r.Collected() {
			continue
		}
		if _, err := os.Stat(r.path("cmd.sh")); err != nil {
			r.MarkCollected() // never started (UI quit while it was queued)
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Meta.Started.Before(out[j].Meta.Started) })
	return out
}

func trimNL(b []byte) []byte {
	return []byte(strings.TrimRight(string(b), "\r\n"))
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
