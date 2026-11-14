package ui

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// recordMu serialises every write to the tracker and STATUS: parallel drafts
// each hand back their record command, and jobberwocky runs them one at a time.
var recordMu sync.Mutex

// the first fenced block after the heading; drafts sometimes add a note in between
var recordBlock = regexp.MustCompile("(?s)## Record command.*?```[a-z]*\\n(.*?)```")

// extractRecordCommand finds the command in a draft's "## Record command" block
// and checks it is exactly a record_application.py call.
func extractRecordCommand(result string) ([]string, error) {
	m := recordBlock.FindStringSubmatch(result)
	if m == nil {
		return nil, errors.New(`no "## Record command" block in the draft result`)
	}
	cmd := strings.TrimSpace(m[1])
	cmd = strings.ReplaceAll(cmd, "\\\n", " ")
	if strings.ContainsAny(cmd, "\n") {
		return nil, errors.New("record command spans several commands")
	}
	args, err := splitArgs(cmd)
	if err != nil {
		return nil, err
	}
	if len(args) < 3 || (args[0] != "python3" && args[0] != "python") || args[1] != "tools/record_application.py" {
		return nil, fmt.Errorf("refusing record command that is not tools/record_application.py: %q", cmd)
	}
	return args[1:], nil // script and its arguments; the interpreter comes from the config
}

// splitArgs splits a command line into words, honouring single and double
// quotes. There is no shell: nothing is expanded or executed by the split.
func splitArgs(s string) ([]string, error) {
	var (
		args  []string
		cur   strings.Builder
		quote rune
		inArg bool
	)
	for i := 0; i < len(s); i++ {
		c := rune(s[i])
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else if c == '\\' && quote == '"' && i+1 < len(s) && (s[i+1] == '"' || s[i+1] == '\\') {
				i++
				cur.WriteByte(s[i])
			} else {
				cur.WriteRune(c)
			}
		case c == '\'' || c == '"':
			quote, inArg = c, true
		case c == ' ' || c == '\t':
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		case strings.ContainsRune(";|&`$<>", c):
			return nil, fmt.Errorf("record command contains shell syntax %q", c)
		default:
			cur.WriteRune(c)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, errors.New("record command has an unclosed quote")
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args, nil
}

// runRecord runs one record command under the lock and returns its last line.
func (r *runner) runRecord(args []string) (string, error) {
	recordMu.Lock()
	defer recordMu.Unlock()
	cmd := exec.Command(r.cfg.Python, args...)
	cmd.Dir = r.cfg.Workspace
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return lastLine(out.String()), err
}

// dropMissingPostingFile removes --posting-file when the file does not exist:
// record_application.py would crash on it, and the posting can be archived later.
func dropMissingPostingFile(args []string, workspace string) ([]string, bool) {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--posting-file" {
			p := args[i+1]
			if !filepath.IsAbs(p) {
				p = filepath.Join(workspace, p)
			}
			if _, err := os.Stat(p); err != nil {
				return append(append([]string{}, args[:i]...), args[i+2:]...), true
			}
			return args, false
		}
	}
	return args, false
}

func flagValue(args []string, name string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == name {
			return args[i+1]
		}
	}
	return ""
}

var cvName = regexp.MustCompile(`\\name\{([^}]*)\}\{([^}]*)\}`)

// copyGeneric copies the compiled CV and cover letter to
// ~/Documents/Job Applications/<Company> - <Role>/ as <First>_<Last>_CV.pdf and
// <First>_<Last>_Cover_Letter.pdf, the same names record_application.py uses,
// so nothing in a file name looks tailored. It never depends on recording.
func copyGeneric(workspace, slug, company, role string) ([]string, error) {
	if slug == "" || company == "" || role == "" {
		return nil, errors.New("missing slug, company or role")
	}
	first, last := "Candidate", "CV"
	if raw, err := os.ReadFile(filepath.Join(workspace, "cv", "main_"+slug+".tex")); err == nil {
		if m := cvName.FindSubmatch(raw); m != nil {
			first, last = strings.TrimSpace(string(m[1])), strings.TrimSpace(string(m[2]))
		}
	}
	safe := strings.NewReplacer("/", "-", ":", "-", "\\", "-")
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dest := filepath.Join(home, "Documents", "Job Applications", strings.TrimSpace(safe.Replace(company))+" - "+strings.TrimSpace(safe.Replace(role)))
	var done []string
	for _, f := range []struct{ src, name string }{
		{filepath.Join(workspace, "cv", "main_"+slug+".pdf"), first + "_" + last + "_CV.pdf"},
		{filepath.Join(workspace, "cover_letters", "cover_"+slug+".pdf"), first + "_" + last + "_Cover_Letter.pdf"},
	} {
		raw, err := os.ReadFile(f.src)
		if err != nil {
			continue
		}
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return done, err
		}
		if err := os.WriteFile(filepath.Join(dest, f.name), raw, 0o644); err != nil {
			return done, err
		}
		done = append(done, f.name)
	}
	if len(done) == 0 {
		return nil, fmt.Errorf("no compiled PDFs for %s", slug)
	}
	return done, nil
}
