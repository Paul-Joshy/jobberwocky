// Command jobberwocky is a terminal UI for an ai-job-search workspace:
// scrape → rank → fit check → draft → fill the form, all from one screen.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Paul-Joshy/jobberwocky/internal/config"
	"github.com/Paul-Joshy/jobberwocky/internal/scrape"
	"github.com/Paul-Joshy/jobberwocky/internal/ui"
)

var version = "0.1.0-dev"

const usage = `jobberwocky - a terminal UI for an ai-job-search workspace

Usage:
  jobberwocky           start the UI
  jobberwocky init      write an example config to %s
  jobberwocky check     check the config, tools and portals
  jobberwocky scrape    run the searches and dedupe, without the UI or any AI step
                        (prints new hits; stores nothing)
  jobberwocky version   print the version
`

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "init":
			p, err := config.WriteExample()
			if err != nil {
				fail(err)
			}
			fmt.Printf("wrote %s\nset \"workspace\" to your ai-job-search folder and edit \"searches\", then run jobberwocky check\n", p)
			return
		case "check":
			os.Exit(runCheck())
		case "scrape":
			os.Exit(runScrape())
		case "version", "--version", "-v":
			fmt.Println(version)
			return
		default:
			fmt.Printf(usage, config.Path())
			if os.Args[1] != "help" && os.Args[1] != "-h" && os.Args[1] != "--help" {
				os.Exit(2)
			}
			return
		}
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "jobberwocky:", err)
		fmt.Fprintln(os.Stderr, "run `jobberwocky init` to create a config")
		os.Exit(1)
	}
	m, err := ui.New(cfg)
	if err != nil {
		fail(err)
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	ui.SetProgram(p)
	if _, err := p.Run(); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "jobberwocky:", err)
	os.Exit(1)
}

// runCheck reports what is missing before the first run, so nothing fails silently later.
func runCheck() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Println("✗ config:", err)
		return 1
	}
	bad := 0
	line := func(ok bool, what, detail string) {
		mark := "✓"
		if !ok {
			mark, bad = "✗", bad+1
		}
		fmt.Printf("%s %-22s %s\n", mark, what, detail)
	}
	line(true, "config", config.Path())
	line(true, "workspace", cfg.Workspace)
	for _, bin := range []struct{ name, path string }{{"bun", cfg.Bun}, {"python", cfg.Python}, {"claude", cfg.Claude}, {"form python", cfg.FormPython}} {
		p, err := exec.LookPath(bin.path)
		line(err == nil, bin.name, firstNonEmpty(p, bin.path+" not found"))
	}
	if out, err := exec.Command(cfg.Python, "-c", "import sys; print(sys.version_info >= (3, 10))").Output(); err == nil {
		ok := strings.TrimSpace(string(out)) == "True"
		line(ok, "python ≥ 3.10", map[bool]string{true: "yes", false: `no: set "python" in the config to a newer python3`}[ok])
	}
	for _, tool := range []string{"scrape_state.py", "fill_form.py", "notify.py", "build_docs.py", "check_docs.py", "record_application.py"} {
		_, err := os.Stat(filepath.Join(cfg.Workspace, "tools", tool))
		line(err == nil, "tools/"+tool, map[bool]string{true: "found", false: "missing (that step will fail)"}[err == nil])
	}
	seen := map[string]bool{}
	for _, s := range cfg.Searches {
		if seen[s.Portal] {
			continue
		}
		seen[s.Portal] = true
		_, err := os.Stat(filepath.Join(cfg.Workspace, ".agents", "skills", s.Portal, "cli", "src", "cli.ts"))
		detail := "installed"
		if err != nil {
			detail = "not installed"
		} else if scrape.Disabled(cfg.Workspace, s.Portal) {
			detail = "installed, enabled: false (skipped)"
		}
		line(err == nil, s.Portal, detail)
	}
	line(true, "data dir", cfg.DataDir)
	if bad > 0 {
		fmt.Printf("\n%d problem(s)\n", bad)
		return 1
	}
	fmt.Println("\nready")
	return 0
}

// runScrape is the UI's scrape step without the UI: portal CLIs in parallel,
// then dedupe. It stores nothing; the UI's quick-fit step does that.
func runScrape() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Println("✗ config:", err)
		return 1
	}
	ctx := context.Background()
	hits := scrape.Run(ctx, cfg, func(p scrape.PortalResult) { fmt.Println(p.String()) })
	sel, file, err := scrape.Candidates(ctx, cfg, hits)
	if err != nil {
		fmt.Println("✗", err)
		return 1
	}
	fmt.Printf("\n%d hits, %d new (saved to %s)\n", len(hits), len(sel), file)
	for _, h := range sel {
		fmt.Printf("  %v · %v · %v · %v\n", h["date"], h["company"], h["title"], h["location"])
	}
	return 0
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
