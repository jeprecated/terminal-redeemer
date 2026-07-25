package sourceinventory

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/jmo/terminal-redeemer/internal/sliceprotocol"
)

const StorageVersion = 1

type State struct {
	StorageVersion     int                          `json:"storage_version"`
	Initialized        bool                         `json:"initialized"`
	SourceHostID       string                       `json:"source_host_id"`
	PrivateFingerprint string                       `json:"private_fingerprint,omitempty"`
	SemanticHash       string                       `json:"semantic_hash,omitempty"`
	Authority          *sliceprotocol.Authoritative `json:"authority,omitempty"`
}

func (state State) Validate() error {
	if state.StorageVersion != StorageVersion || !state.Initialized {
		return fmt.Errorf("invalid source inventory storage version or initialization")
	}
	if strings.TrimSpace(state.SourceHostID) == "" || !sliceprotocol.ValidUUID(state.SourceHostID) {
		return fmt.Errorf("source_host_id must be a UUIDv4")
	}
	if state.Authority == nil {
		if state.PrivateFingerprint != "" || state.SemanticHash != "" {
			return fmt.Errorf("uncommitted authority metadata is present")
		}
		return nil
	}
	if !validDigest(state.PrivateFingerprint) || !validDigest(state.SemanticHash) {
		return fmt.Errorf("authority fingerprint/hash must be SHA-256 digests")
	}
	if err := sliceprotocol.ValidateAuthoritative(*state.Authority); err != nil {
		return err
	}
	hash, err := sliceprotocol.SemanticHash(*state.Authority)
	if err != nil {
		return err
	}
	if hash != state.SemanticHash {
		return fmt.Errorf("semantic hash mismatch")
	}
	return nil
}

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}
