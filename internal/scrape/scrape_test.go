package scrape

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSkill(t *testing.T, ws, portal, body string) {
	t.Helper()
	dir := filepath.Join(ws, ".agents", "skills", portal)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDisabled(t *testing.T) {
	ws := t.TempDir()
	writeSkill(t, ws, "off", "---\nname: off\nenabled: false  # comment\n---\nbody\n")
	writeSkill(t, ws, "on", "---\nname: on\nenabled: true\n---\n")
	writeSkill(t, ws, "nokey", "---\nname: nokey\n---\nenabled: false appears in the body only\n")
	if !Disabled(ws, "off") {
		t.Error("off should be disabled")
	}
	if Disabled(ws, "on") || Disabled(ws, "nokey") || Disabled(ws, "missing") {
		t.Error("on, nokey and missing should be enabled")
	}
}
