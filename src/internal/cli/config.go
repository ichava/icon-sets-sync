package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"

	"github.com/ichava/maintainer-toolkit/src/internal/app"
	"github.com/ichava/maintainer-toolkit/src/internal/core/bundled"
	"github.com/ichava/maintainer-toolkit/src/internal/core/config"
	"github.com/ichava/maintainer-toolkit/src/internal/core/recipes"
	"github.com/ichava/maintainer-toolkit/src/internal/core/settings"
	"github.com/ichava/maintainer-toolkit/src/internal/ui"
)

func newConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Validate the configs, or print the effective settings",
	}
	cmd.AddCommand(newConfigValidateCommand(), newConfigPrintCommand())
	return cmd
}

// ------------------------------------------------------------ config validate

func newConfigValidateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "validate [pack-config-file...]",
		Short: "Validate pack configs against config/schema/pack.schema.json",
		Long: `Validate pack configs against config/schema/pack.schema.json.

With no arguments, validates the registry, every pack it lists (schema, pack
settings, whether a recipe can run it, the bundled set manifest), and the
global config. With file arguments, validates just those pack configs.

Exits 1 if anything is invalid.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var problems int
			ok := func(format string, a ...any) { ui.Success(format, a...) }
			bad := func(format string, a ...any) {
				problems++
				ui.Failure(format, a...)
			}

			if len(args) > 0 {
				for _, path := range args {
					if _, err := config.LoadPackFile(path); err != nil {
						bad("%s", err)
						continue
					}
					ok("%s", path)
				}
				return validationResult(problems)
			}

			if state.globalPath != "" {
				// Already loaded by the pre-run, which fails on a bad file, so
				// reaching here means it parsed and every key was known.
				ok("%s (global config)", state.globalPath)
			}

			dir := configDir()
			registry, err := config.LoadRegistry(dir)
			if err != nil {
				bad("%s", err)
				return validationResult(problems)
			}
			ok("registry: %d packs in %s", len(registry.Packs), dir)

			for _, slug := range registry.Packs {
				validatePack(dir, slug, ok, bad)
			}
			return validationResult(problems)
		},
	}
}

func validatePack(dir, slug string, ok, bad func(string, ...any)) {
	path, err := config.PackFile(slug, dir)
	if err != nil {
		bad("%s", err)
		return
	}
	pack, err := config.LoadPackFile(path)
	if err != nil {
		bad("%s", err)
		return
	}
	if pack.Name != slug {
		bad("%s: name %q does not match its registry slug %q", path, pack.Name, slug)
		return
	}
	if _, err := app.PackSettings(state.stack, pack); err != nil {
		bad("%s: %s", path, err)
		return
	}
	recipe := recipes.Select(pack)
	if recipe == "" {
		bad("%s: no recipe can run source.type=%q; set `recipe`", path, pack.Source.Type)
		return
	}
	if recipe == recipes.RecipeBundled {
		if _, err := bundled.LoadManifest(dir, pack.Name); err != nil {
			bad("%s: %s", path, err)
			return
		}
	}
	if len(pack.Sinks) == 0 {
		ui.Warning("%s: declares no sinks; sync will refuse it outside --dry-run", path)
	}
	ok("%s (recipe %s)", path, recipe)
}

func validationResult(problems int) error {
	if problems > 0 {
		return failure(fmt.Errorf("%d config problem(s)", problems))
	}
	return nil
}

// --------------------------------------------------------------- config print

func newConfigPrintCommand() *cobra.Command {
	var (
		pack    string
		format  string
		origins bool
	)
	cmd := &cobra.Command{
		Use:   "print",
		Short: "Print the effective merged settings (and a pack's config with --pack)",
		Long: `Print the effective merged settings.

Settings resolve flags > ICHAVA_* environment > global config > pack config >
defaults. With --pack, the pack's own settings block is merged in and the
loaded pack config is printed too. --origins says which layer supplied each
value.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolved := state.run
			doc := map[string]any{
				"config_dir":    configDir(),
				"global_config": nilIfEmpty(state.globalPath),
			}

			if pack != "" {
				packs, err := app.LoadPacksIn(configDir(), pack, workspace())
				if err != nil {
					return failure(err)
				}
				p := packs[0]
				r, err := app.PackSettings(state.stack, p)
				if err != nil {
					return failure(err)
				}
				resolved = r
				doc["pack_file"] = p.File
				doc["recipe"] = recipes.Select(p)
				doc["pack"] = p
			}

			doc["settings"] = resolved.Tree()
			if origins {
				doc["origins"] = resolved.Origins()
			}
			return printDocument(cmd.OutOrStdout(), doc, format)
		},
	}
	f := cmd.Flags()
	f.StringVar(&pack, "pack", "", "merge in this pack's settings and print its config")
	f.StringVar(&format, "format", "json", "output format: json or yaml")
	f.BoolVar(&origins, "origins", false, "show which layer each setting came from ("+layerList()+")")
	return cmd
}

func layerList() string {
	out := ""
	for i := len(settings.Precedence) - 1; i >= 0; i-- {
		if out != "" {
			out += " > "
		}
		out += string(settings.Precedence[i])
	}
	return out
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// printDocument writes doc as JSON or YAML. YAML goes through JSON first so
// the field names are the json tags rather than Go's exported names.
func printDocument(w io.Writer, doc any, format string) error {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return failure(err)
	}
	switch format {
	case "json":
		_, err = fmt.Fprintf(w, "%s\n", data)
	case "yaml", "yml":
		var generic any
		if err := json.Unmarshal(data, &generic); err != nil {
			return failure(err)
		}
		out, merr := yaml.Marshal(generic)
		if merr != nil {
			return failure(merr)
		}
		_, err = w.Write(out)
	default:
		return usageErr("--format %q: want json or yaml", format)
	}
	if err != nil {
		return failure(err)
	}
	return nil
}
