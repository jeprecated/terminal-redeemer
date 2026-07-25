package sliceenv

import (
	"context"
	"reflect"
	"testing"
)

func TestResolverUsesOnlyAllowlistedEnvironment(t *testing.T) {
	r := Resolver{Keys: []string{"NIRI_SOCKET", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR"}, Environment: []string{"NIRI_SOCKET=/run/user/1000/niri.sock", "WAYLAND_DISPLAY=wayland-1", "XDG_RUNTIME_DIR=/run/user/1000", "AWS_SECRET_ACCESS_KEY=secret", "BAD=line\nvalue"}}
	got, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got["NIRI_SOCKET"] != "/run/user/1000/niri.sock" {
		t.Fatalf("unexpected private context: %#v", got)
	}
	if _, ok := got["AWS_SECRET_ACCESS_KEY"]; ok {
		t.Fatal("credential escaped allowlist")
	}
}
func TestResolverQueriesOnlyMissingAllowlistedKeys(t *testing.T) {
	var gotArgs []string
	r := Resolver{Keys: []string{"XDG_RUNTIME_DIR", "NIRI_SOCKET", "WAYLAND_DISPLAY"}, Environment: []string{"WAYLAND_DISPLAY=wayland-1", "XDG_RUNTIME_DIR=/run/user/1000"}, Systemctl: "/store/systemctl", RunSystemctl: func(_ context.Context, command string, args, env []string) ([]byte, error) {
		if command != "/store/systemctl" {
			t.Fatal(command)
		}
		gotArgs = append([]string(nil), args...)
		return []byte("NIRI_SOCKET=/run/user/1000/niri.sock\nSSH_AUTH_SOCK=/secret\n"), nil
	}}
	got, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--user", "show-environment"}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("args=%q want=%q", gotArgs, want)
	}
	if len(got) != 3 {
		t.Fatalf("unexpected keys: %#v", got)
	}
}
func TestResolverFailsClosed(t *testing.T) {
	cases := []Resolver{
		{Keys: []string{"HOME"}, Environment: []string{"HOME=/secret"}},
		{Keys: []string{"NIRI_SOCKET"}, Environment: []string{"NIRI_SOCKET=/tmp/a"}},
		{Keys: []string{"NIRI_SOCKET", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR"}, Environment: []string{"NIRI_SOCKET=relative", "WAYLAND_DISPLAY=wayland-1", "XDG_RUNTIME_DIR=/run/user/1000"}},
		{Keys: []string{"NIRI_SOCKET", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR"}, Environment: []string{"NIRI_SOCKET=/tmp/a\nleak", "WAYLAND_DISPLAY=wayland-1", "XDG_RUNTIME_DIR=/run/user/1000"}},
		{Keys: []string{"NIRI_SOCKET", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR", "HOME"}, Environment: []string{}},
	}
	for _, r := range cases {
		if _, err := r.Resolve(context.Background()); err == nil {
			t.Fatalf("expected failure for %#v", r)
		}
	}
}
