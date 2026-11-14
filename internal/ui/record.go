package ui

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

// recordMu serialises every write to the tracker and STATUS: parallel drafts
// each hand back their record command, and jobberwocky runs them one at a time.
var recordMu sync.Mutex

var recordBlock = regexp.MustCompile("(?s)## Record command\\s*```[a-z]*\\n(.*?)```")

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
