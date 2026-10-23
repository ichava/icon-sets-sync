// Package config loads the per-pack JSON configs and the registry that lists
// them, and reads and writes the version a pack repo says it vendors.
//
// The config directory is a mounted volume in production -- each pack's
// sync-upstream.yml bind-mounts the toolkit checkout's config/ at /app/config --
// so this schema is an external interface, not an internal one. It is ported
// from the Pydantic models in core/config.py and must accept every file those
// models accept, unchanged.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/core/docfile"
)

// SourceConfig says where to fetch upstream artefacts from.
//
// Unknown keys are kept rather than rejected, and that is load-bearing: the
// checker reads `version_field` out of them for `type: "url"`, and both shipped
// url-typed packs (icon-sets-bundled, icon-sets-emoji) rely on it. Python
// expressed this as `extra="allow"`.
type SourceConfig struct {
	Type       string   `json:"type"`
	Package    string   `json:"package,omitempty"`
	Owner      string   `json:"owner,omitempty"`
	Repo       string   `json:"repo,omitempty"`
	ArchiveURL string   `json:"archive_url,omitempty"`
	SourcePath string   `json:"source_path,omitempty"`
	Path       string   `json:"path,omitempty"`
	Args       []string `json:"args,omitempty"`

	// Extra holds keys with no field above. See the type comment.
	Extra map[string]any `json:"-"`
}

// SourceTypes is the closed set Python declared as a Literal. A config naming
// anything else is rejected at load time, before any network call.
var SourceTypes = []string{"npm", "github-archive", "github-tag", "github-release", "url", "script"}

// VersionField returns the dot-path into the version-check response for a
// url-typed source, defaulting to "version" exactly as the checker does.
func (s SourceConfig) VersionField() string {
	if v, ok := s.Extra["version_field"].(string); ok && v != "" {
		return v
	}
	return "version"
}

type sourceAlias SourceConfig

func (s *SourceConfig) UnmarshalJSON(data []byte) error {
	var alias sourceAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	*s = SourceConfig(alias)

	var all map[string]any
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	known := map[string]bool{
		"type": true, "package": true, "owner": true, "repo": true,
		"archive_url": true, "source_path": true, "path": true, "args": true,
	}
	for k, v := range all {
		if !known[k] {
			if s.Extra == nil {
				s.Extra = map[string]any{}
			}
			s.Extra[k] = v
		}
	}

	for _, t := range SourceTypes {
		if s.Type == t {
			return nil
		}
	}
	return fmt.Errorf("source: unknown type %q (want one of %s)", s.Type, strings.Join(SourceTypes, ", "))
}

// MarshalJSON writes the extras back alongside the known fields, so
// `config print` shows the source block as the file declared it.
func (s SourceConfig) MarshalJSON() ([]byte, error) {
	return marshalWithExtra(sourceAlias(s), s.Extra)
}

// SinkConfig says where refreshed assets land.
type SinkConfig struct {
	Type string `json:"type"`

	// filesystem
	Root         string  `json:"root,omitempty"`
	Incremental  bool    `json:"incremental,omitempty"`
	MinRetention float64 `json:"min_retention,omitempty"`

	// git-branch. Each of these is a pack-level value for the matching
	// git.* setting: it wins over the default and gives way to the global
	// config, the environment and flags.
	RepoRoot      string `json:"repo_root,omitempty"`
	Branch        string `json:"branch,omitempty"`
	CommitSubject string `json:"commit_subject,omitempty"`
	CommitBody    string `json:"commit_body,omitempty"`
	Base          string `json:"base,omitempty"`
	OpenPR        *bool  `json:"open_pr,omitempty"`

	Extra map[string]any `json:"-"`
}

// SinkTypes is the closed set Python declared as a Literal.
var SinkTypes = []string{"filesystem", "git-branch"}

type sinkAlias SinkConfig

