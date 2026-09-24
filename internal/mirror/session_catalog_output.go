package mirror

import (
	"bytes"
	"context"
	"fmt"

	"github.com/jmo/terminal-redeemer/internal/zellijlive"
)

type sessionCatalogOutput struct {
	buffer   bytes.Buffer
	max      int
	overflow bool
	cancel   context.CancelFunc
}

func (b *sessionCatalogOutput) Write(p []byte) (int, error) {
	if len(p) > b.max-b.buffer.Len() {
		b.overflow = true
		b.cancel()
		return 0, fmt.Errorf("session catalog output exceeds bound")
	}
	return b.buffer.Write(p)
}
func boundedSessionCatalog(ctx context.Context, runner ExecRunner, command Command) ([]byte, error) {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	stdout := &sessionCatalogOutput{max: zellijlive.MaxCatalogBytes, cancel: stop}
	stderr := &sessionCatalogOutput{max: 64 << 10, cancel: stop}
	cmd := runner.command(ctx, command)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if stdout.overflow || stderr.overflow {
		return nil, fmt.Errorf("session catalog output exceeds bound")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, stderr.buffer.String())
	}
	return stdout.buffer.Bytes(), nil
}
