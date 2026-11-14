// Package claude runs Claude Code headless (`claude -p`) in the workspace and
// streams a one-line summary of each step it takes.
package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Paul-Joshy/jobberwocky/internal/config"
)

// Result is the final answer of a headless run.
type Result struct {
	Text    string
	CostUSD float64
	Err     error
}

type streamLine struct {
	Type    string `json:"type"`
	Message struct {
		Content []struct {
			Type  string         `json:"type"`
			Text  string         `json:"text"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		} `json:"content"`
	} `json:"message"`
	Result  string  `json:"result"`
	IsError bool    `json:"is_error"`
	Cost    float64 `json:"total_cost_usd"`
}

// Args are the claude flags for one headless run. The prompt itself goes on
// stdin (`claude -p < prompt`), so it never needs shell quoting.
func Args(cfg *config.Config, model, effort string) []string {
	args := []string{"-p", "--output-format", "stream-json", "--verbose"}
	if model != "" {
		args = append(args, "--model", model)
	}
	if effort != "" {
		args = append(args, "--effort", effort)
	}
	return append(args, cfg.ClaudeArgs...)
}

// Parser turns stream-json lines (live or from a log file) into progress
// steps and a final result.
type Parser struct {
	res Result
	got bool
}

// Line parses one stream line and returns its progress steps, if any.
func (p *Parser) Line(line []byte) []string {
	var ev streamLine
	if json.Unmarshal(line, &ev) != nil {
		return nil
	}
	var steps []string
	switch ev.Type {
	case "assistant":
		for _, c := range ev.Message.Content {
			switch c.Type {
			case "text":
				if t := firstLine(c.Text); t != "" {
					steps = append(steps, t)
				}
			case "tool_use":
				steps = append(steps, "→ "+c.Name+" "+summarize(c.Input))
			}
		}
	case "result":
		p.got = true
		// a run can emit more than one result event; never let a later empty one erase the answer
		if strings.TrimSpace(ev.Result) != "" {
			p.res.Text = ev.Result
		}
		p.res.CostUSD += ev.Cost
		if ev.IsError {
			p.res.Err = fmt.Errorf("claude reported an error: %s", firstLine(ev.Result))
		}
	}
	return steps
}

// Result is the run's outcome once the stream has ended. exitErr is the
// process's exit status, used only when no result arrived.
func (p *Parser) Result(exitErr error) Result {
	res := p.res
	if !p.got && res.Err == nil {
		if exitErr == nil {
			exitErr = fmt.Errorf("claude exited without a result")
		}
		res.Err = exitErr
	}
	return res
}

// Run executes one prompt in-process (used by tests; the UI runs prompts in tmux).
func Run(ctx context.Context, cfg *config.Config, prompt, model, effort string, onStep func(string), log io.Writer) Result {
	cmd := exec.CommandContext(ctx, cfg.Claude, Args(cfg, model, effort)...)
	cmd.Dir = cfg.Workspace
	cmd.Env = Env(cfg)
	cmd.Stdin = strings.NewReader(prompt)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{Err: err}
	}
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		return Result{Err: fmt.Errorf("could not start %s: %w", cfg.Claude, err)}
	}
	var p Parser
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		log.Write(sc.Bytes())
		log.Write([]byte("\n"))
		for _, s := range p.Line(sc.Bytes()) {
			onStep(s)
		}
	}
	return p.Result(cmd.Wait())
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

func summarize(in map[string]any) string {
	for _, k := range []string{"command", "file_path", "url", "query", "description", "pattern", "skill"} {
		if v, ok := in[k].(string); ok && v != "" {
			return firstLine(v)
		}
	}
	return ""
}

// Env puts the configured bun and python first on PATH, so `bun run ...` and
// `python3 tools/...` resolve inside headless runs and match the allow rules.
// Without this, runs hunt for bun and get refused on absolute paths, which
// burns turns (and tokens).
func Env(cfg *config.Config) []string {
	var dirs []string
	for _, bin := range []string{cfg.Bun, cfg.Python} {
		if filepath.IsAbs(bin) {
			dirs = append(dirs, filepath.Dir(bin))
		}
	}
	env := os.Environ()
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + strings.Join(append(dirs, strings.TrimPrefix(kv, "PATH=")), string(os.PathListSeparator))
			return env
		}
	}
	return append(env, "PATH="+strings.Join(dirs, string(os.PathListSeparator)))
}
