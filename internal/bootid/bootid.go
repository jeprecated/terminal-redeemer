package bootid

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

const linuxBootIDPath = "/proc/sys/kernel/random/boot_id"

type Source func() (string, error)

func Current() (string, error) {
	payload, err := os.ReadFile(linuxBootIDPath)
	if err != nil {
		return "", fmt.Errorf("read Linux boot ID: %w", err)
	}
	id := strings.TrimSpace(string(payload))
	if id == "" {
		return "", errors.New("read Linux boot ID: empty value")
	}
	return id, nil
}