func (s *SinkConfig) UnmarshalJSON(data []byte) error {
	var alias sinkAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	*s = SinkConfig(alias)

	var all map[string]any
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	known := map[string]bool{
		"type": true, "root": true, "incremental": true, "min_retention": true,
		"repo_root": true, "branch": true, "commit_subject": true, "commit_body": true,
		"base": true, "open_pr": true,
	}
	for k, v := range all {
		if !known[k] {
			if s.Extra == nil {
				s.Extra = map[string]any{}
			}
			s.Extra[k] = v
		}
	}

	for _, t := range SinkTypes {
		if s.Type == t {
			return nil
		}
	}
	return fmt.Errorf("sink: unknown type %q (want one of %s)", s.Type, strings.Join(SinkTypes, ", "))
}

// MarshalJSON writes the extras back alongside the known fields.
func (s SinkConfig) MarshalJSON() ([]byte, error) {
	return marshalWithExtra(sinkAlias(s), s.Extra)
}

func marshalWithExtra(known any, extra map[string]any) ([]byte, error) {
	data, err := json.Marshal(known)
	if err != nil || len(extra) == 0 {
		return data, err
	}
	var merged map[string]any
	if err := json.Unmarshal(data, &merged); err != nil {
		return nil, err
	}
	for k, v := range extra {
		if _, taken := merged[k]; !taken {
			merged[k] = v
		}
	}
	return json.Marshal(merged)
}

// PackConfig is one config/<slug>.json.
//
// Unlike the source and sink blocks this rejects unknown keys, so a typo fails
// loudly instead of being silently ignored. Python expressed that as
// `extra="forbid"`, which is also why the comment-key strip below has to happen
// before validation rather than after.
type PackConfig struct {
	Name            string           `json:"name"`
	Pack            string           `json:"pack"`
	PackRoot        string           `json:"pack_root"`
	CurrentVersion  string           `json:"current_version,omitempty"`
	VersionFile     string           `json:"version_file,omitempty"`
	VersionKeys     []string         `json:"version_keys,omitempty"`
	VersionCheckURL string           `json:"version_check_url"`
	Source          SourceConfig     `json:"source"`
	Sinks           []SinkConfig     `json:"sinks,omitempty"`
	Transforms      []map[string]any `json:"transforms,omitempty"`

	// Recipe picks the pipeline builder; empty and "auto" dispatch by pack
	// name, then by source type, exactly as before the field existed.
	Recipe string `json:"recipe,omitempty"`

	// Settings is the pack layer of the settings precedence. It is kept as a
	// raw tree here and resolved by internal/core/settings, which owns the
	// key registry; the schema has already checked its shape.
	Settings map[string]any `json:"settings,omitempty"`

	// File is where the config was loaded from. Not part of the document.
	File string `json:"-"`
}

// DefaultVersionFile and DefaultVersionKeys mirror the Python defaults. No
// shipped config overrides either, so changing them changes every pack at once.
const DefaultVersionFile = "resources/assets/svg/config.json"

// DefaultVersionKeys is returned by value; callers must not retain the slice.
func DefaultVersionKeys() []string {
	return []string{"upstream.current_version", "package.upstream_version"}
}

// PacksRegistry is config/packs.json.
type PacksRegistry struct {
	Packs []string `json:"packs"`
}

// DefaultConfigDir resolves the directory the pack configs live in.
//
// ICHAVA_CONFIG_DIR wins (ICHAVA_DEV_CONFIG_DIR is its older name), which is
// how the Docker image points at the bind-mounted /app/config. Otherwise it is `config/` beside the executable,
// falling back to ./config -- Python derived this from the source file's
// location, which has no equivalent in a compiled binary.
func DefaultConfigDir() string {
	for _, env := range []string{"ICHAVA_CONFIG_DIR", "ICHAVA_DEV_CONFIG_DIR"} {
		if dir := os.Getenv(env); dir != "" {
			return dir
		}
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "config")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return "config"
}

