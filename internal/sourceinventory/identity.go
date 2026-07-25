package sourceinventory

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
)

func SourceID(sourceEpoch string, runtimeWindowID uint64) (string, error) {
	epoch, err := hex.DecodeString(strings.ReplaceAll(sourceEpoch, "-", ""))
	if err != nil || len(epoch) != 16 {
		return "", fmt.Errorf("parse source epoch")
	}
	payload := make([]byte, 0, 64)
	payload = append(payload, []byte("terminal-redeemer/source/v1\x00")...)
	payload = append(payload, epoch...)
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], runtimeWindowID)
	payload = append(payload, raw[:]...)
	sum := sha256.Sum256(payload)
	return "src_" + base64.RawURLEncoding.EncodeToString(sum[:]), nil
}
