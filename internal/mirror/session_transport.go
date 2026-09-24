package mirror

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

type sessionOutput struct {
	data   []byte
	events []string
}
type sessionTransportResult struct {
	outcome string
	err     error
}
type sessionTransport struct {
	input  *os.File
	fd     int
	output <-chan sessionOutput
	done   <-chan sessionTransportResult
	stop   func()
}

func startSessionTransport(ctx context.Context, command Command, attempt string, physicalFD int) (*sessionTransport, error) {
	output, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(command.Name, command.Args...)
	// Owned files, not Cmd.StdoutPipe or copying goroutines: Wait cannot close
	// the reader underneath us, nor wait forever for a descriptor-holding child.
	cmd.Stdout = writer
	cmd.Stderr = writer
	var size *pty.Winsize
	if current, e := unix.IoctlGetWinsize(physicalFD, unix.TIOCGWINSZ); e == nil {
		size = &pty.Winsize{Rows: current.Row, Cols: current.Col, X: current.Xpixel, Y: current.Ypixel}
	}
	terminal, err := pty.StartWithSize(cmd, size) // Setsid + controlling PTY, not Setpgid.
	writer.Close()
	if err != nil {
		output.Close()
		return nil, err
	}
	relayCtx, cancelRelay := context.WithCancel(ctx)
	packets := make(chan sessionOutput, 16)
	done := make(chan sessionTransportResult, 1)
	var mu sync.Mutex
	reaped := false
	stop := func() {
		cancelRelay()
		output.Close()
		mu.Lock()
		defer mu.Unlock()
		if !reaped {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}
	stopParent := context.AfterFunc(ctx, stop)
	relayed := make(chan sessionTransportResult, 1)
	go func() {
		decoder := attachmentOutputDecoder{attempt: attempt}
		buf := make([]byte, 4096)
		result := sessionTransportResult{}
		defer func() { relayed <- result }()
		for {
			n, readErr := output.Read(buf)
			data, events := decoder.feed(buf[:n], readErr != nil)
			for _, event := range events {
				if event != "ready" {
					result.outcome = event
				}
			}
			if len(data) > 0 || len(events) > 0 {
				select {
				case packets <- sessionOutput{data, events}:
				case <-relayCtx.Done():
					result.err = relayCtx.Err()
					return
				}
			}
			if readErr != nil {
				return
			}
		}
	}()
	go func() {
		defer stopParent()
		defer cancelRelay()
		defer output.Close()
		// Observe exit WITHOUT reaping first. The leader's PID stays reserved until
		// its group is killed, preventing cleanup from signalling a recycled PGID.
		var info unix.Siginfo
		var waitErr error
		for {
			waitErr = unix.Waitid(unix.P_PID, cmd.Process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
			if waitErr != unix.EINTR {
				break
			}
		}
		mu.Lock()
		if waitErr == nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		} else {
			_ = cmd.Process.Kill()
		}
		err := cmd.Wait()
		reaped = true
		mu.Unlock()
		var result sessionTransportResult
		select {
		case result = <-relayed:
		case <-time.After(time.Second):
			output.Close()
			cancelRelay()
			result = <-relayed
			result.err = errors.New("attachment output drain timed out")
		}
		if result.err == nil {
			result.err = err
		}
		if waitErr != nil {
			result.err = waitErr
		}
		done <- result
	}()
	return &sessionTransport{terminal, int(terminal.Fd()), packets, done, stop}, nil
}
