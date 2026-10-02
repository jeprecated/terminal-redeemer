package mirror

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/x/term"
	"golang.org/x/sys/unix"
)

// SessionControl is the narrow seam for the shared coordinator. Exchange must
// honour ctx. It schedules host checks/admission; the terminal helper never
// probes a host or grants itself another attachment. No public launch uses this
// seam until the cross-process coordinator is implemented.
type SessionControl interface {
	Exchange(context.Context, SessionControlRequest) (SessionControlReply, error)
}
type SessionControlRequest struct {
	Token, Client, Session, SessionID string
	Attempt, Event                    string
	State                             string
	Retry                             bool
}
type SessionGrant struct {
	Attempt  string
	Deadline time.Time
}
type SessionControlReply struct {
	Checking bool
	Reason   string
	RetryAt  time.Time
	Grant    *SessionGrant
	// Ended is reserved for authoritative evidence for this immutable identity.
	// Reset invalidates a pending grant only; healthy transports survive restart.
	Ended, Reset bool
	// An expired known lease can be revoked even if readiness was reported late.
	// Unlike restart Reset, this is bound to exactly one attempt.
	CancelAttempt string
}
type SessionSupervisorConfig struct {
	Remote                    RemoteConfig
	Session, SessionID, Token string
	Input, Output             *os.File
	Control                   SessionControl
	Local                     *SessionLocalControl
}
type sessionControlResult struct {
	request SessionControlRequest
	reply   SessionControlReply
	err     error
}
type sessionReport struct{ attempt, event string }

type sessionPhase string

const (
	sessionOffline    sessionPhase = "offline"
	sessionChecking   sessionPhase = "checking"
	sessionConnecting sessionPhase = "connecting"
	sessionReady      sessionPhase = "ready"
)

