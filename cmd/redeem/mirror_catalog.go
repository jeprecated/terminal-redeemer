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

	"github.com/jmo/terminal-redeemer/internal/config"
	"github.com/jmo/terminal-redeemer/internal/mirror"
)

// Internal source helper: unlike snapshot this requires no graphical session.
func runMirrorSessionCatalog(args []string, cfg config.Config, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mirror session-catalog", flag.ContinueOnError)
	fs.SetOutput(stderr)
	timeout := fs.Duration("timeout", cfg.Resume.Timeout, "maximum catalog observation time")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *timeout <= 0 || fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "mirror session-catalog requires a positive timeout and no positional arguments")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	inventory, err := mirror.ObserveSessionInventory(ctx, nil)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "mirror session-catalog failed: %v\n", err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(inventory); err != nil {
		_, _ = fmt.Fprintf(stderr, "mirror session-catalog encode failed: %v\n", err)
		return 1
	}
	return 0
}
