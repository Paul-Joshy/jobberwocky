package claude

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Paul-Joshy/jobberwocky/internal/config"
)

// TestLiveSmoke calls the real claude CLI with a tiny prompt.
// Run with: JOBBERWOCKY_LIVE=1 go test ./internal/claude -run Live -v
func TestLiveSmoke(t *testing.T) {
	if os.Getenv("JOBBERWOCKY_LIVE") == "" {
		t.Skip("set JOBBERWOCKY_LIVE=1 to call the real claude CLI")
	}
	cfg := &config.Config{Claude: "claude", Workspace: t.TempDir()}
	var steps []string
	res := Run(context.Background(), cfg, "Reply with exactly: pong", "haiku", func(s string) { steps = append(steps, s) }, io.Discard)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if !strings.Contains(strings.ToLower(res.Text), "pong") {
		t.Fatalf("result %q, steps %v", res.Text, steps)
	}
	t.Logf("result %q cost $%.4f steps %v", res.Text, res.CostUSD, steps)
}

func TestSummarize(t *testing.T) {
	if got := summarize(map[string]any{"command": "python3 tools/build_docs.py x\nmore"}); got != "python3 tools/build_docs.py x" {
		t.Fatalf("got %q", got)
	}
	if got := summarize(map[string]any{"other": 1}); got != "" {
		t.Fatalf("got %q", got)
	}
}
