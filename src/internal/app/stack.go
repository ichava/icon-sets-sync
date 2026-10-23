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
	lookup := in.Lookup
	if lookup == nil {
		lookup = os.LookupEnv
	}

	path := in.GlobalPath
	if path == "" {
		found, err := settings.GlobalConfigPath(lookup)
		if err != nil {
			return settings.Stack{}, "", err
		}
		path = found
	}
	global, err := settings.LoadGlobal(path)
	if err != nil {
		return settings.Stack{}, "", err
	}

	envValues, err := settings.FromEnv(lookup)
	if err != nil {
		return settings.Stack{}, "", usage(err)
	}

	flagValues, err := settings.FromAssignments(in.Assignments)
	if err != nil {
		return settings.Stack{}, "", usage(err)
	}
	if in.Overlay != nil {
		if err := in.Overlay(flagValues); err != nil {
			return settings.Stack{}, "", usage(err)
		}
	}

	return settings.Stack{Global: global, Env: envValues, Flags: flagValues}, path, nil
}

// errf is fmt.Errorf, named for the call sites that build a usage error.
func errf(format string, a ...any) error { return fmt.Errorf(format, a...) }
