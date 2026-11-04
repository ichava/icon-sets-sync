package main

import (
	"bytes"
	"crypto/sha256"
	"debug/macho"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/tools/buildlayout"
)

// thin is the smallest Mach-O executable header debug/macho parses: no load
// commands.
func thin(cpu macho.Cpu) []byte {
	var b bytes.Buffer
	for _, v := range []uint32{macho.Magic64, uint32(cpu), 3, uint32(macho.TypeExec), 0, 0, 0, 0} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	return b.Bytes()
}

// fat writes a fat Mach-O holding one thin header per cpu, as lipo does.
func fat(t *testing.T, path string, cpus ...macho.Cpu) {
	t.Helper()
	var b bytes.Buffer
	_ = binary.Write(&b, binary.BigEndian, uint32(macho.MagicFat))
	_ = binary.Write(&b, binary.BigEndian, uint32(len(cpus)))
	offset := uint32(4096)
	var bodies [][]byte
	for _, c := range cpus {
		body := thin(c)
		for _, v := range []uint32{uint32(c), 3, offset, uint32(len(body)), 12} {
			_ = binary.Write(&b, binary.BigEndian, v)
		}
		bodies = append(bodies, body)
		offset += 4096
	}
	for _, body := range bodies {
		b.Write(make([]byte, 4096-b.Len()%4096))
		b.Write(body)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o755); err != nil {
		t.Fatal(err)
	}
}

// plist reads the top-level dict of an XML property list into key -> value,
// booleans as "true"/"false".
func plist(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	d := xml.NewDecoder(f)
	out := map[string]string{}
	var key, text string
	inKey := false
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatalf("Info.plist is not XML: %v", err)
		}
		switch v := tok.(type) {
		case xml.StartElement:
			text = ""
			inKey = v.Name.Local == "key"
			if key != "" && (v.Name.Local == "true" || v.Name.Local == "false") {
				out[key], key = v.Name.Local, ""
			}
		case xml.CharData:
			text += string(v)
		case xml.EndElement:
			switch {
			case inKey && v.Name.Local == "key":
				key, inKey = text, false
			case key != "" && v.Name.Local == "string":
				out[key], key = text, ""
			}
		}
	}
}

