// Command launchers writes what a desktop needs to start icon-sets-sync with
// no terminal in sight, from binaries `make cross` already built.
//
//	make app      # build/macos/icon-sets-sync.app, from the universal binary
//	make desktop  # build/linux/icon-sets-sync.desktop and icon-sets-sync.png
//	make cross    # also build/windows/icon-sets-sync-launcher_windows_<arch>.exe
//
// macOS opens a bare executable inside Terminal, and nothing the process does
// can stop it, so double-clicking needs a bundle. The bundle's binary is the
// universal binary itself, byte for byte: it notices it runs from
// Contents/MacOS, and with no arguments starts the web UI with nothing on
// screen but the browser. With arguments it is the ordinary CLI.
//
// Linux desktops run a .desktop entry with Terminal=false without one.
//
// Windows gives a console program a console window before any of its code
// runs, so the binary's own Explorer double-click handling (internal/cli,
// desktop_windows.go) still flashed a window. The launcher is a second,
// GUI-subsystem program (src/cmd/icon-sets-sync-launcher, -H=windowsgui) that
// opens no console and starts the binary's `serve` beside it. It is built here
// for each Windows target, with the toolchain cross-compiling.
//
// Nothing here signs or notarises; that needs the owner's Apple Developer
// identity (docs/installation.md gives the commands).
package main

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/ichava/icon-sets-sync/src/internal/tools/buildlayout"
)

// bundleID is the app's identifier, reverse-DNS under the owner's domain.
const bundleID = "com.simtabi.ichava.icon-sets-sync"

func main() {
	var (
		app       string
		universal string
		version   string
		desktop   string
		windows   string
	)
	flag.StringVar(&app, "app", "", "write the macOS app bundle here (e.g. ../build/macos/icon-sets-sync.app)")
	flag.StringVar(&universal, "universal", "", "the universal binary the bundle carries (with -app)")
	flag.StringVar(&version, "version", "0.0.0", "version for Info.plist (git describe output is fine)")
	flag.StringVar(&desktop, "desktop", "", "write icon-sets-sync.desktop and icon-sets-sync.png into this directory")
	flag.StringVar(&windows, "windows", "", "build the Windows launcher for each Windows target into this directory")
	flag.Parse()

	if app == "" && desktop == "" && windows == "" {
		fmt.Fprintln(os.Stderr, "launchers: pass -app (with -universal), -desktop or -windows")
		os.Exit(2)
	}
	if app != "" {
		if err := buildApp(universal, app, version); err != nil {
			fmt.Fprintln(os.Stderr, "launchers:", err)
			os.Exit(1)
		}
		fmt.Printf("launchers: %s (open it, or put %s on PATH)\n", app, filepath.Join(app, "Contents", "MacOS", buildlayout.Binary))
	}
	if desktop != "" {
		if err := writeDesktop(desktop); err != nil {
			fmt.Fprintln(os.Stderr, "launchers:", err)
			os.Exit(1)
		}
		fmt.Printf("launchers: %s\n", filepath.Join(desktop, buildlayout.Binary+".desktop"))
	}
	if windows != "" {
		built, err := buildWindowsLaunchers(windows)
		if err != nil {
			fmt.Fprintln(os.Stderr, "launchers:", err)
			os.Exit(1)
		}
		for _, b := range built {
			fmt.Printf("launchers: %s\n", b)
		}
	}
}

// buildWindowsLaunchers cross-builds src/cmd/icon-sets-sync-launcher for each
// of buildlayout.WindowsLauncherArchs as a GUI-subsystem program into dir,
// named buildlayout.WindowsLauncher(arch). It runs from the module directory,
// which is where `go run ./internal/tools/launchers` already is.
func buildWindowsLaunchers(dir string) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(buildlayout.WindowsLauncherArchs))
	for _, arch := range buildlayout.WindowsLauncherArchs {
		dest, err := filepath.Abs(filepath.Join(dir, buildlayout.WindowsLauncher(arch)))
		if err != nil {
			return nil, err
		}
		if err := goBuildGUI(arch, dest); err != nil {
			return nil, err
		}
		out = append(out, dest)
	}
	return out, nil
}

// moduleDir is where `go build` runs: the module root, which is the working
// directory under `go -C src run`. Tests point it there from their package.
var moduleDir = "."

