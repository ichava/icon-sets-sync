package main

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestCandidates(t *testing.T) {
	dir := filepath.Join("C:", "tools")
	for self, want := range map[string][]string{
		"icon-sets-sync-launcher_windows_amd64.exe": {"icon-sets-sync_windows_amd64.exe", "icon-sets-sync.exe"},
		"icon-sets-sync-launcher_windows_arm64.exe": {"icon-sets-sync_windows_arm64.exe", "icon-sets-sync.exe"},
		"icon-sets-sync-launcher.exe":               {"icon-sets-sync.exe"},
		"Icon-Sets-Sync-Launcher_windows_386.exe":   {"icon-sets-sync_windows_386.exe", "icon-sets-sync.exe"},
		"renamed.exe": {"icon-sets-sync.exe"},
	} {
		var full []string
		for _, w := range want {
			full = append(full, filepath.Join(dir, w))
		}
		if got := candidates(filepath.Join(dir, self)); !reflect.DeepEqual(got, full) {
			t.Errorf("candidates(%s) = %v, want %v", self, got, full)
		}
	}
}