// RunSessionSupervisor retains one physical terminal across disposable SSH
// PTYs. It performs no creation, window management, discovery or host backoff.
func RunSessionSupervisor(ctx context.Context, cfg SessionSupervisorConfig) error {
	if cfg.Input == nil || cfg.Output == nil || cfg.Control == nil || cfg.Token == "" {
		return fmt.Errorf("terminal, identity and shared control are required")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	request := SessionControlRequest{Token: cfg.Token, Client: hex.EncodeToString(nonce[:]), Session: cfg.Session, SessionID: cfg.SessionID}
	freshAttempt := func() error {
		if _, err := rand.Read(nonce[:]); err != nil {
			return err
		}
		request.Attempt = hex.EncodeToString(nonce[:])
		return nil
	}
	if err := freshAttempt(); err != nil {
		return err
	}
	// Validate immutable planning inputs before touching the terminal.
	if _, err := PlanSessionAttachment(cfg.Remote, cfg.Session, cfg.SessionID, request.Client); err != nil {
		return err
	}
	var localRequests <-chan sessionLocalCall
	transportIdentity := ""
	if cfg.Local != nil {
		var err error
		transportIdentity, err = recoveryIdentity(cfg.Remote)
		if err != nil {
			return err
		}
		localRequests = cfg.Local.requests
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	inFD, outFD := int(cfg.Input.Fd()), int(cfg.Output.Fd())
	flags := make(map[int]int)
	for _, fd := range []int{inFD, outFD} {
		value, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
		if err != nil {
			return err
		}
		flags[fd] = value
	}
	baseline, err := term.MakeRaw(uintptr(inFD))
	if err != nil {
		return fmt.Errorf("projection requires a terminal: %w", err)
	}
	defer term.Restore(uintptr(inFD), baseline)
	defer func() {
		for fd, value := range flags {
			_, _ = unix.FcntlInt(uintptr(fd), unix.F_SETFL, value)
		}
	}()
	if err := unix.SetNonblock(inFD, true); err != nil {
		return err
	}
	var gate sessionInputGate
	input := make(chan sessionInput, 16)
	inputDone := make(chan struct{})
	go func() { defer close(inputDone); gate.read(ctx, inFD, input) }()
	defer func() {
		cancel()
		<-inputDone
		// No later attachment exists on exit; discard remaining physical input
		// before restoring the caller's terminal discipline.
		flushTerminalInput(inFD)
	}()
	resize := make(chan os.Signal, 1)
	signal.Notify(resize, syscall.SIGWINCH)
	defer signal.Stop(resize)
	replies := make(chan sessionControlResult, 1)
	busy, retry := false, false
	var reports []sessionReport // At most ready + lost for the one current child.
	var child *sessionTransport
	var releaseSlot func()
	releasePending := func() {
		if releaseSlot != nil {
			releaseSlot()
			releaseSlot = nil
		}
	}
	var output <-chan sessionOutput
	var done <-chan sessionTransportResult
	var grant SessionGrant
	proved, stopping := false, false
	var startupOutput []byte
	failure := ""
	last := SessionControlReply{Reason: "Waiting for shared recovery"}
	notice := ""
	shown := "" // Last status screen; redraw only on change, not every tick.
	defer func() {
		gate.close()
		if child != nil {
			child.stop()
			<-child.done
			child.input.Close()
		}
		releasePending()
		_ = writeSessionTerminal(context.Background(), outFD, []byte("\x1b[?2004l\x1b[0m"))
	}()
	phase := func() sessionPhase {
		if gate.current() != 0 {
			return sessionReady
		}
		if child != nil {
			return sessionConnecting
		}
		if last.Checking {
			return sessionChecking
		}
		return sessionOffline
	}
	localState := func() SessionLocalState {
		return SessionLocalState{Transport: transportIdentity, Token: cfg.Token, Session: cfg.Session, SessionID: cfg.SessionID, State: string(phase()), Origin: SessionInputOrigin{Client: request.Client, Attempt: grant.Attempt, Generation: gate.current()}}
	}
	render := func() {
		if phase() == sessionReady {
			return
		}
		detail := notice
		if detail == "" {
			switch phase() {
			case sessionConnecting:
				detail = "Connecting — input is discarded"
				if proved {
					detail = "Discarding unfinished input before enabling attachment"
				}
			case sessionChecking:
				detail = "Checking host…"
			default:
				detail = last.Reason
				if detail == "" {
					detail = "Waiting for attachment admission"
				}
			}
			if child == nil && failure != "" {
				detail = failure + "; " + detail
			}
			// A due or past retry is happening now; never count down "1s" for it.
			if wait := time.Until(last.RetryAt); !last.RetryAt.IsZero() && wait > 0 {
				detail += fmt.Sprintf("; retrying in %ds", int((wait+time.Second-1)/time.Second))
			}
		}
		screen := fmt.Sprintf("%s / %s\r\n\r\n%s\r\n\r\nEnter to retry now\r\n", cfg.Remote.Host, cfg.Session, detail)
		if screen == shown {
			return
		}
		// Bracketed paste lets the input gate retain the origin of a paste spanning
		// readiness, instead of forwarding its tail as fresh typing. A lost
		// attachment leaves Zellij's mouse/focus reporting and hidden cursor on.
		if err := writeSessionTerminal(ctx, outFD, []byte(sessionStatusModes+"\x1b[2J\x1b[H"+screen)); err == nil {
			shown = screen
		}
	}
	send := func() {
		if busy {
			return
		}
		busy = true
		m := request
		m.State = string(phase())
		m.Retry = retry
		retry = false
		if len(reports) > 0 {
			m.Attempt = reports[0].attempt
			m.Event = reports[0].event
		}
		go func() {
			callCtx, stop := context.WithTimeout(ctx, time.Second)
			defer stop()
			reply, err := cfg.Control.Exchange(callCtx, m)
			if late := sessionContextError(callCtx); late != nil {
				err = late
			}
			select {
			case replies <- sessionControlResult{m, reply, err}:
			case <-ctx.Done():
			}
		}()
	}
	lose := func() {
		gate.close()
		proved = false
		stopping = true
		startupOutput = nil
		if child != nil {
			child.stop()
		}
	}
	open := func() {
		if child == nil || stopping || !proved || gate.current() != 0 {
			return
		}
		if !time.Now().Before(grant.Deadline) {
			lose()
			return
		}
		opened, err := gate.open(inFD)
		if err != nil {
			lose()
		} else if opened {
			// Keep the reconnect screen intact until both proof and an input
			// boundary exist. Startup output is bounded and never treated as proof.
			// Queries emitted before the Zellij client started have no reader;
			// their replies must not become keystrokes for this attachment.
			data := append([]byte("\x1b[0m\x1b[2J\x1b[H"), stripPreClientQueries(startupOutput)...)
			startupOutput = nil
			shown = ""
			if err := writeSessionTerminal(ctx, outFD, data); err != nil {
				lose()
				return
			}
			releasePending()
			reports = append(reports, sessionReport{grant.Attempt, "ready"})
			notice = ""
			send()
		}
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	// While the coordinator is checking the host, or a scheduled retry is due,
	// its answer can change at any moment; poll faster than the steady tick.
	// Plain admission waits (slots busy, no retry time) keep the tick rate.
	var admission <-chan time.Time
	awaitAdmission := func() {
		admission = nil
		if child == nil && gate.current() == 0 && !last.Ended && (last.Checking || !last.RetryAt.IsZero() && !time.Now().Before(last.RetryAt)) {
			admission = time.After(sessionAdmissionPoll)
		}
	}
	render()
	send()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case call := <-localRequests:
			state := localState()
			reply := sessionLocalWire{Version: 1}
			if sessionContextError(call.ctx) != nil || !recoveryPeerAlive(call.peer) {
				reply.Error = "expired terminal request"
			} else if origin := call.request.Origin; origin != nil {
				if child == nil || state.Origin.Generation == 0 || *origin != state.Origin {
					reply.Error = "attachment changed or is not ready; paste discarded"
				} else if err := writeSessionTerminal(call.ctx, child.fd, call.request.Input); err != nil {
					lose()
					reply.Error = "cannot write to originating attachment"
				}
			}
			state = localState()
			reply.State = &state
			call.reply <- reply // one buffered reply; never wait for the IPC client
		case item, ok := <-input:
			if !ok {
				return fmt.Errorf("physical terminal closed")
			}
			current := gate.current()
			if child != nil && current != 0 && item.generation == current {
				if err := writeSessionTerminal(ctx, child.fd, item.data); err != nil {
					lose()
				}
			} else if current == 0 && item.generation == 0 && !item.paste && bytesContainEnter(item.data) {
				retry = true
				notice = "Retry requested — checking host…"
				render()
				send()
			}
			open()
		case <-resize:
			shown = ""
			render()
			if child != nil {
				if size, err := unix.IoctlGetWinsize(inFD, unix.TIOCGWINSZ); err == nil {
					_ = unix.IoctlSetWinsize(child.fd, unix.TIOCSWINSZ, size)
				}
			}
		case packet := <-output:
			if gate.current() != 0 {
				if err := writeSessionTerminal(ctx, outFD, packet.data); err != nil {
					lose()
				}
			} else if !stopping {
				if len(startupOutput)+len(packet.data) > maxAttachmentFrame {
					lose()
				} else {
					startupOutput = append(startupOutput, packet.data...)
				}
			}
			for _, event := range packet.events {
				if event == "ready" && !stopping {
					proved = true
				} else {
					lose()
				}
			}
			open()
		case result := <-done:
			gate.close()
			child.stop()
			child.input.Close()
			releasePending()
			child = nil
			output = nil
			done = nil
			proved = false
			startupOutput = nil
			if result.outcome == "detached" {
				return nil
			}
			reports = append(reports, sessionReport{grant.Attempt, "lost"})
			failure = "Connection lost"
			if result.outcome != "" {
				failure += " (" + result.outcome + ")"
			}
			notice = ""
			render()
			send()
		case result := <-replies:
			busy = false
			if result.err != nil {
				retry = retry || result.request.Retry
				notice = "Shared recovery unavailable: " + recoveryDisplayReason(result.err.Error())
				render()
				continue
			}
			if result.request.Event != "" && len(reports) > 0 && reports[0] == (sessionReport{result.request.Attempt, result.request.Event}) {
				reports = reports[1:]
				if result.request.Event == "lost" {
					grant = SessionGrant{}
					if err := freshAttempt(); err != nil {
						return err
					}
				}
			}
			last = result.reply
			notice = ""
			if last.Ended {
				return nil
			}
			if child != nil && last.CancelAttempt == grant.Attempt {
				lose()
			}
			if last.Reset && gate.current() == 0 {
				if child != nil {
					lose()
				} else if len(reports) == 0 {
					grant = SessionGrant{}
					// A grant reply lost to a timeout or suspend leaves the
					// coordinator holding this attempt; release it explicitly.
					if last.CancelAttempt == request.Attempt {
						reports = append(reports, sessionReport{request.Attempt, "lost"})
					}
				}
			}
			if g := last.Grant; g != nil && !last.Reset && result.request.Event == "" && child == nil && len(reports) == 0 && grant.Attempt == "" && g.Attempt == request.Attempt && validAttachmentAttempt(g.Attempt) {
				if !time.Now().Before(g.Deadline) {
					reports = append(reports, sessionReport{g.Attempt, "lost"})
					send()
					continue
				}
				command, e := PlanSessionAttachment(cfg.Remote, cfg.Session, cfg.SessionID, g.Attempt)
				if e != nil {
					return e
				}
				grant = *g
				failure = ""
				startupOutput = nil
				stopping = false
				proved = false
				if admission, ok := cfg.Control.(interface{ acquirePendingSlot() (func(), error) }); ok {
					releaseSlot, e = admission.acquirePendingSlot()
				}
				if e == nil {
					child, e = startSessionTransport(ctx, command, g.Attempt, inFD)
				}
				if e != nil {
					releasePending()
					failure = "Cannot start attachment: " + e.Error()
					reports = append(reports, sessionReport{grant.Attempt, "lost"})
				} else {
					output = child.output
					done = child.done
				}
			}
			render()
			if len(reports) > 0 || retry {
				send()
			}
			awaitAdmission()
		case <-admission:
			admission = nil
			send()
		case <-ticker.C:
			if child != nil && gate.current() == 0 && !time.Now().Before(grant.Deadline) {
				lose()
			}
			open()
			render()
			send()
		}
	}
}

// Status screens reset attributes and whatever reporting modes a lost Zellij
// client left enabled (mouse, focus, hidden cursor, one pushed kitty keyboard
// level; popping an empty stack is a no-op). Bracketed paste stays on.
const sessionStatusModes = "\x1b[0m\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?1015l\x1b[?1004l\x1b[<u\x1b[?25h\x1b[?2004h"

const sessionAdmissionPoll = 100 * time.Millisecond

func bytesContainEnter(data []byte) bool { return strings.ContainsAny(string(data), "\r\n") }
