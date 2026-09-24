package mirror

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jmo/terminal-redeemer/internal/zellijlive"
)

func TestCaptureNamesAndIncarnationsComeFromOneObservation(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "niri.json")
	if err := os.WriteFile(fixture, []byte(`{"windows":[],"workspaces":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog := &inventoryCataloger{catalog: activeCatalog("-Agent", "Alpha")}
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	snapshot, err := Capture(context.Background(), Options{FixturePath: fixture, Cataloger: catalog, Resolver: fakeResolver{}, GeneratedAt: when})
	if err != nil {
		t.Fatal(err)
	}
	if catalog.calls != 1 || !snapshot.GeneratedAt.Equal(when) || !reflect.DeepEqual(snapshot.ActiveSessions, []string{"-Agent", "Alpha"}) || len(snapshot.SessionIDs) != 2 || len(snapshot.Windows) != 2 {
		t.Fatalf("calls=%d snapshot=%+v", catalog.calls, snapshot)
	}
	for _, window := range snapshot.Windows {
		id, err := snapshot.ExactSessionID(SessionName(window))
		if err != nil || !window.Headless || id != catalog.catalog.Sessions[SessionName(window)].ExactID {
			t.Fatalf("headless identity=%q err=%v window=%+v", id, err, window)
		}
	}
}

func TestCaptureNeverPublishesPartialIncarnationInventory(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "niri.json")
	if err := os.WriteFile(fixture, []byte(`{"windows":[],"workspaces":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog := activeCatalog("good", "uncertain")
	session := catalog.Sessions["uncertain"]
	session.Status = zellijlive.StatusSocketInvalid
	catalog.Sessions["uncertain"] = session
	snapshot, err := Capture(context.Background(), Options{FixturePath: fixture, Cataloger: &inventoryCataloger{catalog: catalog}, Resolver: fakeResolver{}})
	if err == nil || snapshot.ActiveSessions != nil || snapshot.SessionIDs != nil || snapshot.Windows != nil {
		t.Fatalf("partial snapshot published: %+v err=%v", snapshot, err)
	}
}
