//go:build unix

package cli

import "syscall"

// consoleAlone is a Windows question; elsewhere the answer is no.
func consoleAlone() bool { return false }

// detachedAttr starts the child in a session of its own, so it outlives the
// launcher and no SIGHUP aimed at the launcher reaches it.
func detachedAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }
