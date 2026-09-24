package mirror

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jmo/terminal-redeemer/internal/zellijlive"
)

type inventoryCataloger struct {
	catalog      zellijlive.Catalog
	err          error
	calls        int
	afterObserve func()
}

func (c *inventoryCataloger) Observe(context.Context) (zellijlive.Catalog, error) {
	c.calls++
	if c.afterObserve != nil {
		c.afterObserve()
	}
	return c.catalog, c.err
}

func activeCatalog(names ...string) zellijlive.Catalog {
	catalog := zellijlive.Catalog{Names: append([]string{}, names...), Sessions: map[string]zellijlive.Session{}}
	for i, name := range names {
		catalog.Sessions[name] = zellijlive.Session{Name: name, ID: "checkpoint-id", ExactID: zellijlive.SessionID("boot", name, 1, uint64(i+1)), Status: zellijlive.StatusActive}
	}
	return catalog
}

func TestSessionInventoryObservesOnceAndPreservesExactNames(t *testing.T) {
	catalog := activeCatalog("-Agent", "agent", "Agent workspace")
	catalog.Names = append(catalog.Names, "dead")
	catalog.Sessions["dead"] = zellijlive.Session{Name: "dead", Status: zellijlive.StatusDeadResurrectable}
	observer := &inventoryCataloger{catalog: catalog}
	inventory, err := ObserveSessionInventory(context.Background(), observer)
	if err != nil {
		t.Fatal(err)
	}
	if observer.calls != 1 || !reflect.DeepEqual(inventory.ActiveSessions, []string{"-Agent", "Agent workspace", "agent"}) {
		t.Fatalf("calls=%d inventory=%+v", observer.calls, inventory)
	}
	for _, name := range inventory.ActiveSessions {
		if inventory.SessionIDs[name] != catalog.Sessions[name].ExactID {
			t.Fatalf("identity changed for %q", name)
		}
	}
	if _, exists := inventory.SessionIDs["dead"]; exists {
		t.Fatal("dead session published as active")
	}
	payload, err := json.Marshal(inventory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeSessionInventory(payload); err != nil {
		t.Fatal(err)
	}
}

func TestSessionInventoryRejectsIncompleteOrAmbiguousCatalogs(t *testing.T) {
	cases := map[string]func(*zellijlive.Catalog){
		"duplicate name":      func(c *zellijlive.Catalog) { c.Names = append(c.Names, "active") },
		"missing map entry":   func(c *zellijlive.Catalog) { delete(c.Sessions, "active") },
		"missing name entry":  func(c *zellijlive.Catalog) { c.Names = []string{} },
		"wrong embedded name": func(c *zellijlive.Catalog) { s := c.Sessions["active"]; s.Name = "other"; c.Sessions["active"] = s },
		"missing identity":    func(c *zellijlive.Catalog) { s := c.Sessions["active"]; s.ExactID = ""; c.Sessions["active"] = s },
		"malformed identity": func(c *zellijlive.Catalog) {
			s := c.Sessions["active"]
			s.ExactID = "ses_not-a-digest"
			c.Sessions["active"] = s
		},
		"nil catalog": func(c *zellijlive.Catalog) { *c = zellijlive.Catalog{} },
	}
	for _, status := range []zellijlive.Status{zellijlive.StatusMissing, zellijlive.StatusDuplicate, zellijlive.StatusSocketInvalid, zellijlive.StatusPrefixOnly, "unknown"} {
		cases[string(status)] = func(c *zellijlive.Catalog) { s := c.Sessions["active"]; s.Status = status; c.Sessions["active"] = s }
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			catalog := activeCatalog("active")
			mutate(&catalog)
			inventory, err := ObserveSessionInventory(context.Background(), &inventoryCataloger{catalog: catalog})
			if err == nil || inventory.ActiveSessions != nil || inventory.SessionIDs != nil {
				t.Fatalf("incomplete observation published: %+v error=%v", inventory, err)
			}
		})
	}
	want := errors.New("catalog unavailable")
	if _, err := ObserveSessionInventory(context.Background(), &inventoryCataloger{err: want}); !errors.Is(err, want) {
		t.Fatalf("lost observer error: %v", err)
	}
}

func TestSessionInventoryRejectsCanceledObservation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	observer := &inventoryCataloger{catalog: activeCatalog(), afterObserve: cancel}
	if _, err := ObserveSessionInventory(ctx, observer); !errors.Is(err, context.Canceled) {
		t.Fatalf("late observation accepted: %v", err)
	}
	if _, err := ObserveSessionInventory(ctx, observer); !errors.Is(err, context.Canceled) || observer.calls != 1 {
		t.Fatalf("canceled observation attempted: calls=%d err=%v", observer.calls, err)
	}
}

