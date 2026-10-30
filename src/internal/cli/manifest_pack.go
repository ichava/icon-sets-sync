package cli

import (
	"github.com/spf13/cobra"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/manifestpack"
	"github.com/ichava/icon-sets-sync/src/internal/ui"
)

// newManifestPackCommand groups the aggregator pack's own tooling.
//
// An aggregator pack is not like the other packs. It vendors many sets, each
// from a different upstream in that upstream's own SVG dialect, and nothing
// records where any of them came from. Refreshing it therefore needs a
// per-set manifest that does not exist, and `audit` is what builds one.
func newManifestPackCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manifest-pack",
		Short: "Tooling for the aggregator pack",
	}
	cmd.AddCommand(newManifestPackAuditCommand())
	return cmd
}

// newManifestPackAuditCommand builds `audit`.
func newManifestPackAuditCommand() *cobra.Command {
	var (
		only     string
		pkg      string
		workDir  string
		parallel int
	)

	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Identify, by measurement, which upstream each vendored set came from",
		Long: `Identify which upstream each vendored set came from.

For every set it fetches candidate npm packages, scores every directory in them
against the committed icons, and reports what it found. Nothing is accepted on
the strength of a name: a candidate is only believed when the bytes it ships
match the bytes already committed.

  exact    every committed icon reproduced byte for byte
  strong   at least 80% reproduced; the right upstream, slightly moved on
  package    at least 90% present by name but no byte matches -- upstream
             identified, version drifted; pin a version. The floor is the same
             one the refresh's retention guard uses, so this grades in what a
             refresh would actually accept
  ambiguous  the right package, matched equally well by two or more sibling
             directories, with no byte match to separate them -- variant trees
             sharing filenames. Pick the variant by hand
  partial  some names line up -- may be a different project entirely
  none     nothing matched

Writes a manifest to stdout with --json, which is the input a per-set refresh
needs.

For a set the name patterns cannot reach, supply the package yourself and let
the tool answer whether its bytes are the ones already committed:

  imt manifest-pack audit --set phosphor-icons --package @phosphor-icons/core`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The survey itself is app.AuditManifestPack, which the web UI
			// runs too. This command maps its errors and renders its results.
			results, err := app.AuditManifestPack(cmd.Context(), app.AuditOptions{
				ConfigDir: configDir(),
				Workspace: workspace(),
				Set:       only,
				Package:   pkg,
				WorkDir:   workDir,
				Parallel:  parallel,
				Starting: func(sets int, filesRoot string) {
					ui.Info("auditing %d sets from %s", sets, filesRoot)
				},
			})
			if err != nil {
				if app.IsUsage(err) {
					return usageErr("%s", err)
				}
				return failure(err)
			}

			if ui.JSONMode() {
				return ui.JSON(results)
			}

			ui.Printf("%s", renderAudit(results))
			summary := manifestpack.Summary(results)
			ui.Info("%d exact, %d strong, %d upstream-identified, %d partial, %d unresolved",
				summary[manifestpack.ConfidenceExact], summary[manifestpack.ConfidenceStrong],
				summary[manifestpack.ConfidencePackage], summary[manifestpack.ConfidencePartial],
				summary[manifestpack.ConfidenceNone])

			// Deliberately exits 0 even with unresolved sets. This is a survey,
			// not a gate: the unresolved ones are work for a person, and a
			// non-zero exit would make it look like the audit itself failed.
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&only, "set", "", "audit one set")
	f.StringVar(&pkg, "package", "", "verify this npm package against --set, instead of guessing")
	f.StringVar(&workDir, "work-dir", "", "keep downloads here instead of a temporary directory")
	f.IntVar(&parallel, "parallel", 0, "concurrent sets (default: CPU count)")
	return cmd
}

func renderAudit(results []manifestpack.Resolution) string {
	rows := make([][]string, 0, len(results))
	for _, r := range results {
		var badge string
		switch r.Confidence {
		case manifestpack.ConfidenceExact:
			badge = ui.OK(ui.Sym.Check + " exact")
		case manifestpack.ConfidenceStrong:
			badge = ui.OK(ui.Sym.Check + " strong")
		case manifestpack.ConfidencePackage:
			badge = ui.Warn(ui.Sym.Info + " package")
		case manifestpack.ConfidencePartial:
			badge = ui.Warn(ui.Sym.Warn + " partial")
		default:
			badge = ui.Err(ui.Sym.Cross + " none")
		}

		pkg := r.Package
		if pkg != "" && r.Version != "" {
			pkg += "@" + r.Version
		}
		if pkg == "" {
			pkg = "-"
		}
		path := r.Path
		if path == "" || path == "." {
			path = "-"
		}

		rows = append(rows, []string{
			r.Set,
			itoa(r.Icons),
			badge,
			itoa(r.Identical) + "/" + itoa(r.Icons),
			pkg,
			path,
		})
	}
	return ui.Table([]string{"Set", "Icons", "Confidence", "Identical", "Package", "Path"}, rows)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
