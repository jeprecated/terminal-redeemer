package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestTermqueryHelperProcess(t *testing.T) {
	if os.Getenv("REDEEM_TERMQUERY_HELPER") != "1" {
		t.Skip("helper process only")
	}
	value, set := os.LookupEnv("CI")
	fmt.Printf("ci=%t:%s\n", set, value)
	os.Exit(0)
}

// Every redeem process links Bubble Tea. On a TTY (the source attach helper
// runs under ssh -tt) its import-time background query must not block startup
// or leave a stale query for the viewer to answer into the remote pane.
func TestBinaryStartupDoesNotQueryTerminal(t *testing.T) {
	for _, tc := range []struct {
		env  []string
		want string
	}{
		{want: "ci=false:"},
		{env: []string{"CI="}, want: "ci=true:\n"},
	} {
		assertStartupDoesNotQuery(t, tc.env, tc.want)
	}
}

func assertStartupDoesNotQuery(t *testing.T, extra []string, want string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestTermqueryHelperProcess$")
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "CI=") && !strings.HasPrefix(entry, "TERM=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "REDEEM_TERMQUERY_HELPER=1", "TERM=xterm-256color")
	cmd.Env = append(cmd.Env, extra...)
	started := time.Now()
	terminal, err := pty.Start(cmd)
	if err != nil {
		t.Skipf("no PTY available: %v", err)
	}
	defer func() { _ = terminal.Close() }()
	output := make(chan []byte, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, terminal) // EIO once the helper exits.
		output <- buf.Bytes()
	}()
	var got []byte
	select {
	case got = <-output:
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("env %v: startup blocked on a terminal query (termenv waits 5s): %q", extra, <-output)
	}
	_ = cmd.Wait()
	got = bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n"))
	if bytes.Contains(got, []byte("\x1b]11;?")) || bytes.Contains(got, []byte("\x1b[6n")) {
		t.Fatalf("env %v: startup wrote a terminal query: %q", extra, got)
	}
	if !bytes.Contains(got, []byte(want)) {
		t.Fatalf("env %v: temporary query suppression not restored (want %q): %q", extra, want, got)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("env %v: startup took %s", extra, elapsed)
	}
}
