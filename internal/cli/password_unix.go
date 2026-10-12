//go:build !windows

package cli

import (
	"bufio"
	"os"
	"os/exec"
	"strings"
)

func readHidden(tty *os.File) (string, error) {
	off := exec.Command("stty", "-echo")
	off.Stdin = tty
	if err := off.Run(); err != nil {
		line, readErr := bufio.NewReader(tty).ReadString('\n')
		if readErr != nil && !isEOF(readErr) {
			return "", readErr
		}
		return strings.TrimSpace(line), nil
	}
	defer func() {
		on := exec.Command("stty", "echo")
		on.Stdin = tty
		_ = on.Run()
	}()
	line, err := bufio.NewReader(tty).ReadString('\n')
	if err != nil && !isEOF(err) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
