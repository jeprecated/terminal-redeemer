package mirror

import "golang.org/x/sys/unix"

const terminalGetState = unix.TIOCGETA

func flushTerminalInput(fd int) { _ = unix.IoctlSetPointerInt(fd, unix.TIOCFLUSH, unix.TCIFLUSH) }
func preserveTerminal(fd int) func() {
	state, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	return func() {
		if err == nil {
			_ = unix.IoctlSetTermios(fd, unix.TIOCSETA, state)
		}
	}
}
