package states

import "testing"

func TestToggleSetsClearsAndPersists(t *testing.T) {
	dir := t.TempDir()
	s := Load(dir)
	set, err := s.Toggle([]string{"a", "b"}, Done)
	if err != nil || !set || s.Get("a") != Done || s.Count(Done) != 2 {
		t.Fatalf("set=%v err=%v a=%q", set, err, s.Get("a"))
	}
	if Load(dir).Get("b") != Done {
		t.Fatal("not persisted")
	}
	// mixed selection: sets the missing one instead of clearing
	s.Toggle([]string{"a"}, Archived)
	set, _ = s.Toggle([]string{"a", "b"}, Archived)
	if !set || s.Get("b") != Archived {
		t.Fatal("mixed toggle should set all")
	}
	// all already archived: clears (undo)
	set, _ = s.Toggle([]string{"a", "b"}, Archived)
	if set || s.Get("a") != "" || Load(dir).Count(Archived) != 0 {
		t.Fatal("second toggle should clear")
	}
}
