package sourceinventory

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jmo/terminal-redeemer/internal/niriipc"
	"github.com/jmo/terminal-redeemer/internal/sliceprotocol"
	"github.com/jmo/terminal-redeemer/internal/storelock"
	"github.com/jmo/terminal-redeemer/internal/zellijlive"
)

type NiriObserver interface {
	Snapshot(context.Context) (niriipc.State, error)
}

type Publisher struct {
	Store       *Store
	Niri        NiriObserver
	Catalog     zellijlive.Cataloger
	Builder     Builder
	Fingerprint FingerprintSource
	UUID        UUIDSource
	Now         func() time.Time
}

func (publisher Publisher) Initialize() (State, error) {
	if publisher.Store == nil {
		return State{}, errors.New("source inventory store is required")
	}
	lock, err := storelock.Acquire(publisher.Store.Root())
	if err != nil {
		return State{}, err
	}
	defer lock.Close()
	markerPresent, err := publisher.Store.EnrollmentMarkerPresent()
	if err != nil {
		return State{}, err
	}
	if _, err := publisher.Store.Read(); err == nil {
		return State{}, fmt.Errorf("source inventory is already initialized")
	} else if !errors.Is(err, ErrStateNotFound) {
		return State{}, err
	}
	if markerPresent {
		return State{}, fmt.Errorf("%w: current authority is missing", ErrNamespaceUsed)
	}
	if err := publisher.Store.CreateEnrollmentMarker(); err != nil {
		return State{}, fmt.Errorf("create source inventory enrollment marker: %w", err)
	}
	uuid := publisher.UUID
	if uuid == nil {
		uuid = RandomUUID
	}
	hostID, err := uuid()
	if err != nil {
		return State{}, err
	}
	state := State{StorageVersion: StorageVersion, Initialized: true, SourceHostID: hostID}
	if err := publisher.Store.Write(state); err != nil {
		return State{}, err
	}
	return state, nil
}

func (publisher Publisher) Snapshot(ctx context.Context) (sliceprotocol.Envelope, error) {
	if publisher.Store == nil || publisher.Niri == nil || publisher.Catalog == nil || publisher.Fingerprint == nil {
		return sliceprotocol.Envelope{}, errors.New("source inventory publisher is not fully configured")
	}
	lock, err := storelock.Acquire(publisher.Store.Root())
	if err != nil {
		return sliceprotocol.Envelope{}, err
	}
	defer lock.Close()
	markerPresent, err := publisher.Store.EnrollmentMarkerPresent()
	if err != nil {
		return sliceprotocol.Envelope{}, err
	}
	if !markerPresent {
		return sliceprotocol.Envelope{}, fmt.Errorf("%w: enrollment marker is missing", ErrStateInvalid)
	}
	state, err := publisher.Store.Read()
	if err != nil {
		return sliceprotocol.Envelope{}, err
	}
	fingerprint, err := publisher.Fingerprint()
	if err != nil {
		return degraded(state, publisher.now(), sliceprotocol.ReasonNiriSocketUnavailable), nil
	}
	niriState, err := publisher.Niri.Snapshot(ctx)
	if err != nil {
		return degraded(state, publisher.now(), niriipc.ReasonCode(err)), nil
	}
	catalog, err := publisher.Catalog.Observe(ctx)
	if err != nil {
		return degraded(state, publisher.now(), sliceprotocol.ReasonZellijCatalogUnavailable), nil
	}
	epoch := ""
	rotating := state.Authority == nil || state.PrivateFingerprint != fingerprint
	if rotating {
		uuid := publisher.UUID
		if uuid == nil {
			uuid = RandomUUID
		}
		epoch, err = uuid()
		if err != nil {
			return degraded(state, publisher.now(), sliceprotocol.ReasonAuthorityUnavailable), nil
		}
	} else {
		epoch = state.Authority.SourceEpoch
	}
	sources, conflicts, err := publisher.Builder.Build(ctx, epoch, niriState, catalog)
	if err != nil {
		code := sliceprotocol.ReasonProcessObservationIncomplete
		if !strings.Contains(err.Error(), string(code)) {
			code = sliceprotocol.ReasonNiriMalformed
		}
		return degraded(state, publisher.now(), code), nil
	}
	secondFingerprint, err := publisher.Fingerprint()
	if err != nil || secondFingerprint != fingerprint {
		return degraded(state, publisher.now(), sliceprotocol.ReasonSourceIdentityChanged), nil
	}
	liveSessionIDs := make([]string, 0, len(catalog.Sessions))
	for _, session := range catalog.Sessions {
		if session.Status == zellijlive.StatusActive {
			liveSessionIDs = append(liveSessionIDs, session.ID)
		}
	}
	completedAt := publisher.now()
	authority := sliceprotocol.Authoritative{SourceEpoch: epoch, ObservedAt: completedAt, WorkspaceNormalization: sliceprotocol.WorkspaceNormalization, LiveSessionIDs: liveSessionIDs, Sources: sources, Conflicts: conflicts}
	hash, err := sliceprotocol.SemanticHash(authority)
	if err != nil {
		return degraded(state, publisher.now(), sliceprotocol.ReasonAuthorityUnavailable), nil
	}
	if rotating || state.Authority == nil {
		authority.Revision = 1
	} else {
		if state.Authority.Revision == math.MaxUint64 {
			return degraded(state, publisher.now(), sliceprotocol.ReasonRevisionOverflow), nil
		}
		// Every successfully completed poll is a distinct authoritative
		// observation. Advance even when its semantic inventory hash is
		// unchanged so consecutive complete evidence remains observable; an
		// exact replay of a committed revision is still rejected as duplicate.
		authority.Revision = state.Authority.Revision + 1
	}
	authority = sliceprotocol.Canonicalize(authority)
	next := State{StorageVersion: StorageVersion, Initialized: true, SourceHostID: state.SourceHostID, PrivateFingerprint: fingerprint, SemanticHash: hash, Authority: &authority}
	if err := publisher.Store.Write(next); err != nil {
		return degraded(state, publisher.now(), sliceprotocol.ReasonAuthorityUnavailable), nil
	}
	return sliceprotocol.Envelope{SchemaVersion: sliceprotocol.SchemaVersion, SourceHostID: state.SourceHostID, Observation: sliceprotocol.Observation{Quality: sliceprotocol.QualityComplete, AttemptedAt: completedAt}, Authoritative: &authority}, nil
}

func degraded(state State, attempted time.Time, code sliceprotocol.ReasonCode) sliceprotocol.Envelope {
	var authority *sliceprotocol.Authoritative
	if state.Authority != nil {
		copy := sliceprotocol.Canonicalize(*state.Authority)
		authority = &copy
	}
	return sliceprotocol.Envelope{SchemaVersion: sliceprotocol.SchemaVersion, SourceHostID: state.SourceHostID, Observation: sliceprotocol.Observation{Quality: sliceprotocol.QualityDegraded, AttemptedAt: attempted, DegradedReasons: []sliceprotocol.Reason{{Code: code}}}, Authoritative: authority}
}

func (publisher Publisher) now() time.Time {
	if publisher.Now != nil {
		return publisher.Now().UTC()
	}
	return time.Now().UTC()
}
