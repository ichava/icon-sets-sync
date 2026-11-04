//go:build windows

package cli

import (
	"os"
	"syscall"
	"unsafe"
)

var procGetConsoleProcessList = syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleProcessList")

// consoleAlone reports whether this process's console is its own, which is
// what Explorer gives a double-clicked console program.
func consoleAlone() bool {
	return explorerLaunch(consoleProcessCount(), parentImageName())
}

func consoleProcessCount() int {
	if procGetConsoleProcessList.Find() != nil {
		return 0
	}
	var ids [4]uint32
	n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&ids[0])), uintptr(len(ids)))
	return int(n)
}

// parentImageName is the parent's executable name, or "" if it has gone.
func parentImageName() string {
	ppid := uint32(os.Getppid())
	snap, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return ""
	}
	defer func() { _ = syscall.CloseHandle(snap) }()
	var e syscall.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = syscall.Process32First(snap, &e); err == nil; err = syscall.Process32Next(snap, &e) {
		if e.ProcessID == ppid {
			return syscall.UTF16ToString(e.ExeFile[:])
		}
	}
	return ""
}

// detachedAttr starts the child with no console (DETACHED_PROCESS) in its
// own process group, so closing this console cannot reach it.
func detachedAttr() *syscall.SysProcAttr {
	const detachedProcess = 0x00000008
	return &syscall.SysProcAttr{
		CreationFlags: detachedProcess | syscall.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
}
