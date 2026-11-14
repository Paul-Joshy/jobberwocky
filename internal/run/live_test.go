//go:build live

package run

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestClaudeInTmux checks that a headless claude run works inside tmux
// (login, PATH). `make live` runs it.
func TestClaudeInTmux(t *testing.T) {
	bin, err := exec.LookPath("claude")
	if err != nil || !Available() {
		t.Skip("claude or tmux missing")
	}
	root := t.TempDir()
	r, _ := New(root, Meta{ID: "live" + time.Now().Format("150405"), Started: time.Now()})
	defer r.Kill()
	if err := r.Start(root, nil, "Reply with exactly: OK", []string{bin, "-p", "--model", "haiku", "--output-format", "stream-json", "--verbose"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var out strings.Builder
	if err := r.Follow(ctx, func(b []byte) { out.Write(b) }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"type":"result"`) {
		t.Fatalf("no result event: %.300s", out.String())
	}
}
