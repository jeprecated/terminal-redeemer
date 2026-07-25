package sourceinventory

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type FingerprintSource func() (string, error)

func NiriFingerprint(bootID, socketPath string) (string, error) {
	if bootID == "" || socketPath == "" {
		return "", fmt.Errorf("boot identity and Niri socket are required")
	}
	cleaned, err := filepath.Abs(filepath.Clean(socketPath))
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(cleaned)
	if err != nil {
		return "", fmt.Errorf("inspect Niri socket: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("Niri socket is not a direct Unix socket")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		return "", fmt.Errorf("Niri socket is not owned by current user")
	}
	hash := sha256.New()
	writeFingerprintPart(hash, []byte("terminal-redeemer/niri-instance/v1"))
	writeFingerprintPart(hash, []byte(bootID))
	var raw [16]byte
	binary.BigEndian.PutUint64(raw[:8], uint64(stat.Dev))
	binary.BigEndian.PutUint64(raw[8:], stat.Ino)
	writeFingerprintPart(hash, raw[:])
	return hex.EncodeToString(hash.Sum(nil)), nil
}

type hashWriter interface{ Write([]byte) (int, error) }

func writeFingerprintPart(writer hashWriter, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = writer.Write(length[:])
	_, _ = writer.Write(value)
}
