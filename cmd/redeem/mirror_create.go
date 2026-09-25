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
	"time"

	"github.com/jmo/terminal-redeemer/internal/mirror"
)

func runMirrorSessionCreate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mirror session-create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	session := fs.String("session", "", "generated mirror session name")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	receipt, err := mirror.RunSessionCreation(ctx, *session)
	if err != nil {
		fmt.Fprintf(stderr, "mirror session-create failed: %v\n", err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(receipt); err != nil {
		fmt.Fprintf(stderr, "creation receipt lost: %v\n", err)
		return 1
	}
	return 0
}
