# jobberwocky

A terminal UI for running a job search from one screen: **scrape → rank → fit check → draft → fill the form**.

```
jobberwocky  42 shown · 180 active · 61 ranked · 7 new
  81 🟢 H ✓ Senior Copywriter        Acme            Bengaluru     │ Senior Copywriter
  77 🟡 H ✎ Content Marketing Lead   Globex          Remote        │ Acme · Bengaluru
  70 🟡 M   UX Writer                Initech         Remote        │ 🟢 posted 2026-11-13
 new 🟢 ·   Brand Strategist         Umbrella        Bengaluru     │ score 81 · Strong Fit
   · 🔴 L   Web Content Writer       Hooli           Mumbai        │ + consumer brand copy
⠋ 2 running  fit · Globex · Content Marketing Lead: → WebFetch …  ✓ 3
fit check started for Globex. Pick another job meanwhile.
```

Everything slow runs in the background, so you never sit and wait: start a fit check, move to the next job, start another, and get a ping on your phone when each one finishes.

jobberwocky is a front end for an [ai-job-search](https://github.com/ai-job-search-dev/ai-job-search) workspace. It doesn't keep its own copy of your CV, profile or job history. It reads the workspace's state files, runs the workspace's own portal CLIs and Python tools, and hands the judgement steps to [Claude Code](https://claude.com/claude-code) running headless (`claude -p`), using the skills already in the workspace.

## What each key does

| Key | Step | How it runs |
|---|---|---|
| `s` | **Scrape**: every configured portal search, in parallel, then dedupe against jobs you've already seen and your application tracker | Portal CLIs (`bun`) + `tools/scrape_state.py`. No AI. New hits then get a quick fit and are stored by a small, cheap model (`haiku` by default). |
| `r` | **Rank** new jobs against your full fit framework | `claude -p "/rank"` |
| `f` | **Fit check** the selected job | `claude -p` running `/apply` Steps 0–1 only. The result is saved, and you read it with `v`. |
| `d` | **Draft** a tailored CV and cover letter (only after a fit check) | `claude -p "/apply …"`: compiles, checks, records it as drafted, and lists any questions instead of asking them |
| `p` | **Fill** a Google Form application | `tools/fill_form.py` (Playwright, your installed Chrome). **It never submits.** You check every answer and press Submit yourself. |
| `o` | Open the posting in your browser | |
| `t` | Task tray: logs, cost per task, `x` to cancel | |
| `/` `a` `R` `?` | filter · show hidden · reload · help | |

Columns: rank score · freshness (🟢 posted ≤1 day ago, 🟡 ≤7 days, 🔴 older, ⚪ unknown) · quick fit (H/M/L) · ✓ fit checked / ✎ drafted.

## Install

You'll need Go 1.22+, plus an ai-job-search workspace set up with `bun`, Python 3.10+ and Claude Code logged in.

```sh
go install github.com/Paul-Joshy/jobberwocky@latest
jobberwocky init     # writes ~/.config/jobberwocky/config.json
$EDITOR ~/.config/jobberwocky/config.json   # set "workspace" and your searches
jobberwocky check    # confirms tools, portals and Python version
jobberwocky
```

`jobberwocky scrape` runs the scrape step with no UI and no AI, prints the new hits and stores nothing. It's handy for checking your searches.

## Config

`~/.config/jobberwocky/config.json` (or `$JOBBERWOCKY_CONFIG`):

```json
{
  "workspace": "~/ai-job-search",
  "claude_args": ["--permission-mode", "acceptEdits",
                  "--allowedTools", "Bash(python3 tools/*),Bash(bun run .agents/skills/*),WebFetch,WebSearch,Agent"],
  "quickfit_model": "haiku",
  "max_parallel_claude": 3,
  "max_parallel_portals": 4,
  "notify": true,
  "searches": [
    {"portal": "linkedin-search", "args": ["-q", "content strategist", "-l", "Remote", "--jobage", "14", "--limit", "20"]}
  ]
}
```

- **searches**: one entry per portal call. `portal` is a skill folder under `.agents/skills/`, and `args` are that portal's own CLI flags (see its `SKILL.md`). `--format json` is added for you. A portal with `enabled: false` in its `SKILL.md` is skipped.
- **claude_args**: passed to every `claude -p` call. Headless runs can't answer permission prompts, so a tool that isn't allowed is simply refused. Keep this list as narrow as you can.
- **notify**: when the workspace has `tools/notify.py` set up (ntfy), finished tasks ping your phone. That script refuses any message containing an email address, phone number or salary.
- Optional: `bun`, `python`, `form_python`, `claude` paths. jobberwocky finds sensible defaults, and prefers Homebrew Python over macOS's built-in 3.9.

## Privacy and safety

- **No personal data in this repo or its config.** Fit checks, draft summaries, logs and run files go to `~/.local/share/jobberwocky/` (or `$XDG_DATA_HOME`), never into a git repo.
- **jobberwocky never writes your workspace's state itself.** Every write goes through the workspace's own tools (`scrape_state.py store`, `record_application.py`), which dedupe, validate and refuse to move an application's status backwards.
- **Nothing is ever submitted or sent for you.** Every prompt tells Claude not to, and the form filler has no submit path: it only ever clicks a page's *Next* button.
- **Respect the portals.** The portal CLIs use an honest user agent, back off when rate-limited, and skip sites whose robots.txt disallows them. Keep your search list small.

## Development

```sh
make test        # unit tests
make live        # also calls the real claude CLI once (costs a fraction of a cent)
make build       # ./bin/jobberwocky
```

## Credits

Built on [ai-job-search](https://github.com/ai-job-search-dev/ai-job-search) by Mads Lorentzen (MIT), using [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Bubbles](https://github.com/charmbracelet/bubbles) and [Lip Gloss](https://github.com/charmbracelet/lipgloss) from Charm.

## License

MIT
