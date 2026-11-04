// Command changelog checks and closes CHANGELOG.md for the CI workflows.
//
//	go -C src run ./internal/tools/changelog check ../CHANGELOG.md
//	go -C src run ./internal/tools/changelog close ../CHANGELOG.md ../.release-please-manifest.json
//
// `check` is the Tests workflow's Changelog order job: every `## [x]` heading
// once, [Unreleased] first, versions newest first. `close` is release-please's
// step that files [Unreleased] under the release version on the release
// branch, writing version= and changed= to $GITHUB_OUTPUT. Both replace inline
// Python scripts and keep their messages and exit codes.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: changelog check CHANGELOG.md | changelog close CHANGELOG.md MANIFEST.json")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "check":
		os.Exit(runCheck(os.Args[2], os.Stdout))
	case "close":
		if len(os.Args) != 4 {
			fmt.Fprintln(os.Stderr, "usage: changelog close CHANGELOG.md MANIFEST.json")
			os.Exit(2)
		}
		out := os.Getenv("GITHUB_OUTPUT")
		if out == "" {
			fmt.Fprintln(os.Stderr, "GITHUB_OUTPUT is not set")
			os.Exit(1)
		}
		today := time.Now().UTC().Format("2006-01-02")
		os.Exit(runClose(os.Args[2], os.Args[3], out, today, os.Stdout))
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}

var (
	headingRe = regexp.MustCompile(`(?m)^## \[([^\]]+)\]`)
	digitsRe  = regexp.MustCompile(`\d+`)
	anyHeadRe = regexp.MustCompile(`(?m)^## \[`)
	unrelRe   = regexp.MustCompile(`(?m)^## \[Unreleased\][^\n]*\n`)
)

// orderKey is the first three numbers in a heading, padded with zeros.
func orderKey(v string) [3]int {
	var k [3]int
	for i, n := range digitsRe.FindAllString(v, 3) {
		k[i], _ = strconv.Atoi(n)
	}
	return k
}

func less(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// checkProblems returns what is wrong with the section headings, in the order
// the workflow reports them.
func checkProblems(heads []string) []string {
	var problems []string

	// Duplicates, in the order each heading first appears.
	count := map[string]int{}
	var order []string
	for _, h := range heads {
		if count[h] == 0 {
			order = append(order, h)
		}
		count[h]++
	}
	var dups []string
	for _, h := range order {
		if count[h] > 1 {
			dups = append(dups, "["+h+"]")
		}
	}
	if len(dups) > 0 {
		problems = append(problems, "duplicate section headings: "+strings.Join(dups, ", ")+
			". Each branch must add to the existing section, not prepend its own.")
	}

	for i, h := range heads {
		if h == "Unreleased" {
			if i != 0 {
				problems = append(problems, fmt.Sprintf(
					"[Unreleased] is section %d, not the first. Closing it in place would file the new version below an older one.", i+1))
			}
			break
		}
	}

	var versions []string
	for _, h := range heads {
		if h != "Unreleased" {
			versions = append(versions, h)
		}
	}
	for i := 0; i+1 < len(versions); i++ {
		older, newer := versions[i], versions[i+1]
		if less(orderKey(older), orderKey(newer)) {
			problems = append(problems, fmt.Sprintf("[%s] is listed above [%s], but is the lower version.", older, newer))
		}
	}
	return problems
}

func runCheck(path string, w io.Writer) int {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		say(w, "::error::%s not found\n", path)
		return 1
	} else if err != nil {
		say(w, "::error::%s: %v\n", path, err)
		return 1
	}
	var heads []string
	for _, m := range headingRe.FindAllStringSubmatch(string(data), -1) {
		heads = append(heads, m[1])
	}
	if len(heads) == 0 {
		say(w, "::error::%s has no \"## [version]\" headings\n", path)
		return 1
	}
	if problems := checkProblems(heads); len(problems) > 0 {
		say(w, "::error::%s sections are out of order:\n", path)
		for _, p := range problems {
			say(w, "::error::  %s\n", p)
		}
		return 1
	}
	say(w, "%s: %d sections, correctly ordered\n", path, len(heads))
	return 0
}

// closeText files [Unreleased] under version. It returns the new text and
// whether it changed, or an error message for the workflow log.
func closeText(text, version, today string) (string, bool, string) {
	if regexp.MustCompile(`(?m)^## \[` + regexp.QuoteMeta(version) + `\]`).MatchString(text) {
		return text, false, ""
	}
	heads := unrelRe.FindAllStringIndex(text, -1)
	if len(heads) != 1 {
		return "", false, fmt.Sprintf("has %d [Unreleased] headings; expected exactly 1.", len(heads))
	}
	start := heads[0][1]
	body := text[start:]
	if loc := anyHeadRe.FindStringIndex(body); loc != nil {
		body = body[:loc[0]]
	}
	if strings.TrimSpace(body) == "" {
		return "", false, "empty"
	}
	closed := "## [Unreleased]\n\n## [" + version + "] - " + today + "\n"
	return text[:heads[0][0]] + closed + text[start:], true, ""
}

func runClose(changelog, manifest, output, today string, w io.Writer) int {
	raw, err := os.ReadFile(manifest)
	if err != nil {
		say(w, "::error::%v\n", err)
		return 1
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		say(w, "::error::%s: %v\n", manifest, err)
		return 1
	}
	version, ok := m["."].(string)
	if !ok {
		say(w, "::error::%s has no \".\" version\n", manifest)
		return 1
	}
	data, err := os.ReadFile(changelog)
	if err != nil {
		say(w, "::error::%v\n", err)
		return 1
	}
	text, changed, msg := closeText(string(data), version, today)
	switch {
	case msg == "empty":
		say(w, "::error::[Unreleased] in %s is empty. Write the notes for %s on main, then re-run this workflow.\n", changelog, version)
		return 1
	case msg != "":
		say(w, "::error::%s %s\n", changelog, msg)
		return 1
	}
	if changed {
		if err := os.WriteFile(changelog, []byte(text), 0o644); err != nil {
			say(w, "::error::%v\n", err)
			return 1
		}
		say(w, "closed [Unreleased] as [%s] - %s\n", version, today)
	} else {
		say(w, "%s already has a [%s] section\n", changelog, version)
	}
	f, err := os.OpenFile(output, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		say(w, "::error::%v\n", err)
		return 1
	}
	_, werr := fmt.Fprintf(f, "version=%s\nchanged=%t\n", version, changed)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		say(w, "::error::%v\n", werr)
		return 1
	}
	return 0
}

// say writes one line of workflow log; a log that cannot be written has
// nowhere to report that either.
func say(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}