// stripCommentKeys drops top-level keys beginning with an underscore, and the
// `$schema` editor hint.
//
// JSON has no comments, so the shipped configs carry `_note`, `_version_note`
// and `_status` as authoring annotations. They have to go before the
// unknown-key check, or every shipped config fails to load. Top level only,
// matching Python -- an underscore key inside `source` is still an error.
func stripCommentKeys(data []byte) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	for k := range raw {
		if strings.HasPrefix(k, "_") || k == "$schema" {
			delete(raw, k)
		}
	}
	return json.Marshal(raw)
}

// PackFile returns the path of a pack's config: <slug>.json, .yaml or .yml.
func PackFile(slug, configDir string) (string, error) {
	if configDir == "" {
		configDir = DefaultConfigDir()
	}
	path, err := docfile.Find(configDir, slug)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("missing pack config: %s", filepath.Join(configDir, slug+".json"))
	}
	return path, err
}

// LoadPack reads and validates one pack config, in JSON or YAML.
func LoadPack(slug, configDir string) (*PackConfig, error) {
	path, err := PackFile(slug, configDir)
	if err != nil {
		return nil, err
	}
	return LoadPackFile(path)
}

// LoadPackFile reads, schema-validates and decodes one pack config.
//
// Validation runs against the whole document before anything is decoded, so
// a file with three mistakes reports all three rather than the first one the
// decoder trips on. The strict decode after it is a second gate, and catches
// a schema that has drifted from the Go types.
func LoadPackFile(path string) (*PackConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("missing pack config: %s", path)
		}
		return nil, err
	}
	return ParsePackBytes(path, data)
}

// ParsePackBytes is LoadPackFile on bytes already in hand: it validates and
// decodes them exactly as if they had been read from path. The path decides
// the format (JSON, or YAML by extension), prefixes every error, and is
// recorded as the pack's File.
//
// It exists for an editor, which has to validate the exact bytes it is about
// to write through the same loader that will later read them. Re-marshalling
// a decoded PackConfig instead would silently drop every `_note` annotation,
// because loading strips them.
func ParsePackBytes(path string, data []byte) (*PackConfig, error) {
	canonical, err := docfile.ToJSON(path, data)
	if err != nil {
		return nil, err
	}
	if err := ValidatePackDocument(canonical); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	cleaned, err := stripCommentKeys(canonical)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	var pack PackConfig
	dec := json.NewDecoder(strings.NewReader(string(cleaned)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&pack); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	if pack.VersionFile == "" {
		pack.VersionFile = DefaultVersionFile
	}
	if len(pack.VersionKeys) == 0 {
		pack.VersionKeys = DefaultVersionKeys()
	}
	pack.PackRoot = expandUser(pack.PackRoot)
	pack.File = path

	return &pack, nil
}

// WritePackFileAtomic validates data as a pack config for path and, only if
// it is valid, replaces the file atomically (see docfile.WriteAtomic). An
// invalid document never reaches the disk, so a reader can never load half a
// save or a save that would not load.
func WritePackFileAtomic(path string, data []byte) error {
	if _, err := ParsePackBytes(path, data); err != nil {
		return err
	}
	return docfile.WriteAtomic(path, data)
}

// LoadRegistry reads and validates config/packs.json (or packs.yaml).
func LoadRegistry(configDir string) (*PacksRegistry, error) {
	if configDir == "" {
		configDir = DefaultConfigDir()
	}
	registryFile, err := docfile.Find(configDir, "packs")
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("missing pack registry: %s", filepath.Join(configDir, "packs.json"))
	}
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(registryFile)
	if err != nil {
		return nil, err
	}
	canonical, err := docfile.ToJSON(registryFile, data)
	if err != nil {
		return nil, err
	}

	var registry PacksRegistry
	dec := json.NewDecoder(strings.NewReader(string(canonical)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&registry); err != nil {
		return nil, fmt.Errorf("%s: %w", registryFile, err)
	}
	seen := map[string]bool{}
	for i, slug := range registry.Packs {
		if slug == "" {
			return nil, fmt.Errorf("%s: packs[%d] is empty", registryFile, i)
		}
		if seen[slug] {
			return nil, fmt.Errorf("%s: %q is listed twice", registryFile, slug)
		}
		seen[slug] = true
	}
	return &registry, nil
}

// LoadAll loads every pack in the registry, in registry order.
func LoadAll(configDir string) ([]*PackConfig, error) {
	registry, err := LoadRegistry(configDir)
	if err != nil {
		return nil, err
	}
	packs := make([]*PackConfig, 0, len(registry.Packs))
	for _, slug := range registry.Packs {
		pack, err := LoadPack(slug, configDir)
		if err != nil {
			return nil, err
		}
		packs = append(packs, pack)
	}
	return packs, nil
}

// Interpolate replaces {key} placeholders with the given bindings.
//
// This is a replacement loop, not format-string semantics: an unknown token is
// left in the output verbatim rather than raising. Several templates in the
// shipped configs rely on that, carrying tokens a later stage substitutes.
func Interpolate(template string, bindings map[string]string) string {
	out := template
	for k, v := range bindings {
		out = strings.ReplaceAll(out, "{"+k+"}", v)
	}
	return out
}

func expandUser(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
		}
	}
	return path
}

