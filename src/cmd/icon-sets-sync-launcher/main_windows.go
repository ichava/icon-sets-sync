//go:build windows

package main

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

// desktopLaunchEnv tells the CLI a person at a desktop started it, so it
// opens the browser (internal/cli, desktop.go).
const desktopLaunchEnv = "ICHAVA_DESKTOP_LAUNCH"

func main() {
	self, err := os.Executable()
	if err != nil {
		fail("icon-sets-sync-launcher cannot find its own location: " + err.Error())
	}
	paths := candidates(self)
	for _, exe := range paths {
		if fi, err := os.Stat(exe); err != nil || !fi.Mode().IsRegular() {
			continue
		}
		cmd := exec.Command(exe, "serve")
		cmd.Env = append(os.Environ(), desktopLaunchEnv+"=1")
		// DETACHED_PROCESS: no console for the server either.
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | syscall.CREATE_NEW_PROCESS_GROUP, HideWindow: true}
		if err := cmd.Start(); err != nil {
			fail("icon-sets-sync-launcher could not start " + exe + ":\n" + err.Error())
		}
		_ = cmd.Process.Release()
		return
	}
	fail("icon-sets-sync-launcher could not find the icon-sets-sync program beside it. It looked for:\n\n" +
		strings.Join(paths, "\n") + "\n\nKeep the launcher in the same folder as icon-sets-sync.")
}

// fail shows a native error box, since a GUI program has no console to
// print to, and exits non-zero.
func fail(message string) {
	const mbIconError = 0x00000010
	text, _ := syscall.UTF16PtrFromString(message)
	title, _ := syscall.UTF16PtrFromString("icon-sets-sync")
	_, _, _ = syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW").Call(0,
		uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), mbIconError)
	os.Exit(1)
}
