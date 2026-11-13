package app

import (
	"errors"
	"fmt"
	"os"

	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
)

// UsageError marks an error the caller made in what it asked for, as opposed
// to one the machine or a file produced. The CLI maps it onto exit code 2;
// the web UI onto a 400. Keeping the distinction here means neither front-end
// has to guess it from a message.
type UsageError struct{ Err error }

func (e *UsageError) Error() string { return e.Err.Error() }
func (e *UsageError) Unwrap() error { return e.Err }

// IsUsage reports whether err is, or wraps, a UsageError.
func IsUsage(err error) bool {
	var u *UsageError
	return errors.As(err, &u)
}

func usage(err error) error { return &UsageError{Err: err} }

// StackInput is what BuildStack needs to assemble the run-wide settings.
type StackInput struct {
	// GlobalPath is an explicit global config file. Empty means discover it
	// the way GlobalConfigPath does (ICHAVA_GLOBAL_CONFIG, then XDG).
	GlobalPath string

	// Lookup reads the environment. Nil means os.LookupEnv. A server passes
	// its own so a test can run it against a controlled environment.
	Lookup func(string) (string, bool)

	// Assignments are --set key=value pairs, the start of the flag layer.
	Assignments []string

	// Overlay applies the dedicated flags (--dry-run, --concurrency, a web
	// form's toggles) on top of the assignments. It runs last, so a
	// dedicated flag beats a --set of the same key, exactly as on the
	// command line. An error it returns is a usage error. May be nil.
	Overlay func(flags settings.Values) error
}

// BuildStack resolves the settings layers that do not vary by pack: the
// global config file, the environment, and the flag layer.
//
// It is the body of the CLI's resolveState, moved here so the web UI resolves
// precedence through exactly the same code. Errors in the environment, the
// assignments or the overlay are *UsageError; a missing or malformed global
// config is a plain error.
func BuildStack(in StackInput) (settings.Stack, string, error) {
	stack, loc, err := BuildStackLocated(in)
	return stack, loc.Path, err
}

// BuildStackLocated is BuildStack, also returning where the global config was
// found and where it belongs, so a front-end can explain it and warn about a
// file at a legacy location.
func BuildStackLocated(in StackInput) (settings.Stack, settings.GlobalLocation, error) {
	lookup := in.Lookup
	if lookup == nil {
		lookup = os.LookupEnv
	}

	loc, err := settings.FindGlobalConfig(lookup, settings.LegacyConfigDirs())
	if err != nil && in.GlobalPath == "" {
		return settings.Stack{}, loc, err
	}
	if in.GlobalPath != "" {
		loc.Path, loc.Layer, loc.Legacy = in.GlobalPath, "--global-config", false
	}
	path := loc.Path
	global, err := settings.LoadGlobal(path)
	if err != nil {
		// Every command fails here on a bad global config, so say where.
		if data, rerr := os.ReadFile(path); rerr == nil {
			if line, col := Locate(data, err); line > 0 {
				err = fmt.Errorf("%w (at %s:%d:%d; `config fix global` or the web UI's editor can help)", err, path, line, col)
			}
		}
		return settings.Stack{}, loc, err
	}

	envValues, err := settings.FromEnv(lookup)
	if err != nil {
		return settings.Stack{}, loc, usage(err)
	}

	flagValues, err := settings.FromAssignments(in.Assignments)
	if err != nil {
		return settings.Stack{}, loc, usage(err)
	}
	if in.Overlay != nil {
		if err := in.Overlay(flagValues); err != nil {
			return settings.Stack{}, loc, usage(err)
		}
	}

	return settings.Stack{Global: global, Env: envValues, Flags: flagValues}, loc, nil
}

// errf is fmt.Errorf, named for the call sites that build a usage error.
func errf(format string, a ...any) error { return fmt.Errorf(format, a...) }