// guiLDFlags links the launcher for the GUI subsystem, so Windows opens no
// console for it.
const guiLDFlags = "-s -w -H=windowsgui"

// goBuildGUI builds the launcher for windows/arch at dest with guiLDFlags.
func goBuildGUI(arch, dest string) error {
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", guiLDFlags, "-o", dest, "./cmd/icon-sets-sync-launcher")
	cmd.Dir = moduleDir
	cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH="+arch, "CGO_ENABLED=0")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("building %s: %w", filepath.Base(dest), err)
	}
	return nil
}

// buildApp writes the bundle at out, replacing any earlier one:
//
//	Contents/Info.plist                    LSUIElement: no Dock icon, no menu bar
//	Contents/MacOS/icon-sets-sync          the universal binary, unchanged
//	Contents/Resources/icon-sets-sync.icns
//
// It refuses anything but a universal (arm64 + amd64) Mach-O, and leaves no
// bundle behind when it does.
func buildApp(universal, out, version string) error {
	if universal == "" {
		return errors.New("-universal is required with -app; run make cross first")
	}
	if err := isUniversal(universal); err != nil {
		return fmt.Errorf("%s: %w (run make cross to build it)", universal, err)
	}
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(out)
		}
	}()
	contents := filepath.Join(out, "Contents")
	for _, d := range []string{"MacOS", "Resources"} {
		if err := os.MkdirAll(filepath.Join(contents, d), 0o755); err != nil {
			return err
		}
	}
	if err := copyExecutable(universal, filepath.Join(contents, "MacOS", buildlayout.Binary)); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(infoPlist(shortVersion(version))), 0o644); err != nil {
		return err
	}
	icns, err := iconICNS()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(contents, "Resources", buildlayout.Binary+".icns"), icns, 0o644); err != nil {
		return err
	}
	ok = true
	return nil
}

func isUniversal(path string) error {
	f, err := macho.OpenFat(path)
	if err != nil {
		return fmt.Errorf("not a universal Mach-O binary: %w", err)
	}
	defer func() { _ = f.Close() }()
	have := map[macho.Cpu]bool{}
	for _, a := range f.Arches {
		have[a.Cpu] = true
	}
	if !have[macho.CpuArm64] || !have[macho.CpuAmd64] {
		return errors.New("a universal binary needs both arm64 and amd64")
	}
	return nil
}

func copyExecutable(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dest, 0o755)
}

var leadingVersion = regexp.MustCompile(`^v?(\d+(?:\.\d+){0,2})`)

// shortVersion is the numeric part CFBundleShortVersionString allows:
// v0.3.1-4-gabc123 is 0.3.1; a bare commit is 0.0.0.
func shortVersion(v string) string {
	if m := leadingVersion.FindStringSubmatch(v); m != nil {
		return m[1]
	}
	return "0.0.0"
}

