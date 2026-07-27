#!/usr/bin/env bash
set -euo pipefail

iterations=2500
case "${1:-}" in
  "") ;;
  --iterations)
    [[ $# == 2 && "$2" =~ ^[0-9]+$ ]] || { echo "usage: $0 [--iterations 2000..50000]" >&2; exit 2; }
    iterations="$2"
    ;;
  *) echo "usage: $0 [--iterations 2000..50000]" >&2; exit 2 ;;
esac
(( iterations >= 2000 && iterations <= 50000 )) || { echo "iterations must be within [2000,50000]" >&2; exit 2; }

export GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off TMPDIR=/tmp
if [[ -d vendor && -z "${GOFLAGS:-}" ]]; then
  export GOFLAGS=-mod=vendor
fi
export TERMINAL_REDEEMER_SOAK_ITERATIONS="$iterations"
unset NIRI_SOCKET WAYLAND_DISPLAY WAYLAND_SOCKET SSH_AUTH_SOCK SSH_AGENT_PID
unset ZELLIJ ZELLIJ_SESSION_NAME ZELLIJ_SOCKET_DIR ZELLIJ_CACHE_DIR

go test -count=1 -run '^TestBoundedHostLeechSoak$' -timeout=30m ./internal/hostleechsoak
