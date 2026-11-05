// Package pathguard confines a path built from config, a manifest, an archive
// or a request under the directory it is meant to stay in.
//
// It is the one place that rule is written. Every caller that joins a name it
// did not choose onto a directory -- a pack slug from the web UI, a set name
// from the audit form, an archive member, a fingerprint key -- goes through
// Join, so the checks cannot drift apart between them.
package pathguard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrEscape is wrapped by every refusal, so a caller can tell a path that
// would leave its root from an I/O error.
var ErrEscape = errors.New("path escapes its root")

// Join returns root joined with elem, cleaned, or an error wrapping ErrEscape
// when the result could leave root. A relative root gives a relative result,
// as filepath.Join would; the checks themselves run on the absolute path.
//
// It refuses, before touching the filesystem:
//
//   - an empty root;
//   - an absolute element, or one with a volume name;
//   - any `..` component, split on both `/` and `\`, even one that would
//     resolve back inside root: a name that needs `..` is not a name any
//     caller here produces honestly.
//
// Then, because a name with no `..` can still leave through a link, it
// resolves the deepest part of the result that exists and refuses it unless
// it is still under root with root's own links resolved too. A dangling link
// is refused, since creating the file would follow it. A root that does not
// exist yet holds no links, so a path under it is accepted.
//
// The returned path is root joined with the relative path filepath.Rel
// computed, never with the caller's input as given.
func Join(root string, elem ...string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("%w: empty root", ErrEscape)
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	for _, e := range elem {
		if filepath.IsAbs(e) || filepath.VolumeName(e) != "" || strings.HasPrefix(e, "/") || strings.HasPrefix(e, `\`) {
			return "", fmt.Errorf("%w: %q is absolute", ErrEscape, e)
		}
		for _, part := range strings.FieldsFunc(e, func(r rune) bool { return r == '/' || r == '\\' }) {
			if part == ".." {
				return "", fmt.Errorf("%w: %q climbs with ..", ErrEscape, e)
			}
		}
	}

	joined := filepath.Join(append([]string{base}, elem...)...)
	rel, err := filepath.Rel(base, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: %q is not under %s", ErrEscape, strings.Join(elem, "/"), base)
	}
	if err := withinAfterLinks(base, rel); err != nil {
		return "", err
	}
	return filepath.Join(root, rel), nil
}

// withinAfterLinks refuses rel when the deepest part of base/rel that exists
// resolves, through any symlink, to somewhere outside base.
func withinAfterLinks(base, rel string) error {
	p := filepath.Join(base, rel)
	for {
		if _, err := os.Lstat(p); err == nil {
			break
		}
		if p == base {
			// Nothing under the root exists yet, so nothing can be a link.
			return nil
		}
		p = filepath.Dir(p)
	}
	if p == base {
		return nil
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return fmt.Errorf("%w: %s cannot be resolved: %v", ErrEscape, p, err)
	}
	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return fmt.Errorf("%w: %s cannot be resolved: %v", ErrEscape, base, err)
	}
	r, err := filepath.Rel(realBase, real)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) || filepath.IsAbs(r) {
		return fmt.Errorf("%w: %s resolves to %s, outside %s", ErrEscape, p, real, realBase)
	}
	return nil
}
