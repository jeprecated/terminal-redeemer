package mirror

import "golang.org/x/sys/unix"

const terminalGetState = unix.TCGETS

func flushTerminalInput(fd int) { _ = unix.IoctlSetInt(fd, unix.TCFLSH, unix.TCIFLUSH) }
func preserveTerminal(fd int) func() {
	state, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	return func() {
		if err == nil {
			_ = unix.IoctlSetTermios(fd, unix.TCSETS, state)
		}
	}
}
