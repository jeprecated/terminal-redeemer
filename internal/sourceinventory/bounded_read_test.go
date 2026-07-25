package sourceinventory

import (
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
			if err := store.CreateEnrollmentMarker(); err != nil {
				t.Fatal(err)
			}
			if err := store.Write(State{StorageVersion: StorageVersion, Initialized: true, SourceHostID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}); err != nil {
				t.Fatal(err)
			}
			path := store.Path()
			if target == "marker" {
				path = store.MarkerPath()
			}
			const sparseSize = int64(MaxStateBytes) + (1 << 30)
			if err := os.Truncate(path, sparseSize); err != nil {
				t.Fatal(err)
			}
			if target == "marker" {
				if present, err := store.EnrollmentMarkerPresent(); err == nil || present {
					t.Fatalf("sparse oversized marker accepted: present=%t err=%v", present, err)
				}
			} else if _, err := store.Read(); err == nil {
				t.Fatal("sparse oversized current authority accepted")
			}
			publisher := Publisher{Store: store, UUID: func() (string, error) { return "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", nil }}
			if _, err := publisher.Initialize(); err == nil {
				t.Fatalf("rejected %s returned nil Initialize error", target)
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
