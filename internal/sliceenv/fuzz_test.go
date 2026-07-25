package sliceenv

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func FuzzProcessEnvironmentMetadata(f *testing.F) {
	for _, seed := range []string{"/run/user/1000/niri.sock", "relative", "/tmp/nul\x00sock", "/tmp/line\nsock", string([]byte{0xff})} {
		f.Add(seed, "wayland-1", "/run/user/1000", uint8(0))
	}
	f.Add("/run/user/1000/niri.sock", "wayland-1", "relative-runtime", uint8(0))
	f.Add("", "", "", uint8(1))
	f.Add("", "", "", uint8(2))
	f.Add("", "", "", uint8(3))
	f.Fuzz(func(t *testing.T, socket, display, runtime string, mode uint8) {
		if len(socket)+len(display)+len(runtime) > MaxContextBytes {
			return
		}
		switch mode % 4 {
		case 1:
			socket = "/" + strings.Repeat("s", 106)
			display, runtime = "wayland-1", "/run/user/1000"
		case 2:
			socket = "/" + strings.Repeat("s", 107)
			display, runtime = "wayland-1", "/run/user/1000"
		case 3:
			socket, display, runtime = "/run/niri.sock", "bad\x00display", "/run/user/1000"
		}
		environment := []string{"NIRI_SOCKET=" + socket, "WAYLAND_DISPLAY=" + display, "XDG_RUNTIME_DIR=" + runtime, "SSH_AUTH_SOCK=/secret", "HOME=/secret"}
		resolver := Resolver{Keys: []string{"NIRI_SOCKET", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR"}, Environment: environment}
		first, errA := resolver.Resolve(context.Background())
		second, errB := resolver.Resolve(context.Background())
		if (errA == nil) != (errB == nil) || !reflect.DeepEqual(first, second) {
			t.Fatal("environment resolution is not deterministic")
		}
		if (mode%4 == 2 || mode%4 == 3) && errA == nil {
			t.Fatal("accepted over-limit path or NUL environment")
		}
		if errA != nil {
			return
		}
		if len(first) != 3 || first["SSH_AUTH_SOCK"] != "" || first["HOME"] != "" {
			t.Fatal("environment resolver escaped allowlist")
		}
		if !filepath.IsAbs(first["NIRI_SOCKET"]) || len(first["NIRI_SOCKET"]) > 107 || !filepath.IsAbs(first["XDG_RUNTIME_DIR"]) {
			t.Fatal("environment resolver accepted unsafe path metadata")
		}
		for _, value := range first {
			if !utf8.ValidString(value) || len(value) > MaxValueBytes || strings.ContainsAny(value, "\x00\r\n") {
				t.Fatal("accepted unsafe environment metadata")
			}
		}
	})
}
