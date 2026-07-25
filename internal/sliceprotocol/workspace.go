package sliceprotocol

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const MaxWorkspaceKeyBytes = 512

func NormalizeWorkspaceName(name string) (string, error) {
	if !utf8.ValidString(name) {
		return "", fmt.Errorf("workspace name is not valid UTF-8")
	}
	value := strings.TrimFunc(name, unicode.IsSpace)
	value = norm.NFKC.String(value)
	value = cases.Fold().String(value)
	value = norm.NFKC.String(value)
	if value == "" {
		return "", fmt.Errorf("workspace name is empty after normalization")
	}
	if len(value) > MaxWorkspaceKeyBytes {
		return "", fmt.Errorf("workspace key exceeds %d bytes", MaxWorkspaceKeyBytes)
	}
	for _, r := range value {
		if r == 0 || unicode.IsControl(r) {
			return "", fmt.Errorf("workspace key contains a control character")
		}
	}
	return value, nil
}
