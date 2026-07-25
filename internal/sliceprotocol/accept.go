package sliceprotocol

import (
	"fmt"
	"time"
)

type AcceptanceDecision string

const (
	DecisionAccepted   AcceptanceDecision = "accepted"
	DecisionDuplicate  AcceptanceDecision = "duplicate"
	DecisionStale      AcceptanceDecision = "stale"
	DecisionConflict   AcceptanceDecision = "conflict"
	DecisionFullResync AcceptanceDecision = "full_resync"
	DecisionReplay     AcceptanceDecision = "replay"
	DecisionDegraded   AcceptanceDecision = "degraded"
)

const MaxRetiredEpochTombstones = 256

type AcceptanceState struct {
	SourceHostID        string
	SourceEpoch         string
	Revision            uint64
	SemanticHash        string
	RetiredEpochs       []string
	AuthorityReceivedAt time.Time
	LastResponseAt      time.Time
}

type AcceptanceResult struct {
	Decision AcceptanceDecision
	State    AcceptanceState
}

func Accept(current AcceptanceState, envelope Envelope, receivedAt time.Time) (AcceptanceResult, error) {
	if err := ValidateAcceptanceState(current); err != nil {
		return AcceptanceResult{}, err
	}
	if err := ValidateEnvelope(envelope); err != nil {
		return AcceptanceResult{}, err
	}
	if receivedAt.IsZero() {
		return AcceptanceResult{}, fmt.Errorf("receive time is required")
	}
	next := current
	next.LastResponseAt = receivedAt.UTC()
	if current.SourceHostID != "" && current.SourceHostID != envelope.SourceHostID {
		return AcceptanceResult{Decision: DecisionConflict, State: next}, nil
	}
	if envelope.Observation.Quality == QualityDegraded {
		return AcceptanceResult{Decision: DecisionDegraded, State: next}, nil
	}
	authority := *envelope.Authoritative
	hash, err := SemanticHash(authority)
	if err != nil {
		return AcceptanceResult{}, err
	}
	if current.SourceHostID == "" {
		next.SourceHostID = envelope.SourceHostID
		next.SourceEpoch = authority.SourceEpoch
		next.Revision = authority.Revision
		next.SemanticHash = hash
		next.AuthorityReceivedAt = receivedAt.UTC()
		return AcceptanceResult{Decision: DecisionAccepted, State: next}, nil
	}
	if retiredContains(current, authority.SourceEpoch) {
		return AcceptanceResult{Decision: DecisionReplay, State: next}, nil
	}
	if authority.SourceEpoch != current.SourceEpoch {
		if err := addRetired(&next, current.SourceEpoch); err != nil {
			return AcceptanceResult{}, err
		}
		next.SourceEpoch = authority.SourceEpoch
		next.Revision = authority.Revision
		next.SemanticHash = hash
		next.AuthorityReceivedAt = receivedAt.UTC()
		return AcceptanceResult{Decision: DecisionFullResync, State: next}, nil
	}
	if authority.Revision < current.Revision {
		return AcceptanceResult{Decision: DecisionStale, State: next}, nil
	}
	if authority.Revision == current.Revision {
		if hash != current.SemanticHash {
			return AcceptanceResult{Decision: DecisionConflict, State: next}, nil
		}
		next.AuthorityReceivedAt = receivedAt.UTC()
		return AcceptanceResult{Decision: DecisionDuplicate, State: next}, nil
	}
	next.Revision = authority.Revision
	next.SemanticHash = hash
	next.AuthorityReceivedAt = receivedAt.UTC()
	return AcceptanceResult{Decision: DecisionAccepted, State: next}, nil
}

func (state AcceptanceState) Fresh(now time.Time, maxAge time.Duration) bool {
	return state.Revision > 0 && maxAge > 0 && !state.AuthorityReceivedAt.IsZero() && now.Sub(state.AuthorityReceivedAt) <= maxAge
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func addRetired(state *AcceptanceState, value string) error {
	if value == "" || retiredContains(*state, value) {
		return nil
	}
	if len(state.RetiredEpochs) >= MaxRetiredEpochTombstones {
		return fmt.Errorf("retired epoch tombstone capacity exhausted; explicit maintenance/re-enrollment required")
	}
	state.RetiredEpochs = append(state.RetiredEpochs, value)
	return nil
}
func retiredContains(state AcceptanceState, value string) bool {
	return contains(state.RetiredEpochs, value)
}
func CompactAcceptanceState(state *AcceptanceState) {}
func ValidateAcceptanceState(state AcceptanceState) error {
	if len(state.RetiredEpochs) > MaxRetiredEpochTombstones {
		return fmt.Errorf("retired epoch tombstone capacity exhausted; explicit maintenance/re-enrollment required")
	}
	seen := map[string]bool{}
	for _, epoch := range state.RetiredEpochs {
		if !ValidUUID(epoch) || seen[epoch] || epoch == state.SourceEpoch {
			return fmt.Errorf("invalid retired epoch tombstones")
		}
		seen[epoch] = true
	}
	return nil
}
