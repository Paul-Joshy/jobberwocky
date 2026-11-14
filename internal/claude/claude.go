// Package claude runs Claude Code headless (`claude -p`) in the workspace and
// streams a one-line summary of each step it takes.
package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
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

// Run executes one prompt. onStep receives short progress lines; the raw
// stream is copied to log.
func Run(ctx context.Context, cfg *config.Config, prompt, model string, onStep func(string), log io.Writer) Result {
	args := []string{"-p", prompt, "--output-format", "stream-json", "--verbose"}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, cfg.ClaudeArgs...)
	cmd := exec.CommandContext(ctx, cfg.Claude, args...)
	cmd.Dir = cfg.Workspace
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{Err: err}
	}
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		return Result{Err: fmt.Errorf("could not start %s: %w", cfg.Claude, err)}
	}
	var res Result
	gotResult := false
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		log.Write(line)
		log.Write([]byte("\n"))
		var ev streamLine
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		switch ev.Type {
		case "assistant":
			for _, c := range ev.Message.Content {
				switch c.Type {
				case "text":
					if t := firstLine(c.Text); t != "" {
						onStep(t)
					}
				case "tool_use":
					onStep("→ " + c.Name + " " + summarize(c.Input))
				}
			}
		case "result":
			gotResult = true
			res.Text, res.CostUSD = ev.Result, ev.Cost
			if ev.IsError {
				res.Err = fmt.Errorf("claude reported an error: %s", firstLine(ev.Result))
			}
		}
	}
	waitErr := cmd.Wait()
	if !gotResult && res.Err == nil {
		if waitErr == nil {
			waitErr = fmt.Errorf("claude exited without a result")
		}
		res.Err = waitErr
	}
	return res
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
