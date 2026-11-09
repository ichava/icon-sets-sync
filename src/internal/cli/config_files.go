package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/confighistory"
	"github.com/ichava/icon-sets-sync/src/internal/ui"
	"github.com/ichava/icon-sets-sync/src/internal/web"
)

// The config file commands are the web UI's config editor on the command
// line: the same engine lists, validates, fixes and saves, so a save from
// either keeps a copy in the same history.

func newConfigShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show [name]",
		Short: "List the config files, or print one",
		Long: `With no name, list every config file the toolkit reads -- the registry,
each pack config, the manifest-pack's set manifest and the global config --
and say where the global config was found and where it belongs. With a name
from that list ("global" for the global config), print the file.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng := configEngine()
			out := cmd.OutOrStdout()
			if len(args) == 1 {
				doc, err := readConfigDoc(eng, args[0])
				if err != nil {
					return err
				}
				if !doc.Exists {
					return failure(fmt.Errorf("%s does not exist yet (%s)", doc.Name, doc.Path))
				}
				_, err = out.Write(doc.Data)
				return err
			}
			files, err := eng.ConfigFiles()
			if err != nil {
				return failure(err)
			}
			if ui.JSONMode() {
				return ui.JSON(map[string]any{"files": files, "global": state.global})
			}
			rows := make([][]string, 0, len(files))
			for _, f := range files {
				rows = append(rows, []string{f.Name, f.Kind, f.Path})
			}
			fmt.Fprint(out, ui.Table([]string{"NAME", "KIND", "PATH"}, rows))
			g := state.global
			fmt.Fprintf(out, "\nglobal config: %s\n", orNone(g.Path))
			if g.Path != "" {
				fmt.Fprintf(out, "  chosen by:   %s\n", g.Layer)
			}
			fmt.Fprintf(out, "  belongs at:  %s\n", g.Default)
			fmt.Fprintf(out, "precedence, highest first: flags, ICHAVA_* environment, global config, pack settings, defaults\n")
			return nil
		},
	}
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func readConfigDoc(eng *web.AppEngine, name string) (web.ConfigDoc, error) {
	doc, err := eng.ReadConfig(name)
	if errors.Is(err, os.ErrNotExist) {
		return doc, usageErr("no config file named %q; `config show` lists them", name)
	}
	if err != nil {
		return doc, failure(err)
	}
	return doc, nil
}

// printFindings renders validation findings with their line and column.
func printFindings(name string, fs []app.Finding) int {
	problems := 0
	for _, f := range fs {
		where := name
		if f.Line > 0 {
			where = fmt.Sprintf("%s:%d:%d", name, f.Line, f.Col)
		}
		switch f.Level {
		case app.LevelError:
			problems++
			ui.Failure("%s: %s", where, f.Text)
		case app.LevelWarn:
			ui.Warning("%s: %s", where, f.Text)
		default:
			ui.Success("%s: %s", where, f.Text)
		}
	}
	return problems
}

// save writes through the engine and explains a refusal.
func save(eng *web.AppEngine, doc web.ConfigDoc, data []byte) error {
	err := eng.WriteConfig(doc.Name, data, doc.Rev)
	var invalid *web.ValidationError
	switch {
	case err == nil:
		ui.Success("saved %s; the previous copy is in the history (`config history %s`)", doc.Path, doc.Name)
		return nil
	case errors.As(err, &invalid):
		printFindings(doc.Name, invalid.Findings)
		return failure(fmt.Errorf("%s: %w", doc.Name, err))
	case errors.Is(err, web.ErrConflict):
		return failure(fmt.Errorf("%s changed on disk while this ran; nothing was written", doc.Path))
	}
	return failure(err)
}

func newConfigFixCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "fix <name>",
		Short: "Apply the safe repairs to a config file, after showing the diff",
		Long: `Apply the safe repairs to a config file: two-space formatting, deprecated
keys renamed (packages_dir becomes packages_dirs) and a $schema an editor can
complete against. Key order is kept. Broken JSON is reported, not guessed at;
YAML is left alone. Shows the diff, then asks; --yes answers.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng := configEngine()
			doc, err := readConfigDoc(eng, args[0])
			if err != nil {
				return err
			}
			if !doc.Exists {
				return failure(fmt.Errorf("%s does not exist yet; nothing to fix", doc.Path))
			}
			fixed, err := eng.FixConfig(doc.Name, doc.Data)
			if err != nil {
				return failure(err)
			}
			if bytes.Equal(fixed.Data, doc.Data) {
				ui.Success("%s: nothing to fix", doc.Name)
				return nil
			}
			for _, c := range fixed.Changes {
				ui.Info("%s", c)
			}
			fmt.Fprint(cmd.OutOrStdout(), confighistory.Unified(doc.Path, doc.Path+" (fixed)", string(doc.Data), string(fixed.Data)))
			if err := confirm(fmt.Sprintf("Write these changes to %s?", doc.Path)); err != nil {
				return err
			}
			return save(eng, doc, fixed.Data)
		},
	}
}

func newConfigHistoryCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "history <name>",
		Short: "List the copies kept before each save of a config file",
		Long: `List the copies of a config file kept before each save, newest first. They
live in $XDG_STATE_HOME/icon-sets-sync/history/ (else ~/.local/state/...);
history_keep sets how many are kept. ` + "`config diff`" + ` and ` + "`config restore`" + ` take an id from here.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng := configEngine()
			doc, err := readConfigDoc(eng, args[0])
			if err != nil {
				return err
			}
			entries, err := eng.ConfigHistory(doc.Name)
			if err != nil {
				return failure(err)
			}
			if ui.JSONMode() {
				return ui.JSON(entries)
			}
			if len(entries) == 0 {
				ui.Info("%s: no copies yet; one is kept the first time it is saved", doc.Name)
				return nil
			}
			rows := make([][]string, 0, len(entries))
			for _, e := range entries {
				rows = append(rows, []string{e.ID, e.Time.Local().Format("2006-01-02 15:04:05"), fmt.Sprintf("%d bytes", e.Size)})
			}
			fmt.Fprint(cmd.OutOrStdout(), ui.Table([]string{"ID", "SAVED", "SIZE"}, rows))
			return nil
		},
	}
}

// historyCopy reads one copy, the newest when id is empty.
func historyCopy(eng *web.AppEngine, name, id string) (string, []byte, error) {
	if id == "" {
		entries, err := eng.ConfigHistory(name)
		if err != nil {
			return "", nil, failure(err)
		}
		if len(entries) == 0 {
			return "", nil, failure(fmt.Errorf("%s has no copies in the history yet", name))
		}
		id = entries[0].ID
	}
	data, err := eng.ConfigHistoryRead(name, id)
	if err != nil {
		return "", nil, failure(err)
	}
	return id, data, nil
}

func newConfigDiffCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "diff <name> [id]",
		Short: "Show how a config file differs from a copy in its history (the newest by default)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng := configEngine()
			doc, err := readConfigDoc(eng, args[0])
			if err != nil {
				return err
			}
			id := ""
			if len(args) == 2 {
				id = args[1]
			}
			id, data, err := historyCopy(eng, doc.Name, id)
			if err != nil {
				return err
			}
			d := confighistory.Unified("now", "copy "+id, string(doc.Data), string(data))
			if d == "" {
				ui.Success("%s is identical to copy %s", doc.Name, id)
				return nil
			}
			fmt.Fprint(cmd.OutOrStdout(), d)
			return nil
		},
	}
}

func newConfigRestoreCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "restore <name> <id>",
		Short: "Write a copy from a config file's history back, after showing the diff",
		Long: `Write a copy from a config file's history back. It is validated like any
save, and the current text is kept in the history first, so a restore can be
undone. Shows the diff, then asks; --yes answers.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng := configEngine()
			doc, err := readConfigDoc(eng, args[0])
			if err != nil {
				return err
			}
			id, data, err := historyCopy(eng, doc.Name, args[1])
			if err != nil {
				return err
			}
			d := confighistory.Unified("now", "copy "+id, string(doc.Data), string(data))
			if d == "" {
				ui.Success("%s is already identical to copy %s", doc.Name, id)
				return nil
			}
			fmt.Fprint(cmd.OutOrStdout(), d)
			if err := confirm(fmt.Sprintf("Restore copy %s over %s?", id, doc.Path)); err != nil {
				return err
			}
			return save(eng, doc, data)
		},
	}
}

func newConfigEditCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "edit <name>",
		Short: "Edit a config file in $VISUAL or $EDITOR, validated before it is saved",
		Long: `Open a config file in $VISUAL, else $EDITOR (else vi, or notepad on Windows),
then validate what you saved and write it atomically, keeping the previous
copy in the history. A new global config opens on its $schema line. Needs a
terminal; in CI, write the file and run ` + "`config validate`" + `.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !ui.Interactive() {
				return usageErr("config edit needs a terminal; write the file and run `config validate` instead")
			}
			eng := configEngine()
			doc, err := readConfigDoc(eng, args[0])
			if err != nil {
				return err
			}
			if doc.ReadOnly {
				return failure(errors.New(doc.Why))
			}
			text := doc.Data
			if !doc.Exists && doc.Kind == web.KindGlobal {
				text = []byte(web.GlobalSeed())
			}
			edited, err := runEditor(doc.Name, text)
			if err != nil {
				return failure(err)
			}
			if bytes.Equal(edited, doc.Data) {
				ui.Info("%s: no change", doc.Name)
				return nil
			}
			if err := save(eng, doc, edited); err != nil {
				keep := filepath.Join(os.TempDir(), "icon-sets-sync-"+filepath.Base(doc.Path))
				if werr := os.WriteFile(keep, edited, 0o600); werr == nil {
					ui.Warning("your edit is kept at %s", keep)
				}
				return err
			}
			return nil
		},
	}
}

func runEditor(name string, data []byte) ([]byte, error) {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
		if runtime.GOOS == "windows" {
			editor = "notepad"
		}
	}
	f, err := os.CreateTemp("", "icon-sets-sync-*-"+filepath.Base(name))
	if err != nil {
		return nil, err
	}
	path := f.Name()
	defer func() { _ = os.Remove(path) }()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	parts := strings.Fields(editor)
	c := exec.Command(parts[0], append(parts[1:], path)...) //nolint:gosec // the user's own $EDITOR
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		return nil, fmt.Errorf("%s: %w", editor, err)
	}
	return os.ReadFile(path)
}

// validateNamed validates config files by their `config show` name, with line
// and column on each problem.
func validateNamed(names []string) error {
	eng := configEngine()
	problems := 0
	for _, n := range names {
		doc, err := readConfigDoc(eng, n)
		if err != nil {
			return err
		}
		problems += printFindings(doc.Name, eng.ValidateConfig(doc.Name, doc.Data))
	}
	return validationResult(problems)
}
