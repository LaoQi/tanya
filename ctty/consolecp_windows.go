//go:build windows

package ctty

import (
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const cpUTF8 = 65001

var (
	cpDLL                   = windows.NewLazySystemDLL("kernel32.dll")
	procGetConsoleCP        = cpDLL.NewProc("GetConsoleCP")
	procGetConsoleOutputCP  = cpDLL.NewProc("GetConsoleOutputCP")
	procSetConsoleCP        = cpDLL.NewProc("SetConsoleCP")
	procSetConsoleOutputCP  = cpDLL.NewProc("SetConsoleOutputCP")
	procGetOEMCP            = cpDLL.NewProc("GetOEMCP")
	procMultiByteToWideChar = cpDLL.NewProc("MultiByteToWideChar")
	procWideCharToMultiByte = cpDLL.NewProc("WideCharToMultiByte")
)

func ConsoleCP() (uint32, bool) {
	r, _, _ := procGetConsoleCP.Call()
	return uint32(r), r != 0
}

func ConsoleOutputCP() (uint32, bool) {
	r, _, _ := procGetConsoleOutputCP.Call()
	return uint32(r), r != 0
}

func SetConsoleCP(cp uint32) bool {
	r, _, _ := procSetConsoleCP.Call(uintptr(cp))
	return r != 0
}

func SetConsoleOutputCP(cp uint32) bool {
	r, _, _ := procSetConsoleOutputCP.Call(uintptr(cp))
	return r != 0
}

func OEMCP() uint32 {
	r, _, _ := procGetOEMCP.Call()
	return uint32(r)
}

var (
	utf8Mu      sync.Mutex
	utf8OrigIn  uint32
	utf8OrigOut uint32
)

func EnsureUTF8() {
	utf8Mu.Lock()
	defer utf8Mu.Unlock()
	if utf8OrigIn == 0 {
		if cp, ok := ConsoleCP(); ok && cp != cpUTF8 && SetConsoleCP(cpUTF8) {
			utf8OrigIn = cp
		}
	}
	if utf8OrigOut == 0 {
		if cp, ok := ConsoleOutputCP(); ok && cp != cpUTF8 && SetConsoleOutputCP(cpUTF8) {
			utf8OrigOut = cp
		}
	}
}

func RestoreUTF8() {
	utf8Mu.Lock()
	defer utf8Mu.Unlock()
	if utf8OrigIn != 0 {
		SetConsoleCP(utf8OrigIn)
		utf8OrigIn = 0
	}
	if utf8OrigOut != 0 {
		SetConsoleOutputCP(utf8OrigOut)
		utf8OrigOut = 0
	}
}

func FallbackCP() uint32 {
	utf8Mu.Lock()
	defer utf8Mu.Unlock()
	if utf8OrigOut != 0 {
		return utf8OrigOut
	}
	if utf8OrigIn != 0 {
		return utf8OrigIn
	}
	return OEMCP()
}

func DecodeCP(cp uint32, b []byte) []byte {
	if cp == 0 || len(b) == 0 {
		return b
	}
	n, _, _ := procMultiByteToWideChar.Call(uintptr(cp), 0, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 0, 0)
	if n == 0 {
		return b
	}
	wide := make([]uint16, n)
	n, _, _ = procMultiByteToWideChar.Call(uintptr(cp), 0, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), uintptr(unsafe.Pointer(&wide[0])), uintptr(n))
	if n == 0 {
		return b
	}
	m, _, _ := procWideCharToMultiByte.Call(cpUTF8, 0, uintptr(unsafe.Pointer(&wide[0])), uintptr(n), 0, 0, 0, 0)
	if m == 0 {
		return b
	}
	out := make([]byte, m)
	m, _, _ = procWideCharToMultiByte.Call(cpUTF8, 0, uintptr(unsafe.Pointer(&wide[0])), uintptr(n), uintptr(unsafe.Pointer(&out[0])), uintptr(m), 0, 0)
	if m == 0 {
		return b
	}
	return out
}
