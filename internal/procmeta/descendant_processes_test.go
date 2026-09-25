package procmeta

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestObserveDescendantProcessesNeverReturnsPartialOrReusedIdentity(t *testing.T) {
	for _, change := range []string{"unreadable", "reused", "reparented"} {
		t.Run(change, func(t *testing.T) {
			root := t.TempDir()
			writeProcessTreeFixture(t, root, 100, 1, 10, []byte("kitty\x00"))
			writeProcessTreeFixture(t, root, 101, 100, 11, []byte("first\x00"))
			writeProcessTreeFixture(t, root, 102, 100, 12, []byte("second\x00"))
			result, err := ObserveDescendantProcesses(context.Background(), root, 100)
			if err != nil || len(result) != 2 || result[0].PID != 101 || result[0].ParentPID != 100 || result[0].StartTime != "11" {
				t.Fatalf("complete observation: %+v %v", result, err)
			}
			result, err = observeDescendantProcesses(context.Background(), root, 100, func() {
				switch change {
				case "unreadable":
					if err := os.Remove(filepath.Join(root, "102", "cmdline")); err != nil {
						t.Fatal(err)
					}
				case "reused":
					writeProcessStat(t, root, 102, 100, 99)
				case "reparented":
					writeProcessStat(t, root, 102, 1, 12)
				}
			})
			if err == nil || len(result) != 0 {
				t.Fatalf("partial/replaced evidence escaped: %+v %v", result, err)
			}
		})
	}
}
