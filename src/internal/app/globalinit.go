package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/docfile"
	"github.com/ichava/icon-sets-sync/src/internal/core/packcheck"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
)

// GlobalInit is the global config a machine starts from: the $schema line
// and the settings that say where this machine keeps things, found rather
// than guessed. Everything else stays at its default, so a later default
// change still reaches it.
type GlobalInit struct {
	Data []byte
	// Notes say, in words, what was found and what was left unset.
	Notes []string
}

// initDoc fixes the key order of a generated config.
type initDoc struct {
	Schema           string   `json:"$schema"`
	PackagesDirs     []string `json:"packages_dirs,omitempty"`
	PrivateConfigDir string   `json:"private_config_dir,omitempty"`
}

// InitialGlobal builds the global config for this machine: packages_dirs is
// the directory holding this checkout, when it holds ichava packs;
// private_config_dir is the one pack checkout carrying a private config,
// when exactly one does.
func InitialGlobal(workspace string) GlobalInit {
	var roots []string
	cwd, _ := os.Getwd()
	for _, start := range []string{cwd, config.ExecutableDir()} {
		if start == "" {
			continue
		}
		if root := config.FindToolkitRoot(start); root != "" {
			roots = append(roots, filepath.Dir(root))
		}
	}
	if workspace != "" {
		roots = append(roots, workspace)
	}
	return initialGlobalFrom(roots, nil)
}

// initialGlobalFrom is InitialGlobal over given candidate roots. extra are
// private config candidates found some other way.
func initialGlobalFrom(roots, extra []string) GlobalInit {
	doc := initDoc{Schema: settings.GlobalSchemaURL}
	var notes []string
	seen := map[string]bool{}
	for _, r := range roots {
		abs, err := filepath.Abs(r)
		if err != nil || seen[abs] {
			continue
		}
		seen[abs] = true
		if packs, _ := packcheck.Discover([]string{abs}); len(packs) > 0 {
			doc.PackagesDirs = append(doc.PackagesDirs, abs)
			notes = append(notes, fmt.Sprintf("packages_dirs: %s holds %d ichava pack(s)", abs, len(packs)))
		}
	}
	if len(doc.PackagesDirs) == 0 {
		notes = append(notes, "packages_dirs: no directory of ichava pack checkouts found beside this tool; add one on the Settings page")
	}
	privates := append(privateConfigCandidatesIn(doc.PackagesDirs), extra...)
	switch len(privates) {
	case 0:
		notes = append(notes, "private_config_dir: no pack checkout carries a private config")
	case 1:
		doc.PrivateConfigDir = privates[0]
		notes = append(notes, "private_config_dir: "+privates[0])
	default:
		notes = append(notes, fmt.Sprintf("private_config_dir: %d candidates (%s); choose one on the Manifest pack audit page",
			len(privates), strings.Join(privates, ", ")))
	}
	data, _ := json.MarshalIndent(doc, "", "  ")
	return GlobalInit{Data: append(data, '\n'), Notes: notes}
}

// GlobalState is how a global config file stands against the generator.
type GlobalState string

// The states. Only an edited file is left alone by a start.
const (
	GlobalMissing   GlobalState = "missing"
	GlobalEmpty     GlobalState = "empty"     // nothing but $schema (or blank)
	GlobalGenerated GlobalState = "generated" // byte for byte what was last generated
	GlobalEdited    GlobalState = "edited"    // anything else: a person's file
)

// ClassifyGlobal says how the global config stands. A file that is exactly
// what the generator last wrote (rec) is still the generator's; one with
// nothing in it but $schema has nothing to lose; anything else a person made.
func ClassifyGlobal(data []byte, exists bool, rec InitRecord) GlobalState {
	switch {
	case !exists:
		return GlobalMissing
	case len(bytes.TrimSpace(data)) == 0:
		return GlobalEmpty
	case rec.SHA256 != "" && rec.SHA256 == sha(data):
		return GlobalGenerated
	}
	var tree map[string]json.RawMessage
	if json.Unmarshal(data, &tree) != nil {
		return GlobalEdited
	}
	for k := range tree {
		if !strings.HasPrefix(k, "$") {
			return GlobalEdited
		}
	}
	return GlobalEmpty
}

// InitRecord remembers what the generator last wrote, so a start can tell
// its own file from one a person edited.
type InitRecord struct {
	SHA256 string    `json:"sha256"`
	Time   time.Time `json:"time"`
}

// InitRecordPath is $XDG_STATE_HOME/icon-sets-sync/global-init.json (else
// ~/.local/state/...), beside the config history.
func InitRecordPath(historyDir string) string {
	if historyDir == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(historyDir), "global-init.json")
}

// ReadInitRecord reads the record; a missing or unreadable one is empty.
func ReadInitRecord(path string) InitRecord {
	var r InitRecord
	if path == "" {
		return r
	}
	if data, err := os.ReadFile(path); err == nil { //nolint:gosec // our own state file
		_ = json.Unmarshal(data, &r)
	}
	return r
}

// WriteInitRecord records data as what the generator wrote.
func WriteInitRecord(path string, data []byte) error {
	if path == "" {
		return nil
	}
	b, _ := json.MarshalIndent(InitRecord{SHA256: sha(data), Time: time.Now().UTC()}, "", "  ")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return docfile.WriteAtomic(path, append(b, '\n'))
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
