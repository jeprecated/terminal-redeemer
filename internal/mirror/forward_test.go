package mirror

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func fixedPort(port int) func(int) (int, error) {
	return func(int) (int, error) { return port, nil }
}

func TestPlanForwardPortReusesRemotePortAndSleepsBoundedly(t *testing.T) {
	var asked int
	plan, err := PlanForward(ForwardConfig{Host: "lattice", SSHCommand: "ssh", SSHOptions: []string{"-p", "2222"}, Target: "8765"},
		func(preferred int) (int, error) { asked = preferred; return preferred, nil })
	if err != nil {
		t.Fatal(err)
	}
	if asked != 8765 || plan.URL != "http://localhost:8765/" {
		t.Fatalf("asked=%d url=%q", asked, plan.URL)
	}
	args := plan.SSH.Args
	for _, want := range [][]string{{"-p", "2222"}, {"-L", "8765:localhost:8765"}, {"-o", "ExitOnForwardFailure=yes"}, {"-o", "ControlMaster=no"}} {
		if !containsSequence(args, want) {
			t.Fatalf("args %q missing %q", args, want)
		}
	}
	if !slices.Equal(args[len(args)-3:], []string{"--", "lattice", "'sleep' '300'"}) {
		t.Fatalf("unexpected args %q", args)
	}
}

func TestPlanForwardPortFallsBackToFreeLocalPort(t *testing.T) {
	plan, err := PlanForward(ForwardConfig{Host: "lattice", SSHCommand: "ssh", Target: "3000"}, fixedPort(41000))
	if err != nil {
		t.Fatal(err)
	}
	if !containsSequence(plan.SSH.Args, []string{"-L", "41000:localhost:3000"}) || plan.URL != "http://localhost:41000/" {
		t.Fatalf("args=%q url=%q", plan.SSH.Args, plan.URL)
	}
}

func TestPlanForwardPathServesParentDirectoryOnTunnelPort(t *testing.T) {
	plan, err := PlanForward(ForwardConfig{Host: "lattice", SSHCommand: "ssh", Target: "proj/out/report.html"}, fixedPort(41001))
	if err != nil {
		t.Fatal(err)
	}
	if plan.URL != "http://localhost:41001/report.html" {
		t.Fatalf("url=%q", plan.URL)
	}
	if !containsSequence(plan.SSH.Args, []string{"-L", "41001:127.0.0.1:41001"}) {
		t.Fatalf("args=%q", plan.SSH.Args)
	}
	remote := plan.SSH.Args[len(plan.SSH.Args)-1]
	if !strings.HasPrefix(remote, "'python3' '-c' ") || !strings.HasSuffix(remote, " '41001' 'proj/out/report.html' '300'") {
		t.Fatalf("remote command %q", remote)
	}
}

func TestPlanForwardRejectsBadTargetsAndHosts(t *testing.T) {
	for _, cfg := range []ForwardConfig{
		{Host: "lattice", SSHCommand: "ssh", Target: ""},
		{Host: "lattice", SSHCommand: "ssh", Target: "70000"},
		{Host: "lattice", SSHCommand: "ssh", Target: "0"},
		{Host: "-oProxyCommand=x", SSHCommand: "ssh", Target: "80"},
		{Host: "lattice", SSHCommand: "ssh", SSHOptions: []string{"-L", "1:x:1"}, Target: "80"},
	} {
		if _, err := PlanForward(cfg, fixedPort(41002)); err == nil {
			t.Fatalf("expected error for %+v", cfg)
		}
	}
}

func TestFreeLocalPortAvoidsBusyPreferredPort(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()
	busyPort := busy.Addr().(*net.TCPAddr).Port
	port, err := FreeLocalPort(busyPort)
	if err != nil || port == busyPort || port == 0 {
		t.Fatalf("port=%d err=%v busy=%d", port, err, busyPort)
	}
}

func TestWaitForwardReadyReturnsOnceHTTPAnswers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := WaitForwardReady(ctx, server.URL, nil); err != nil {
		t.Fatal(err)
	}
	server.Close()
	ctx, cancel = context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := WaitForwardReady(ctx, server.URL, nil); err == nil {
		t.Fatal("expected timeout for closed server")
	}
}

func containsSequence(args []string, want []string) bool {
	for i := 0; i+len(want) <= len(args); i++ {
		if slices.Equal(args[i:i+len(want)], want) {
			return true
		}
	}
	return false
}
