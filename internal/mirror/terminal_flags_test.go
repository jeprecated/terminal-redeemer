package mirror

import (
	"reflect"
	"runtime"

	"golang.org/x/sys/unix"
)

func terminalStateEqual(before, after *unix.Termios) bool {
	if before == nil || after == nil {
		return before == after
	}
	left, right := *before, *after
	if runtime.GOOS == "darwin" {
		left.Lflag &^= unix.PENDIN
		right.Lflag &^= unix.PENDIN
	}
	return reflect.DeepEqual(left, right)
}

func terminalFlagsEqual(before, after int) bool {
	if runtime.GOOS == "darwin" {
		const darwinWasWritten = 0x00010000
		return before&^darwinWasWritten == after&^darwinWasWritten
	}
	return before == after
}
