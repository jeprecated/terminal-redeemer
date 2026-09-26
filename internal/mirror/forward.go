package mirror

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// forwardIdleSeconds bounds a forward's life: a port tunnel stays up at least
// this long and then until its last connection closes; a file server exits
// after this long without a request.
const forwardIdleSeconds = 300

// fileServerScript serves the directory containing argv[2] on
// 127.0.0.1:argv[1] until it has been idle for argv[3] seconds. A missing
// path exits at once so the tunnel fails instead of serving a 404.
const fileServerScript = `import functools,http.server as h,os,sys
p=sys.argv[2]
if not os.path.exists(p):sys.exit("no such file or directory: "+p)
s=h.ThreadingHTTPServer(("127.0.0.1",int(sys.argv[1])),functools.partial(h.SimpleHTTPRequestHandler,directory=os.path.dirname(p) or "."))
s.timeout=int(sys.argv[3])
idle=[]
s.handle_timeout=lambda:idle.append(1)
while not idle:s.handle_request()`

type ForwardConfig struct {
	Host       string
	SSHCommand string
	SSHOptions []string
	// Target is a remote port number or a remote file/directory path.
	Target string
}

type ForwardPlan struct {
	URL string
	SSH Command
}

// PlanForward builds one self-closing `ssh -L` tunnel. freePort returns
// preferred when it is free locally, otherwise any free local port.
func PlanForward(cfg ForwardConfig, freePort func(preferred int) (int, error)) (ForwardPlan, error) {
	target := strings.TrimSpace(cfg.Target)
	if target == "" {
		return ForwardPlan{}, fmt.Errorf("forward target is required (port or remote path)")
	}
	remotePort, isPort := parsePort(target)
	if !isPort && strings.Trim(target, "0123456789") == "" {
		return ForwardPlan{}, fmt.Errorf("invalid port %q", target)
	}
	localPort, err := freePort(remotePort)
	if err != nil {
		return ForwardPlan{}, fmt.Errorf("choose local port: %w", err)
	}
	remoteHost := "localhost"
	remoteCommand := []string{"sleep", strconv.Itoa(forwardIdleSeconds)}
	urlPath := "/"
	if !isPort {
		// Serve the parent directory so a file's relative assets resolve and a
		// directory path redirects to its own listing or index.html.
		clean := path.Clean(target)
		remotePort, remoteHost = localPort, "127.0.0.1"
		remoteCommand = []string{"python3", "-c", fileServerScript, strconv.Itoa(remotePort), clean, strconv.Itoa(forwardIdleSeconds)}
		if base := path.Base(clean); base != "/" {
			urlPath += base
		}
	}
	forward := fmt.Sprintf("%d:%s:%d", localPort, remoteHost, remotePort)
	options := append(unattendedSSHOptions(), cfg.SSHOptions...)
	args, err := buildSSHArgs(options, []string{"-n", "-o", "ExitOnForwardFailure=yes", "-L", forward}, cfg.Host, QuoteCommand(remoteCommand))
	if err != nil {
		return ForwardPlan{}, err
	}
	return ForwardPlan{
		URL: fmt.Sprintf("http://localhost:%d%s", localPort, urlPath),
		SSH: Command{Name: cfg.SSHCommand, Args: args},
	}, nil
}

func parsePort(value string) (int, bool) {
	port, err := strconv.Atoi(value)
	return port, err == nil && port >= 1 && port <= 65535
}

// FreeLocalPort returns preferred when it can be bound on loopback, otherwise
// a kernel-chosen free port. ExitOnForwardFailure covers the reuse race.
func FreeLocalPort(preferred int) (int, error) {
	if preferred > 0 {
		if listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", preferred)); err == nil {
			_ = listener.Close()
			return preferred, nil
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = listener.Close() }()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// StartForward launches ssh detached, since the tunnel outlives Redeem, and
// returns once plan.URL answers. On failure it kills the tunnel rather than
// leaving it running until the idle bound. Output goes to a file because the
// tunnel keeps its descriptors open for its whole life.
func StartForward(ctx context.Context, plan ForwardPlan) error {
	output, err := os.CreateTemp("", "redeem-forward-*.log")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(output.Name()); _ = output.Close() }()
	cmd := exec.Command(plan.SSH.Name, plan.SSH.Args...)
	cmd.Stdout, cmd.Stderr = output, output
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	if err := WaitForwardReady(ctx, plan.URL, exited); err != nil {
		_ = cmd.Process.Kill()
		// Report only ssh's last line; refused channel opens repeat per probe.
		detail, _ := os.ReadFile(output.Name())
		lines := strings.Split(strings.TrimSpace(string(detail)), "\n")
		if message := lines[len(lines)-1]; message != "" {
			return fmt.Errorf("%w: %s", err, message)
		}
		return err
	}
	return nil
}

// forwardRemoteGrace is how long a bound tunnel may keep failing while the
// remote end starts before the target is reported as not listening.
const forwardRemoteGrace = 3 * time.Second

// WaitForwardReady polls url until the remote end answers HTTP, so the
// browser never opens onto a server that is still starting or absent. It
// stops early when exited reports the tunnel process has gone.
func WaitForwardReady(ctx context.Context, url string, exited <-chan error) error {
	client := http.Client{Timeout: time.Second}
	var bound time.Time
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		response, err := client.Do(request)
		if err == nil {
			_ = response.Body.Close()
			return nil
		}
		// Refused means ssh has not bound the local port yet; anything else
		// means the tunnel is up and the remote end is not answering.
		if !errors.Is(err, syscall.ECONNREFUSED) {
			if bound.IsZero() {
				bound = time.Now()
			} else if time.Since(bound) > forwardRemoteGrace {
				return fmt.Errorf("nothing answered HTTP through the tunnel at %s", url)
			}
		}
		select {
		case waitErr := <-exited:
			return fmt.Errorf("tunnel exited: %v", waitErr)
		case <-ctx.Done():
			return fmt.Errorf("nothing answered HTTP at %s: %w", url, err)
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// OpenURL launches the desktop opener detached, since it may exec a browser
// that outlives Redeem.
func OpenURL(opener string, url string) error {
	cmd := exec.Command(opener, url)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
