// Package version holds the build metadata stamped in at link time.
package version

import "runtime/debug"

// These are set with -ldflags -X on a release build. The defaults are what a
// `go build` or `go run` from a working tree reports.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

// Version returns the release version.
//
// When it was not stamped -- a `go install ...@latest` rather than a GoReleaser
// build -- the module's own version from the build info is used, so the binary
// still reports something true rather than "dev".
func Version() string {
	if version != "dev" && version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

// Commit returns the commit the binary was built from, falling back to the
// VCS stamp the Go toolchain embeds.
func Commit() string {
	if commit != "" {
		return commit
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				return s.Value
			}
		}
	}
	return ""
}

// Date returns the build date, falling back to the VCS timestamp.
func Date() string {
	if date != "" {
		return date
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.time" {
				return s.Value
			}
		}
	}
	return ""
}

// Name is the primary binary name, and the one the Docker ENTRYPOINT uses.
//
// It names what the tool does -- sync icon sets from upstream -- rather than
// the repository it happened to live in. The repository is being renamed to
// match (ichava/maintainer-toolkit -> ichava/icon-sets-sync); the Go module
// path is left alone until that rename lands, so nothing importing it breaks.
const Name = "icon-sets-sync"

// ShortName is the alias for interactive use, kept from the first Go release.
const ShortName = "imt"

// LegacyName is the console script the Python package installed. The Docker
// image keeps it as a symlink so a script or Makefile that called it by name
// keeps working; it is deprecated and prints a notice when invoked as itself.
const LegacyName = "ichava-maintainer-toolkit"
