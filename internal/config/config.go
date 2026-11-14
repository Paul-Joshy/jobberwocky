// Package config loads jobberwocky's settings from a JSON file outside any repo.
package config

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed example.json
var exampleJSON []byte

// Search is one portal query: the portal skill name and its CLI flags.
// "--format json" is appended automatically.
type Search struct {
	Portal string   `json:"portal"`
	Args   []string `json:"args"`
}

// Config is the user's settings. Paths may start with "~".
type Config struct {
	Workspace          string   `json:"workspace"`
	Bun                string   `json:"bun"`
	Python             string   `json:"python"`
	FormPython         string   `json:"form_python"`
	Claude             string   `json:"claude"`
	ClaudeArgs         []string `json:"claude_args"`
	QuickFitModel      string   `json:"quickfit_model"`
	MaxParallelClaude  int      `json:"max_parallel_claude"`
	MaxParallelPortals int      `json:"max_parallel_portals"`
	Notify             bool     `json:"notify"`
	Searches           []Search `json:"searches"`

	// DataDir holds fit checks, logs and run files. Never inside the workspace repo.
	DataDir string `json:"-"`
}

// Path is where the config lives: $JOBBERWOCKY_CONFIG, else $XDG_CONFIG_HOME/jobberwocky/config.json,
// else ~/.config/jobberwocky/config.json.
func Path() string {
	if p := os.Getenv("JOBBERWOCKY_CONFIG"); p != "" {
		return Expand(p)
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = Expand("~/.config")
	}
	return filepath.Join(base, "jobberwocky", "config.json")
}

func dataDir() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		base = Expand("~/.local/share")
	}
	return filepath.Join(base, "jobberwocky")
}

// Expand replaces a leading "~" with the home directory.
func Expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// Load reads, defaults, expands and validates the config.
func Load() (*Config, error) {
	raw, err := os.ReadFile(Path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no config at %s", Path())
		}
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", Path(), err)
	}
	c.applyDefaults()
	if err := c.validate(); err != nil {
		return nil, err
	}
	c.DataDir = dataDir()
	for _, sub := range []string{"fit", "drafts", "logs", "runs", "forms"} {
		if err := os.MkdirAll(filepath.Join(c.DataDir, sub), 0o700); err != nil {
			return nil, err
		}
	}
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.Bun == "" {
		c.Bun = "bun"
		if p := Expand("~/.bun/bin/bun"); fileExists(p) {
			c.Bun = p
		}
	}
	if c.Python == "" {
		// macOS ships Python 3.9 as /usr/bin/python3; the workspace tools need 3.10+.
		c.Python = "python3"
		for _, p := range []string{"/opt/homebrew/bin/python3", "/usr/local/bin/python3"} {
			if fileExists(p) {
				c.Python = p
				break
			}
		}
	}
	if c.Claude == "" {
		c.Claude = "claude"
	}
	if c.QuickFitModel == "" {
		c.QuickFitModel = "haiku"
	}
	if c.MaxParallelClaude <= 0 {
		c.MaxParallelClaude = 3
	}
	if c.MaxParallelPortals <= 0 {
		c.MaxParallelPortals = 4
	}
	c.Workspace = Expand(c.Workspace)
	c.Bun = Expand(c.Bun)
	c.Python = Expand(c.Python)
	c.FormPython = Expand(c.FormPython)
	c.Claude = Expand(c.Claude)
	if c.FormPython == "" {
		venv := filepath.Join(c.Workspace, "tools", ".venv", "bin", "python")
		if fileExists(venv) {
			c.FormPython = venv
		} else {
			c.FormPython = c.Python
		}
	}
}

func (c *Config) validate() error {
	if c.Workspace == "" {
		return errors.New(`config: "workspace" is required (path to your ai-job-search checkout)`)
	}
	if !fileExists(filepath.Join(c.Workspace, "tools", "scrape_state.py")) {
		return fmt.Errorf("config: %s does not look like an ai-job-search workspace (tools/scrape_state.py missing)", c.Workspace)
	}
	return nil
}

// WriteExample writes the example config if none exists and returns its path.
func WriteExample() (string, error) {
	p := Path()
	if fileExists(p) {
		return p, fmt.Errorf("config already exists at %s", p)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return p, err
	}
	return p, os.WriteFile(p, exampleJSON, 0o600)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
