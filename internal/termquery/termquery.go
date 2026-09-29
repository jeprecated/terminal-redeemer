// Package termquery suppresses Bubble Tea v1's import-time terminal query.
//
// bubbletea's init calls lipgloss.HasDarkBackground, which writes OSC 11 and
// DSR 6n to a TTY stdout and blocks up to termenv.OSCTimeout (5s) for replies.
// Every redeem process links bubbletea, including the source-side attach helper
// under `ssh -tt`, whose output nobody answers before readiness. The stale
// query then reached the viewer and its replies became input to Zellij.
//
// This package imports only the standard library, so the Go initialization
// order (dependencies first, then import path order) runs it before termenv
// and bubbletea. termenv treats a non-empty CI as "not a TTY" and skips the
// query. Restore runs from mirrortui's init, after bubbletea's, so no child
// process or later lookup observes the temporary value.
//
// Caveat: termenv's default Output and lipgloss's default renderer are built
// and queried while suppressed, so they cache an Ascii profile / default dark
// background. redeem renders its own ANSI; adding lipgloss styling requires
// revisiting this (for example an explicit renderer built after Restore).
package termquery

import "os"

const suppressVariable = "CI"

var restore func()

func init() { restore = suppress() }

// suppress sets CI when termenv would otherwise query, and returns the undo
// that puts back the exact prior state (unset, or set but empty).
func suppress() func() {
	prior, set := os.LookupEnv(suppressVariable)
	if prior != "" {
		return nil
	}
	if os.Setenv(suppressVariable, "1") != nil {
		return nil
	}
	if set {
		return func() { _ = os.Setenv(suppressVariable, "") }
	}
	return func() { _ = os.Unsetenv(suppressVariable) }
}

// Restore undoes the temporary suppression. It is idempotent.
func Restore() {
	if restore != nil {
		restore()
		restore = nil
	}
}
