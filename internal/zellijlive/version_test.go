package zellijlive

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogUsesInstalledCommandWithoutVersionProbe(t *testing.T) {
	for _, version := range []string{"0.44.3", "0.45.1", "99.0.0", "custom-build"} {
		t.Run(version, func(t *testing.T) {
			root := t.TempDir()
			command := filepath.Join(root, "zellij")
			log := filepath.Join(root, "calls")
			script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + log + "'\n" +
				"if [ \"$1\" = --version ]; then echo 'zellij " + version + "'; exit 0; fi\n" +
				"[ \"$*\" = 'list-sessions --short --no-formatting' ] || exit 2\n" +
				"echo 'No active zellij sessions found.' >&2\nexit 1\n"
			if err := os.WriteFile(command, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			// Exercise default remote PATH selection rather than a bundled binary.
			t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
			catalog, err := (CommandCataloger{SocketBase: filepath.Join(root, "sockets"), CacheHome: filepath.Join(root, "cache"), BootID: "boot"}).Observe(context.Background())
			if err != nil || len(catalog.Names) != 0 {
				t.Fatalf("catalog=%+v err=%v", catalog, err)
			}
			calls, err := os.ReadFile(log)
			if err != nil || strings.TrimSpace(string(calls)) != "list-sessions --short --no-formatting" {
				t.Fatalf("unexpected command calls: %q (%v)", calls, err)
			}
		})
	}
}
