package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/jmo/terminal-redeemer/internal/mirror"
)

func runMirrorRecovery(args []string, supervisor bool, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("private mirror recovery", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var remoteJSON, runtime, session, id, token string
	var lockFD, readyFD, parentFD, cancellationFD int
	fs.StringVar(&remoteJSON, "remote-json", "", "configured transport identity")
	fs.StringVar(&runtime, "runtime-dir", "", "private runtime base")
	if supervisor {
		fs.StringVar(&session, "session", "", "literal session")
		fs.StringVar(&id, "session-id", "", "immutable incarnation")
		fs.StringVar(&token, "token", "", "projection correlation")
	} else {
		fs.IntVar(&lockFD, "lock-fd", -1, "inherited election lock")
		fs.IntVar(&readyFD, "ready-fd", -1, "startup receipt pipe")
		fs.IntVar(&parentFD, "probe-parent-fd", -1, "probe owner pidfd")
		fs.IntVar(&cancellationFD, "probe-cancel-fd", -1, "probe cancellation pipe")
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 || len(remoteJSON) == 0 || len(remoteJSON) > 64<<10 {
		fmt.Fprintln(stderr, "private recovery requires bounded transport configuration and no positional arguments")
		return 2
	}
	var remote mirror.RemoteConfig
	if err := json.Unmarshal([]byte(remoteJSON), &remote); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if supervisor {
		output, ok := stdout.(*os.File)
		if !ok {
			fmt.Fprintln(stderr, "supervisor requires physical terminal output")
			return 2
		}
		client := &mirror.SessionRecoveryClient{Remote: remote, SelfCommand: self, RuntimeDir: runtime}
		defer client.Close()
		err = mirror.RunSessionSupervisor(ctx, mirror.SessionSupervisorConfig{Remote: remote, Session: session, SessionID: id, Token: token, Input: os.Stdin, Output: output, Control: client})
	} else if parentFD >= 0 || cancellationFD >= 0 {
		if lockFD < 3 || parentFD < 3 || cancellationFD < 3 || lockFD == parentFD || lockFD == cancellationFD || parentFD == cancellationFD || readyFD != -1 {
			fmt.Fprintln(stderr, "probe requires distinct inherited descriptors")
			return 2
		}
		err = mirror.RunSessionRecoveryProbe(ctx, remote, runtime, os.NewFile(uintptr(lockFD), "lock"), os.NewFile(uintptr(parentFD), "parent"), os.NewFile(uintptr(cancellationFD), "cancel"), stdout)
	} else {
		if lockFD < 3 || readyFD < 3 || lockFD == readyFD {
			fmt.Fprintln(stderr, "recovery daemon requires distinct inherited descriptors")
			return 2
		}
		err = mirror.RunSessionRecovery(ctx, remote, runtime, self, os.NewFile(uintptr(lockFD), "recovery-lock"), os.NewFile(uintptr(readyFD), "recovery-ready"))
	}
	if err != nil {
		fmt.Fprintf(stderr, "mirror recovery: %v\n", err)
		return 1
	}
	return 0
}
