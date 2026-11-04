//go:build !unix && !windows

package cli

import "syscall"

func consoleAlone() bool { return false }

func detachedAttr() *syscall.SysProcAttr { return nil }
