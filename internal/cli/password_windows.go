//go:build windows

package cli

import (
	"bufio"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

func readHidden(tty *os.File) (string, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getMode := kernel32.NewProc("GetConsoleMode")
	setMode := kernel32.NewProc("SetConsoleMode")
	var mode uint32
	handle := syscall.Handle(tty.Fd())
	getMode.Call(uintptr(handle), uintptr(unsafe.Pointer(&mode)))
	const enableEchoInput = 0x0004
	setMode.Call(uintptr(handle), uintptr(mode&^enableEchoInput))
	defer setMode.Call(uintptr(handle), uintptr(mode))
	line, err := bufio.NewReader(tty).ReadString('\n')
	if err != nil && !isEOF(err) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
