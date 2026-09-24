package mirror

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSessionCatalogNativeOutputBounds(t *testing.T) {
	for _, redirect := range []string{"", " >&2"} {
		t.Run(redirect, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			started := time.Now()
			_, err := boundedSessionCatalog(ctx, ExecRunner{}, Command{Name: "sh", Args: []string{"-c", "head -c 2097152 /dev/zero" + redirect}})
			if err == nil || !strings.Contains(err.Error(), "exceeds bound") || time.Since(started) > time.Second {
				t.Fatalf("output not bounded promptly: %v", err)
			}
		})
	}
}
