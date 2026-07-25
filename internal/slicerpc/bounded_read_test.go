package slicerpc

import (
	"os"
	"testing"
	"time"
)

func TestTokenStoreSparseOversizedFilesAreBoundedAndNotRewritten(t *testing.T) {
	for _, target := range []string{"marker", "record"} {
		t.Run(target, func(t *testing.T) {
			stateDir := t.TempDir()
			store, err := NewTokenStore(stateDir)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := store.CreatePending("host", "bounded-token", time.Unix(1, 0).UTC()); err != nil {
				t.Fatal(err)
			}
			path := store.marker
			if target == "record" {
				path, _ = store.path("bounded-token")
			}
			const sparseSize = int64(MaxTokenRecordBytes) + (1 << 30)
			if err := os.Truncate(path, sparseSize); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Read("bounded-token"); err == nil {
				t.Fatalf("sparse oversized %s accepted", target)
			}
			if target == "marker" {
				if _, err := NewTokenStore(stateDir); err == nil {
					t.Fatal("oversized marker was silently re-enrolled")
				}
			} else if _, err := NewTokenStore(stateDir); err != nil {
				t.Fatalf("existing enrollment rejected for record corruption: %v", err)
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
