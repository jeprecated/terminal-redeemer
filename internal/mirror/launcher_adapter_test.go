package mirror

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestLauncherAdapterPreservesExactSupervisorArgv(t *testing.T) {
	request := recoveryRequest(0)
	cfg := LaunchConfig{SourceHost: "source-example", SSHCommand: "/usr/bin/ssh", LauncherCommand: "kitty", SelfCommand: "/path with spaces/redeem", AppID: "owned", SessionID: request.SessionID, CorrelationToken: request.Token, SnapshotCommand: []string{"redeem", "mirror", "snapshot"}}
	window := Window{ZellijSession: "-literal' ; $(touch nope)", Title: "literal title"}
	builtin, err := PlanLaunch(window, cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.LauncherAdapter = []string{"/adapter with spaces", "--group", "group one"}
	adapter, err := PlanLaunch(window, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if adapter.Command.Name != cfg.LauncherAdapter[0] || adapter.Title != builtin.Title {
		t.Fatal(adapter)
	}
	boundary := slices.Index(adapter.Command.Args, "--")
	expected := []string{"--group", "group one", "--protocol-version=1", "--title=" + builtin.Title, "--source-host=" + cfg.SourceHost, "--session=" + window.ZellijSession, "--session-id=" + cfg.SessionID}
	if boundary < 0 || !reflect.DeepEqual(adapter.Command.Args[:boundary], expected) {
		t.Fatalf("adapter metadata: %v", adapter.Command.Args)
	}
	child := builtin.Command.Args[slices.Index(builtin.Command.Args, "-e")+1:]
	if !reflect.DeepEqual(adapter.Command.Args[boundary+1:], child) {
		t.Fatalf("supervisor changed: %v", adapter.Command.Args)
	}
	for _, flag := range []string{"--class", "--detach", "--override", "-e"} {
		if slices.Contains(adapter.Command.Args[:boundary], flag) {
			t.Fatalf("Kitty flag %s leaked", flag)
		}
	}
	if !reflect.DeepEqual(cfg.LauncherAdapter, []string{"/adapter with spaces", "--group", "group one"}) {
		t.Fatal("mutated config")
	}
}

func TestLauncherAdapterNewRequiresReceiptAndRejectsClipboard(t *testing.T) {
	cfg := LaunchConfig{SourceHost: "source-example", SSHCommand: "ssh", SelfCommand: "redeem", LauncherAdapter: []string{"adapter"}, SessionID: recoveryRequest(0).SessionID, SnapshotCommand: []string{"redeem", "mirror", "snapshot"}}
	session := "redeem-" + strings.Repeat("a", 32)
	plan, err := PlanNew(session, cfg)
	if err != nil || plan.Command.Name != "adapter" {
		t.Fatalf("new adapter: %+v %v", plan, err)
	}
	cfg.Clipboard = true
	if _, err := PlanNew(session, cfg); err == nil {
		t.Fatal("accepted incompatible clipboard")
	}
	cfg.Clipboard = false
	cfg.SessionID = ""
	if _, err := PlanNew(session, cfg); err == nil {
		t.Fatal("accepted missing receipt")
	}
}
