package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/ichava/icon-sets-sync/src/internal/core/docfile"
	"github.com/ichava/icon-sets-sync/src/internal/ui"
)

func newConfigMigrateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Copy a global config from its old location to ~/.config/icon-sets-sync",
		Long: `Copy a global config found at an old location (the OS config directory:
%AppData% on Windows, ~/Library/Application Support on macOS) to where it
belongs: $XDG_CONFIG_HOME/icon-sets-sync/config.json, else
~/.config/icon-sets-sync/config.json, on every OS.

It copies; the old file is left in place for you to delete. It refuses when a
config already exists at the new location. Asks first; --yes answers.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			loc := state.global
			if !loc.Legacy {
				if loc.Path == "" {
					ui.Info("no global config at an old location; nothing to migrate (it belongs at %s)", loc.Default)
				} else {
					ui.Info("the global config is already at %s; nothing to migrate", loc.Path)
				}
				return nil
			}
			dest := loc.Default
			if filepath.Ext(loc.Path) != ".json" {
				dest = filepath.Join(filepath.Dir(loc.Default), "config"+filepath.Ext(loc.Path))
			}
			if _, err := os.Stat(dest); err == nil {
				return failure(fmt.Errorf("%s already exists; not overwriting it", dest))
			} else if !errors.Is(err, os.ErrNotExist) {
				return failure(err)
			}
			if err := confirm(fmt.Sprintf("Copy %s to %s?", loc.Path, dest)); err != nil {
				return err
			}
			data, err := os.ReadFile(loc.Path)
			if err != nil {
				return failure(err)
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return failure(err)
			}
			if err := docfile.WriteAtomic(dest, data); err != nil {
				return failure(err)
			}
			ui.Success("copied %s to %s", loc.Path, dest)
			ui.Info("the old file is still at %s; delete it once you are happy", loc.Path)
			return nil
		},
	}
}