func TestAppIsAValidBundle(t *testing.T) {
	dir := t.TempDir()
	uni := filepath.Join(dir, "icon-sets-sync_macos_universal")
	fat(t, uni, macho.CpuArm64, macho.CpuAmd64)
	app := filepath.Join(dir, buildlayout.AppBundle)
	if err := buildApp(uni, app, "v0.3.1-4-gabc123"); err != nil {
		t.Fatal(err)
	}

	keys := plist(t, filepath.Join(app, "Contents", "Info.plist"))
	for k, want := range map[string]string{
		"CFBundleExecutable":         "icon-sets-sync",
		"CFBundleIdentifier":         bundleID,
		"CFBundlePackageType":        "APPL",
		"CFBundleIconFile":           "icon-sets-sync",
		"CFBundleShortVersionString": "0.3.1",
		"LSUIElement":                "true",
	} {
		if keys[k] != want {
			t.Errorf("Info.plist %s = %q, want %q", k, keys[k], want)
		}
	}

	exe := filepath.Join(app, "Contents", "MacOS", "icon-sets-sync")
	fi, err := os.Stat(exe)
	if err != nil || fi.Mode().Perm()&0o111 != 0o111 {
		t.Fatalf("Contents/MacOS/icon-sets-sync not executable: %v %v", fi, err)
	}
	ff, err := macho.OpenFat(exe)
	if err != nil {
		t.Fatalf("the bundled binary is not universal: %v", err)
	}
	_ = ff.Close()
	want, _ := os.ReadFile(uni)
	if got, _ := os.ReadFile(exe); !bytes.Equal(got, want) {
		t.Error("the bundled binary is not the universal binary, byte for byte")
	}

	icns, err := os.ReadFile(filepath.Join(app, "Contents", "Resources", "icon-sets-sync.icns"))
	if err != nil {
		t.Fatal(err)
	}
	if len(icns) < 8 || string(icns[:4]) != "icns" || binary.BigEndian.Uint32(icns[4:8]) != uint32(len(icns)) {
		t.Fatalf("not an icns file (%d bytes)", len(icns))
	}

	// The layout guard, beside the universal binary it was built from.
	build := filepath.Join(dir, "build")
	if err := os.MkdirAll(filepath.Join(build, "macos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(uni, filepath.Join(build, "macos", "icon-sets-sync_macos_universal")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(app, filepath.Join(build, "macos", buildlayout.AppBundle)); err != nil {
		t.Fatal(err)
	}
	sum := sha(t, filepath.Join(build, "macos", "icon-sets-sync_macos_universal"))
	if err := os.WriteFile(filepath.Join(build, "checksums.txt"), []byte(sum+"  macos/icon-sets-sync_macos_universal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, problems, err := buildlayout.Check(build); err != nil || len(problems) != 0 {
		t.Fatalf("the layout guard refused the bundle: %v %v", problems, err)
	}
}

// Only a universal binary goes in: a thin one would run on half the Macs.
func TestAppRefusesAnythingButAUniversalBinary(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]func(path string){
		"a thin arm64 binary": func(p string) {
			if err := os.WriteFile(p, thin(macho.CpuArm64), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"arm64 only, in a fat wrapper": func(p string) { fat(t, p, macho.CpuArm64) },
		"not a binary":                 func(p string) { _ = os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755) },
	}
	for name, write := range cases {
		t.Run(name, func(t *testing.T) {
			in := filepath.Join(dir, strings.ReplaceAll(name, " ", "-"))
			write(in)
			out := filepath.Join(dir, name+".app")
			if err := buildApp(in, out, "0.1.0"); err == nil {
				t.Fatal("built a bundle")
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Fatal("left a bundle behind")
			}
		})
	}
}

// A rebuilt bundle replaces the old one rather than merging into it.
func TestAppReplacesAnEarlierBundle(t *testing.T) {
	dir := t.TempDir()
	uni := filepath.Join(dir, "u")
	fat(t, uni, macho.CpuArm64, macho.CpuAmd64)
	app := filepath.Join(dir, buildlayout.AppBundle)
	stray := filepath.Join(app, "Contents", "MacOS", "old-helper")
	if err := os.MkdirAll(filepath.Dir(stray), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stray, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := buildApp(uni, app, "0.1.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Fatal("the earlier bundle's files survived")
	}
}

func TestDesktopEntry(t *testing.T) {
	dir := t.TempDir()
	if err := writeDesktop(dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "icon-sets-sync.desktop"))
	if err != nil {
		t.Fatal(err)
	}
	entry := string(b)
	for _, line := range []string{
		"[Desktop Entry]", "Type=Application", "Terminal=false",
		"Exec=env ICHAVA_DESKTOP_LAUNCH=1 icon-sets-sync", "TryExec=icon-sets-sync", "Icon=icon-sets-sync",
	} {
		if !strings.Contains(entry, "\n"+line+"\n") && !strings.HasPrefix(entry, line+"\n") {
			t.Errorf("the .desktop file lacks %q:\n%s", line, entry)
		}
	}
	f, err := os.Open(filepath.Join(dir, "icon-sets-sync.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	img, err := png.Decode(f)
	if err != nil || img.Bounds().Dx() != 256 {
		t.Fatalf("icon-sets-sync.png is not a 256px PNG: %v", err)
	}
}

func TestShortVersion(t *testing.T) {
	for in, want := range map[string]string{
		"v1.2.3": "1.2.3", "1.2.3-SNAPSHOT-abc": "1.2.3", "v0.3.1-4-gabc123": "0.3.1",
		"abc123": "0.0.0", "": "0.0.0", "v2": "2",
	} {
		if got := shortVersion(in); got != want {
			t.Errorf("shortVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func sha(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
