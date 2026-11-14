package ui

import (
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
