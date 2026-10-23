package web

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// minTemplates is the number of template files this package ships. The walk
// below must inspect at least this many, so a glob that stops matching fails
// rather than passing over nothing. Raise it when you add a template.
const minTemplates = 8

var (
	inlineScript = regexp.MustCompile(`(?i)<script(?:\s[^>]*)?>`)
	scriptSrc    = regexp.MustCompile(`(?i)\ssrc=`)
	styleAttr    = regexp.MustCompile(`(?i)\sstyle\s*=`)
	styleTag     = regexp.MustCompile(`(?i)<style[\s>]`)
	handlerAttr  = regexp.MustCompile(`(?i)\son[a-z]+\s*=`)
	remoteURL    = regexp.MustCompile(`(?i)(?:src|href)\s*=\s*"(?:https?:)?//`)
	jsURL        = regexp.MustCompile(`(?i)href\s*=\s*"javascript:`)
)

// The CSP forbids inline script and style; this keeps the templates from
// relying on either, and from loading anything off this server.
func TestTemplatesHaveNoInlineCodeOrRemoteAssets(t *testing.T) {
	n := 0
	err := fs.WalkDir(templateFS, "templates", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		n++
		body, err := templateFS.ReadFile(p)
		if err != nil {
			return err
		}
		text := string(body)
		for _, tag := range inlineScript.FindAllString(text, -1) {
			if !scriptSrc.MatchString(tag) {
				t.Errorf("%s: inline <script>: %s", p, tag)
			}
		}
		for name, re := range map[string]*regexp.Regexp{
			"style attribute": styleAttr, "<style> element": styleTag, "event handler attribute": handlerAttr,
			"remote asset URL": remoteURL, "javascript: URL": jsURL,
		} {
			if m := re.FindString(text); m != "" {
				t.Errorf("%s: %s: %q", p, name, m)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < minTemplates {
		t.Fatalf("inspected %d templates, want at least %d", n, minTemplates)
	}
}

// The guard has to be able to fail: each pattern must match a sample of what
// it exists to catch.
func TestHygienePatternsCatchTheirSamples(t *testing.T) {
	for re, sample := range map[*regexp.Regexp]string{
		styleAttr: `<div style="color:red">`, styleTag: `<style>`, handlerAttr: `<a onclick="x()">`,
		remoteURL: `<script src="https://cdn.example/x.js">`, jsURL: `<a href="javascript:x()">`,
	} {
		if !re.MatchString(sample) {
			t.Errorf("%s does not catch %q", re, sample)
		}
	}
	if tag := inlineScript.FindString(`<script>alert(1)</script>`); tag == "" || scriptSrc.MatchString(tag) {
		t.Error("an inline script is not caught")
	}
	if strings.Contains(ContentSecurityPolicy, "unsafe-inline") {
		t.Error("the CSP allows inline code")
	}
}

// app.js is the UI's whole script; it must not grow an eval or innerHTML.
func TestAppJSAvoidsDynamicCode(t *testing.T) {
	js, err := assetFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"eval(", "new Function", "innerHTML", "document.write", "setAttribute(\"style\""} {
		if strings.Contains(string(js), bad) {
			t.Errorf("app.js uses %s", bad)
		}
	}
}
