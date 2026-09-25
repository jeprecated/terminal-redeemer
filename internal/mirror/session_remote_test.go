package mirror

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSessionCatalogPreservesPrefixAndForcesUnattendedSSH(t *testing.T) {
	cfg := RemoteConfig{Host: "user@source", SSHCommand: "ssh", SSHOptions: []string{"-p", "2222", "-o", "BatchMode=no", "-o", "ConnectTimeout=0"}, SnapshotCommand: []string{"env", "PROFILE=Agent's", "/nix/store/redeem bin", "mirror", "snapshot"}}
	original := append([]string{}, cfg.SSHOptions...)
	command, err := PlanSessionCatalog(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.SSHOptions, original) {
		t.Fatal("mutated configured options")
	}
	if command.Name != "ssh" || !reflect.DeepEqual(command.Args[len(command.Args)-5:len(command.Args)-1], []string{"-T", "-n", "--", cfg.Host}) {
		t.Fatalf("unsafe command: %+v", command)
	}
	want := QuoteCommand([]string{"env", "PROFILE=Agent's", "/nix/store/redeem bin", "mirror", "session-catalog"})
	if command.Args[len(command.Args)-1] != want {
		t.Fatalf("lost remote prefix: %+v", command)
	}
	if !reflect.DeepEqual(command.Args[:4], []string{"-o", "BatchMode=yes", "-o", "ConnectionAttempts=1"}) {
		t.Fatalf("retry policy not first: %+v", command)
	}
	if !reflect.DeepEqual(command.Args[10:14], []string{"-o", "ControlMaster=no", "-o", "ControlPath=none"}) {
		t.Fatalf("connection sharing not disabled before configured options: %+v", command)
	}
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("OpenSSH unavailable; argv checks passed")
	}
	// -G only evaluates configuration. No SSH connection is opened.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, ssh, append([]string{"-G", "-F", "/dev/null"}, command.Args...)...).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, option := range []string{"batchmode yes", "connectionattempts 1", "connecttimeout 5", "serveraliveinterval 5", "serveralivecountmax 2", "requesttty false", "stdinnull yes", "controlmaster false"} {
		if !strings.Contains(string(output), option+"\n") {
			t.Fatalf("missing effective option %q: %s", option, output)
		}
	}
	if strings.Contains(string(output), "\ncontrolpath ") {
		t.Fatalf("shared control socket still configured: %s", output)
	}
}

func TestAcquireSessionInventoryRequiresBoundedValidEvidence(t *testing.T) {
	cfg := RemoteConfig{Host: "source", SSHCommand: "ssh", SnapshotCommand: []string{"redeem", "mirror", "snapshot"}}
	inventory, err := ObserveSessionInventory(context.Background(), &inventoryCataloger{catalog: activeCatalog("Alpha")})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(inventory)
	runner := &recordingRunner{outputs: []outputResult{{data: payload}}}
	if _, err := AcquireSessionInventory(context.Background(), runner, cfg); err == nil || len(runner.outputCalls) != 0 {
		t.Fatal("unbounded acquisition attempted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := AcquireSessionInventory(ctx, runner, cfg)
	if err != nil || !reflect.DeepEqual(got.SessionIDs, inventory.SessionIDs) || len(runner.outputCalls) != 1 {
		t.Fatalf("inventory=%+v error=%v", got, err)
	}
	cancel()
	if _, err := AcquireSessionInventory(ctx, runner, cfg); !errors.Is(err, context.Canceled) || len(runner.outputCalls) != 1 {
		t.Fatalf("canceled request executed: %v", err)
	}
	for _, result := range []outputResult{{data: []byte(`{}`)}, {data: []byte(`{"active_zellij_sessions":[]}`)}, {err: errors.New("host unavailable")}} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		got, err := AcquireSessionInventory(ctx, &recordingRunner{outputs: []outputResult{result}}, cfg)
		cancel()
		if err == nil || got.ActiveSessions != nil || got.SessionIDs != nil {
			t.Fatalf("unsupported/failed response became authoritative: %+v %v", got, err)
		}
	}
}

func TestSessionCatalogRejectsAmbiguousConfiguration(t *testing.T) {
	cases := []RemoteConfig{
		{Host: "--bad", SSHCommand: "ssh", SnapshotCommand: []string{"redeem", "mirror", "snapshot"}},
		{Host: "source", SSHCommand: "", SnapshotCommand: []string{"redeem", "mirror", "snapshot"}},
		{Host: "source", SSHCommand: "ssh", SnapshotCommand: []string{"custom", "snapshot"}},
		{Host: "source", SSHCommand: "ssh", SnapshotCommand: []string{"", "mirror", "snapshot"}},
		{Host: "source", SSHCommand: "ssh", SSHOptions: []string{"--", "different-host"}, SnapshotCommand: []string{"redeem", "mirror", "snapshot"}},
	}
	for _, cfg := range cases {
		if _, err := PlanSessionCatalog(cfg); err == nil {
			t.Fatalf("accepted %+v", cfg)
		}
	}
}
