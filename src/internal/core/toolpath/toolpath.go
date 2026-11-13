// Package toolpath finds the external programs a sync runs (git, gh, and npm
// for its fallback) when the process's PATH does not hold them.
//
// A server started from Finder, launchd or a desktop shortcut gets a minimal
// PATH with none of Homebrew, nvm, Volta or fnm on it, so a tool that is
// installed is still "not found" by exec.LookPath alone. That is how a tabler
// sync failed with "npm not on PATH" on a machine with npm installed.
package toolpath

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// Dirs lists the usual install directories, searched after PATH, newest node
// first where a version manager keeps several. A var so tests can replace it.
var Dirs = defaultDirs

// LookPath is exec.LookPath, then Dirs. It returns the path and whether it
// came from PATH.
func LookPath(name string) (string, bool, error) {
	if p, err := exec.LookPath(name); err == nil {
		return p, true, nil
	}
	for _, dir := range Dirs() {
		for _, n := range Names(name) {
			if p := filepath.Join(dir, n); IsExecutable(p) {
				return p, false, nil
			}
		}
	}
	return "", false, exec.ErrNotFound
}

// Names is the file names name has on this OS.
func Names(name string) []string {
	if runtime.GOOS == "windows" {
		return []string{name + ".exe", name + ".cmd"}
	}
	return []string{name}
}

// IsExecutable reports whether p is a file this process could run.
func IsExecutable(p string) bool {
	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode()&0o111 != 0
}

func defaultDirs() []string {
	home, _ := os.UserHomeDir()
	var dirs []string
	if runtime.GOOS == "windows" {
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
			if v := os.Getenv(env); v != "" {
				dirs = append(dirs, filepath.Join(v, "nodejs"), filepath.Join(v, "Git", "cmd"), filepath.Join(v, "GitHub CLI"))
			}
		}
		if v := os.Getenv("APPDATA"); v != "" {
			dirs = append(dirs, filepath.Join(v, "npm"))
		}
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			dirs = append(dirs, filepath.Join(v, "Volta", "bin"))
		}
		if v := os.Getenv("NVM_SYMLINK"); v != "" {
			dirs = append(dirs, v)
		}
		return dirs
	}
	dirs = append(dirs, "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin")
	if home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".volta", "bin"),
			filepath.Join(home, ".local", "share", "fnm", "aliases", "default", "bin"),
			filepath.Join(home, ".fnm", "aliases", "default", "bin"),
			filepath.Join(home, ".asdf", "shims"),
			filepath.Join(home, ".local", "bin"),
		)
		dirs = append(dirs, NewestFirst(filepath.Join(home, ".nvm", "versions", "node", "*", "bin"))...)
	}
	return dirs
}

// NewestFirst globs pattern and orders the matches by the version in their
// path, newest first, so v22 beats v9.
func NewestFirst(pattern string) []string {
	matches, _ := filepath.Glob(pattern)
	key := func(p string) []int {
		v := strings.TrimPrefix(filepath.Base(filepath.Dir(p)), "v")
		var out []int
		for _, part := range strings.Split(v, ".") {
			n, _ := strconv.Atoi(part)
			out = append(out, n)
		}
		return out
	}
	sort.Slice(matches, func(i, j int) bool {
		a, b := key(matches[i]), key(matches[j])
		for k := 0; k < len(a) && k < len(b); k++ {
			if a[k] != b[k] {
				return a[k] > b[k]
			}
		}
		return len(a) > len(b)
	})
	return matches
}
