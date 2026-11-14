// Package scrape runs the workspace's portal CLIs in parallel and dedupes the
// hits through the workspace's own tools/scrape_state.py. No LLM involved.
package scrape

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Paul-Joshy/jobberwocky/internal/config"
)

// PortalResult reports one search call.
type PortalResult struct {
	Search  config.Search
	Count   int
	Skipped bool // portal disabled in its SKILL.md
	Err     error
}

func (r PortalResult) String() string {
	label := r.Search.Portal + " " + strings.Join(r.Search.Args, " ")
	switch {
	case r.Skipped:
		return "skip  " + r.Search.Portal + " (enabled: false)"
	case r.Err != nil:
		return "fail  " + label + ": " + r.Err.Error()
	default:
		return fmt.Sprintf("ok    %s → %d", label, r.Count)
	}
}

// Disabled reports whether a portal's SKILL.md frontmatter sets enabled: false.
func Disabled(ws, portal string) bool {
	f, err := os.Open(filepath.Join(ws, ".agents", "skills", portal, "SKILL.md"))
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	dashes := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "---" {
			if dashes++; dashes == 2 {
				return false
			}
			continue
		}
		if strings.HasPrefix(line, "enabled:") {
			v := strings.TrimSpace(strings.SplitN(strings.TrimPrefix(line, "enabled:"), "#", 2)[0])
			return v == "false"
		}
	}
	return false
}

// Run executes every configured search, at most MaxParallelPortals at a time.
func Run(ctx context.Context, cfg *config.Config, progress func(PortalResult)) []map[string]any {
	var (
		mu   sync.Mutex
		pool []map[string]any
		wg   sync.WaitGroup
		sem  = make(chan struct{}, cfg.MaxParallelPortals)
	)
	for _, s := range cfg.Searches {
		if Disabled(cfg.Workspace, s.Portal) {
			progress(PortalResult{Search: s, Skipped: true})
			continue
		}
		wg.Add(1)
		go func(s config.Search) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			hits, err := runOne(ctx, cfg, s)
			mu.Lock()
			pool = append(pool, hits...)
			mu.Unlock()
			progress(PortalResult{Search: s, Count: len(hits), Err: err})
		}(s)
	}
	wg.Wait()
	return pool
}

func runOne(ctx context.Context, cfg *config.Config, s config.Search) ([]map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cli := filepath.Join(".agents", "skills", s.Portal, "cli", "src", "cli.ts")
	args := append([]string{"run", cli, "search"}, s.Args...)
	args = append(args, "--format", "json")
	cmd := exec.CommandContext(ctx, cfg.Bun, args...)
	cmd.Dir = cfg.Workspace
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(lastLine(stderr.Bytes()), &e) == nil && e.Error != "" {
			return nil, fmt.Errorf("%s", e.Error)
		}
		return nil, err
	}
	var out struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return nil, fmt.Errorf("unreadable output: %w", err)
	}
	for _, r := range out.Results {
		r["portal"] = s.Portal
		r["source"] = "cli"
	}
	return out.Results, nil
}

func lastLine(b []byte) []byte {
	b = bytes.TrimSpace(b)
	if i := bytes.LastIndexByte(b, '\n'); i >= 0 {
		return b[i+1:]
	}
	return b
}

// Candidates dedupes hits against seen_jobs.json and the tracker via
// tools/scrape_state.py candidates. It returns the new hits and the path of
// the file holding them (for the quick-fit step).
func Candidates(ctx context.Context, cfg *config.Config, hits []map[string]any) ([]map[string]any, string, error) {
	stamp := time.Now().Format("20060102-150405")
	in := filepath.Join(cfg.DataDir, "runs", "hits-"+stamp+".json")
	raw, err := json.Marshal(hits)
	if err != nil {
		return nil, "", err
	}
	if err := os.WriteFile(in, raw, 0o600); err != nil {
		return nil, "", err
	}
	cmd := exec.CommandContext(ctx, cfg.Python, "tools/scrape_state.py", "candidates", "--results", in)
	cmd.Dir = cfg.Workspace
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, "", fmt.Errorf("scrape_state candidates: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	var res struct {
		Selected []map[string]any `json:"selected"`
	}
	if err := json.Unmarshal(lastLine(stdout.Bytes()), &res); err != nil {
		return nil, "", fmt.Errorf("scrape_state candidates: unreadable output: %w", err)
	}
	out := filepath.Join(cfg.DataDir, "runs", "candidates-"+stamp+".json")
	sel, _ := json.MarshalIndent(res.Selected, "", "  ")
	if err := os.WriteFile(out, sel, 0o600); err != nil {
		return nil, "", err
	}
	return res.Selected, out, nil
}
