package docfile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// renameFile is os.Rename, swappable so a test can fail the last step and
// check that nothing is left behind.
var renameFile = os.Rename

// WriteAtomic replaces path with data so that a reader sees either the old
// file or the new one, never a partial write.
//
// The bytes go to a temporary file in the same directory (a rename is only
// atomic within one filesystem), are fsynced, take the existing file's
// permissions (0644 for a new file), and are renamed over the target. The
// directory is fsynced afterwards where the platform allows it, so the
// rename itself survives a crash. On any failure the temporary file is
// removed and the target is untouched.
func WriteAtomic(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	mode := fs.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s: not a regular file", path)
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return statErr
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmpName, mode); err != nil {
		return err
	}
	if err = renameFile(tmpName, path); err != nil {
		return err
	}

	// Best effort: not every platform can open a directory for fsync, and the
	// file itself is already durable.
	if d, openErr := os.Open(dir); openErr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
