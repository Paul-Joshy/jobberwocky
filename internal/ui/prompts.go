package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/Paul-Joshy/jobberwocky/internal/workspace"
)

// Every prompt says it is non-interactive: a headless run has no one to answer
// a question, so it must list open questions instead of asking them.
const headless = "This is a non-interactive run started from jobberwocky (a terminal UI). " +
	"There is no one to answer questions: never ask, never wait for confirmation. " +
	"Never submit a form or send a message on the candidate's behalf."

func quickFitPrompt(candidatesFile string) string {
	return fmt.Sprintf(`%s

The file %s holds new job-search hits that already passed `+"`python3 tools/scrape_state.py candidates`"+`.
Run only Steps 3 and 4 of the job-scraper skill on them:
- give each hit a quick fit (high, medium or low) from its title, company and location, applying the Language Gate;
- build entries with first_seen=%s, posted_date from the hit's "date" field (null if missing), deadline null, status "new", and keep "portal", "source", "location";
- write them to a temporary JSON file and run `+"`python3 tools/scrape_state.py store --results <that file>`"+`.
Do not search portals, do not fetch details, do not present a table.
End with exactly one line: stored N (H high, M medium, L low).`, headless, candidatesFile, time.Now().Format("2006-01-02"))
}

func rankPrompt() string {
	return "/rank\n\n" + headless + " Rank every unranked new entry, then end with the top five as `score | company | title`."
}

// rankSelectedPrompt ranks only the marked jobs in one /rank run: /rank scores
// them in parallel with its own posting-scorer agents, and a single run means a
// single writer of seen_jobs.json (parallel /rank runs would overwrite each other).
func rankSelectedPrompt(jobs []workspace.Job) string {
	var b strings.Builder
	for _, j := range jobs {
		fmt.Fprintf(&b, "- %s (%s at %s)\n", j.URL, j.Title, j.Company)
	}
	return fmt.Sprintf(`/rank --all --limit %d

%s Score only these entries of job_scraper/seen_jobs.json (match each by url), dispatching the scorers in parallel as Step 2 describes, and leave every other entry untouched:
%s
End with them as `+"`score | company | title`"+`, best first.`, len(jobs), headless, b.String())
}

func fitPrompt(j workspace.Job) string {
	return fmt.Sprintf(`%s

Run only Step 0 and Step 1 of /apply (fetch the posting, run the integrity gate, evaluate fit) for:
%s
(%s at %s)

Do not draft a CV or cover letter and do not write any files. End with the evaluation in the usual format: score out of 100, verdict, posted date and freshness, applicants if shown, open or closed, strengths, gaps, deal-breaker checks, and every [PAUL] blank the application form will need.`,
		headless, j.URL, j.Title, j.Company)
}

func draftPrompt(j workspace.Job, fit string) string {
	if len(fit) > 6000 {
		fit = fit[:6000] + "\n[fit check truncated]"
	}
	return fmt.Sprintf(`/apply %s

%s The candidate read this fit check in jobberwocky and approved drafting:

<fit_check>
%s
</fit_check>

Rules for this run:
- Run Steps 2 to 6 of /apply for this one job. Skip Step 1's question round; the fit check above is approved.
- Where a [PAUL] answer is needed, leave it out of the documents and list it under "## Questions" at the end.
- Compile with python3 tools/build_docs.py and verify with python3 tools/check_docs.py until there are no FAILs.
- Do NOT run tools/record_application.py and do not edit job_search_tracker.csv or STATUS.md: other drafts run in parallel, and jobberwocky records each one in turn. Instead end your answer with a section headed exactly "## Record command" holding one fenced code block with the single command you would have run, starting `+"`python3 tools/record_application.py <slug>`"+` and including --company, --role, --fit, --status drafted, --source and --posting-file.
- Before that, list: file paths, the check summary, every stretch claim, the questions, and the apply link.`,
		j.URL, headless, fit)
}
