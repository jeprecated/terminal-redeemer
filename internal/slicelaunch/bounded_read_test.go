package slicelaunch

import (
	"os"
	"testing"
	"time"
)

func TestStoreSparseOversizedFilesAreBoundedAndNotRewritten(t *testing.T) {
	for _, target := range []string{"marker", "mode", "intent"} {
		t.Run(target, func(t *testing.T) {
			stateDir := t.TempDir()
			store, err := NewStore(stateDir)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Mode(false); err != nil {
				t.Fatal(err)
			}
			intent, err := store.Create("Work", time.Minute, time.Unix(1, 0).UTC())
			if err != nil {
				t.Fatal(err)
			}
			path := store.marker
			switch target {
			case "mode":
				path = store.modePath()
			case "intent":
				path, _ = store.intentPath(intent.Token)
			}
			const sparseSize = int64(MaxIntentBytes) + (1 << 30)
			if err := os.Truncate(path, sparseSize); err != nil {
				t.Fatal(err)
			}
			switch target {
			case "marker", "mode":
				if _, err := store.Mode(false); err == nil {
					t.Fatalf("sparse oversized %s accepted", target)
				}
			case "intent":
				if _, err := store.Read(intent.Token); err == nil {
					t.Fatal("sparse oversized intent accepted")
				}
			}
			if target == "marker" {
				if _, err := NewStore(stateDir); err == nil {
					t.Fatal("oversized marker was silently re-enrolled")
				}
			} else if _, err := NewStore(stateDir); err != nil {
				t.Fatalf("existing enrollment rejected for unrelated %s corruption: %v", target, err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Size() != sparseSize {
				t.Fatalf("rejected %s was rewritten: size=%d", target, info.Size())
			}
		})
	}
}
