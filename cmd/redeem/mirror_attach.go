package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jmo/terminal-redeemer/internal/mirror"
)

// Private source-side operation. No Niri, discovery, creation, or retry policy.
func runMirrorSessionAttach(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mirror session-attach", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var cfg mirror.SessionAttachConfig
	fs.StringVar(&cfg.Session, "session", "", "exact session name")
	fs.StringVar(&cfg.SessionID, "session-id", "", "expected incarnation")
	fs.StringVar(&cfg.Attempt, "attempt", "", "fresh attachment attempt")
	fs.DurationVar(&cfg.StartupTimeout, "timeout", 15*time.Second, "attachment readiness deadline")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "mirror session-attach accepts no positional arguments")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)
	defer stop()
	cfg.Stdin, cfg.Stdout, cfg.Stderr = os.Stdin, stdout, stderr
	status, err := mirror.RunSessionAttachment(ctx, cfg)
	// Written only after the child is reaped: cannot interleave with its Render.
	if marker := mirror.AttachmentMarker(cfg.Attempt, status); marker != "" {
		_, _ = io.WriteString(stdout, marker)
	}
	if err != nil {
		fmt.Fprintf(stderr, "mirror session-attach: %v\n", err)
		return 1
	}
	return 0
}