func infoPlist(version string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDevelopmentRegion</key>
	<string>en</string>
	<key>CFBundleDisplayName</key>
	<string>icon-sets-sync</string>
	<key>CFBundleExecutable</key>
	<string>` + buildlayout.Binary + `</string>
	<key>CFBundleIconFile</key>
	<string>` + buildlayout.Binary + `</string>
	<key>CFBundleIdentifier</key>
	<string>` + bundleID + `</string>
	<key>CFBundleInfoDictionaryVersion</key>
	<string>6.0</string>
	<key>CFBundleName</key>
	<string>icon-sets-sync</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>` + version + `</string>
	<key>CFBundleVersion</key>
	<string>` + version + `</string>
	<key>LSApplicationCategoryType</key>
	<string>public.app-category.developer-tools</string>
	<key>LSMinimumSystemVersion</key>
	<string>12.0</string>
	<key>LSUIElement</key>
	<true/>
	<key>NSHighResolutionCapable</key>
	<true/>
	<key>NSHumanReadableCopyright</key>
	<string>Copyright (c) Simtabi LLC. MIT licence.</string>
</dict>
</plist>
`
}

// desktopEntry runs the binary from PATH with no terminal. ICHAVA_DESKTOP_LAUNCH
// tells it a person at a desktop started it, so it opens the browser though
// it has no terminal (internal/cli, launch.go).
const desktopEntry = `[Desktop Entry]
Type=Application
Version=1.5
Name=icon-sets-sync
GenericName=Icon pack sync
Comment=Open the icon-sets-sync web UI in your browser
Exec=env ICHAVA_DESKTOP_LAUNCH=1 icon-sets-sync
TryExec=icon-sets-sync
Icon=icon-sets-sync
Terminal=false
Categories=Development;
StartupNotify=false
`

func writeDesktop(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, buildlayout.Binary+".desktop"), []byte(desktopEntry), 0o644); err != nil {
		return err
	}
	b, err := iconPNG(256)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, buildlayout.Binary+".png"), b, 0o644)
}

// iconICNS packs PNGs into an .icns, the format macOS reads from 10.7 on:
// ic08 256px, ic09 512px, ic10 1024px (512px at 2x).
func iconICNS() ([]byte, error) {
	var body bytes.Buffer
	for _, e := range []struct {
		kind string
		px   int
	}{{"ic08", 256}, {"ic09", 512}, {"ic10", 1024}} {
		b, err := iconPNG(e.px)
		if err != nil {
			return nil, err
		}
		body.WriteString(e.kind)
		_ = binary.Write(&body, binary.BigEndian, uint32(8+len(b)))
		body.Write(b)
	}
	var out bytes.Buffer
	out.WriteString("icns")
	_ = binary.Write(&out, binary.BigEndian, uint32(8+body.Len()))
	out.Write(body.Bytes())
	return out.Bytes(), nil
}

// iconPNG draws the icon at px square: a blue rounded tile holding a 3 x 3
// grid of white tiles, one of them amber, for a set of icons kept in step.
// Drawn here rather than tracked as a binary, so it has a source.
func iconPNG(px int) ([]byte, error) {
	img := image.NewNRGBA(image.Rect(0, 0, px, px))
	s := float64(px)
	top := [3]float64{0x31, 0x7d, 0xe0}
	bottom := [3]float64{0x1a, 0x4f, 0xa0}
	const samples = 4
	for y := 0; y < px; y++ {
		for x := 0; x < px; x++ {
			var bg, tile, accent float64
			for sy := 0; sy < samples; sy++ {
				for sx := 0; sx < samples; sx++ {
					u := (float64(x) + (float64(sx)+0.5)/samples) / s
					v := (float64(y) + (float64(sy)+0.5)/samples) / s
					if inRoundRect(u, v, 0.06, 0.06, 0.94, 0.94, 0.2) {
						bg++
						if k, ok := gridCell(u, v); ok {
							if k == 4 {
								accent++
							} else {
								tile++
							}
						}
					}
				}
			}
			n := float64(samples * samples)
			if bg == 0 {
				continue
			}
			t := float64(y) / s
			var c [3]float64
			for i := range c {
				c[i] = top[i] + (bottom[i]-top[i])*t
			}
			// Blend the tiles over the background, within the covered part.
			wt, wa := tile/bg, accent/bg
			r := c[0]*(1-wt-wa) + 255*wt + 0xf5*wa
			g := c[1]*(1-wt-wa) + 255*wt + 0x9f*wa
			b := c[2]*(1-wt-wa) + 255*wt + 0x00*wa
			img.SetNRGBA(x, y, color.NRGBA{R: clamp(r), G: clamp(g), B: clamp(b), A: clamp(255 * bg / n)})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// gridCell reports which of the nine tiles (0-8, row by row) holds u, v.
func gridCell(u, v float64) (int, bool) {
	const lo, cell, gap = 0.22, 0.18, 0.03
	for row := 0; row < 3; row++ {
		for col := 0; col < 3; col++ {
			x0 := lo + float64(col)*(cell+gap)
			y0 := lo + float64(row)*(cell+gap)
			if inRoundRect(u, v, x0, y0, x0+cell, y0+cell, 0.045) {
				return row*3 + col, true
			}
		}
	}
	return 0, false
}

func inRoundRect(u, v, x0, y0, x1, y1, r float64) bool {
	if u < x0 || u > x1 || v < y0 || v > y1 {
		return false
	}
	cx := math.Max(x0+r, math.Min(u, x1-r))
	cy := math.Max(y0+r, math.Min(v, y1-r))
	return (u-cx)*(u-cx)+(v-cy)*(v-cy) <= r*r
}

func clamp(f float64) uint8 {
	switch {
	case f <= 0:
		return 0
	case f >= 255:
		return 255
	}
	return uint8(math.Round(f))
}