// VendoredVersionFile is the pack repo's own version record.
func VendoredVersionFile(pack *PackConfig) string {
	return filepath.Join(pack.PackRoot, pack.VersionFile)
}

// ReadVendoredVersion returns the version the pack repo says it vendors.
//
// Returns "" when the file is missing, unreadable, unparseable, or declares
// none of the keys -- callers then fall back to the toolkit config's
// current_version. Swallowing the error is deliberate and matches Python: a
// pack that has not been synced yet is an ordinary state, not a failure.
func ReadVendoredVersion(pack *PackConfig) string {
	target := VendoredVersionFile(pack)

	info, err := os.Stat(target)
	if err != nil || info.IsDir() {
		return ""
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return ""
	}

	for _, dotted := range pack.VersionKeys {
		if s, ok := getStringAtPath(data, dotted); ok {
			if v := strings.TrimSpace(s); v != "" {
				return v
			}
		}
	}
	return ""
}

// ResolvedCurrentVersion is the version upstream is compared against.
//
// The pack repo wins. The toolkit config's current_version is a fallback for a
// pack with no vendored record yet: it lives in *this* repository, so a bump
// written during a sync run is discarded with the runner. That asymmetry is the
// whole reason the sync converges (V49).
func ResolvedCurrentVersion(pack *PackConfig) string {
	if v := ReadVendoredVersion(pack); v != "" {
		return v
	}
	return pack.CurrentVersion
}

// WriteVendoredVersion records version at every declared key in the pack repo.
//
// Returns the file written, or "" when the pack ships no such file. It only
// rewrites a leaf that already exists and already differs -- it never creates a
// key. That is what keeps it from inventing `package.upstream_version` in a
// pack that deliberately has none (icon-sets-bundled), and from touching
// `package.version`, which is the pack's own release number rather than the
// upstream one.
//
// The edit is surgical rather than parse-modify-reserialise. Go maps have no
// key order, so round-tripping the document would reorder every key in it and
// bury a one-line version bump in a whole-file diff on the sync PR.
func WriteVendoredVersion(pack *PackConfig, version string) (string, error) {
	target := VendoredVersionFile(pack)

	info, err := os.Stat(target)
	if err != nil || info.IsDir() {
		return "", nil //nolint:nilerr // absent file is a documented no-op, not a failure
	}

	data, err := os.ReadFile(target)
	if err != nil {
		return "", err
	}
	if !json.Valid(data) {
		return "", fmt.Errorf("%s: not valid JSON", target)
	}

	changed := false
	for _, dotted := range pack.VersionKeys {
		existing, ok := getStringAtPath(data, dotted)
		if !ok || existing == version {
			continue
		}
		updated, ok, err := setStringAtPath(data, dotted, version)
		if err != nil {
			return "", fmt.Errorf("%s: setting %s: %w", target, dotted, err)
		}
		if ok {
			data = updated
			changed = true
		}
	}

	if changed {
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return "", err
		}
	}
	return target, nil
}
