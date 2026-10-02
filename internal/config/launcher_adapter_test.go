package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLauncherAdapterConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("mirror:\n  launcherAdapter: ['/adapter', '--group', 'two words']\n  clipboard:\n    enabled: false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Mirror.LauncherAdapter, []string{"/adapter", "--group", "two words"}) {
		t.Fatal(cfg.Mirror.LauncherAdapter)
	}
	cfg.Mirror.Clipboard.Enabled = true
	if Validate(cfg) == nil {
		t.Fatal("accepted Kitty clipboard with adapter")
	}
	cfg.Mirror.Clipboard.Enabled = false
	for _, adapter := range [][]string{{""}, {"adapter", "bad\x00argument"}} {
		cfg.Mirror.LauncherAdapter = adapter
		if Validate(cfg) == nil {
			t.Fatalf("accepted %q", adapter)
		}
	}
}
