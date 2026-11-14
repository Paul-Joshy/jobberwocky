package run

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunSurvivesAndFollows(t *testing.T) {
	if !Available() {
		t.Skip("tmux not installed")
	}
	root := t.TempDir()
	id := "test-" + time.Now().Format("150405.000")
	id = strings.ReplaceAll(id, ".", "")
	r, err := New(root, Meta{ID: id, Kind: "fill", Label: "x", Started: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Kill()
	if err := r.Start(root, map[string]string{"GREETING": "it's here"}, "line from stdin\n",
		[]string{"/bin/sh", "-c", `echo "$GREETING"; cat; sleep 1; printf tail`}); err != nil {
		t.Fatal(err)
	}
	if len(Pending(root)) != 1 {
		t.Fatal("a started run should be pending")
	}
	var got []string
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.Follow(ctx, func(b []byte) { got = append(got, string(b)) }); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "it's here|line from stdin|tail" {
		t.Fatalf("got %q", got)
	}
	r.MarkCollected()
	if len(Pending(root)) != 0 {
		t.Fatal("a collected run should not be pending")
	}
}

func TestFollowReportsFailure(t *testing.T) {
	if !Available() {
		t.Skip("tmux not installed")
	}
	root := t.TempDir()
	r, _ := New(root, Meta{ID: "fail" + time.Now().Format("150405"), Started: time.Now()})
	defer r.Kill()
	if err := r.Start(root, nil, "", []string{"/bin/sh", "-c", "echo oops >&2; exit 3"}); err != nil {
		t.Fatal(err)
	}
	err := r.Follow(context.Background(), func([]byte) {})
	if err == nil || !strings.Contains(err.Error(), "exit status 3") || !strings.Contains(err.Error(), "oops") {
		t.Fatalf("err = %v", err)
	}
}

func TestCancelKillsSession(t *testing.T) {
	if !Available() {
		t.Skip("tmux not installed")
	}
	root := t.TempDir()
	r, _ := New(root, Meta{ID: "cancel" + time.Now().Format("150405"), Started: time.Now()})
	if err := r.Start(root, nil, "", []string{"sleep", "30"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(500 * time.Millisecond); cancel() }()
	if err := r.Follow(ctx, func([]byte) {}); err == nil {
		t.Fatal("want a cancel error")
	}
	if r.Alive() {
		r.Kill()
		t.Fatal("session should be gone after cancel")
	}
}
