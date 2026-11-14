package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestExtractRecordCommand(t *testing.T) {
	res := "files...\n\n## Record command\n```bash\npython3 tools/record_application.py acme_writer --company \"Acme Inc\" --role 'Senior Writer' --fit 80 --status drafted\n```\n"
	got, err := extractRecordCommand(res)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tools/record_application.py", "acme_writer", "--company", "Acme Inc", "--role", "Senior Writer", "--fit", "80", "--status", "drafted"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestExtractRecordCommandRefusesOthers(t *testing.T) {
	for _, body := range []string{
		"python3 tools/other.py x",
		"python3 tools/record_application.py x; rm -rf ~",
		"python3 tools/record_application.py x $(whoami)",
		"python3 tools/record_application.py x\npython3 tools/other.py",
		"bash -c 'python3 tools/record_application.py x'",
	} {
		res := "## Record command\n```\n" + body + "\n```"
		if _, err := extractRecordCommand(res); err == nil {
			t.Errorf("accepted %q", body)
		}
	}
	if _, err := extractRecordCommand("no block here"); err == nil {
		t.Error("accepted a result with no block")
	}
}

func TestSplitArgsLineContinuation(t *testing.T) {
	res := "## Record command\n```\npython3 tools/record_application.py s \\\n  --company X\n```"
	got, err := extractRecordCommand(res)
	if err != nil || len(got) != 4 || got[3] != "X" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestRecordBlockWithNoteBetween(t *testing.T) {
	res := "## Record command\n`--posting-file` points to a missing file.\n\n```\npython3 tools/record_application.py nwl_cw --company \"Northwind Learning\" --role \"Creative Copywriter\" --posting-file documents/nope.txt\n```\n"
	args, err := extractRecordCommand(res)
	if err != nil {
		t.Fatal(err)
	}
	args, dropped := dropMissingPostingFile(args, t.TempDir())
	if !dropped || flagValue(args, "--posting-file") != "" || flagValue(args, "--role") != "Creative Copywriter" {
		t.Fatalf("got %q dropped=%v", args, dropped)
	}
}

func TestCopyGeneric(t *testing.T) {
	ws, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(ws, "cv"), 0o755)
	os.MkdirAll(filepath.Join(ws, "cover_letters"), 0o755)
	os.WriteFile(filepath.Join(ws, "cv", "main_acme_writer.tex"), []byte(`\\name{Ada}{Lovelace}`), 0o644)
	os.WriteFile(filepath.Join(ws, "cv", "main_acme_writer.pdf"), []byte("cv"), 0o644)
	os.WriteFile(filepath.Join(ws, "cover_letters", "cover_acme_writer.pdf"), []byte("cl"), 0o644)
	files, err := copyGeneric(ws, "acme_writer", "Acme/Co", "Senior Writer")
	if err != nil || len(files) != 2 {
		t.Fatalf("%v %v", files, err)
	}
	dest := filepath.Join(home, "Documents", "Job Applications", "Acme-Co - Senior Writer")
	for _, n := range []string{"Ada_Lovelace_CV.pdf", "Ada_Lovelace_Cover_Letter.pdf"} {
		if _, err := os.Stat(filepath.Join(dest, n)); err != nil {
			t.Errorf("missing %s", n)
		}
	}
}
