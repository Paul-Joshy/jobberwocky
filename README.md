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
| `space` `ctrl+a` `esc` | **Mark** jobs (● in the first column): `r`, `f`, `d` and `p` then act on every marked job at once, in parallel | |
| `r` | **Rank** the marked jobs, or all new jobs when nothing is marked | One `claude -p "/rank"` run that scores the jobs in parallel. Only one ranker runs at a time, because it's the only process allowed to write `seen_jobs.json`. |
| `f` | **Fit check** each marked job (or the one under the cursor) | One `claude -p` per job, running `/apply` Steps 0–1 only, `max_parallel_claude` at a time. Read the result with `v`. |
| `d` | **Draft** a tailored CV and cover letter for each (each job needs a fit check first) | One `claude -p "/apply …"` per job, in parallel. Each compiles and checks its own files, then hands back its record command. jobberwocky runs those one at a time, so parallel drafts never overwrite each other's tracker or STATUS changes. Questions are listed, never asked. |
| `p` | **Fill** each job's application form | Asks for each form link in turn (prefilled with the posting URL; `tab` skips a job). Each runs `tools/fill_form.py` in its own Chrome window and profile, and works on Google Forms and ordinary careers-portal forms. **It never submits.** You check every answer and press Submit yourself. |
| `o` | Open the posting in your browser | |
| `t` | Task tray: logs, cost per task, `x` to cancel | |
| `c` / `x` | **Done** (applied or finished) / **archive** (not interested). Works on marked jobs too, and pressing the key again undoes it. | Saved in `~/.local/share/jobberwocky/states.json`. Your workspace job list is never changed. |
| `1`–`7` / `tab` | Views: **1 active · 2 to check · 3 checked · 4 drafted · 5 done · 6 archived · 7 all**, with counts | |
| `*` | Mark the ★ top 3 picks | |
| `/` `R` `?` | filter · reload · help | |

Columns: ● marked · ★ top pick · score (green when it comes from your saved fit check) · freshness (🟢 posted ≤1 day ago, 🟡 ≤7 days, 🔴 older, ⚪ unknown) · band (H/M/L) · stages **F D P A** (fit checked, drafted, form prefilled, applied; lit when done, `…` while running).

Fit checks, drafts and form reports are saved in `~/.local/share/jobberwocky/` and survive restarts.

### Tasks keep running when you quit

With [tmux](https://github.com/tmux/tmux) installed (`brew install tmux`), every rank, fit check, draft and form fill runs in its own detached tmux session (`jw-…`). Quitting jobberwocky, or closing the terminal, doesn't stop them: open jobberwocky again and it picks up every task that is still running, plus any that finished while it was closed, and saves their results as usual. Only a scrape, and tasks still queued for a free slot, stop when you quit.

- `tmux ls` lists the running sessions; `tmux attach -t jw-…` shows one live (detach with `ctrl+b d`).
- `x` in the task tray (`t`) still cancels a task and ends its session.
- Each run's prompt, output and exit code are in `~/.local/share/jobberwocky/runs/<run>/`.

Without tmux, tasks run inside jobberwocky and stop when it quits. A fit check's score beats the `/rank` score for sorting.

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