func TestSessionInventoryDistinguishesEmptyFromUnsupported(t *testing.T) {
	inventory, err := ObserveSessionInventory(context.Background(), &inventoryCataloger{catalog: activeCatalog()})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(inventory)
	if !strings.Contains(string(payload), `"active_zellij_sessions":[]`) || !strings.Contains(string(payload), `"active_zellij_session_ids":{}`) {
		t.Fatalf("empty inventory lost capability: %s", payload)
	}
	if _, err := DecodeSessionInventory(payload); err != nil {
		t.Fatal(err)
	}
	for _, payload := range []string{`{}`, `{"generated_at":"2026-01-01T00:00:00Z","active_zellij_sessions":[]}`, `{"generated_at":"2026-01-01T00:00:00Z","active_zellij_sessions":[],"active_zellij_session_ids":null}`} {
		if _, err := DecodeSessionInventory([]byte(payload)); err == nil {
			t.Fatalf("unsupported source accepted: %s", payload)
		}
	}
}

func TestSessionInventoryRejectsMalformedWireEvidence(t *testing.T) {
	id := zellijlive.SessionID("boot", "Alpha", 1, 2)
	cases := map[string]SessionInventory{
		"missing timestamp":  {ActiveSessions: []string{}, SessionIDs: map[string]string{}},
		"nil names":          {GeneratedAt: time.Now(), SessionIDs: map[string]string{}},
		"missing identity":   {GeneratedAt: time.Now(), ActiveSessions: []string{"Alpha"}, SessionIDs: map[string]string{}},
		"extra identity":     {GeneratedAt: time.Now(), ActiveSessions: []string{}, SessionIDs: map[string]string{"Alpha": id}},
		"case mismatch":      {GeneratedAt: time.Now(), ActiveSessions: []string{"alpha"}, SessionIDs: map[string]string{"Alpha": id}},
		"duplicate name":     {GeneratedAt: time.Now(), ActiveSessions: []string{"Alpha", "Alpha"}, SessionIDs: map[string]string{"Alpha": id}},
		"duplicate identity": {GeneratedAt: time.Now(), ActiveSessions: []string{"Alpha", "Beta"}, SessionIDs: map[string]string{"Alpha": id, "Beta": id}},
		"malformed identity": {GeneratedAt: time.Now(), ActiveSessions: []string{"Alpha"}, SessionIDs: map[string]string{"Alpha": "ses_bad"}},
		"unsafe name":        {GeneratedAt: time.Now(), ActiveSessions: []string{"../Alpha"}, SessionIDs: map[string]string{"../Alpha": id}},
	}
	for name, inventory := range cases {
		t.Run(name, func(t *testing.T) {
			payload, _ := json.Marshal(inventory)
			if _, err := DecodeSessionInventory(payload); err == nil {
				t.Fatalf("accepted malformed inventory: %s", payload)
			}
		})
	}
	if _, err := DecodeSessionInventory([]byte(strings.Repeat(" ", zellijlive.MaxCatalogBytes+1))); err == nil {
		t.Fatal("oversized response accepted")
	}
}

func TestSnapshotExactIdentityIsAdditiveButNeverGuessed(t *testing.T) {
	snapshot := Snapshot{Host: "source", GeneratedAt: time.Now(), Windows: []Window{}, ActiveSessions: []string{"Alpha"}}
	payload, _ := json.Marshal(snapshot)
	legacy, err := DecodeSnapshot(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExactSessionID("Alpha"); err == nil || !strings.Contains(err.Error(), "upgrade") {
		t.Fatalf("legacy name treated as incarnation: %v", err)
	}
	snapshot.SessionIDs = map[string]string{"Alpha": zellijlive.SessionID("boot", "Alpha", 1, 2)}
	payload, _ = json.Marshal(snapshot)
	current, err := DecodeSnapshot(payload)
	if err != nil {
		t.Fatal(err)
	}
	if id, err := current.ExactSessionID("Alpha"); err != nil || id != snapshot.SessionIDs["Alpha"] {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if _, err := current.ExactSessionID("alpha"); err == nil {
		t.Fatal("case-insensitive identity lookup")
	}
	snapshot.SessionIDs["extra"] = zellijlive.SessionID("boot", "extra", 1, 3)
	payload, _ = json.Marshal(snapshot)
	if _, err := DecodeSnapshot(payload); err == nil {
		t.Fatal("snapshot accepted inconsistent identities")
	}
}
