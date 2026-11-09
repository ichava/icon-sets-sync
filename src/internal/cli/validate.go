package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/packcheck"
	"github.com/ichava/icon-sets-sync/src/internal/ui"
)

func newValidateCommand() *cobra.Command {
	var (
		github, strict, skipSVG bool
	)
	cmd := &cobra.Command{
		Use:   "validate [pack-dir...]",
		Short: "Check the structure of the ichava pack checkouts",
		Long: `Check every ichava pack checkout: composer.json (type ichava-icon-set), the
manifest (resources/assets/svg/config.json, naming the same package), that a
pack config syncs it, the sets under resources/assets/svg/files/, licence
files, empty categories, files that are not SVGs, and SVGs the toolkit's parser
rejects.

With no arguments, checks every pack under packages_dirs, else every
registered pack's checkout. Exits 1 on an error (with --strict, on a warning
too). --json prints a report; in GitHub Actions (or with --github) each
problem is also an annotation on its file, and a summary goes to the step
summary.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			roots, err := validateRoots(args)
			if err != nil {
				return err
			}
			if len(roots) == 0 {
				return failure(fmt.Errorf("no ichava pack found: set packages_dirs (--packages-dir) or pass a pack directory"))
			}
			configs := map[string]string{}
			if packs, err := app.LoadPacksIn(configDir(), "", workspace()); err == nil {
				for _, p := range packs {
					configs[p.Pack] = p.Name + ".json"
				}
			}
			var reports []packcheck.Report
			for _, root := range roots {
				reports = append(reports, packcheck.Check(root, packcheck.Options{PackConfigs: configs, ParseSVGs: !skipSVG}))
			}
			errs, warns := 0, 0
			for _, r := range reports {
				errs += r.Errors()
				warns += r.Warnings()
			}

			out := cmd.OutOrStdout()
			annotate := github || os.Getenv("GITHUB_ACTIONS") == "true"
			switch {
			case ui.JSONMode():
				if err := ui.JSON(map[string]any{"packs": reports, "errors": errs, "warnings": warns}); err != nil {
					return failure(err)
				}
			default:
				printPackReports(reports)
			}
			if annotate {
				writePackAnnotations(out, reports)
				if path := os.Getenv("GITHUB_STEP_SUMMARY"); path != "" {
					if err := appendFile(path, packSummaryMarkdown(reports, errs, warns)); err != nil {
						return failure(err)
					}
				}
			}
			if errs > 0 || (strict && warns > 0) {
				return &ExitError{Code: ExitFailure}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&github, "github", false, "write annotations and a step summary (automatic when GITHUB_ACTIONS=true)")
	f.BoolVar(&strict, "strict", false, "fail on a warning too")
	f.BoolVar(&skipSVG, "skip-svg-parse", false, "do not parse every SVG (faster on the largest packs)")
	return cmd
}

// validateRoots is the pack checkouts to check: the arguments, else every
// pack under packages_dirs, else every registered pack's checkout.
func validateRoots(args []string) ([]string, error) {
	if len(args) > 0 {
		var out []string
		for _, a := range args {
			abs, err := filepath.Abs(a)
			if err != nil {
				return nil, failure(err)
			}
			out = append(out, abs)
		}
		return out, nil
	}
	if dirs := app.PackagesDirs(); len(dirs) > 0 {
		packs, _ := packcheck.Discover(dirs)
		return packs, nil
	}
	packs, err := app.LoadPacksIn(configDir(), "", workspace())
	if err != nil {
		return nil, failure(err)
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range packs {
		if info, err := os.Stat(p.PackRoot); err == nil && info.IsDir() && !seen[p.PackRoot] {
			seen[p.PackRoot] = true
			out = append(out, p.PackRoot)
		}
	}
	return out, nil
}

func printPackReports(reports []packcheck.Report) {
	for _, r := range reports {
		name := r.Name
		if name == "" {
			name = r.Root
		}
		switch {
		case r.Errors() > 0:
			ui.Failure("%s: %d error(s), %d warning(s) -- %d sets, %d SVGs", name, r.Errors(), r.Warnings(), r.Sets, r.SVGs)
		case r.Warnings() > 0:
			ui.Warning("%s: %d warning(s) -- %d sets, %d SVGs", name, r.Warnings(), r.Sets, r.SVGs)
		default:
			ui.Success("%s: %d sets, %d SVGs", name, r.Sets, r.SVGs)
		}
		for _, f := range r.Findings {
			where := ""
			if f.Path != "" {
				where = f.Path + ": "
			}
			if f.Level == packcheck.Error {
				ui.Failure("  %s%s", where, f.Text)
			} else {
				ui.Warning("  %s%s", where, f.Text)
			}
		}
	}
}

// writePackAnnotations emits one workflow command per finding, on its file
// relative to the repository the pack is (its root, in a pack's own job).
func writePackAnnotations(w io.Writer, reports []packcheck.Report) {
	prop := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C")
	body := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")
	for _, r := range reports {
		for _, f := range r.Findings {
			kind := "error"
			if f.Level == packcheck.Warn {
				kind = "warning"
			}
			props := "title=" + prop.Replace(r.Name+" "+f.Check)
			if f.Path != "" {
				props = "file=" + prop.Replace(f.Path) + "," + props
			}
			fmt.Fprintf(w, "::%s %s::%s\n", kind, props, body.Replace(f.Text))
		}
	}
}

func packSummaryMarkdown(reports []packcheck.Report, errs, warns int) string {
	var b strings.Builder
	b.WriteString("### Pack structure\n\n| Pack | Sets | SVGs | Errors | Warnings |\n|---|---|---|---|---|\n")
	for _, r := range reports {
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d |\n", cell(r.Name), r.Sets, r.SVGs, r.Errors(), r.Warnings())
	}
	fmt.Fprintf(&b, "\n%d pack(s): %d error(s), %d warning(s).\n\n", len(reports), errs, warns)
	return b.String()
}
