package zellijlive

import "fmt"

func ExactSocketIDAt(dirfd int, path, boot, name string) (string, error) {
	return "", fmt.Errorf("local Zellij socket identity requires a Linux source")
}
