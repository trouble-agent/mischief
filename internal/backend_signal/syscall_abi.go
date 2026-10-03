package backend_signal

import (
	"syscall"
	"unsafe"
)

// Thin syscall shims kept in one place so the ABI wiring (and its amd64
// precondition) is greppable from a single file. The unsafe import lives
// here; the rest of the package passes plain pointers through these.

// syscall3 is syscall.Syscall (kept as a name so the call sites read).
func syscall3(trap, a1, a2, a3 uintptr) (uintptr, uintptr, syscall.Errno) {
	return syscall.Syscall(trap, a1, a2, a3)
}

// ptrOf is the unsafe.Pointer bridge for the syscall sites; every shape
// passed through it is listed here (a missing case is a compile error at
// the call site, not a nil at runtime).
func ptrOf(p any) unsafe.Pointer {
	switch v := p.(type) {
	case *[3]uint16:
		return unsafe.Pointer(v)
	}
	return nil
}
