package mirror

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func kittyLaunchPlans(t *testing.T) []LaunchPlan {
	t.Helper()
	cfg := LaunchConfig{
		SourceHost: "source-example", SessionID: recoveryRequest(0).SessionID,
		SSHCommand: "ssh", LauncherCommand: "kitty", SelfCommand: "/must-not-run/redeem",
		AppID: "redeem-mirror", CorrelationToken: recoveryRequest(0).Token,
		SnapshotCommand: []string{"redeem", "mirror", "snapshot"},
	}
	opened, err := PlanLaunch(Window{ZellijSession: "existing", Title: "Project title"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	created, err := PlanNew("redeem-0123456789abcdef0123456789abcdef", cfg)
	if err != nil {
		t.Fatal(err)
	}
	return []LaunchPlan{opened, created}
}

func TestKittyLaunchPlatformFlags(t *testing.T) {
	for _, plan := range kittyLaunchPlans(t) {
		args := plan.Command.Args
		classIndex := slices.Index(args, "--class")
		if runtime.GOOS == "darwin" {
			if classIndex != -1 {
				t.Fatalf("Linux-only class on macOS: %v", args)
			}
		} else if classIndex < 0 || args[classIndex+1] != "redeem-mirror" {
			t.Fatalf("missing Linux ownership class: %v", args)
		}
		titleIndex := slices.Index(args, "--title")
		if titleIndex < 0 || args[titleIndex+1] != plan.Title || !slices.Contains(args, "--detach") || !slices.Contains(args, "session-supervisor") {
			t.Fatalf("lost detached launch, title or supervisor: %v", args)
		}
	}
}

func TestKittyLaunchNativeParser(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("native macOS Kitty parser")
	}
	kitty := os.Getenv("REDEEM_TEST_KITTY")
	if kitty == "" {
		t.Skip("set REDEEM_TEST_KITTY to test the packaged Kitty parser")
	}
	for _, plan := range kittyLaunchPlans(t) {
		t.Run(plan.Session, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			args := append([]string{"--version"}, plan.Command.Args...)
			output, err := exec.CommandContext(ctx, kitty, args...).CombinedOutput()
			if err != nil || !strings.HasPrefix(string(output), "kitty ") {
				t.Fatalf("Kitty rejected launch plan without opening a window: %v: %s", err, output)
			}
		})
	}
}
