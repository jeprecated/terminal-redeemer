package slicecontroller

import (
	"errors"
	"os"
	"testing"
)

func TestStoreSparseOversizedAuthorityIsBoundedAndNeverReenrolled(t *testing.T) {
	for _, target := range []string{"marker", "current"} {
		t.Run(target, func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			state, err := store.Initialize(Namespace{Host: "host", Leech: "leech"})
			if err != nil {
				t.Fatal(err)
			}
			path := store.current
			if target == "marker" {
				path = store.marker
			}
			const sparseSize = int64(MaxControllerStateBytes) + (1 << 30)
			if err := os.Truncate(path, sparseSize); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Read(); !errors.Is(err, ErrInvalidState) {
				t.Fatalf("sparse oversized %s accepted: %v", target, err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Size() != sparseSize {
				t.Fatalf("rejected %s was rewritten: size=%d", target, info.Size())
			}
			if _, err := store.Initialize(state.Namespace); !errors.Is(err, ErrAlreadyInitialized) {
				t.Fatalf("rejected %s was re-enrolled: %v", target, err)
			}
		})
	}
}
