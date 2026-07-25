package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jmo/terminal-redeemer/internal/slicecontroller"
)

func TestSliceDefaultsAreBoundedAndClipboardIndependent(t *testing.T) {
	cfg := Defaults()
	if cfg.Slice.Clipboard.Enabled {
		t.Fatal("slice clipboard must default disabled")
	}
	if cfg.Slice.Controller.Enabled {
		t.Fatal("slice controller must default disabled")
	}
	if cfg.Slice.Controller.RetryWindow <= 0 || cfg.Slice.Controller.SourceGoneConfirmations < 2 {
		t.Fatalf("controller defaults are not bounded: %#v", cfg.Slice.Controller)
	}
	if !cfg.Mirror.Clipboard.Enabled {
		t.Fatal("legacy mirror clipboard default changed")
	}
	if err := Validate(cfg); err != nil {
		t.Fatal(err)
	}
}
func TestSliceLeechModeDefaultsDisabledAndCanBeExplicitlyEnabled(t *testing.T) {
	if Defaults().Slice.LeechModeEnabled {
		t.Fatal("leech mode must default disabled")
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("slice:\n  leechModeEnabled: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, true)
	if err != nil || !cfg.Slice.LeechModeEnabled {
		t.Fatalf("cfg=%+v err=%v", cfg.Slice, err)
	}
}

func TestSliceYAMLRetainsValidatedArgvOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	payload := `slice:
  sourceHost: user@host
  transportCommand: /store/ssh
  transportOptions: [-p, "2222", -o, IdentityAgent=/operator/agent]
  rpcCommand: [/store/redeem, slice, rpc]
  kittyCommand: /store/kitty
  selfCommand: /store/redeem
  zellijCommand: /store/zellij
  niriCommand: /store/niri
  systemctlCommand: /store/systemctl
`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Slice.SourceHost != "user@host" || !reflect.DeepEqual(cfg.Slice.RPCCommand, []string{"/store/redeem", "slice", "rpc"}) || !reflect.DeepEqual(cfg.Slice.TransportOptions, []string{"-p", "2222", "-o", "IdentityAgent=/operator/agent"}) {
		t.Fatalf("slice config=%#v", cfg.Slice)
	}
}

func TestSliceTransportOptionAggregateBoundaryMatchesProjectionBudget(t *testing.T) {
	cfg := Defaults()
	cfg.Slice.TransportOptions = make([]string, 8)
	for i := range cfg.Slice.TransportOptions {
		cfg.Slice.TransportOptions[i] = strings.Repeat("x", slicecontroller.MaxProjectionArgvEntryBytes)
	}
	if got := len(strings.Join(cfg.Slice.TransportOptions, "")); got != slicecontroller.MaxProjectionTransportOptionBytes {
		t.Fatalf("boundary fixture bytes=%d", got)
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("exact aggregate boundary rejected: %v", err)
	}
	cfg.Slice.TransportOptions = append(cfg.Slice.TransportOptions, "x")
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "aggregate") {
		t.Fatalf("over-bound aggregate accepted: %v", err)
	}
}

func TestSliceGraphicalContextSetIsExactAndOrderInsensitive(t *testing.T) {
	cfg := Defaults()
	cfg.Slice.GraphicalContextKeys = []string{"XDG_RUNTIME_DIR", "NIRI_SOCKET", "WAYLAND_DISPLAY"}
	if err := Validate(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Slice.GraphicalContextKeys = []string{"NIRI_SOCKET"}
	if err := Validate(cfg); err == nil {
		t.Fatal("accepted graphical context subset")
	}
}

func TestSliceControllerValidation(t *testing.T) {
	cfg := Defaults()
	cfg.Slice.Controller.Enabled = true
	if err := Validate(cfg); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Config){
		func(c *Config) { c.Slice.Controller.HostID = "bad/name" },
		func(c *Config) { c.Slice.Controller.PollInterval = 0 },
		func(c *Config) { c.Slice.Controller.SourceGoneConfirmations = 1 },
		func(c *Config) { c.Slice.Controller.AuthorityMode = "unknown" },
		func(c *Config) { c.Slice.Controller.AuthorityMode = "leech_location" },
		func(c *Config) {
			c.Slice.Controller.AuthorityMode = "host_location"
			c.Slice.Controller.LeechWriteAuthorized = true
		},
	} {
		next := Defaults()
		mutate(&next)
		if err := Validate(next); err == nil {
			t.Fatalf("accepted invalid controller config: %#v", next.Slice.Controller)
		}
	}
}

func TestSliceValidationRejectsUnsafeArgvTimingContextAndClipboard(t *testing.T) {
	cases := []func(*Config){func(c *Config) { c.Slice.RPCCommand = []string{"redeem\nevil"} }, func(c *Config) {
		c.Slice.TransportOptions = make([]string, 65)
		for i := range c.Slice.TransportOptions {
			c.Slice.TransportOptions[i] = "-v"
		}
	}, func(c *Config) { c.Slice.RequestTimeout = 0 }, func(c *Config) { c.Slice.RetryInitialBackoff = 3 * time.Second; c.Slice.RetryMaxBackoff = time.Second }, func(c *Config) { c.Slice.GraphicalContextKeys = []string{"NIRI_SOCKET", "SSH_AUTH_SOCK"} }, func(c *Config) { c.Slice.Clipboard.Enabled = true }, func(c *Config) { c.Slice.ExpectedNiriVersion = "latest" }}
	for _, mutate := range cases {
		cfg := Defaults()
		mutate(&cfg)
		if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "slice") {
			t.Fatalf("expected slice validation error for %#v, got %v", cfg.Slice, err)
		}
	}
}
