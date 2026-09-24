package mirror

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jmo/terminal-redeemer/internal/bootid"
	"github.com/jmo/terminal-redeemer/internal/zellijlive"
)

// SessionInventory is a complete, single observation of active session
// incarnations. It deliberately does not depend on Niri or visible windows.
// Nil inventories mean unsupported/unavailable, never an empty host.
type SessionInventory struct {
	GeneratedAt    time.Time         `json:"generated_at"`
	ActiveSessions []string          `json:"active_zellij_sessions"`
	SessionIDs     map[string]string `json:"active_zellij_session_ids"`
}

func ObserveSessionInventory(ctx context.Context, observer zellijlive.Cataloger) (SessionInventory, error) {
	if err := ctx.Err(); err != nil {
		return SessionInventory{}, err
	}
	if observer == nil {
		boot, err := bootid.Current()
		if err != nil {
			return SessionInventory{}, err
		}
		observer = zellijlive.CommandCataloger{BootID: boot}
	}
	catalog, err := observer.Observe(ctx)
	if err != nil {
		return SessionInventory{}, fmt.Errorf("observe session inventory: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return SessionInventory{}, err
	}
	if catalog.Names == nil || catalog.Sessions == nil || len(catalog.Names) != len(catalog.Sessions) || len(catalog.Names) > zellijlive.MaxCatalogEntries {
		return SessionInventory{}, fmt.Errorf("incomplete session catalog")
	}
	inventory := SessionInventory{GeneratedAt: time.Now().UTC(), ActiveSessions: []string{}, SessionIDs: map[string]string{}}
	seen := make(map[string]bool, len(catalog.Names))
	for _, name := range catalog.Names {
		session, exists := catalog.Sessions[name]
		if !exists || session.Name != name || seen[name] {
			return SessionInventory{}, fmt.Errorf("inconsistent session catalog entry %q", name)
		}
		seen[name] = true
		switch session.Status {
		case zellijlive.StatusActive:
			inventory.ActiveSessions = append(inventory.ActiveSessions, name)
			if session.ExactID == "" {
				return SessionInventory{}, fmt.Errorf("session %q lacks exact socket birth identity; source helper/filesystem capability required", name)
			}
			inventory.SessionIDs[name] = session.ExactID
		case zellijlive.StatusDeadResurrectable:
			// Cache entries are not active and must never be reattached by recovery.
		default:
			return SessionInventory{}, fmt.Errorf("session %q has uncertain catalog status %q", name, session.Status)
		}
	}
	sort.Strings(inventory.ActiveSessions)
	if err := inventory.validate(); err != nil {
		return SessionInventory{}, err
	}
	return inventory, nil
}

func DecodeSessionInventory(payload []byte) (SessionInventory, error) {
	if len(payload) > zellijlive.MaxCatalogBytes {
		return SessionInventory{}, fmt.Errorf("session inventory exceeds response bound")
	}
	var inventory SessionInventory
	if err := json.Unmarshal(payload, &inventory); err != nil {
		return SessionInventory{}, fmt.Errorf("decode session inventory: %w", err)
	}
	if err := inventory.validate(); err != nil {
		return SessionInventory{}, err
	}
	return inventory, nil
}

func (inventory SessionInventory) validate() error {
	if inventory.ActiveSessions == nil || inventory.SessionIDs == nil {
		return fmt.Errorf("source has no complete session incarnation inventory (source Redeem upgrade required)")
	}
	if inventory.GeneratedAt.IsZero() {
		return fmt.Errorf("session inventory has no observation timestamp")
	}
	if len(inventory.ActiveSessions) > zellijlive.MaxCatalogEntries || len(inventory.ActiveSessions) != len(inventory.SessionIDs) {
		return fmt.Errorf("session incarnation inventory has inconsistent size")
	}
	seenNames := make(map[string]bool, len(inventory.ActiveSessions))
	seenIDs := make(map[string]bool, len(inventory.ActiveSessions))
	for _, name := range inventory.ActiveSessions {
		if ValidateSession(name) != nil || !zellijlive.SafeSessionName(name) || seenNames[name] {
			return fmt.Errorf("session incarnation inventory has invalid or duplicate name %q", name)
		}
		id := inventory.SessionIDs[name]
		if !validSessionID(id) || seenIDs[id] {
			return fmt.Errorf("session %q has missing, invalid or duplicate incarnation", name)
		}
		seenNames[name], seenIDs[id] = true, true
	}
	return nil
}

func validSessionID(id string) bool {
	if !strings.HasPrefix(id, "ses_") {
		return false
	}
	digest, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, "ses_"))
	return err == nil && len(digest) == 32 && "ses_"+base64.RawURLEncoding.EncodeToString(digest) == id
}

// ExactSessionID is mandatory for future reconnect callers. Legacy snapshots
// still support discovery, but a name alone cannot authorize reattachment.
func (snapshot Snapshot) ExactSessionID(name string) (string, error) {
	inventory := SessionInventory{GeneratedAt: snapshot.GeneratedAt, ActiveSessions: snapshot.ActiveSessions, SessionIDs: snapshot.SessionIDs}
	if err := inventory.validate(); err != nil {
		return "", err
	}
	id, exists := inventory.SessionIDs[name]
	if !exists {
		return "", fmt.Errorf("exact ACTIVE session %q is unavailable", name)
	}
	return id, nil
}
