package ui

import (
	"regexp"
	"strings"
	"testing"
)

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestRenderMarkdown(t *testing.T) {
	md := "# Fit check\n\n**Score: 78** · Strong Fit\n\n- consumer brand copy\n- launch messaging\n\n| Check | Result |\n|---|---|\n| Language | PASS |\n"
	out := ansiSeq.ReplaceAllString(renderMarkdown(md, 80), "")
	for _, raw := range []string{"**Score", "|---|", "# Fit"} {
		if strings.Contains(out, raw) {
			t.Errorf("markdown syntax %q left unrendered:\n%s", raw, out)
		}
	}
	for _, text := range []string{"Score: 78", "consumer brand copy", "Language", "PASS"} {
		if !strings.Contains(out, text) {
			t.Errorf("lost text %q", text)
		}
	}
}
