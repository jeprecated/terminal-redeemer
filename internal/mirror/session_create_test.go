package mirror

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jmo/terminal-redeemer/internal/zellijlive"
)

type creationCatalog func(context.Context) (zellijlive.Catalog, error)

func (f creationCatalog) Observe(ctx context.Context) (zellijlive.Catalog, error) { return f(ctx) }

func TestSessionCreationRunsOnceAndRequiresReceipt(t *testing.T) {
	const name = "redeem-0123456789abcdef0123456789abcdef"
	for _, mode := range []string{"success", "collision", "create failed", "observation failed", "replaced before receipt"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			observes, creates := 0, 0
			observer := creationCatalog(func(context.Context) (zellijlive.Catalog, error) {
				observes++
				if observes == 1 && mode != "collision" {
					return activeCatalog(), nil
				}
				if mode == "observation failed" {
					return zellijlive.Catalog{}, errors.New("lost observation")
				}
				catalog := activeCatalog(name)
				if mode == "replaced before receipt" {
					session := catalog.Sessions[name]
					session.ExactID = recoveryRequest(1).SessionID
					catalog.Sessions[name] = session
				}
				return catalog, nil
			})
			receipt, err := createSessionOnce(ctx, name, observer, func(context.Context) (string, error) {
				creates++
				if mode == "create failed" {
					return "", errors.New("lost creator acknowledgement")
				}
				return activeCatalog(name).Sessions[name].ExactID, nil
			})
			if mode == "success" {
				if err != nil || receipt.Session != name || !validSessionID(receipt.SessionID) {
					t.Fatalf("receipt %+v: %v", receipt, err)
				}
			} else if err == nil || receipt.SessionID != "" {
				t.Fatalf("uncertain/colliding creation became attachment: %+v %v", receipt, err)
			}
			want := 1
			if mode == "collision" {
				want = 0
			}
			if creates != want {
				t.Fatalf("creation replayed: %d", creates)
			}
			if mode == "create failed" && observes != 1 {
				t.Fatal("uncertain creation reconciled by generated name")
			}
		})
	}
}

func TestRemoteCreationLostOrWrongReceiptNeverRetries(t *testing.T) {
	const name = "redeem-0123456789abcdef0123456789abcdef"
	remote := RemoteConfig{Host: "source", SSHCommand: "ssh", SnapshotCommand: []string{"env", "PROFILE=Agent's", "redeem", "mirror", "snapshot"}}
	for _, mode := range []string{"success", "lost", "truncated", "foreign"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			runner := runnerFunc{run: func(context.Context, Command) error { t.Fatal("unexpected second operation"); return nil }, output: func(_ context.Context, command Command) ([]byte, error) {
				calls++
				text := RenderCommand(command)
				if !strings.Contains(text, "session-create") || !strings.Contains(text, "ConnectionAttempts=1") {
					t.Fatalf("unbounded/wrong creation plan: %s", text)
				}
				if mode == "lost" {
					return nil, errors.New("transport lost after creation")
				}
				if mode == "truncated" {
					return []byte("{"), nil
				}
				receipt := CreatedSession{Session: name, SessionID: recoveryRequest(0).SessionID}
				if mode == "foreign" {
					receipt.Session = "other"
				}
				return json.Marshal(receipt)
			}}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			receipt, err := CreateRemoteSession(ctx, runner, remote, name)
			if (err == nil) != (mode == "success") || calls != 1 {
				t.Fatalf("mode=%s receipt=%+v err=%v calls=%d", mode, receipt, err, calls)
			}
		})
	}
}

func TestSessionCreateProcessHelper(t *testing.T) {
	name := os.Getenv("REDEEM_TEST_CREATE")
	if name == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	receipt, err := RunSessionCreation(ctx, name)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	json.NewEncoder(os.Stdout).Encode(receipt)
	os.Exit(0)
}

func TestRealZellijCreationReceiptAttachesExactly(t *testing.T) {
	f := realAttachmentFixture(t)
	const name = "redeem-0123456789abcdef0123456789abcdef"
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, f.command, "kill-session", name)
		cmd.Env = f.env
		cmd.WaitDelay = 200 * time.Millisecond
		_ = cmd.Run()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSessionCreateProcessHelper$")
	cmd.Env = append(append([]string{}, f.env...), "REDEEM_TEST_CREATE="+name, "ZELLIJ=1", "ZELLIJ_SESSION_NAME=foreign")
	payload, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			t.Fatalf("creation: %v %s", err, exit.Stderr)
		}
		t.Fatal(err)
	}
	var receipt CreatedSession
	if err := json.Unmarshal(payload, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Session != name || !validSessionID(receipt.SessionID) {
		t.Fatalf("invalid receipt: %+v", receipt)
	}
	p := f.attach(t, name, receipt.SessionID)
	p.waitText(t, AttachmentMarker(testAttachmentAttempt, "ready"))
	p.file.Write([]byte{5})
	p.waitText(t, AttachmentMarker(testAttachmentAttempt, "detached"))
	// A replay of the source operation refuses the existing generated name.
	replay := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSessionCreateProcessHelper$")
	replay.Env = cmd.Env
	if err := replay.Run(); err == nil {
		t.Fatal("source creation replay adopted an existing session")
	}
}

func TestSessionCreationPinsReceiptSocketBase(t *testing.T) {
	env := sessionCreationEnv([]string{"PATH=/bin", "ZELLIJ=1", "ZELLIJ_SESSION_NAME=foreign", "ZELLIJ_SOCKET_DIR=/elsewhere"}, "/run/user/1000/zellij")
	want := []string{"PATH=/bin", "ZELLIJ_SOCKET_DIR=/run/user/1000/zellij"}
	if !reflect.DeepEqual(env, want) {
		t.Fatalf("creation env = %q, want %q", env, want)
	}
}
