package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/checker"
	"github.com/ichava/icon-sets-sync/src/internal/core/text"
)

// failOn values for `check --fail-on`. "stale" is the default and was the
// only behaviour before C1.
var failOnModes = []string{"stale", "error", "any", "never"}

func validFailOn(v string) bool {
	for _, m := range failOnModes {
		if v == m {
			return true
		}
	}
	return false
}

// checkFails reports whether results fail a check run under mode.
func checkFails(mode string, results []checker.Result) bool {
	stale := checker.AnyStale(results)
	errored := checker.CountErrored(results) > 0
	switch mode {
	case "error":
		return errored
	case "any":
		return stale || errored
	case "never":
		return false
	}
	return stale
}

// resultName is the label a CI report uses: the config's slug, which tells
// the sets of a shared pack apart, else the pack.
func resultName(r checker.Result) string {
	if r.Name != "" {
		return r.Name
	}
	return r.Package
}

// githubFiles are the two files a GitHub Actions step writes to. --github
// without them is a usage error: outside Actions there is nowhere to report.
func githubFiles() (summary, output string, err error) {
	summary, output = os.Getenv("GITHUB_STEP_SUMMARY"), os.Getenv("GITHUB_OUTPUT")
	if summary == "" || output == "" {
		return "", "", fmt.Errorf("--github needs GITHUB_STEP_SUMMARY and GITHUB_OUTPUT, which GitHub Actions sets; outside Actions, drop --github")
	}
	return summary, output, nil
}

func appendFile(path, body string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(f, body); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// annotation escapes a workflow-command message: GitHub reads %, CR and LF
// as encoded, and ':' and ',' as separators in a property.
func annotation(kind, title, msg string) string {
	prop := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C")
	body := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")
	return fmt.Sprintf("::%s title=%s::%s\n", kind, prop.Replace(title), body.Replace(msg))
}

func cell(s string) string { return strings.ReplaceAll(text.OrDash(s), "|", `\|`) }

// reportCheckToGitHub writes the check results for a GitHub Actions job.
func reportCheckToGitHub(w io.Writer, results []checker.Result, annotate bool) error {
	summaryPath, outputPath, err := githubFiles()
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("### Upstream check\n\n| Pack | Current | Latest | Status |\n|---|---|---|---|\n")
	var stalePacks []string
	for _, r := range results {
		status := "up to date"
		switch r.Status() {
		case checker.StatusUpdateAvailable:
			status = "update available"
			stalePacks = append(stalePacks, resultName(r))
			if annotate {
				fmt.Fprint(w, annotation("warning", resultName(r)+" is behind upstream", fmt.Sprintf("%s -> %s", text.OrDash(r.Current), r.Latest)))
			}
		case checker.StatusError:
			status = "could not be checked: " + r.Reason
			if annotate {
				fmt.Fprint(w, annotation("error", resultName(r)+" could not be checked", r.Reason))
			}
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", cell(resultName(r)), cell(r.Current), cell(r.Latest), cell(status))
	}
	stale, errored := checker.CountStale(results), checker.CountErrored(results)
	fmt.Fprintf(&b, "\n%d of %d packs checked: %d behind upstream, %d could not be checked.\n\n", len(results)-errored, len(results), stale, errored)
	if err := appendFile(summaryPath, b.String()); err != nil {
		return err
	}
	return appendFile(outputPath, fmt.Sprintf("stale=%d\nerrored=%d\nstale-packs=%s\n", stale, errored, strings.Join(stalePacks, ",")))
}

// reportSyncToGitHub writes sync outcomes for a GitHub Actions job.
func reportSyncToGitHub(w io.Writer, outcomes []app.Outcome, annotate bool) error {
	summaryPath, outputPath, err := githubFiles()
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("### Sync\n\n| Pack | From | To | Outcome |\n|---|---|---|---|\n")
	var synced, failed []string
	for _, o := range outcomes {
		outcome := string(o.Status)
		switch {
		case o.Failed():
			failed = append(failed, o.Pack)
			if o.Err != nil {
				outcome += ": " + o.Err.Error()
				if annotate {
					fmt.Fprint(w, annotation("error", o.Pack+" did not sync", o.Err.Error()))
				}
			}
		case o.Status == app.StatusSynced:
			synced = append(synced, o.Pack)
			outcome += ": " + o.Result.Headline()
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", cell(o.Pack), cell(o.Current), cell(o.Latest), cell(outcome))
	}
	fmt.Fprintf(&b, "\n%d synced, %d failed, of %d.\n\n", len(synced), len(failed), len(outcomes))
	if err := appendFile(summaryPath, b.String()); err != nil {
		return err
	}
	return appendFile(outputPath, fmt.Sprintf("synced=%d\nfailed=%d\nsynced-packs=%s\nfailed-packs=%s\n",
		len(synced), len(failed), strings.Join(synced, ","), strings.Join(failed, ",")))
}
