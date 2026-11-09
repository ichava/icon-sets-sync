package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// packages_dirs takes a string or an array, and packages_dir is its alias.
func TestPackagesDirsTakesAStringOrAnArray(t *testing.T) {
	for _, tc := range []struct {
		doc  string
		want []string
	}{
		{`{"packages_dirs": "/a"}`, []string{"/a"}},
		{`{"packages_dirs": ["/a", "/b"]}`, []string{"/a", "/b"}},
		{`{"packages_dir": "/a"}`, []string{"/a"}},
		{`{"packages_dirs": []}`, []string{}},
	} {
		v, err := ParseGlobal("/etc/icon-sets-sync/config.json", []byte(tc.doc))
		if err != nil {
			t.Fatalf("%s: %v", tc.doc, err)
		}
		got := Stack{Global: v}.Resolve(nil).Strings("packages_dirs")
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.doc, got, tc.want)
		}
	}
	if _, err := ParseGlobal("/c.json", []byte(`{"packages_dirs": [1]}`)); err == nil {
		t.Error("a number in the list was accepted")
	}
}

// A relative path resolves against the config file that names it, and ~ is
// the home directory.
func TestPackagesDirsResolveAgainstTheirConfigFile(t *testing.T) {
	home, _ := os.UserHomeDir()
	v, err := ParseGlobal("/etc/iss/config.json", []byte(`{"packages_dirs": ["packs", "~/work", "/abs"]}`))
	if err != nil {
		t.Fatal(err)
	}
	got := Stack{Global: v}.Resolve(nil).Strings("packages_dirs")
	want := []string{filepath.Join("/etc/iss", "packs"), filepath.Join(home, "work"), "/abs"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%v, want %v", got, want)
	}
}

// ICHAVA_PACKAGES_DIRS uses the OS list separator; --set too.
func TestPackagesDirsFromEnvAndFlags(t *testing.T) {
	sep := string(os.PathListSeparator)
	env, err := FromEnv(envOf(map[string]string{"ICHAVA_PACKAGES_DIRS": "/a" + sep + "/b"}))
	if err != nil {
		t.Fatal(err)
	}
	if got := (Stack{Env: env}).Resolve(nil).Strings("packages_dirs"); !reflect.DeepEqual(got, []string{"/a", "/b"}) {
		t.Errorf("env: %v", got)
	}
	flags, err := FromAssignments([]string{"packages_dirs=/c"})
	if err != nil {
		t.Fatal(err)
	}
	if got := (Stack{Env: env, Flags: flags}).Resolve(nil).Strings("packages_dirs"); !reflect.DeepEqual(got, []string{"/c"}) {
		t.Errorf("flags beat env: %v", got)
	}
	if got := (Stack{}).Resolve(nil).Strings("packages_dirs"); len(got) != 0 {
		t.Errorf("default: %v", got)
	}
}

func TestPackagesDirsPrintsAsAList(t *testing.T) {
	v, _ := ParseGlobal("/c.json", []byte(`{"packages_dirs": ["/a", "/b"]}`))
	tree := Stack{Global: v}.Resolve(nil).Tree()
	if got := tree["packages_dirs"]; !reflect.DeepEqual(got, []string{"/a", "/b"}) {
		t.Errorf("tree: %#v", got)
	}
	if !strings.Contains(FormatValue(Stack{Global: v}.Resolve(nil), "packages_dirs"), "/a") {
		t.Error("FormatValue")
	}
}
